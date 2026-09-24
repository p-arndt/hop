package tui

// The host list: reloading, filtering, and the row model the sidebar, the scrollbar and
// the mouse all measure in. Its rows are the hosts and, under an opened-out host, its tabs.

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
			m.cursor, m.cursorTab, m.recentAt = i, target{}, 0
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

// recentRows is how many recent places the content area offers with no host in front.
const recentRows = 5

// buildRows assumes m.filtered is already in section order; a section with no matches gets
// no heading, so a filter never draws an empty block. Under each open host go its tabs, in
// the order they were opened, then its tunnels. The recent places are not rows of the
// sidebar: with no host in front the content area offers them, and only while nothing
// narrows the list, since a filter is a search for a host.
func (m *model) buildRows() {
	m.rows = m.rows[:0]
	m.recent = nil
	if m.active == "" && !m.filtering && strings.TrimSpace(m.filter) == "" {
		m.recent = m.recentPlaces(recentRows)
	}
	m.recentAt = min(m.recentAt, len(m.recent))

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
			m.hostRows(i)
		}
	} else {
		if matched > 0 {
			m.rows = append(m.rows, listRow{heading: "PINNED", count: matched, total: pinned})
		}
		for i := 0; i < matched; i++ {
			m.hostRows(i)
		}
		if rest := len(m.filtered) - matched; rest > 0 {
			m.rows = append(m.rows, listRow{heading: "HOSTS", count: rest, total: len(m.hosts) - pinned})
			for i := matched; i < len(m.filtered); i++ {
				m.hostRows(i)
			}
		}
	}

	// The cursor rides its entry: a tab that closed, or a host folded up under it, puts it
	// back on the host.
	if m.cursorTab.alias != "" && !m.rowOf(m.cursor, m.cursorTab) {
		m.cursorTab = target{}
	}
}

// hostRows appends the host at filtered index fi and, while it is opened out, its tabs and
// its tunnels.
func (m *model) hostRows(fi int) {
	m.rows = append(m.rows, listRow{fi: fi})
	alias := m.hosts[m.filtered[fi]].Alias
	if !m.expanded(alias) {
		return
	}
	for _, t := range m.hostTabs(alias) {
		m.rows = append(m.rows, listRow{fi: fi, tab: t})
	}
	if s := m.sessions[alias]; !s.dead && len(s.tunnels) > 0 {
		m.rows = append(m.rows, listRow{fi: fi, tab: target{alias: alias, kind: targetTunnels}})
	}
}

// rowOf reports whether a row for the host at fi and tab t is drawn.
func (m *model) rowOf(fi int, t target) bool {
	for _, r := range m.rows {
		if r.heading == "" && r.fi == fi && r.tab == t {
			return true
		}
	}
	return false
}

// hasSections is true exactly when something is pinned.
func (m *model) hasSections() bool {
	for _, r := range m.rows {
		if r.heading != "" {
			return true
		}
	}
	return false
}

// ---- opening a host out ----

// expanded reports whether alias shows its tabs under it: the host in front does, the
// others do not, unless the user said otherwise since the host in front last changed. A
// host with neither tabs nor tunnels has nothing to show.
func (m *model) expanded(alias string) bool {
	s := m.sessions[alias]
	if len(m.hostTabs(alias)) == 0 && (s == nil || s.dead || len(s.tunnels) == 0) {
		return false
	}
	if open, ok := m.fold[alias]; ok {
		return open
	}
	return alias == m.active
}

// setExpanded opens alias out or folds it up, and rebuilds the rows to match.
func (m *model) setExpanded(alias string, open bool) {
	if m.fold == nil {
		m.fold = make(map[string]bool)
	}
	m.fold[alias] = open
	m.buildRows()
}

// collapseSelected is ← in the sidebar: from a tab the cursor goes up to its host; on an
// open host, the host folds up.
func (m *model) collapseSelected() {
	if m.cursorTab.alias != "" {
		m.cursorTab = target{}
		return
	}
	if h, ok := m.selectedHost(); ok && m.expanded(h.Alias) {
		m.setExpanded(h.Alias, false)
	}
}

// ---- the cursor ----

// cursorRow is the cursor's position in row space, headings included; 0 while it stands on
// a recent place in the content area.
func (m *model) cursorRow() int {
	if m.recentAt > 0 {
		return 0
	}
	for i, r := range m.rows {
		if r.heading == "" && r.fi == m.cursor && r.tab == m.cursorTab {
			return i
		}
	}
	return 0
}

// selectRow stands the cursor on row r: a host or one of its tabs.
func (m *model) selectRow(r listRow) {
	m.cursor, m.cursorTab, m.recentAt = r.fi, r.tab, 0
}

// selectPlace stands the cursor on t — the row of its tab when one is drawn, else its host.
func (m *model) selectPlace(t target) {
	for i, idx := range m.filtered {
		if m.hosts[idx].Alias != t.alias {
			continue
		}
		m.cursor, m.cursorTab, m.recentAt = i, target{}, 0
		if t.kind != targetHost && m.rowOf(i, t) {
			m.cursorTab = t
		}
		return
	}
}

// stepCursor moves the cursor delta rows over hosts and tabs alike. With no host in front
// the recent places sit above the first host, so going up from it lands on them.
func (m *model) stepCursor(delta int) {
	if m.recentAt > 0 {
		m.recentAt += delta
		switch {
		case m.recentAt < 1:
			m.recentAt = 1
		case m.recentAt > len(m.recent):
			m.recentAt = 0
			m.cursor, m.cursorTab = 0, target{}
			if r, ok := m.firstRow(); ok {
				m.selectRow(r)
			}
		}
		return
	}
	i := m.cursorRow()
	for n := 0; n < abs(delta); n++ {
		next, ok := m.nextRow(i, sign(delta))
		if !ok {
			if delta < 0 && len(m.recent) > 0 {
				m.recentAt = len(m.recent)
			}
			return
		}
		i = next
		m.selectRow(m.rows[i])
	}
}

// nextRow is the first row after i going dir that is not a heading.
func (m *model) nextRow(i, dir int) (int, bool) {
	for j := i + dir; j >= 0 && j < len(m.rows); j += dir {
		if m.rows[j].heading == "" {
			return j, true
		}
	}
	return i, false
}

// firstRow is the first row that is not a heading.
func (m *model) firstRow() (listRow, bool) {
	if i, ok := m.nextRow(-1, 1); ok {
		return m.rows[i], true
	}
	return listRow{}, false
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

func sign(n int) int {
	switch {
	case n < 0:
		return -1
	case n > 0:
		return 1
	}
	return 0
}

// selectedRecent is the recent place under the cursor, false while it is in the sidebar.
func (m *model) selectedRecent() (target, bool) {
	if m.recentAt < 1 || m.recentAt > len(m.recent) {
		return target{}, false
	}
	return m.recent[m.recentAt-1], true
}

// selectedPlace is where enter on the cursor goes: a recent place, a tab, or the host.
func (m *model) selectedPlace() (target, bool) {
	if t, ok := m.selectedRecent(); ok {
		return t, true
	}
	if m.cursorTab.alias != "" {
		return m.cursorTab, true
	}
	h, ok := m.selectedHost()
	return target{alias: h.Alias}, ok
}

// selectedHost is the host under the cursor — for a tab or a recent place, the host it is on.
func (m *model) selectedHost() (store.Host, bool) {
	if t, ok := m.selectedRecent(); ok {
		return m.hostByAlias(t.alias)
	}
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
	if c := clamp(m.cursor, 0, len(m.filtered)-1); c != m.cursor {
		m.cursor, m.cursorTab = c, target{}
	}
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
