package tui

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"

	"hop/internal/keys"

	"hop/internal/store"
)

// A dropped session is marked dead and left on screen, so the panes keep the last screen the
// host drew and 'r' can reconnect. Two detectors race into markDead, so it is idempotent.

// reconnectBrowserCmd is openBrowserCmd behind a variable so a test can assert on the size a
// reconnect hands a browser without dialing.
var reconnectBrowserCmd = openBrowserCmd

// reconnectPlan is what a session was holding when its connection dropped, captured when 'r'
// is pressed rather than when the link died.
type reconnectPlan struct {
	shells int

	browser    bool
	browserDir string

	// editors are deliberately not restored: reopening the file on a fresh pty would discard
	// the buffer while looking like it had not. The count is kept for the status line.
	editors int

	tunnels []int64

	// browsingFirst decides which half of the session is dialed first, which is the half
	// that ends up focused.
	browsingFirst bool
}

// markDead records that the connection under a session has gone; safe to call more than once.
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

// sessionLost names the connection, not just the host, because it also fires for every close
// hop makes itself.
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

// deadConnection is what a shell's or editor's exit is checked against, since every channel
// ends at once on a dropped link.
func (s *session) deadConnection() bool {
	return s.dead || (s.client != nil && s.client.IsLost())
}

func lostReason(s *session) string {
	if s.client == nil {
		return ""
	}
	if err := s.client.LostErr(); err != nil {
		return err.Error()
	}
	return ""
}

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

// reconnect dials a dead session's host again and puts back what was open on it. The plan is
// parked in m.pending rather than threaded through the dial, because the dial can detour
// through a host-key confirmation or a 2FA code.
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
		// The browser is built at the size of the tree column it will live in: browserLanded
		// relayouts a frame later, but the listing is laid out against this size first.
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

// applyPlan restores the rest of the plan once the new connection has landed.
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

// restored is the status parenthetical; have is how many shells the landing already brought.
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

func (m *model) restoreDir(alias string) string {
	return m.pending[alias].browserDir
}

// browserStartDir prefers a pending reconnect's directory over the host's default.
func (m *model) browserStartDir(h store.Host) string {
	if dir := m.restoreDir(h.Alias); dir != "" {
		return dir
	}
	return h.DefaultDir
}

// dropPlan forgets a pending reconnect, so the next connect restores nothing unasked.
func (m *model) dropPlan(alias string) {
	delete(m.pending, alias)
}

// reconnectSelected is 'r' in the host list.
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

func (m *model) activeDead() bool {
	s := m.sessions[m.active]
	return s != nil && s.dead
}

// handleDeadPaneKey is the whole keyboard of a dead pane; nothing is forwarded.
func (m *model) handleDeadPaneKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch m.binds.Action(keys.DeadPane, msg.String(), m.cfg.VimKeys) {
	case keys.DeadReconnect:
		h, ok := m.hostByAlias(m.active)
		if !ok {
			return m, nil
		}
		return m, m.reconnect(h)

	case keys.DeadLeave:
		// A single esc is enough: the double-tap exists to leave a remote program an esc of
		// its own, and there is no longer a program to leave one to.
		m.mode = modeList
		m.reader.Reset()
		m.clearStatus()

	case keys.DeadHelp:
		m.openHelp()

	case keys.DeadDrop:
		// 'x' is bound here too because "delete" is the reflex — it drops the session, never
		// the host.
		m.disconnect(m.active)
	}
	return m, nil
}
