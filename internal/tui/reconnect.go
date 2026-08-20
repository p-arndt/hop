package tui

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"hop/internal/keys"

	"hop/internal/store"
)

// A dropped session is marked dead and left on screen rather than closed: the panes keep
// the last screen the host drew, and 'r' reconnects it.
//
// Two things notice the loss, both funnelling into markDead:
//
//   - sessionLostMsg, from the watcher on the connection's Lost channel. sshx's
//     keepalives are what make it fire on a blackholed link and not just a clean reset.
//   - a shell or editor exiting while its connection is already gone. Those exits must
//     not be read as somebody typing "exit", which would take the reconnect offer down.
//
// Whichever arrives first wins; markDead is idempotent.

// reconnectBrowserCmd is openBrowserCmd, behind a variable so a test can watch the size a
// reconnect hands a browser without dialing anything: the command itself only opens SFTP
// on a live connection, so the arguments are the only thing there is to assert on. It is
// never reassigned outside tests.
var reconnectBrowserCmd = openBrowserCmd

// reconnectPlan is what a session was holding when its connection dropped, kept so the
// reconnect can put it back. Captured when 'r' is pressed rather than when the link
// died: nothing has changed since, and it needs no bookkeeping while the pane sits dead.
type reconnectPlan struct {
	// shells is how many shell tabs to reopen — the same number, not the same shells: a
	// shell is a process on the far end and died with the connection.
	shells int

	// browser says the host had an SFTP browser open, and browserDir where it was
	// standing — the one piece of the browser's state worth keeping.
	browser    bool
	browserDir string

	// editors is how many editor tabs were open. They are deliberately not restored:
	// reopening the file on a fresh pty would discard the buffer while looking like it
	// had not. The count is kept so the reconnect can say what it left behind.
	editors int

	// tunnels are the persistent ids of the forwarding definitions that were
	// running. Unlike shell processes, they can be restored exactly.
	tunnels []int64

	// browsingFirst is true when the keyboard was in the browser at the drop and there is
	// a browser to come back to. It decides which half of the session is dialed first,
	// which is the half that ends up focused.
	browsingFirst bool
}

// markDead records that the connection under a session has gone. Nothing is torn down —
// that is the point — and it is safe to call more than once, since the two detectors race.
func (m *model) markDead(alias, why string) {
	s := m.sessions[alias]
	if s == nil || s.dead {
		return
	}
	s.dead = true
	s.lostWhy = why

	// Scrollback is a live pane's mode; the dead pane shows its last screen instead.
	if m.active == alias && m.mode == modeScrollback {
		m.mode = modeShell
	}
	m.setStatus(statusErr, "%s: connection lost — r to reconnect", alias)
}

// sessionLost handles the watcher's report that a connection has gone. The message names
// the connection, not just the host, because it also fires for every close hop makes
// itself — by then the session is gone or holding a different connection.
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

// deadConnection reports whether the session's connection has already gone — what a
// shell's or editor's exit is checked against, since every channel ends at once on a
// dropped connection.
func (s *session) deadConnection() bool {
	return s.dead || (s.client != nil && s.client.IsLost())
}

// lostReason is what the connection said on its way out, for the banner, or "" when it
// said nothing.
func lostReason(s *session) string {
	if s.client == nil {
		return ""
	}
	if err := s.client.LostErr(); err != nil {
		return err.Error()
	}
	return ""
}

// plan captures what is open on the session so a reconnect can put it back.
// browsingFirst is the caller's answer to "was the keyboard in the browser?".
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
// nothing rather than from a second source of truth. Only the plan carries over.
//
// It is parked in m.pending rather than threaded through the dial, because the dial can
// detour through a host-key confirmation or a 2FA code; the waiting plan is picked up by
// whichever landing arrives.
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
		// Hand the keyboard back to the list for the length of the dial; the landing puts
		// it wherever the plan says.
		m.leaveAll()
		m.active = h.Alias
	}

	if m.pending == nil {
		m.pending = make(map[string]reconnectPlan)
	}
	m.pending[h.Alias] = plan
	m.connecting[h.Alias] = true
	m.setStatus(statusInfo, "reconnecting to %s…", h.Alias)

	// Which half is dialed first decides which ends up focused: the primary landing takes
	// the keyboard, everything restored after it lands quietly.
	if plan.browser && (plan.browsingFirst || plan.shells == 0) {
		// A tunnel-only session falls through to the tunnel primary below rather than
		// manufacturing a shell to carry a connection.
		//
		// The browser is built at the size of the tree column it will live in, not at the
		// content area's: browserLanded relayouts and would resize it a frame later, but
		// the size handed over here is the one the listing is laid out — and its columns
		// elided — against before that happens.
		bw, bh := m.browserSize()
		return m.withSpinner(reconnectBrowserCmd(h, nil, "", m.prompter(h.Alias), m.browserOptions(), plan.browserDir, bw, bh, false))
	}
	if plan.shells == 0 && len(plan.tunnels) > 0 {
		defs := forwardDefinitions(h, plan.tunnels)
		return m.withSpinner(startTunnelsCmd(h, nil, "", m.prompter(h.Alias), defs, true))
	}
	m.nextShID++
	cols, rows := m.shellSize(1)
	return m.withSpinner(connectCmd(h, "", m.prompter(h.Alias), false, m.nextShID, cols, rows, m.notify))
}

// applyPlan fills in the rest of what a reconnected session was holding once its new
// connection has landed. Editor tabs are not restored (see reconnectPlan); they are
// counted in the status instead, so what was dropped is said out loud.
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
		// As above: the tree column's interior, since that is where it is going.
		bw, bh := m.browserSize()
		cmds = append(cmds, reconnectBrowserCmd(h, s.client, "", nil, m.browserOptions(), plan.browserDir, bw, bh, true))
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

// restored is the parenthetical the reconnect status carries. have is how many shells
// the landing already brought, so a plain one-shell reconnect says nothing extra.
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

// restoreDir is the directory a pending reconnect wants its browser opened in, or "".
// It carries the browser's place across a dial that detoured through the host-key card.
func (m *model) restoreDir(alias string) string {
	return m.pending[alias].browserDir
}

// browserStartDir is where a browser about to open should land: the directory a pending
// reconnect was standing in, otherwise the host's default. Where the user was a moment
// ago is a stronger claim than a setting made once.
func (m *model) browserStartDir(h store.Host) string {
	if dir := m.restoreDir(h.Alias); dir != "" {
		return dir
	}
	return h.DefaultDir
}

// dropPlan forgets a pending reconnect. A dial that failed for good must not leave a
// plan under the alias, or the next ordinary connect would restore tabs nobody asked for.
func (m *model) dropPlan(alias string) {
	delete(m.pending, alias)
}

// reconnectSelected is 'r' in the host list: reconnect the host under the cursor when
// its session is dead, and say why not when it is not.
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
// connection — the guard the pane key handlers open with.
func (m *model) activeDead() bool {
	s := m.sessions[m.active]
	return s != nil && s.dead
}

// handleDeadPaneKey is the whole keyboard of a pane whose connection has gone. Nothing
// is forwarded; what is left is the three things you can do — get back on the host,
// leave the pane, or drop it.
func (m *model) handleDeadPaneKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch m.binds.Action(keys.DeadPane, msg.String(), m.cfg.VimKeys) {
	case keys.DeadReconnect:
		h, ok := m.hostByAlias(m.active)
		if !ok {
			return m, nil
		}
		return m, m.reconnect(h)

	case keys.DeadLeave:
		// Back to the list, as leavePane does; the pane stays on screen unfocused. A
		// single esc is enough: the double-tap exists to leave a remote program an esc of
		// its own, and there is no longer a program to leave one to.
		m.mode = modeList
		m.reader.Reset()
		m.clearStatus()

	case keys.DeadHelp:
		// hop owns this keyboard outright — a dead pane forwards nothing — so the card
		// is reachable by the key the footer names.
		m.openHelp()

	case keys.DeadDrop:
		// Give up on it: the pane goes, the host goes back to idle. 'x' is bound here too
		// because "delete" is the reflex — it drops the session, never the host.
		m.disconnect(m.active)
	}
	return m, nil
}
