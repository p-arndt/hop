package tui

import (
	"slices"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/lipgloss"
	"github.com/sahilm/fuzzy"
)

// switchItem is one row of the switcher: a target, a host, or — in the tree — a heading the
// cursor steps over.
type switchItem struct {
	t     target
	label string
	// heading is set on a line that is not a place.
	heading string
	// num is the tab's number among its host's tabs, 0 for anything that is not a tab.
	num int
	// tree marks a row drawn as part of the unfiltered tree, indented under its host.
	tree bool
}

// hostSwitchUI is the switcher's state; the matches are held, as the palette's are, since
// the cursor indexes them.
type hostSwitchUI struct {
	open bool
	picker
	items []switchItem
	// here is where the keyboard was when the card opened; hereOK is false from the list.
	here   target
	hereOK bool
}

// switchAliasMax caps the alias column, so one long alias cannot squeeze every path.
const switchAliasMax = 14

// switchTreeRows is how many rows of the tree the card shows at once; the tree is an
// overview, so it is taller than the palette.
const switchTreeRows = 16

// openHostSwitch raises the switcher on everything, unfiltered.
func (m *model) openHostSwitch() {
	here, ok := m.here()
	m.hostSwitch = hostSwitchUI{open: true, here: here, hereOK: ok}
	m.filterHostSwitch()
	m.clearStatus()
}

func (m *model) closeHostSwitch() { m.hostSwitch = hostSwitchUI{} }

// switchRows is every row the query narrows: what is open, most recently used first, then
// every host — connected ones first, the most recently used of those first, then the list's order.
func (m *model) switchRows() []switchItem {
	var ts []target
	for _, alias := range m.sessionAliases() {
		ts = append(ts, m.openTargets(alias)...)
	}
	slices.SortStableFunc(ts, func(a, b target) int { return m.used[b] - m.used[a] })

	rows := make([]switchItem, 0, len(ts)+len(m.hosts))
	for _, t := range append(ts, m.hostsByUse()...) {
		rows = append(rows, switchItem{t: t, label: m.targetLabel(t)})
	}
	return rows
}

// hostsByUse is every host: connected ones first, the most recently used of those first,
// then the list's order.
func (m *model) hostsByUse() []target {
	used := make(map[string]int, len(m.sessions))
	for alias := range m.sessions {
		used[alias] = m.hostUsed(alias)
	}
	hosts := make([]target, 0, len(m.hosts))
	for _, h := range m.hosts {
		hosts = append(hosts, target{alias: h.Alias})
	}
	slices.SortStableFunc(hosts, func(a, b target) int {
		if d := m.sessionRank(a.alias) - m.sessionRank(b.alias); d != 0 {
			return d
		}
		return used[b.alias] - used[a.alias]
	})
	return hosts
}

// switchTree is the switcher before a query: every host with a session and, under it, its
// tabs numbered as the leader's digits count them, the terminal panel and the tunnels; then the
// hosts without one, under a heading.
func (m *model) switchTree() []switchItem {
	var rows []switchItem
	for _, alias := range m.sessionAliases() {
		rows = append(rows, switchItem{t: target{alias: alias}, label: m.targetLabel(target{alias: alias}), tree: true})
		tabs := m.hostTabs(alias)
		for _, t := range m.openTargets(alias) {
			it := switchItem{t: t, label: m.targetLabel(t), tree: true}
			if i := slices.Index(tabs, t); i >= 0 {
				it.num = i + 1
			}
			rows = append(rows, it)
		}
	}
	var idle []switchItem
	for _, h := range m.hosts {
		if m.sessions[h.Alias] == nil {
			idle = append(idle, switchItem{t: target{alias: h.Alias}, label: m.targetLabel(target{alias: h.Alias})})
		}
	}
	if len(idle) > 0 {
		rows = append(rows, switchItem{heading: "not connected"})
		rows = append(rows, idle...)
	}
	return rows
}

// filterHostSwitch re-runs the query. With none the card is the tree, its cursor on the
// place used last that is not where the keyboard is, so opening it and pressing enter goes
// back. A query flattens it into the fuzzy list: over alias and label for what is open, and
// over alias, user and host name for a host, as the list's filter does.
func (m *model) filterHostSwitch() {
	q := m.hostSwitch.query
	if q == "" {
		rows := m.switchTree()
		m.hostSwitch.items = rows
		m.hostSwitch.cursor = m.backCursor(rows)
		return
	}

	rows := m.switchRows()
	hay := make([]string, len(rows))
	for i, r := range rows {
		hay[i] = r.t.alias + " " + r.label
		if r.t.kind == targetHost {
			h, _ := m.hostByAlias(r.t.alias)
			hay[i] = h.Alias + " " + h.User + " " + h.HostName
		}
	}
	matched := make([]switchItem, 0, len(rows))
	for _, mt := range fuzzy.Find(q, hay) {
		matched = append(matched, rows[mt.Index])
	}
	m.hostSwitch.items = matched
	m.hostSwitch.cursor = 0
}

// backCursor is the row of the place used last that is not here, else the first place.
func (m *model) backCursor(rows []switchItem) int {
	best, seen := -1, 0
	for i, r := range rows {
		if r.heading != "" || r.t.kind == targetHost || r.t.kind == targetTunnels {
			continue
		}
		if m.hostSwitch.hereOK && r.t == m.hostSwitch.here {
			continue
		}
		if n := m.used[r.t]; n > seen {
			best, seen = i, n
		}
	}
	if best >= 0 {
		return best
	}
	for i, r := range rows {
		if r.heading == "" && !(m.hostSwitch.hereOK && r.t == m.hostSwitch.here) {
			return i
		}
	}
	return 0
}

// sessionRank sorts a host with a session ahead of one without.
func (m *model) sessionRank(alias string) int {
	if m.sessions[alias] != nil {
		return 0
	}
	return 1
}

// handleHostSwitchKey routes a key while the switcher is up; like the palette it swallows
// everything, or a key would reach the pane underneath.
func (m *model) handleHostSwitchKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.closeHostSwitch()

	case "enter":
		if m.hostSwitch.cursor >= len(m.hostSwitch.items) {
			return m, nil
		}
		it := m.hostSwitch.items[m.hostSwitch.cursor]
		if it.heading != "" {
			return m, nil
		}
		m.closeHostSwitch()
		return m, m.jumpTo(it.t)

	default:
		from := m.hostSwitch.cursor
		if m.hostSwitch.key(msg, len(m.hostSwitch.items)) {
			m.filterHostSwitch()
			return m, nil
		}
		m.stepOverHeading(m.hostSwitch.cursor - from)
	}
	return m, nil
}

// stepOverHeading moves the cursor off a heading, on in the direction it was going, or back
// when the heading is the last row that way.
func (m *model) stepOverHeading(dir int) {
	items, c := m.hostSwitch.items, m.hostSwitch.cursor
	if c >= len(items) || items[c].heading == "" {
		return
	}
	if dir == 0 {
		dir = 1
	}
	for _, d := range []int{dir, -dir} {
		for i := c + d; i >= 0 && i < len(items); i += d {
			if items[i].heading == "" {
				m.hostSwitch.cursor = i
				return
			}
		}
	}
}

func (m *model) renderHostSwitch() string {
	if m.hostSwitch.query == "" {
		return m.renderPicker("GO TO", m.hostSwitch.picker, len(m.hostSwitch.items), "go", switchTreeRows,
			m.switchTreeRow)
	}
	aw := 0
	for _, it := range m.hostSwitch.items {
		aw = max(aw, lipgloss.Width(stripControl(it.t.alias)))
	}
	aw = min(aw, switchAliasMax)

	return m.renderPicker("GO TO", m.hostSwitch.picker, len(m.hostSwitch.items), "go", paletteRows,
		func(i int, selected bool, w int) string {
			it := m.hostSwitch.items[i]
			right := ""
			switch {
			case m.hostSwitch.hereOK && it.t == m.hostSwitch.here:
				right = faint.Render("here")
			case it.t.kind == targetHost:
				right = m.dotFor(it.t.alias)
			}
			head := padTo(truncate(stripControl(it.t.alias), aw), aw) + "  "
			room := w - 2 - lipgloss.Width(head) - lipgloss.Width(right) - 1
			return pickRow(head+elideLabel(it, room), right, selected, w)
		})
}

// switchTreeRow draws one row of the tree: a host flush left with its dot, what is open on
// it indented and numbered as the leader's digits count it, how long ago each was used on the
// right; then the hosts with no session, under their heading.
func (m *model) switchTreeRow(i int, selected bool, w int) string {
	it := m.hostSwitch.items[i]
	if it.heading != "" {
		head := faint.Render("── " + it.heading + " ")
		return head + faint.Render(strings.Repeat("─", max(w-lipgloss.Width(head), 0)))
	}
	here := m.hostSwitch.hereOK && it.t == m.hostSwitch.here
	right := ""
	switch {
	case here:
		right = faint.Render("here")
	case it.t.kind != targetHost:
		right = faint.Render(m.usedAgo(it.t))
	}

	var left string
	switch {
	case it.t.kind == targetHost && it.tree:
		left = m.dotFor(it.t.alias) + " " + aliasStyle.Render(stripControl(it.t.alias))
		if s := m.sessions[it.t.alias]; s != nil && s.dead {
			right = redText.Render("disconnected")
		}
	case it.t.kind == targetHost:
		h, _ := m.hostByAlias(it.t.alias)
		who := stripControl(h.HostName)
		if h.User != "" {
			who = stripControl(h.User) + "@" + who
		}
		left = m.dotFor(it.t.alias) + " " + padTo(stripControl(it.t.alias), switchAliasMax) + " " + dimStyle.Render(who)
	default:
		num := "  "
		if it.num > 0 {
			num = strconv.Itoa(it.num) + " "
		}
		room := w - 2 - 4 - lipgloss.Width(num) - lipgloss.Width(right) - 1
		left = "    " + faint.Render(num) + elideLabel(it, max(room, 4))
	}
	return pickRow(left, right, selected, w)
}

// elideLabel fits a row's label to w. A path says more by its end, so a target keeps its
// glyph and loses the middle; a host's summary is cut at the end like any other text.
func elideLabel(it switchItem, w int) string {
	r := []rune(it.label)
	if it.t.kind == targetHost || len(r) < 3 || lipgloss.Width(it.label) <= w {
		return it.label
	}
	return string(r[:2]) + elideLeft(string(r[2:]), w-2)
}

// ---- last host ----

// noteHost keeps the last host current: when the active host changes, the one being left
// becomes the last host. Run after every message, so no path that moves the keyboard can
// forget to.
func (m *model) noteHost() {
	if m.active == "" || m.active == m.shown {
		return
	}
	if m.shown != "" {
		m.last = m.shown
	}
	// The sidebar opens out the new host in front and folds up the old one, whatever the
	// user said about either while the other was in front.
	delete(m.fold, m.shown)
	delete(m.fold, m.active)
	m.shown = m.active
}

// backToLastHost lands on the last host's last place, reconnecting it if it went down.
func (m *model) backToLastHost() tea.Cmd {
	alias := m.last
	if alias == "" {
		m.setStatus(statusWarn, "no last host to go back to")
		return nil
	}
	s := m.sessions[alias]
	if s == nil {
		m.setStatus(statusWarn, "%s has no session any more", alias)
		return nil
	}
	if s.dead {
		if h, ok := m.hostByAlias(alias); ok {
			return m.enterHost(h)
		}
	}
	t, ok := m.lastPlace(alias)
	if !ok {
		m.setStatus(statusWarn, "nothing open on %s to go back to", alias)
		return nil
	}
	return m.jumpTo(t)
}
