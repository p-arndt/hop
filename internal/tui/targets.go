package tui

// Targets are the places on a connected host the keyboard can be put back into: a shell
// tab, the browser, an editor tab, the terminal panel. The switcher, the session bar and
// entering a host all land on one, so all three go back to the same place the same way.

import (
	"fmt"
	"slices"
	"strconv"
	"strings"

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
	m.useSeq++
	m.used[t] = m.useSeq
	m.current = t
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

// openTargets is what is open on alias, in the order the session bar draws it. A dead
// session has nothing to land on: reaching it goes through its host, which reconnects.
func (m *model) openTargets(alias string) []target {
	s := m.sessions[alias]
	if s == nil || s.dead {
		return nil
	}
	var ts []target
	for _, sh := range s.shells {
		ts = append(ts, target{alias: alias, kind: targetShell, id: sh.id})
	}
	if s.browser != nil {
		ts = append(ts, target{alias: alias, kind: targetBrowser})
	}
	for _, e := range s.editors {
		ts = append(ts, target{alias: alias, kind: targetEditor, id: e.id})
	}
	if s.drawer != nil {
		ts = append(ts, target{alias: alias, kind: targetDrawer})
	}
	if len(s.tunnels) > 0 {
		ts = append(ts, target{alias: alias, kind: targetTunnels})
	}
	return ts
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
