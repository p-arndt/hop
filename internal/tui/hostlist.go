package tui

// The host list: reloading, filtering, and the row model the sidebar, the scrollbar and
// the mouse all measure in.

import (
	"sort"
	"strings"

	"github.com/sahilm/fuzzy"

	"hop/internal/store"
)

// reloadHosts re-reads the host list; a read failure leaves the list hop already has.
func (m *model) reloadHosts() {
	// Hold the cursor on its host, even when the new order moved it.
	alias := ""
	if h, ok := m.selectedHost(); ok {
		alias = h.Alias
	}
	m.reloadHostsSelecting(alias)
}

// reloadHostsSelecting parks the cursor on alias, for a save landing on a host that was
// not selected a moment ago.
func (m *model) reloadHostsSelecting(alias string) {
	if m.st == nil {
		return
	}
	hosts, err := m.st.Hosts()
	if err != nil {
		return
	}
	m.hosts = hosts
	m.applyFilter()
	if alias == "" {
		return
	}
	for i, idx := range m.filtered {
		if m.hosts[idx].Alias == alias {
			m.cursor = i
			return
		}
	}
}

// applyFilter recomputes m.filtered, the per-alias match offsets, and clamps the cursor.
func (m *model) applyFilter() {
	if m.highlights == nil {
		m.highlights = make(map[int][]int)
	}
	clear(m.highlights)

	if strings.TrimSpace(m.filter) == "" {
		m.filtered = m.filtered[:0]
		for i := range m.hosts {
			m.filtered = append(m.filtered, i)
		}
		// The store hands hosts over pinned-first, so this is already in section order.
		m.buildRows()
		m.clampCursor()
		return
	}

	// Haystack is alias+user+hostname, so "root" matches on who you log in as.
	hay := make([]string, len(m.hosts))
	for i, h := range m.hosts {
		hay[i] = h.Alias + " " + h.User + " " + h.HostName
	}
	matches := fuzzy.Find(m.filter, hay)

	m.filtered = m.filtered[:0]
	for _, mt := range matches {
		m.filtered = append(m.filtered, mt.Index)
		// Only alias offsets are of use: it is the one part drawn character by character.
		alias := len(m.hosts[mt.Index].Alias)
		var in []int
		for _, at := range mt.MatchedIndexes {
			if at < alias {
				in = append(in, at)
			}
		}
		if len(in) > 0 {
			m.highlights[mt.Index] = in
		}
	}
	// A pin outranks match score; the partition is stable, so ranking survives inside it.
	m.pinnedFirst()
	m.buildRows()
	m.clampCursor()
}

// pinnedFirst orders pinned hosts by pin order, not match score: shift+j/k move within the
// order the user arranged by hand.
func (m *model) pinnedFirst() {
	sorted := make([]int, 0, len(m.filtered))
	for _, idx := range m.filtered {
		if m.hosts[idx].Pinned {
			sorted = append(sorted, idx)
		}
	}
	if len(sorted) == 0 {
		return
	}
	sort.SliceStable(sorted, func(a, b int) bool {
		return m.hosts[sorted[a]].PinOrder < m.hosts[sorted[b]].PinOrder
	})
	if len(sorted) == len(m.filtered) {
		m.filtered = append(m.filtered[:0], sorted...)
		return
	}
	for _, idx := range m.filtered {
		if !m.hosts[idx].Pinned {
			sorted = append(sorted, idx)
		}
	}
	m.filtered = append(m.filtered[:0], sorted...)
}

// buildRows assumes m.filtered is already in section order; a section with no matches gets
// no heading, so a filter never draws an empty block.
func (m *model) buildRows() {
	m.rows = m.rows[:0]

	pinned, matched := 0, 0
	for _, h := range m.hosts {
		if h.Pinned {
			pinned++
		}
	}
	for _, idx := range m.filtered {
		if m.hosts[idx].Pinned {
			matched++
		}
	}
	if pinned == 0 {
		for i := range m.filtered {
			m.rows = append(m.rows, listRow{fi: i})
		}
		return
	}

	if matched > 0 {
		m.rows = append(m.rows, listRow{heading: "PINNED", count: matched, total: pinned})
	}
	for i := 0; i < matched; i++ {
		m.rows = append(m.rows, listRow{fi: i})
	}
	if rest := len(m.filtered) - matched; rest > 0 {
		m.rows = append(m.rows, listRow{heading: "HOSTS", count: rest, total: len(m.hosts) - pinned})
		for i := matched; i < len(m.filtered); i++ {
			m.rows = append(m.rows, listRow{fi: i})
		}
	}
}

// hasSections is true exactly when something is pinned.
func (m *model) hasSections() bool {
	return len(m.rows) > len(m.filtered)
}

// cursorRow is the cursor's position in row space, headings included.
func (m *model) cursorRow() int {
	for i, r := range m.rows {
		if r.heading == "" && r.fi == m.cursor {
			return i
		}
	}
	return 0
}

func (m *model) selectedHost() (store.Host, bool) {
	if m.cursor < 0 || m.cursor >= len(m.filtered) {
		return store.Host{}, false
	}
	i := m.filtered[m.cursor]
	if i < 0 || i >= len(m.hosts) {
		return store.Host{}, false
	}
	return m.hosts[i], true
}

// hostByAlias searches every host, not the filtered ones: the caller's host may be hidden.
func (m *model) hostByAlias(alias string) (store.Host, bool) {
	for _, h := range m.hosts {
		if h.Alias == alias {
			return h, true
		}
	}
	return store.Host{}, false
}

func (m *model) clampCursor() {
	m.cursor = clamp(m.cursor, 0, len(m.filtered)-1)
}

// clamp holds v inside [lo, hi], returning lo for an empty range.
func clamp(v, lo, hi int) int {
	if hi < lo {
		return lo
	}
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
