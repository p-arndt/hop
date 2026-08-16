package store

// The host metadata that an OpenSSH config file has no business carrying.
//
// A host is split in two: everything OpenSSH itself understands — HostName, User, Port,
// IdentityFile, ProxyCommand, ProxyJump, the forwards — goes in the config file, so hop's
// hosts work with plain ssh too. Everything that is hop's own idea of a host — tags,
// group, pin state, how often you connect — goes here, keyed by alias.
//
// This half lives under the "hosts" key of hop's own config.json, beside the settings,
// because it is the same kind of thing: hop's preferences about your hosts. That makes
// the file shared, so both writers merge rather than replace (see save and
// config.Config.Save); neither may drop the other's keys.
//
// It is advisory: losing it costs the pin order and the frecency, never a host. A host
// present in the config file but absent here loads with zero values, which is exactly
// what a hand-written Host block should do.

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
)

// metaVersion is stamped into the file so a future format change can be recognised
// rather than misread.
const metaVersion = 1

// metaKey is the single key in config.json that all of this lives under.
const metaKey = "hosts"

// meta is the on-disk shape of that key's value.
type meta struct {
	Version int                  `json:"version"`
	NextID  int64                `json:"nextId"`
	Hosts   map[string]*hostMeta `json:"entries"`
}

// hostMeta is the hop-only half of one host. Fields are omitempty so a plain host with
// no tags and no pin writes a one-line entry rather than a wall of zeros.
type hostMeta struct {
	ID          int64    `json:"id"`
	Tags        []string `json:"tags,omitempty"`
	Group       string   `json:"group,omitempty"`
	Visits      int      `json:"visits,omitempty"`
	LastConnect int64    `json:"lastConnect,omitempty"`
	Pinned      bool     `json:"pinned,omitempty"`
	PinOrder    int      `json:"pinOrder,omitempty"`
	DefaultDir  string   `json:"defaultDir,omitempty"`
}

// loadMeta reads the metadata out of the config file. A missing file is the first-run
// case and a corrupt one is treated the same way: the hosts themselves live in the config
// file, so starting from empty metadata loses preferences, never connections. Returning
// an error instead would mean a stray keystroke in an editor locks you out of your own
// host list.
func loadMeta(path string) *meta {
	m := &meta{Version: metaVersion, Hosts: map[string]*hostMeta{}}
	data, err := os.ReadFile(path)
	if err != nil {
		return m
	}
	var doc map[string]json.RawMessage
	if err := json.Unmarshal(data, &doc); err != nil {
		return m
	}
	raw, ok := doc[metaKey]
	if !ok {
		return m
	}
	var parsed meta
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return m
	}
	if parsed.Hosts == nil {
		parsed.Hosts = map[string]*hostMeta{}
	}
	if parsed.Version == 0 {
		parsed.Version = metaVersion
	}
	return &parsed
}

// save writes the metadata back under its key, keeping every other key in the file: the
// settings live in the same object and are written by a different component. The write
// is atomic, so an interrupted save cannot leave a half-file that the next load would
// discard — losing both the settings and the pins.
func (m *meta) save(path string) error {
	m.Version = metaVersion

	doc := map[string]json.RawMessage{}
	if existing, err := os.ReadFile(path); err == nil {
		// A file that does not parse is replaced rather than propagated, matching how
		// both readers already treat it: as absent.
		_ = json.Unmarshal(existing, &doc)
	}
	encoded, err := json.Marshal(m)
	if err != nil {
		return fmt.Errorf("encode host metadata: %w", err)
	}
	doc[metaKey] = encoded

	out, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return fmt.Errorf("encode host metadata: %w", err)
	}
	return writeFileAtomic(path, append(out, '\n'), 0o600)
}

// get returns the entry for alias, creating it with a fresh id when absent. Ids are
// handed out here because this is the only file that outlives a process and can remember
// which numbers are taken.
func (m *meta) get(alias string) *hostMeta {
	if hm, ok := m.Hosts[alias]; ok && hm != nil {
		if hm.ID == 0 {
			hm.ID = m.claimID()
		}
		return hm
	}
	hm := &hostMeta{ID: m.claimID()}
	m.Hosts[alias] = hm
	return hm
}

// claimID returns an id no host currently holds. NextID normally does it in one step;
// the scan is the guard for a sidecar that was hand-edited to a lower counter.
func (m *meta) claimID() int64 {
	if m.NextID < 1 {
		m.NextID = 1
	}
	used := make(map[int64]bool, len(m.Hosts))
	for _, hm := range m.Hosts {
		if hm != nil {
			used[hm.ID] = true
		}
	}
	for used[m.NextID] {
		m.NextID++
	}
	id := m.NextID
	m.NextID++
	return id
}

// prune drops entries for aliases the config file no longer defines, so a sidecar cannot
// accumulate metadata for hosts deleted years ago.
func (m *meta) prune(live map[string]bool) {
	for alias := range m.Hosts {
		if !live[alias] {
			delete(m.Hosts, alias)
		}
	}
}

// aliases returns the known aliases in a stable order, for tests and for the pin
// renumbering that must not depend on Go's map iteration.
func (m *meta) aliases() []string {
	out := make([]string, 0, len(m.Hosts))
	for alias := range m.Hosts {
		out = append(out, alias)
	}
	sort.Strings(out)
	return out
}
