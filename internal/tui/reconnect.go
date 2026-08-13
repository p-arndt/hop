package tui

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"hop/internal/store"
)

// A dropped session is marked dead and left on screen rather than closed: the panes
// keep the last screen the host drew — the command that was running, the error on
// the way out — and 'r' reconnects it.
//
// Two things notice the loss, both funnelling into markDead:
//
//   - sessionLostMsg, from the watcher on the connection's Lost channel (see
//     watchClientCmd). sshx's keepalives are what make it fire on a blackholed link
//     and not just a clean reset.
//   - a shell or editor exiting while its connection is already gone. Every channel
//     ends at once on a dropped connection, and those exits must not be read as
//     somebody typing "exit" — that would take the reconnect offer down with it.
//
// Whichever arrives first wins; markDead is idempotent.

// reconnectPlan is what a session was holding when its connection dropped, kept so
// the reconnect can put it back. It is captured at the moment 'r' is pressed rather
// than when the link died: what is open has not changed since, and a plan built
// from the live session needs no bookkeeping while the pane sits there dead.
type reconnectPlan struct {
	// shells is how many shell tabs to reopen. They are the same *number* of shells,
	// not the same shells: a shell is a process on the far end, and it died with the
	// connection. What survives is the tab it lived in.
	shells int

	// browser says the host had an SFTP browser open, and browserDir where it was
	// standing — a reconnect that dropped you back at the remote home would lose the
	// one piece of the browser's state that is worth keeping.
	browser    bool
	browserDir string

	// editors is how many editor tabs were open. They are deliberately *not*
	// restored: an editor is a live process holding a buffer, and reopening the file
	// on a fresh pty would silently discard whatever was in it while looking like it
	// had not. The count is kept only so the reconnect can say what it left behind.
	editors int

	// tunnels are the persistent ids of the forwarding definitions that were
	// running. Unlike shell processes, they can be restored exactly.
	tunnels []int64

	// browsingFirst is true when the keyboard was in the browser (or in an editor
	// opened from it) at the drop, and there is a browser to come back to. It decides
	// which half of the session the reconnect dials *first*, which is the half that
	// ends up focused — so you come back where you were.
	browsingFirst bool
}

// markDead records that the connection under a session has gone. Nothing is torn
// down: that is the whole point (see the note above). It is safe to call more than
// once, since the two detectors race and both funnel through here.
func (m *model) markDead(alias, why string) {
	s := m.sessions[alias]
	if s == nil || s.dead {
		return
	}
	s.dead = true
	s.lostWhy = why

	// Scrollback is a live pane's mode, and the keys that drive it now have nothing
	// to scroll toward; the dead pane shows its last screen instead.
	if m.active == alias && m.mode == modeScrollback {
		m.mode = modeShell
	}
	m.setStatus(statusErr, "%s: connection lost — r to reconnect", alias)
}

// sessionLost handles the watcher's report that a connection has gone. The message
// names the connection, not just the host, because it also fires for every close
// hop makes itself — a 'd', a quit, the re-dial a reconnect does — and by then the
// session either no longer exists or is holding a different connection. Either way
// there is nothing to mark.
func (m *model) sessionLost(msg sessionLostMsg) (tea.Model, tea.Cmd) {
	s := m.sessions[msg.alias]
	if s == nil || s.client != msg.client {
		return m, nil
	}
	why := ""
	if msg.err != nil {
		why = msg.err.Error()
	}
	m.markDead(msg.alias, why)
	return m, nil
}

// deadConnection reports whether the session's connection has already gone. It is
// what a shell's or an editor's exit is checked against: on a dropped connection
// every channel ends at once, and those endings mean "the link died", not "the
// program quit".
func (s *session) deadConnection() bool {
	return s.dead || (s.client != nil && s.client.IsLost())
}

// lostReason is what the connection said on its way out, for the banner — "" when
// it said nothing, which is the usual case for a far end that closed cleanly.
func lostReason(s *session) string {
	if s.client == nil {
		return ""
	}
	if err := s.client.LostErr(); err != nil {
		return err.Error()
	}
	return ""
}

// plan captures what is open on the session, so a reconnect can put it back.
// browsingFirst is the caller's answer to "was the keyboard in the browser?", which
// only the model knows.
func (s *session) plan(browsingFirst bool) reconnectPlan {
	p := reconnectPlan{
		shells:  len(s.shells),
		browser: s.browser != nil,
		editors: len(s.editors),
	}
	for id := range s.tunnels {
		p.tunnels = append(p.tunnels, id)
	}
	sort.Slice(p.tunnels, func(i, j int) bool { return p.tunnels[i] < p.tunnels[j] })
	if s.browser != nil {
		p.browserDir = s.browser.Path()
		p.browsingFirst = browsingFirst
	}
	return p
}

// reconnect dials a dead session's host again and puts back what was open on it.
//
// The dead session is closed and dropped first, so the new connection starts from
// nothing — a half-torn-down old session would be a second source of truth about
// what is open. Only the plan carries over: how many shells, and the browser's
// directory.
//
// It is parked in m.pending rather than threaded through the dial, because the dial
// can detour through a host-key confirmation or a 2FA code. Those replay it through
// their own retry paths, and the waiting plan is picked up by whichever landing
// arrives.
func (m *model) reconnect(h store.Host) tea.Cmd {
	s := m.sessions[h.Alias]
	if s == nil || !s.dead {
		return nil
	}
	if m.connecting[h.Alias] {
		m.setStatus(statusInfo, "reconnecting to %s…", h.Alias)
		return nil
	}

	inBrowser := m.active == h.Alias && (m.browsing() || m.editing())
	plan := s.plan(inBrowser)

	s.close()
	delete(m.sessions, h.Alias)
	if m.active == h.Alias {
		// Hand the keyboard back to the list for the length of the dial; the landing
		// puts it wherever the plan says (see the primary choice below).
		m.leaveAll()
		m.active = h.Alias
	}

	if m.pending == nil {
		m.pending = make(map[string]reconnectPlan)
	}
	m.pending[h.Alias] = plan
	m.connecting[h.Alias] = true
	m.setStatus(statusInfo, "reconnecting to %s…", h.Alias)

	// Which half of the session is dialed first decides which one ends up focused:
	// the primary landing takes the keyboard, everything the plan restores after it
	// lands quietly. So the browser goes first for someone who was browsing, and a
	// shell goes first for everyone else — including a browser-only session, where
	// "everyone else" cannot happen.
	if plan.browser && (plan.browsingFirst || plan.shells == 0) {
		// A tunnel-only session has no browser, so it falls through to the tunnel
		// primary below rather than manufacturing a shell just to carry a connection.
		return m.withSpinner(openBrowserCmd(h, nil, "", m.prompter(h.Alias), m.browserOptions(), plan.browserDir, m.paneW, m.paneH, false))
	}
	if plan.shells == 0 && len(plan.tunnels) > 0 {
		defs := forwardDefinitions(h, plan.tunnels)
		return m.withSpinner(startTunnelsCmd(h, nil, "", m.prompter(h.Alias), defs, true))
	}
	m.nextShID++
	cols, rows := m.shellSize(1)
	return m.withSpinner(connectCmd(h, "", m.prompter(h.Alias), false, m.nextShID, cols, rows, m.notify))
}

// applyPlan fills in the rest of what a reconnected session was holding, once its
// new connection has landed: the remaining shell tabs, and the browser when a shell
// was dialed first.
//
// Editor tabs are not restored (see reconnectPlan); they are counted in the status
// instead, so what was dropped is said out loud rather than quietly missing.
func (m *model) applyPlan(alias string) tea.Cmd {
	plan, ok := m.pending[alias]
	if !ok {
		return nil
	}
	delete(m.pending, alias)

	s := m.sessions[alias]
	if s == nil || s.client == nil {
		return nil
	}
	h, ok := m.hostByAlias(alias)
	if !ok {
		return nil
	}

	var cmds []tea.Cmd
	for i := len(s.shells); i < plan.shells; i++ {
		m.nextShID++
		cols, rows := m.shellSize(i + 1)
		cmds = append(cmds, shellCmd(alias, h.DefaultDir, s.client, m.nextShID, cols, rows, m.notify, true))
	}
	if plan.browser && s.browser == nil {
		cmds = append(cmds, openBrowserCmd(h, s.client, "", nil, m.browserOptions(), plan.browserDir, m.paneW, m.paneH, true))
	}
	var missing []int64
	for _, id := range plan.tunnels {
		if s.tunnels[id] == nil {
			missing = append(missing, id)
		}
	}
	if len(missing) > 0 {
		defs := forwardDefinitions(h, missing)
		if len(defs) > 0 {
			cmds = append(cmds, startTunnelsCmd(h, s.client, "", nil, defs, true))
		}
	}

	m.setStatus(statusOK, "reconnected to %s%s", alias, plan.restored(len(s.shells)))
	return tea.Batch(cmds...)
}

// restored is the parenthetical the reconnect status carries: what came back, and
// what did not. have is how many shells the landing already brought, so a plain
// one-shell reconnect says nothing extra rather than announcing "1 shell".
func (p reconnectPlan) restored(have int) string {
	var parts []string
	if p.shells > 1 || p.shells > have {
		parts = append(parts, strconv.Itoa(p.shells)+" "+plural(p.shells, "shell", "shells"))
	}
	if p.browser {
		parts = append(parts, "sftp browser")
	}
	if p.editors > 0 {
		parts = append(parts, fmt.Sprintf("%d %s not reopened",
			p.editors, plural(p.editors, "editor", "editors")))
	}
	if n := len(p.tunnels); n > 0 {
		parts = append(parts, strconv.Itoa(n)+" "+plural(n, "tunnel", "tunnels"))
	}
	if len(parts) == 0 {
		return ""
	}
	return " (" + strings.Join(parts, ", ") + ")"
}

// restoreDir is the directory a pending reconnect wants its browser opened in, or
// "" when there is none. It is what carries the browser's place across a dial that
// took a detour through the host-key card.
func (m *model) restoreDir(alias string) string {
	return m.pending[alias].browserDir
}

// browserStartDir is where a browser hop is about to open should land: the
// directory a pending reconnect was standing in when the connection dropped, and
// otherwise the host's own default directory. The dropped session's directory wins
// — it is where the user was a moment ago, which is a stronger claim on "where I
// meant" than a setting made once.
func (m *model) browserStartDir(h store.Host) string {
	if dir := m.restoreDir(h.Alias); dir != "" {
		return dir
	}
	return h.DefaultDir
}

// dropPlan forgets a pending reconnect. A dial that failed for good — bad auth, a
// host that is still down — must not leave a plan lying under the alias, or the
// next ordinary connect to that host would quietly restore tabs nobody asked for.
func (m *model) dropPlan(alias string) {
	delete(m.pending, alias)
}

// reconnectSelected is 'r' in the host list: reconnect the host under the cursor
// when its session is dead, and say why not when it is not.
func (m *model) reconnectSelected() tea.Cmd {
	h, ok := m.selectedHost()
	if !ok {
		return nil
	}
	s := m.sessions[h.Alias]
	switch {
	case s == nil:
		m.setStatus(statusWarn, "no dropped session for %s", h.Alias)
	case !s.dead:
		m.setStatus(statusWarn, "%s is still connected", h.Alias)
	default:
		return m.reconnect(h)
	}
	return nil
}

// activeDead reports whether the session the right pane is showing has lost its
// connection. It is the guard the pane key handlers open with: a dead pane forwards
// nothing to the far end, and answers 'r' instead.
func (m *model) activeDead() bool {
	s := m.sessions[m.active]
	return s != nil && s.dead
}

// handleDeadPaneKey is the whole keyboard of a pane whose connection has gone.
// Nothing is forwarded — there is nothing on the other end to forward to, and a
// pane that swallowed keys in silence would look like a hung terminal rather than a
// dropped one. What is left is the three things you can actually do: get back on the
// host, leave the pane, or drop it.
func (m *model) handleDeadPaneKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "r", "enter":
		h, ok := m.hostByAlias(m.active)
		if !ok {
			return m, nil
		}
		return m, m.reconnect(h)

	case "ctrl+o", "esc", "q":
		// Back to the list, the way leavePane does it — the pane stays on screen as
		// the host's last known state, unfocused. A single esc is enough here: the
		// double-tap exists to leave a *remote program* an esc of its own, and there
		// is no longer a program to leave one to.
		m.mode = modeList
		m.chords.esc = time.Time{}
		m.clearStatus()

	case "d", "x":
		// Give up on it: the pane goes, the host goes back to idle in the list. 'x' is
		// spelled here too because the pane is showing a dead thing and "delete" is
		// the reflex — it deletes the session, never the host, which is what 'x' means
		// in the list.
		m.disconnect(m.active)
	}
	return m, nil
}
