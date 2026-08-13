package tui

// The host list itself: reloading it from the store, filtering it, and the row model
// the sidebar, the scrollbar and the mouse all measure in.

import (
	"sort"
	"strings"

	"github.com/sahilm/fuzzy"

	"hop/internal/store"
)

// reloadHosts re-reads the host list, so a connect's bump to visits and
// last-connect shows up in the list's frecency order and in the details card
// without a restart. A read failure leaves the list hop already has.
func (m *model) reloadHosts() {
	// Hold the cursor on the host it is on, even when the new order moved it.
	alias := ""
	if h, ok := m.selectedHost(); ok {
		alias = h.Alias
	}
	m.reloadHostsSelecting(alias)
}

// reloadHostsSelecting re-reads the list and then parks the cursor on the host with
// the given alias, or leaves it clamped in place when that alias is gone (a delete)
// or filtered out. It is the shared body of reloadHosts and the one thing a save
// needs on top of it: land the cursor on a host that was not selected a moment ago
// — a brand-new one, or one whose alias just changed.
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

// applyFilter recomputes m.filtered from the current filter text, records which
// characters of each alias matched (for highlighting), and clamps the cursor into
// range.
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
		// The store already hands hosts over pinned-first, so an unfiltered list is
		// in section order as it stands.
		m.buildRows()
		m.clampCursor()
		return
	}

	// The haystack is what a host *is* — its alias, its user and its hostname —
	// so "root" finds a host whose alias says nothing about who you log in as.
	hay := make([]string, len(m.hosts))
	for i, h := range m.hosts {
		hay[i] = h.Alias + " " + h.User + " " + h.HostName
	}
	matches := fuzzy.Find(m.filter, hay)

	m.filtered = m.filtered[:0]
	for _, mt := range matches {
		m.filtered = append(m.filtered, mt.Index)
		// Only the offsets that landed in the alias are of any use to the row
		// renderer: it is the one part of the haystack it draws character by
		// character.
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
	// A pin outranks a match score: a filter narrows the list, it does not dissolve
	// the sections. The partition is stable, so inside each section the hits stay in
	// the order the fuzzy matcher ranked them.
	m.pinnedFirst()
	m.buildRows()
	m.clampCursor()
}

// pinnedFirst moves the pinned hosts to the front of m.filtered, in their pin
// order — not in the order the fuzzy matcher ranked them. The section is drawn in
// the order the user arranged by hand, and shift+j/k move within that same order,
// so a filter that reshuffled the section would leave the reorder keys moving a
// host somewhere other than where it looks like it is going. The unpinned tail
// keeps the match ranking, which is the only place a score has anything to say.
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

// buildRows recomputes the drawn rows from m.filtered, which is already in section
// order. With nothing pinned there are no headings at all — the sidebar keeps the
// single HOSTS title it has always had (see renderList), and this is a row per
// host. A section with no matches left in it does not get a heading, so a filter
// never draws an empty block.
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

// hasSections reports whether the sidebar is drawing section headings — which it
// does exactly when something is pinned, and which is what costs the single HOSTS
// title at the top of the pane.
func (m *model) hasSections() bool {
	return len(m.rows) > len(m.filtered)
}

// cursorRow is where the cursor sits in row space (headings included), which is
// what the scroll window and the scrollbar are measured in.
func (m *model) cursorRow() int {
	for i, r := range m.rows {
		if r.heading == "" && r.fi == m.cursor {
			return i
		}
	}
	return 0
}

// selectedHost returns the host under the cursor, or false if the list is empty.
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

// hostByAlias returns the host with this alias. It looks over every host rather
// than the filtered ones, because the modes that ask — a focused pane, a browser
// — are on a host the filter may well have hidden since.
func (m *model) hostByAlias(alias string) (store.Host, bool) {
	for _, h := range m.hosts {
		if h.Alias == alias {
			return h, true
		}
	}
	return store.Host{}, false
}

// clampCursor holds the list cursor inside the filtered host list.
func (m *model) clampCursor() {
	m.cursor = clamp(m.cursor, 0, len(m.filtered)-1)
}

// clamp holds v inside [lo, hi], and returns lo for an empty range.
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
