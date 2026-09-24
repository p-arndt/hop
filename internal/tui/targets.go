package tui

// Targets are the places on a connected host the keyboard can be put back into: a shell
// tab, the browser, an editor tab, the terminal panel. Go to, the sidebar and entering a
// host all land on one, so all three go back to the same place the same way.

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"hop/internal/store"
)

type targetKind int

const (
	// targetHost is the host itself: landing there lands on its last place.
	targetHost targetKind = iota
	targetShell
	targetBrowser
	targetEditor
	targetDrawer
	// targetTunnels holds no keyboard; landing there opens the tunnel manager.
	targetTunnels
)

// target names one place. id is the tab's stable id, since an index shifts when a tab closes.
type target struct {
	alias string
	kind  targetKind
	id    int
}

// here is the target the keyboard is in; false in the host list or on a dead session.
func (m *model) here() (target, bool) {
	s := m.sessions[m.active]
	if s == nil || s.dead {
		return target{}, false
	}
	t := target{alias: m.active}
	switch m.mode {
	case modeShell, modeScrollback:
		if s.shell() == nil {
			return target{}, false
		}
		t.kind, t.id = targetShell, s.shell().id
	case modeBrowser:
		if s.browser == nil {
			return target{}, false
		}
		// The tree column is part of the editor tab it stands beside.
		if m.frontOf(s) == tabEditor {
			t.kind, t.id = targetEditor, s.editor().id
			break
		}
		t.kind = targetBrowser
	case modeEditor:
		if s.editor() == nil {
			return target{}, false
		}
		t.kind, t.id = targetEditor, s.editor().id
	case modeDrawer:
		if s.drawer == nil {
			return target{}, false
		}
		t.kind = targetDrawer
	default:
		return target{}, false
	}
	return t, true
}

// noteTarget stamps the target the keyboard is in each time it moves to another. Run after
// every message, beside noteHost, so no path that moves the keyboard can skip it.
func (m *model) noteTarget() {
	t, ok := m.here()
	if !ok || t == m.current {
		return
	}
	if m.used == nil {
		m.used = make(map[target]int)
	}
	if m.usedAt == nil {
		m.usedAt = make(map[target]time.Time)
	}
	m.useSeq++
	m.used[t] = m.useSeq
	m.usedAt[t] = m.now()
	m.current = t
	m.forgetClosed()
}

// forgetClosed drops the stamps and tab order of tabs that have closed. Ids are never
// reused, so a stale entry can do no harm, but a long-lived session opening many short
// shells would otherwise make every sidebar redraw slower.
func (m *model) forgetClosed() {
	open := make(map[target]bool)
	for alias, s := range m.sessions {
		ts := m.openTargets(alias)
		if s.dead {
			// Its tabs are still drawn, greyed, until it is reconnected or dropped.
			ts = m.hostTabs(alias)
		}
		for _, t := range ts {
			open[t] = true
		}
		s.order = slices.DeleteFunc(s.order, func(t target) bool { return !open[t] })
	}
	for t := range m.used {
		if t.kind != targetHost && !open[t] {
			delete(m.used, t)
			delete(m.usedAt, t)
		}
	}
}

// sessionAliases is every host with a session, in the host list's order; a session the list
// no longer names goes last, so nothing open is ever left out.
func (m *model) sessionAliases() []string {
	var out []string
	for _, h := range m.hosts {
		if m.sessions[h.Alias] != nil {
			out = append(out, h.Alias)
		}
	}
	var rest []string
	for alias := range m.sessions {
		if !slices.Contains(out, alias) {
			rest = append(rest, alias)
		}
	}
	slices.Sort(rest)
	return append(out, rest...)
}

// hostTabs is alias's tabs in the order they were opened, which is the order the sidebar
// lists them under their host and what the leader's digits count. Unlike openTargets it
// names a dropped host's tabs too, so the sidebar can show what went.
func (m *model) hostTabs(alias string) []target {
	s := m.sessions[alias]
	if s == nil {
		return nil
	}
	var open []target
	for _, sh := range s.shells {
		open = append(open, target{alias: alias, kind: targetShell, id: sh.id})
	}
	if s.browser != nil {
		open = append(open, target{alias: alias, kind: targetBrowser})
	}
	for _, e := range s.editors {
		open = append(open, target{alias: alias, kind: targetEditor, id: e.id})
	}
	tabs := make([]target, 0, len(open))
	for _, t := range s.order {
		if slices.Contains(open, t) && !slices.Contains(tabs, t) {
			tabs = append(tabs, t)
		}
	}
	// A tab opened by a path that did not note it still gets a place, at the end.
	for _, t := range open {
		if !slices.Contains(tabs, t) {
			tabs = append(tabs, t)
		}
	}
	return tabs
}

// openTargets is everything on alias the keyboard can be put back into: its tabs, then the
// terminal panel and the tunnels. A dead session has nothing to land on: reaching it goes
// through its host, which reconnects.
func (m *model) openTargets(alias string) []target {
	s := m.sessions[alias]
	if s == nil || s.dead {
		return nil
	}
	ts := m.hostTabs(alias)
	if s.drawer != nil {
		ts = append(ts, target{alias: alias, kind: targetDrawer})
	}
	if len(s.tunnels) > 0 {
		ts = append(ts, target{alias: alias, kind: targetTunnels})
	}
	return ts
}

// frontTarget is the tab the host in front shows, which the sidebar marks.
func (m *model) frontTarget() (target, bool) {
	s := m.sessions[m.active]
	t := target{alias: m.active}
	switch m.frontOf(s) {
	case tabShell:
		t.kind, t.id = targetShell, s.shell().id
	case tabFiles:
		t.kind = targetBrowser
	case tabEditor:
		t.kind, t.id = targetEditor, s.editor().id
	default:
		return target{}, false
	}
	return t, true
}

// lastPlace is where entering alias lands: the target last used there, or else its shell,
// browser or editor, in that order.
func (m *model) lastPlace(alias string) (target, bool) {
	best, seen := target{}, 0
	for _, t := range m.openTargets(alias) {
		if n := m.used[t]; t.kind != targetTunnels && n > seen {
			best, seen = t, n
		}
	}
	if seen > 0 {
		return best, true
	}
	s := m.sessions[alias]
	switch {
	case s == nil || s.dead:
		return target{}, false
	case s.shell() != nil:
		return target{alias: alias, kind: targetShell, id: s.shell().id}, true
	case s.browser != nil:
		return target{alias: alias, kind: targetBrowser}, true
	case s.editor() != nil:
		return target{alias: alias, kind: targetEditor, id: s.editor().id}, true
	}
	return target{}, false
}

// hostUsed is when anything on alias last had the keyboard, 0 for never.
func (m *model) hostUsed(alias string) int {
	n := 0
	for t, at := range m.used {
		if t.alias == alias {
			n = max(n, at)
		}
	}
	return n
}

// enterHost lands on h's last place, and only when there is none opens a shell — connecting
// first, or reconnecting a dead session.
func (m *model) enterHost(h store.Host) tea.Cmd {
	if s := m.sessions[h.Alias]; s != nil && !s.dead && !m.connecting[h.Alias] {
		if t, ok := m.lastPlace(h.Alias); ok {
			return m.jumpTo(t)
		}
	}
	// While m.active is still the host being left, so its pane snaps back to live.
	m.exitScrollback()
	m.reader.Reset()
	return m.openShell(h, false)
}

// jumpTo puts the keyboard exactly on t. A target that has gone since it was offered lands
// on its host instead, the way entering the host would.
func (m *model) jumpTo(t target) tea.Cmd {
	s := m.sessions[t.alias]
	if s != nil && !s.dead && t.kind != targetHost && !m.targetOpen(t) {
		if lp, ok := m.lastPlace(t.alias); ok {
			return m.jumpTo(lp)
		}
	}
	if t.kind == targetHost || s == nil || s.dead || !m.targetOpen(t) {
		h, ok := m.hostByAlias(t.alias)
		if !ok {
			m.setStatus(statusWarn, "%s is not in the host list", t.alias)
			return nil
		}
		return m.enterHost(h)
	}
	if t.kind == targetTunnels {
		if h, ok := m.hostByAlias(t.alias); ok {
			m.openTunnels(h)
		}
		return nil
	}

	m.exitScrollback()
	m.reader.Reset()
	m.clearSelection()
	m.active = t.alias
	switch t.kind {
	case targetShell:
		s.activeSh = slices.IndexFunc(s.shells, func(sh *shellTab) bool { return sh.id == t.id })
		m.mode = modeShell
	case targetBrowser:
		// Said outright: the browser's keys alone could also be the editor tab's tree column.
		s.front = tabFiles
		m.mode = modeBrowser
	case targetEditor:
		s.focusTab(s.findEditorID(t.id))
		m.mode = modeEditor
	case targetDrawer:
		s.drawerOpen = true
		m.mode = modeDrawer
	}
	m.clearStatus()
	m.relayout()
	return nil
}

// targetOpen reports whether t still names something on its session.
func (m *model) targetOpen(t target) bool {
	return t.kind == targetHost || slices.Contains(m.openTargets(t.alias), t)
}

// targetLabel is how the switcher names t: a glyph for its kind, then what makes it this one.
// Everything in it came from the remote, so it is stripped.
func (m *model) targetLabel(t target) string {
	s := m.sessions[t.alias]
	if s == nil {
		if t.kind == targetHost {
			return "connect"
		}
		return ""
	}
	var l string
	switch t.kind {
	case targetHost:
		switch parts := s.summary(); {
		case s.dead:
			l = "reconnect"
		case len(parts) == 0:
			l = "connected"
		default:
			l = strings.Join(parts, ", ")
		}
	case targetShell:
		i := slices.IndexFunc(s.shells, func(sh *shellTab) bool { return sh.id == t.id })
		if i < 0 {
			return ""
		}
		l = "$ shell " + strconv.Itoa(i+1)
		if cwd := s.shells[i].pane.Cwd(); cwd != "" {
			l += " · " + cwd
		}
	case targetBrowser:
		l = "▤ " + s.browser.Path()
	case targetEditor:
		if i := s.findEditorID(t.id); i >= 0 {
			l = "✎ " + s.editors[i].path
		}
	case targetDrawer:
		l = "▭ terminal"
		if cwd := s.drawer.pane.Cwd(); cwd != "" {
			l += " · " + cwd
		}
	case targetTunnels:
		n := len(s.tunnels)
		l = fmt.Sprintf("⇄ %d %s", n, plural(n, "tunnel", "tunnels"))
	}
	return stripControl(l)
}

// findEditorID returns the index of the tab with the given id, or -1.
func (s *session) findEditorID(id int) int {
	return slices.IndexFunc(s.editors, func(e *editorTab) bool { return e.id == id })
}

// ---- moving between tabs and hosts ----

// gotoTab lands on tab i of alias, counted from 0 in the order the sidebar lists them.
func (m *model) gotoTab(alias string, i int) tea.Cmd {
	s := m.sessions[alias]
	if s == nil || s.dead {
		return nil
	}
	tabs := m.hostTabs(alias)
	if i < 0 || i >= len(tabs) {
		return nil
	}
	return m.jumpTo(tabs[i])
}

// stepTab moves delta tabs along the host in front's tabs, wrapping at both ends. From the
// terminal panel the tab it sits under is the one stepped from.
func (m *model) stepTab(delta int) tea.Cmd {
	s := m.sessions[m.active]
	if s == nil || s.dead {
		return nil
	}
	tabs := m.hostTabs(m.active)
	if len(tabs) < 2 {
		return nil
	}
	i := 0
	if t, ok := m.frontTarget(); ok {
		i = max(slices.Index(tabs, t), 0)
	}
	return m.jumpTo(tabs[cycle(i, delta, len(tabs))])
}

// stepHost moves delta hosts along the open ones, in the sidebar's order, wrapping at both
// ends. With no host in front the first step lands on the first or last of them.
func (m *model) stepHost(delta int) tea.Cmd {
	aliases := m.sessionAliases()
	if len(aliases) == 0 {
		m.setStatus(statusWarn, "no host is connected")
		return nil
	}
	i := slices.Index(aliases, m.active)
	switch {
	case i < 0 && delta > 0:
		i, delta = 0, 0
	case i < 0:
		i, delta = len(aliases)-1, 0
	}
	return m.showHost(aliases[cycle(i, delta, len(aliases))])
}

// showHost brings alias to the front on its last place. A dropped host is shown as it is,
// with what reconnecting would open again: stepping onto a red host never dials by itself.
func (m *model) showHost(alias string) tea.Cmd {
	s := m.sessions[alias]
	if s == nil || !s.dead {
		return m.jumpTo(target{alias: alias})
	}
	m.exitScrollback()
	m.reader.Reset()
	m.clearSelection()
	m.active = alias
	m.mode = modeFor(m.frontOf(s))
	m.clearStatus()
	m.relayout()
	return nil
}

// modeFor is the mode whose keys drive tab kind f.
func modeFor(f tabKind) paneMode {
	switch f {
	case tabFiles:
		return modeBrowser
	case tabEditor:
		return modeEditor
	}
	return modeShell
}

// lastShell lands on the shell tab of alias used last, starting one when it has none.
func (m *model) lastShell(alias string) tea.Cmd {
	s := m.sessions[alias]
	h, ok := m.hostByAlias(alias)
	if !ok {
		return nil
	}
	if s == nil || s.dead || len(s.shells) == 0 {
		m.reader.Reset()
		return m.openShell(h, false)
	}
	best, seen := target{alias: alias, kind: targetShell, id: s.shells[0].id}, -1
	for _, sh := range s.shells {
		t := target{alias: alias, kind: targetShell, id: sh.id}
		if n := m.used[t]; n > seen {
			best, seen = t, n
		}
	}
	return m.jumpTo(best)
}

// usedAgo is how long ago the keyboard was last in t, "" for never.
func (m *model) usedAgo(t target) string {
	at, ok := m.usedAt[t]
	if !ok {
		return ""
	}
	switch d := m.now().Sub(at); {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd ago", int(d.Hours()/24))
	}
}

// recentPlaces is up to n places across every connected host, the one used last first —
// what the content area offers with no host in front.
func (m *model) recentPlaces(n int) []target {
	var ts []target
	for _, alias := range m.sessionAliases() {
		for _, t := range m.openTargets(alias) {
			if t.kind != targetTunnels && m.used[t] > 0 {
				ts = append(ts, t)
			}
		}
	}
	slices.SortStableFunc(ts, func(a, b target) int { return m.used[b] - m.used[a] })
	return ts[:min(len(ts), n)]
}
