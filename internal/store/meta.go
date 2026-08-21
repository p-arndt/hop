package store

// The hop-only half of a host (tags, group, pin state, frecency), keyed by alias under the
// "hosts" key of config.json. The file is shared with the settings, so every writer must
// merge rather than replace (see save and config.Config.Save). It is advisory: losing it
// costs pins and frecency, never a host.

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
)

// metaVersion is stamped on disk so a future format change is recognised, not misread.
const metaVersion = 1

const metaKey = "hosts"

type meta struct {
	Version int                  `json:"version"`
	NextID  int64                `json:"nextId"`
	Hosts   map[string]*hostMeta `json:"entries"`
}

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

// loadMeta reads the metadata; a missing or corrupt file yields empty metadata rather than
// an error, so a bad edit cannot lock the user out of their host list.
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

// save rewrites only metaKey, preserving the settings keys another component owns.
func (m *meta) save(path string) error {
	m.Version = metaVersion

	doc := map[string]json.RawMessage{}
	if existing, err := os.ReadFile(path); err == nil {
		// An unparseable file is replaced, matching how both readers treat it: as absent.
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

// get returns the entry for alias, creating it with a fresh id when absent.
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

// claimID returns an unused id; the scan guards a sidecar hand-edited to a lower NextID.
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

// prune drops entries for aliases the config file no longer defines.
func (m *meta) prune(live map[string]bool) {
	for alias := range m.Hosts {
		if !live[alias] {
			delete(m.Hosts, alias)
		}
	}
}

// aliases returns the known aliases in a stable order, independent of map iteration.
func (m *meta) aliases() []string {
	out := make([]string, 0, len(m.Hosts))
	for alias := range m.Hosts {
		out = append(out, alias)
	}
	sort.Strings(out)
	return out
}
