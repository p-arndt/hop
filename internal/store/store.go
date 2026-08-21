// Package store holds hop's saved SSH connections, split across an OpenSSH config file
// (the directives ssh understands) and a JSON sidecar keyed by alias (hop's own metadata).
package store

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/kevinburke/ssh_config"

	"hop/internal/config"
)

type Host struct {
	ID           int64
	Alias        string
	HostName     string
	User         string
	Port         int
	IdentityFile string
	Tags         []string
	Group        string
	Visits       int
	LastConnect  int64
	// DefaultDir is the remote directory a session starts in; blank means the login default.
	DefaultDir string
	// ProxyCommand is OpenSSH's directive: a local program whose stdio carries the transport.
	ProxyCommand string
	// ProxyJump is a bastion alias or bare [user@]host[:port]; it wins over ProxyCommand.
	ProxyJump string
	// PinOrder is 1-based and dense over the pinned hosts (see renumberPins), 0 when unpinned.
	Pinned   bool
	PinOrder int

	Forwards []Forward
}

// ForwardKind is which side owns the listening socket.
type ForwardKind string

const (
	ForwardLocal  ForwardKind = "local"
	ForwardRemote ForwardKind = "remote"
)

type Forward struct {
	ID         int64
	HostID     int64
	Kind       ForwardKind
	BindHost   string
	BindPort   int
	TargetHost string
	TargetPort int
}

// Validate rejects definitions that cannot name TCP endpoints; a blank BindHost is allowed.
func (f Forward) Validate() error {
	if f.Kind != ForwardLocal && f.Kind != ForwardRemote {
		return fmt.Errorf("forward kind must be local or remote")
	}
	if f.BindPort < 1 || f.BindPort > 65535 {
		return fmt.Errorf("bind port must be between 1 and 65535")
	}
	if strings.TrimSpace(f.TargetHost) == "" {
		return fmt.Errorf("target host can't be empty")
	}
	if f.TargetPort < 1 || f.TargetPort > 65535 {
		return fmt.Errorf("target port must be between 1 and 65535")
	}
	return nil
}

// Store is the in-memory host list backing the two files; its methods are safe for
// concurrent use.
type Store struct {
	hostsPath string
	metaPath  string

	mu    sync.Mutex
	hosts []Host
	meta  *meta
	// nextForwardID is per-process: the config file identifies a forward by its listener,
	// so no forward id is ever persisted.
	nextForwardID int64

	// includeErr is written once before the store is handed out, read-only after.
	includeErr error
}

// Open opens the default store: ~/.ssh/hop.config, config.json metadata, and the Include.
func Open() (*Store, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	sshDir := filepath.Join(home, ".ssh")
	if err := os.MkdirAll(sshDir, 0o700); err != nil {
		return nil, err
	}
	hostsPath := filepath.Join(sshDir, "hop.config")

	metaPath, err := defaultMetaPath()
	if err != nil {
		return nil, err
	}
	if err := migrateLegacyDB(legacyDBPath(), hostsPath, metaPath); err != nil {
		return nil, err
	}
	s, err := OpenAt(hostsPath, metaPath)
	if err != nil {
		return nil, err
	}
	// A missing Include costs only the ssh/scp integration, so it must not fail Open.
	if err := ensureInclude(filepath.Join(sshDir, "config"), hostsPath); err != nil {
		s.includeErr = err
	}
	return s, nil
}

// legacyDBPath is where hop kept its database before the SQLite removal.
func legacyDBPath() string {
	cfgDir, err := os.UserConfigDir()
	if err != nil {
		return ""
	}
	return filepath.Join(cfgDir, "hop", "hop.db")
}

func defaultMetaPath() (string, error) { return config.Path() }

// OpenAt opens the store at hostsPath with metadata under the "hosts" key of metaPath;
// a blank metaPath puts it beside the hosts file, and a SQLite hostsPath is migrated first.
func OpenAt(hostsPath, metaPath string) (*Store, error) {
	if metaPath == "" {
		metaPath = hostsPath + ".json"
	}
	if isSQLiteFile(hostsPath) {
		if err := migrateLegacyDB(hostsPath, hostsPath, metaPath); err != nil {
			return nil, err
		}
	}
	for _, dir := range []string{filepath.Dir(hostsPath), filepath.Dir(metaPath)} {
		if dir == "" {
			continue
		}
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return nil, err
		}
	}
	s := &Store{hostsPath: hostsPath, metaPath: metaPath}
	if err := s.load(); err != nil {
		return nil, err
	}
	return s, nil
}

// load reads both files: ids and metadata from the sidecar, the rest from the config file.
func (s *Store) load() error {
	hosts, err := readHosts(s.hostsPath)
	if err != nil {
		return fmt.Errorf("read %s: %w", s.hostsPath, err)
	}
	s.meta = loadMeta(s.metaPath)

	live := make(map[string]bool, len(hosts))
	for i := range hosts {
		live[hosts[i].Alias] = true
		hm := s.meta.get(hosts[i].Alias)
		hosts[i].ID = hm.ID
		hosts[i].Tags = append([]string(nil), hm.Tags...)
		hosts[i].Group = hm.Group
		hosts[i].Visits = hm.Visits
		hosts[i].LastConnect = hm.LastConnect
		hosts[i].Pinned = hm.Pinned
		hosts[i].PinOrder = hm.PinOrder
		hosts[i].DefaultDir = hm.DefaultDir
		for j := range hosts[i].Forwards {
			s.nextForwardID++
			hosts[i].Forwards[j].ID = s.nextForwardID
			hosts[i].Forwards[j].HostID = hosts[i].ID
		}
	}
	s.meta.prune(live)

	// File order is id order, so a rewrite never reshuffles the file.
	sort.SliceStable(hosts, func(i, j int) bool { return hosts[i].ID < hosts[j].ID })
	s.hosts = hosts
	s.renumberPins()
	return nil
}

// persist writes the config file before the sidecar: a sidecar entry for a not-yet-written
// host is harmless, the reverse is not.
func (s *Store) persist() error {
	if err := writeHosts(s.hostsPath, s.hosts); err != nil {
		return fmt.Errorf("write %s: %w", s.hostsPath, err)
	}
	for i := range s.hosts {
		h := &s.hosts[i]
		hm := s.meta.get(h.Alias)
		hm.Tags = append([]string(nil), h.Tags...)
		hm.Group = h.Group
		hm.Visits = h.Visits
		hm.LastConnect = h.LastConnect
		hm.Pinned = h.Pinned
		hm.PinOrder = h.PinOrder
		hm.DefaultDir = h.DefaultDir
	}
	if err := s.meta.save(s.metaPath); err != nil {
		return err
	}
	return nil
}

func (s *Store) Close() error { return nil }

// IncludeWarning reports a failure to add the Include line to ~/.ssh/config, if any.
func (s *Store) IncludeWarning() error { return s.includeErr }

// find looks up a host by alias. Callers hold s.mu.
func (s *Store) find(alias string) *Host {
	for i := range s.hosts {
		if s.hosts[i].Alias == alias {
			return &s.hosts[i]
		}
	}
	return nil
}

// findID looks up a host by id. Callers hold s.mu.
func (s *Store) findID(id int64) *Host {
	for i := range s.hosts {
		if s.hosts[i].ID == id {
			return &s.hosts[i]
		}
	}
	return nil
}

// clone deep-copies a host so callers cannot mutate the store's slices.
func clone(h Host) Host {
	out := h
	out.Tags = append([]string(nil), h.Tags...)
	out.Forwards = append([]Forward(nil), h.Forwards...)
	return out
}

func (s *Store) Hosts() ([]Host, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	out := make([]Host, 0, len(s.hosts))
	for _, h := range s.hosts {
		out = append(out, clone(h))
	}
	sort.SliceStable(out, func(i, j int) bool { return lessHost(out[i], out[j]) })
	return out, nil
}

// lessHost is the list order: pinned first in pin order, then most-used, then most-recent.
func lessHost(a, b Host) bool {
	if a.Pinned != b.Pinned {
		return a.Pinned
	}
	if a.Pinned && a.PinOrder != b.PinOrder {
		return a.PinOrder < b.PinOrder
	}
	if a.Visits != b.Visits {
		return a.Visits > b.Visits
	}
	return a.LastConnect > b.LastConnect
}

func (s *Store) HostByAlias(alias string) (Host, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	h := s.find(alias)
	if h == nil {
		return Host{}, false, nil
	}
	return clone(*h), true, nil
}

// Upsert inserts or updates a host by Alias; an update leaves visits, pin state and
// forwards alone, since the edit form does not carry them.
func (s *Store) Upsert(h Host) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	h = normalizeHost(h)
	if existing := s.find(h.Alias); existing != nil {
		existing.HostName = h.HostName
		existing.User = h.User
		existing.Port = h.Port
		existing.IdentityFile = h.IdentityFile
		existing.Tags = append([]string(nil), h.Tags...)
		existing.Group = h.Group
		existing.DefaultDir = h.DefaultDir
		existing.ProxyCommand = h.ProxyCommand
		existing.ProxyJump = h.ProxyJump
		id := existing.ID
		return id, s.persist()
	}
	return s.insert(h)
}

// Add inserts a new host, failing when the alias is taken, so a stale list cannot clobber
// a host added since.
func (s *Store) Add(h Host) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.find(h.Alias) != nil {
		return 0, fmt.Errorf("host %q already exists", h.Alias)
	}
	return s.insert(normalizeHost(h))
}

// insert appends a host and persists. Callers hold s.mu.
func (s *Store) insert(h Host) (int64, error) {
	if strings.TrimSpace(h.Alias) == "" {
		return 0, fmt.Errorf("host alias can't be empty")
	}
	// Forwards only ever arrive through AddForward.
	h.Forwards = nil
	h.ID = s.meta.get(h.Alias).ID
	s.hosts = append(s.hosts, h)
	return h.ID, s.persist()
}

// Delete removes the host, closing the hole a pinned one leaves in the pin order.
func (s *Store) Delete(alias string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	for i := range s.hosts {
		if s.hosts[i].Alias == alias {
			s.hosts = append(s.hosts[:i], s.hosts[i+1:]...)
			delete(s.meta.Hosts, alias)
			s.renumberPins()
			return s.persist()
		}
	}
	return nil
}

// AddForward persists a new forward for hostID, rejecting a duplicate listener.
func (s *Store) AddForward(hostID int64, f Forward) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	f = normalizeForward(hostID, f)
	if err := f.Validate(); err != nil {
		return 0, err
	}
	h := s.findID(hostID)
	if h == nil {
		return 0, fmt.Errorf("forward: no such host")
	}
	for _, existing := range h.Forwards {
		if sameListener(existing, f) {
			return 0, fmt.Errorf("add forward: a %s forward already listens on %s", f.Kind, joinHostPort(f.BindHost, f.BindPort))
		}
	}
	s.nextForwardID++
	f.ID = s.nextForwardID
	h.Forwards = append(h.Forwards, f)
	return f.ID, s.persist()
}

func sameListener(a, b Forward) bool {
	return a.Kind == b.Kind && a.BindHost == b.BindHost && a.BindPort == b.BindPort
}

// UpdateForward replaces a definition, keeping its id so a running tunnel stays matchable.
func (s *Store) UpdateForward(f Forward) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	f = normalizeForward(f.HostID, f)
	if err := f.Validate(); err != nil {
		return err
	}
	h := s.findID(f.HostID)
	if h == nil {
		return fmt.Errorf("update forward: no such forward")
	}
	for i := range h.Forwards {
		if h.Forwards[i].ID != f.ID {
			continue
		}
		for j := range h.Forwards {
			if j != i && sameListener(h.Forwards[j], f) {
				return fmt.Errorf("update forward: a %s forward already listens on %s", f.Kind, joinHostPort(f.BindHost, f.BindPort))
			}
		}
		h.Forwards[i] = f
		return s.persist()
	}
	return fmt.Errorf("update forward: no such forward")
}

// upsertImportedForward syncs an imported forward by listener, updating the target behind
// an existing one rather than erroring as AddForward does. Callers hold s.mu.
func (s *Store) upsertImportedForward(hostID int64, f Forward) error {
	f = normalizeForward(hostID, f)
	if err := f.Validate(); err != nil {
		return err
	}
	h := s.findID(hostID)
	if h == nil {
		return fmt.Errorf("forward: no such host")
	}
	for i := range h.Forwards {
		if sameListener(h.Forwards[i], f) {
			h.Forwards[i].TargetHost = f.TargetHost
			h.Forwards[i].TargetPort = f.TargetPort
			return nil
		}
	}
	s.nextForwardID++
	f.ID = s.nextForwardID
	h.Forwards = append(h.Forwards, f)
	return nil
}

func (s *Store) DeleteForward(hostID, id int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	h := s.findID(hostID)
	if h == nil {
		return fmt.Errorf("delete forward: no such forward")
	}
	for i := range h.Forwards {
		if h.Forwards[i].ID == id {
			h.Forwards = append(h.Forwards[:i], h.Forwards[i+1:]...)
			return s.persist()
		}
	}
	return fmt.Errorf("delete forward: no such forward")
}

// Rename changes a host's alias, preserving its id, pin and frecency.
func (s *Store) Rename(oldAlias, newAlias string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if oldAlias == newAlias {
		return nil
	}
	if s.find(newAlias) != nil {
		return fmt.Errorf("rename: host %q already exists", newAlias)
	}
	h := s.find(oldAlias)
	if h == nil {
		return fmt.Errorf("rename: no such host %q", oldAlias)
	}
	h.Alias = newAlias
	if hm, ok := s.meta.Hosts[oldAlias]; ok {
		delete(s.meta.Hosts, oldAlias)
		s.meta.Hosts[newAlias] = hm
	}
	return s.persist()
}

// SetPinned pins or unpins a host, appending a newly pinned one so MovePin's order holds.
func (s *Store) SetPinned(alias string, pinned bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	h := s.find(alias)
	if h == nil {
		return fmt.Errorf("pin: no such host %q", alias)
	}
	if h.Pinned == pinned {
		return nil
	}
	if pinned {
		next := 0
		for _, other := range s.hosts {
			if other.Pinned && other.PinOrder > next {
				next = other.PinOrder
			}
		}
		h.Pinned, h.PinOrder = true, next+1
	} else {
		h.Pinned, h.PinOrder = false, 0
	}
	s.renumberPins()
	return s.persist()
}

// MovePin moves a pinned host delta places (-1 up, +1 down) and reports whether it moved;
// hitting the edge is a no-op, not an error, because it is a held-down key.
func (s *Store) MovePin(alias string, delta int) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if delta == 0 {
		return false, nil
	}
	order := s.pinnedOrder()
	at := -1
	for i, name := range order {
		if name == alias {
			at = i
			break
		}
	}
	if at < 0 {
		return false, nil
	}
	to := at + delta
	if to < 0 || to >= len(order) {
		return false, nil
	}

	moved := order[at]
	order = append(order[:at], order[at+1:]...)
	order = append(order[:to], append([]string{moved}, order[to:]...)...)
	s.writePinOrder(order)
	return true, s.persist()
}

// pinnedOrder lists the pinned aliases in draw order, so "up" is up on screen. Callers
// hold s.mu.
func (s *Store) pinnedOrder() []string {
	pinned := make([]Host, 0, len(s.hosts))
	for _, h := range s.hosts {
		if h.Pinned {
			pinned = append(pinned, h)
		}
	}
	sort.SliceStable(pinned, func(i, j int) bool {
		if pinned[i].PinOrder != pinned[j].PinOrder {
			return pinned[i].PinOrder < pinned[j].PinOrder
		}
		if pinned[i].Visits != pinned[j].Visits {
			return pinned[i].Visits > pinned[j].Visits
		}
		return pinned[i].LastConnect > pinned[j].LastConnect
	})
	out := make([]string, len(pinned))
	for i, h := range pinned {
		out[i] = h.Alias
	}
	return out
}

// renumberPins closes holes MovePin's arithmetic cannot tolerate. Callers hold s.mu.
func (s *Store) renumberPins() { s.writePinOrder(s.pinnedOrder()) }

// writePinOrder stamps PinOrder 1..n in the order given. Callers hold s.mu.
func (s *Store) writePinOrder(order []string) {
	for i, alias := range order {
		if h := s.find(alias); h != nil {
			h.PinOrder = i + 1
		}
	}
}

// Touch increments the visit count and records the current connect time.
func (s *Store) Touch(alias string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	h := s.find(alias)
	if h == nil {
		return nil
	}
	h.Visits++
	h.LastConnect = time.Now().Unix()
	return s.persist()
}

// ImportSSHConfig upserts each concrete Host alias from an OpenSSH config file.
func (s *Store) ImportSSHConfig(path string) (int, error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, err
	}
	defer f.Close()

	cfg, err := ssh_config.Decode(f)
	if err != nil {
		return 0, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	count := 0
	seen := map[string]bool{}
	for _, block := range cfg.Hosts {
		for _, pattern := range block.Patterns {
			alias := pattern.String()
			if alias == "" || strings.ContainsAny(alias, "*?") || seen[alias] {
				continue
			}
			seen[alias] = true

			imported := hostFromConfig(cfg, alias)
			hostID := int64(0)
			if existing := s.find(alias); existing != nil {
				existing.HostName = imported.HostName
				existing.User = imported.User
				existing.Port = imported.Port
				existing.IdentityFile = imported.IdentityFile
				existing.ProxyCommand = imported.ProxyCommand
				existing.ProxyJump = imported.ProxyJump
				hostID = existing.ID
			} else {
				forwards := imported.Forwards
				imported.Forwards = nil
				id, err := s.insertLocked(imported)
				if err != nil {
					return count, err
				}
				hostID, imported.Forwards = id, forwards
			}
			for _, forward := range imported.Forwards {
				if err := s.upsertImportedForward(hostID, forward); err != nil {
					return count, err
				}
			}
			count++
		}
	}
	if err := s.persist(); err != nil {
		return count, err
	}
	return count, nil
}

// insertLocked appends a host without persisting, for batch callers. Callers hold s.mu.
func (s *Store) insertLocked(h Host) (int64, error) {
	if strings.TrimSpace(h.Alias) == "" {
		return 0, fmt.Errorf("host alias can't be empty")
	}
	h.ID = s.meta.get(h.Alias).ID
	s.hosts = append(s.hosts, h)
	return h.ID, nil
}

// normalizeHost applies the defaults the SQLite schema used to apply on write.
func normalizeHost(h Host) Host {
	h.Alias = strings.TrimSpace(h.Alias)
	if h.Port == 0 {
		h.Port = 22
	}
	return h
}

func normalizeForward(hostID int64, f Forward) Forward {
	f.HostID = hostID
	f.BindHost = strings.TrimSpace(f.BindHost)
	if f.BindHost == "" {
		f.BindHost = "127.0.0.1"
	}
	if f.BindHost == "*" {
		f.BindHost = "0.0.0.0"
	}
	f.TargetHost = strings.TrimSpace(f.TargetHost)
	return f
}

// parseSSHForward accepts OpenSSH's TCP shape, "[bind_address:]port host:hostport"; the
// socket-path and dynamic forms are rejected rather than misrepresented as TCP.
func parseSSHForward(value string, kind ForwardKind) (Forward, bool) {
	fields := strings.Fields(value)
	if len(fields) != 2 {
		return Forward{}, false
	}
	bindHost, bindPort, ok := splitForwardEndpoint(fields[0], true)
	if !ok {
		return Forward{}, false
	}
	targetHost, targetPort, ok := splitForwardEndpoint(fields[1], false)
	if !ok {
		return Forward{}, false
	}
	if bindHost == "" {
		bindHost = "127.0.0.1"
	}
	return Forward{Kind: kind, BindHost: bindHost, BindPort: bindPort, TargetHost: targetHost, TargetPort: targetPort}, true
}

func splitForwardEndpoint(value string, portOnly bool) (string, int, bool) {
	if portOnly {
		if port, err := strconv.Atoi(value); err == nil && port >= 1 && port <= 65535 {
			return "", port, true
		}
	}
	host, portText, err := netSplitHostPortLoose(value)
	if err != nil {
		return "", 0, false
	}
	port, err := strconv.Atoi(portText)
	if err != nil || port < 1 || port > 65535 || (!portOnly && strings.TrimSpace(host) == "") {
		return "", 0, false
	}
	return host, port, true
}

// netSplitHostPortLoose is net.SplitHostPort plus OpenSSH's unbracketed hostname:port;
// IPv6 stays bracketed.
func netSplitHostPortLoose(value string) (string, string, error) {
	if strings.HasPrefix(value, "[") {
		end := strings.LastIndex(value, "]:")
		if end < 0 {
			return "", "", fmt.Errorf("missing port")
		}
		return value[1:end], value[end+2:], nil
	}
	i := strings.LastIndex(value, ":")
	if i < 0 {
		return "", "", fmt.Errorf("missing port")
	}
	return value[:i], value[i+1:], nil
}

// splitTags parses the comma-separated tag column of the legacy SQLite schema.
func splitTags(s string) []string {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := parts[:0]
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// normalizeProxyCommand maps ssh's "none" (the disabled spelling) to blank.
func normalizeProxyCommand(v string) string {
	v = strings.TrimSpace(v)
	if strings.EqualFold(v, "none") {
		return ""
	}
	return v
}

// writeFileAtomic renames a same-directory temp file into place, so a crash mid-write
// leaves the previous file rather than a truncated one.
func writeFileAtomic(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+".tmp*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer os.Remove(name)

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(name, perm); err != nil {
		return err
	}
	return os.Rename(name, path)
}
