package tui

import (
	"slices"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/lipgloss"
	"github.com/sahilm/fuzzy"
)

// switchItem is one row of the switcher: a target, or a host.
type switchItem struct {
	t     target
	label string
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

// openHostSwitch raises the switcher on everything, unfiltered.
func (m *model) openHostSwitch() {
	here, ok := m.here()
	m.hostSwitch = hostSwitchUI{open: true, here: here, hereOK: ok}
	m.filterHostSwitch()
	m.clearStatus()
}

func (m *model) closeHostSwitch() { m.hostSwitch = hostSwitchUI{} }

// switchRows is every row before the query: what is open, most recently used first, then
// every host — connected ones first, the most recently used of those first, then the list's order.
func (m *model) switchRows() []switchItem {
	var ts []target
	for _, alias := range m.sessionAliases() {
		ts = append(ts, m.openTargets(alias)...)
	}
	slices.SortStableFunc(ts, func(a, b target) int { return m.used[b] - m.used[a] })

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

	rows := make([]switchItem, 0, len(ts)+len(hosts))
	for _, t := range append(ts, hosts...) {
		rows = append(rows, switchItem{t: t, label: m.targetLabel(t)})
	}
	return rows
}

// filterHostSwitch re-runs the query: over alias and label for what is open, and over alias,
// user and host name for a host, as the list's filter does. The cursor goes to the best
// match — or, with no query, to the row after where the keyboard is, so enter goes back.
func (m *model) filterHostSwitch() {
	rows := m.switchRows()
	if q := m.hostSwitch.query; q != "" {
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
		rows = matched
	}

	m.hostSwitch.items = rows
	m.hostSwitch.cursor = 0
	if m.hostSwitch.query == "" && m.hostSwitch.hereOK && len(rows) > 1 && rows[0].t == m.hostSwitch.here {
		m.hostSwitch.cursor = 1
	}
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
		t := m.hostSwitch.items[m.hostSwitch.cursor].t
		m.closeHostSwitch()
		return m, m.jumpTo(t)

	default:
		if m.hostSwitch.key(msg, len(m.hostSwitch.items)) {
			m.filterHostSwitch()
		}
	}
	return m, nil
}

func (m *model) renderHostSwitch() string {
	aw := 0
	for _, it := range m.hostSwitch.items {
		aw = max(aw, lipgloss.Width(stripControl(it.t.alias)))
	}
	aw = min(aw, switchAliasMax)

	return m.renderPicker("GO TO", m.hostSwitch.picker, len(m.hostSwitch.items), "go",
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
