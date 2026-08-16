// Package store holds hop's saved SSH connections.
//
// A host is kept in two files. Everything OpenSSH understands — HostName, User, Port,
// IdentityFile, ProxyCommand, ProxyJump and the port forwards — is written as a real Host
// block in an OpenSSH config file that hop manages and ~/.ssh/config includes, so every
// host you save in hop is a host plain ssh, scp and rsync can reach too. Everything that
// is hop's own — tags, group, pin order, how often you connect — sits in a JSON sidecar
// keyed by alias, where it cannot confuse OpenSSH.
//
// The whole set is small enough to hold in memory: hop reads both files once at Open and
// rewrites them on each change. That costs a file rewrite per edit and buys the absence
// of a SQL engine, which is the shape this data always had.
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

// Host represents a saved SSH connection target.
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
	// DefaultDir is the remote directory a session starts in: shells cd there on connect
	// and the file browser opens there. Blank means wherever the login shell lands.
	DefaultDir string
	// ProxyCommand is a local program whose stdin/stdout carry the SSH transport, as
	// OpenSSH's directive: how a host behind a broker (AWS SSM, cloudflared) is dialled.
	ProxyCommand string
	// ProxyJump is a bastion to tunnel through: an alias in this store, or a bare
	// [user@]host[:port]. Set alongside ProxyCommand it wins, as in ssh.
	ProxyJump string
	// Pinned lifts a host out of the frecency order into the PINNED section; PinOrder is
	// its place inside it, 1-based and dense (see renumberPins), and zero when unpinned.
	Pinned   bool
	PinOrder int

	// Forwards are the TCP tunnels defined for this host, loaded with it so View never
	// queries. They are written as LocalForward and RemoteForward directives, which means
	// ssh -N runs the same tunnels hop does.
	Forwards []Forward
}

// ForwardKind is which side of the SSH connection owns the listening socket: a local
// forward listens on the machine running hop, a remote one on the server.
type ForwardKind string

const (
	ForwardLocal  ForwardKind = "local"
	ForwardRemote ForwardKind = "remote"
)

// Forward is one persisted TCP port-forwarding definition.
type Forward struct {
	ID         int64
	HostID     int64
	Kind       ForwardKind
	BindHost   string
	BindPort   int
	TargetHost string
	TargetPort int
}

// Validate rejects definitions that cannot name TCP endpoints. BindHost may be blank:
// the runtime applies the loopback default for the forward's side.
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

// Store is the saved host list, held in memory and backed by the two files described in
// the package comment. Its methods are safe for concurrent use: the TUI touches it from
// its update loop while a dial in flight calls HostByAlias to resolve a jump.
type Store struct {
	hostsPath string
	metaPath  string

	mu    sync.Mutex
	hosts []Host
	meta  *meta
	// nextForwardID hands out forward identities. They are per-process: nothing persists
	// a forward id, because the config file identifies a forward by its listening
	// endpoint, which is what makes it a forward in the first place.
	nextForwardID int64

	// includeErr records a failed ~/.ssh/config update. It is written once, before the
	// store is handed to a caller, and read-only after — losing the Include costs the
	// ssh/scp integration, not hop's own host list, so it does not fail Open.
	includeErr error
}

// Open opens the default store: hosts in ~/.ssh/hop.config, where OpenSSH can read them,
// their hop-only metadata under the "hosts" key of hop's own config.json, where OpenSSH
// will never trip over it, and an Include in ~/.ssh/config so the rest of the toolchain
// sees the hosts.
//
// A hop.db left by an older version is migrated on the way past, once.
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
	// A failure here costs the ssh/scp integration, not hop's own host list, so it is
	// reported through the store rather than refusing to start.
	if err := ensureInclude(filepath.Join(sshDir, "config"), hostsPath); err != nil {
		s.includeErr = err
	}
	return s, nil
}

// legacyDBPath is where versions of hop before the SQLite removal kept their database.
func legacyDBPath() string {
	cfgDir, err := os.UserConfigDir()
	if err != nil {
		return ""
	}
	return filepath.Join(cfgDir, "hop", "hop.db")
}

// defaultMetaPath is hop's config.json. Tags, pins and visit counts are hop's own
// preferences about your hosts, so they sit beside the rest of the settings rather than
// in ~/.ssh, which belongs to OpenSSH, or in a third file of their own.
func defaultMetaPath() (string, error) { return config.Path() }

// OpenAt opens the store whose hosts live at hostsPath and whose hop-only metadata lives
// under the "hosts" key of the JSON file at metaPath. The two are named separately
// because they belong in different places: the hosts where OpenSSH reads them, the
// metadata where it does not.
//
// A blank metaPath puts the metadata in a JSON file beside the hosts file, which is what
// a self-contained store in one directory — a test, the demo server — wants.
//
// When hostsPath is a SQLite database left by an older hop, it is migrated in place first.
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

// load reads both files into memory and reconciles them: ids and metadata come from the
// sidecar, everything else from the config file, and the sidecar is pruned of aliases the
// config no longer has.
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

// persist writes both files. The config file goes first: it holds the hosts, and a
// sidecar naming an alias that does not exist yet is harmless where the reverse is not.
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

// Close releases the store. There is no open handle to release — every change is already
// on disk — but callers close it, and keeping the method keeps them honest if that
// changes.
func (s *Store) Close() error { return nil }

// IncludeWarning reports a failure to add the Include line to ~/.ssh/config, if any. The
// host list works regardless; what is lost is ssh and scp seeing hop's hosts.
func (s *Store) IncludeWarning() error { return s.includeErr }

// find returns a pointer to the stored host with this alias. Callers hold s.mu.
func (s *Store) find(alias string) *Host {
	for i := range s.hosts {
		if s.hosts[i].Alias == alias {
			return &s.hosts[i]
		}
	}
	return nil
}

// findID returns a pointer to the stored host with this id. Callers hold s.mu.
func (s *Store) findID(id int64) *Host {
	for i := range s.hosts {
		if s.hosts[i].ID == id {
			return &s.hosts[i]
		}
	}
	return nil
}

// clone deep-copies a host, so a caller mutating the slices it gets back cannot reach
// into the store's own state.
func clone(h Host) Host {
	out := h
	out.Tags = append([]string(nil), h.Tags...)
	out.Forwards = append([]Forward(nil), h.Forwards...)
	return out
}

// Hosts returns all hosts: the pinned ones in the user's order, then the rest by Visits
// desc then LastConnect desc.
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

// HostByAlias returns the single host with this alias. Forwards come with it, which the
// SQLite version skipped as an optimisation that an in-memory list no longer needs.
func (s *Store) HostByAlias(alias string) (Host, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	h := s.find(alias)
	if h == nil {
		return Host{}, false, nil
	}
	return clone(*h), true, nil
}

// Upsert inserts or updates a host keyed by its Alias and returns its id. An update
// leaves visits, last connect, pin state and forwards alone: an edit must not reset the
// frecency Touch has been accumulating, nor drop tunnels the form does not carry.
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

// Add inserts a new host, failing when the alias is taken. Unlike Upsert it never
// overwrites, so a stale in-memory list cannot clobber a host added since — from the
// CLI, say. Returns the new id.
func (s *Store) Add(h Host) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.find(h.Alias) != nil {
		return 0, fmt.Errorf("host %q already exists", h.Alias)
	}
	return s.insert(normalizeHost(h))
}

// insert appends a new host and persists. Callers hold s.mu.
func (s *Store) insert(h Host) (int64, error) {
	if strings.TrimSpace(h.Alias) == "" {
		return 0, fmt.Errorf("host alias can't be empty")
	}
	// Forwards are added through AddForward, matching the old schema where they were a
	// table of their own and an insert never carried them.
	h.Forwards = nil
	h.ID = s.meta.get(h.Alias).ID
	s.hosts = append(s.hosts, h)
	return h.ID, s.persist()
}

// Delete removes the host with the given alias, closing the hole a pinned one leaves in
// the pin order.
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

// AddForward persists a new forwarding definition for hostID. A host cannot have two
// forwards competing for the same listener on the same side.
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

// sameListener reports whether two forwards claim the same socket on the same side,
// which is what the old schema's UNIQUE constraint enforced.
func sameListener(a, b Forward) bool {
	return a.Kind == b.Kind && a.BindHost == b.BindHost && a.BindPort == b.BindPort
}

// UpdateForward replaces an existing definition, preserving its identity so a running
// tunnel can be matched and stopped first.
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

// upsertImportedForward syncs one OpenSSH LocalForward/RemoteForward by its listening
// endpoint. User-created definitions go through AddForward and still get a duplicate
// error; re-importing is allowed to update the target behind an existing listener.
// Callers hold s.mu.
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

// DeleteForward removes one definition belonging to hostID.
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

// Rename changes a host's alias, preserving its visit count and connect history, which
// a plain Upsert of a new alias would zero. A no-op when the two are equal; it fails when
// newAlias is taken or oldAlias does not exist.
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
	// Carry the metadata across so the rename keeps the id, the pin and the frecency.
	if hm, ok := s.meta.Hosts[oldAlias]; ok {
		delete(s.meta.Hosts, oldAlias)
		s.meta.Hosts[newAlias] = hm
	}
	return s.persist()
}

// SetPinned pins or unpins a host. A newly pinned host goes to the end of the section:
// pinning is "keep this where I can find it", and reshuffling would fight the order set
// with MovePin. It fails when there is no such host.
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

// MovePin moves a pinned host delta places within the pinned section (-1 up, +1 down)
// and reports whether it moved. An unpinned host, or one already at the end, is a no-op
// rather than an error: it is a held-down key hitting the edge of the list.
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

// pinnedOrder lists the pinned aliases in draw order, so "up" here is up on screen.
// Callers hold s.mu.
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

// renumberPins rewrites PinOrder as 1..n over the pinned hosts in their current order, so
// a delete or unpin cannot leave a hole for MovePin's arithmetic. Callers hold s.mu.
func (s *Store) renumberPins() { s.writePinOrder(s.pinnedOrder()) }

// writePinOrder stamps aliases with PinOrder 1..n, in the order given. Callers hold s.mu.
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

// ImportSSHConfig parses an OpenSSH config file and upserts each concrete Host alias,
// skipping wildcard patterns, and returns how many were imported.
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

// insertLocked appends a host without persisting, for callers that write once at the end
// of a batch. Callers hold s.mu.
func (s *Store) insertLocked(h Host) (int64, error) {
	if strings.TrimSpace(h.Alias) == "" {
		return 0, fmt.Errorf("host alias can't be empty")
	}
	h.ID = s.meta.get(h.Alias).ID
	s.hosts = append(s.hosts, h)
	return h.ID, nil
}

// normalizeHost fills in the defaults the SQLite schema used to apply on write, so a
// caller that leaves Port at zero still gets a host that dials port 22.
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

// parseSSHForward accepts OpenSSH's TCP forwarding shape, [bind_address:]port
// host:hostport. Socket-path and dynamic forms are left to OpenSSH rather than
// misrepresented as TCP definitions here.
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

// netSplitHostPortLoose is net.SplitHostPort plus OpenSSH's unbracketed hostname:port
// spelling. IPv6 remains bracketed, as OpenSSH documents it.
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

// splitTags parses the comma-separated tag column the SQLite schema used, for the
// migration to read.
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

// normalizeProxyCommand maps ssh's "none" — how the directive is disabled — to blank, so
// hop does not try to run a program by that name.
func normalizeProxyCommand(v string) string {
	v = strings.TrimSpace(v)
	if strings.EqualFold(v, "none") {
		return ""
	}
	return v
}

// writeFileAtomic writes via a temporary file in the same directory and renames it into
// place, so a crash mid-write leaves the previous file rather than a truncated one.
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
