package tui

// The terminal panel: a shell under the files, the way an IDE keeps one under its editor.
// It is its own shell rather than one of the host's tabs, so the shell view's shells keep
// their full size whatever the panel does.

import (
	"strings"

	tea "charm.land/bubbletea/v2"

	"hop/internal/keys"
	"hop/internal/sshx"
)

// drawerLandedMsg delivers the panel's shell, or why it could not start.
type drawerLandedMsg struct {
	alias string
	tab   *shellTab
	err   error
}

// startDrawerCmd is drawerCmd behind a variable so a test can see where the panel starts.
var startDrawerCmd = drawerCmd

func drawerCmd(alias, dir string, cli *sshx.Client, id, cols, rows int, notify chan struct{}) tea.Cmd {
	return func() tea.Msg {
		tab, err := newShell(cli, dir, id, cols, rows, notify)
		return drawerLandedMsg{alias: alias, tab: tab, err: err}
	}
}

// filesSession is the host in front, when it has files for a panel to sit under.
func (m *model) filesSession() *session {
	s := m.sessions[m.active]
	if s == nil || s.dead || s.client == nil {
		return nil
	}
	if s.browser == nil && len(s.editors) == 0 {
		m.setStatus(statusWarn, "the terminal panel sits under the files · ctrl+o f opens them")
		return nil
	}
	if !m.drawerFits() {
		m.setStatus(statusWarn, "too short for the terminal panel")
		return nil
	}
	return s
}

// toggleDrawer is one key for three steps: show the panel, move into it, put it away.
func (m *model) toggleDrawer() tea.Cmd {
	s := m.filesSession()
	if s == nil {
		return nil
	}
	switch {
	case m.mode == modeDrawer:
		s.drawerOpen = false
		m.focusFiles()
	case s.drawer != nil:
		s.drawerOpen = true
		m.focusDrawer()
	default:
		dir := ""
		if s.browser != nil {
			dir = s.browser.CursorDir()
		}
		return m.startDrawer(s, dir)
	}
	m.relayout()
	return nil
}

// drawerHere puts the panel in dir: a running one is told to cd there, so its history stays.
func (m *model) drawerHere(dir string) tea.Cmd {
	s := m.filesSession()
	if s == nil {
		return nil
	}
	if s.drawer == nil {
		return m.startDrawer(s, dir)
	}
	s.drawerOpen = true
	m.focusDrawer()
	m.relayout()
	if !s.drawer.pane.SendLine("cd -- " + shellQuote(dir)) {
		m.setStatus(statusWarn, "the terminal is running a full-screen program; not changing its directory")
	}
	return nil
}

func (m *model) startDrawer(s *session, dir string) tea.Cmd {
	m.nextShID++
	cols, rows := m.drawerSize(s)
	m.setStatus(statusInfo, "opening a terminal on %s…", m.active)
	return startDrawerCmd(m.active, dir, s.client, m.nextShID, cols, rows, m.notify)
}

func (m *model) drawerLanded(msg drawerLandedMsg) (tea.Model, tea.Cmd) {
	s := m.sessions[msg.alias]
	if msg.err != nil {
		m.setStatus(statusErr, "terminal on %s failed: %v", msg.alias, msg.err)
		return m, nil
	}
	if s == nil || s.dead || s.drawer != nil {
		// The session went, or a second key raced the first start.
		msg.tab.pane.Close()
		return m, nil
	}
	s.drawer, s.drawerOpen = msg.tab, true
	m.armClipboard(msg.tab.pane)
	if m.active == msg.alias && m.filesView() {
		m.focusDrawer()
	}
	m.clearStatus()
	m.relayout()
	return m, waitShellCmd(msg.alias, msg.tab.id, msg.tab.sess)
}

// dropDrawer answers the panel's shell exiting: the panel goes, the files stay.
func (m *model) dropDrawer(alias string) {
	s := m.sessions[alias]
	if s == nil {
		return
	}
	s.closeDrawer()
	if m.active == alias && m.mode == modeDrawer {
		m.focusFiles()
	}
	if s.empty() {
		s.close()
		delete(m.sessions, alias)
		if m.active == alias {
			m.leaveAll()
		}
		return
	}
	m.relayout()
}

func (m *model) focusDrawer() {
	m.mode = modeDrawer
	m.reader.Reset()
	m.clearSelection()
}

// focusFiles hands the keyboard back to what is above the panel.
func (m *model) focusFiles() {
	s := m.sessions[m.active]
	m.reader.Reset()
	switch {
	case s != nil && s.editor() != nil:
		m.mode = modeEditor
	case s != nil && s.browser != nil:
		m.mode = modeBrowser
	default:
		m.mode = modeList
	}
}

// handleDrawerKey forwards everything but the leader and the double esc, as a shell pane does.
func (m *model) handleDrawerKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	s := m.sessions[m.active]
	switch m.reader.Read(m.binds, keys.Pane, msg.String(), m.cfg.VimKeys).Action {
	case keys.LeaderKey:
		m.armLeader()
		return m, nil
	case keys.PaneLeave:
		m.focusFiles()
		return m, nil
	}
	if s != nil && s.drawer != nil {
		m.reportInput(s.drawer.pane.SendKey(msg))
	}
	return m, nil
}

// mouseDrawer is the pointer over the panel. Row 0 of the box is its header.
func (m *model) mouseDrawer(msg mouseEvt) (tea.Model, tea.Cmd) {
	s := m.sessions[m.active]
	if s == nil || s.drawer == nil || s.dead {
		return m, nil
	}
	r := m.frame.drawer
	if msg.Y == r.y && msg.Button == tea.MouseLeft && msg.action == actPress && !m.sel.dragging {
		m.resizingDrawer = true
		return m, nil
	}
	x, y, ok := r.inner(msg.X, msg.Y)
	y--
	if !ok || y < 0 {
		if !m.sel.dragging {
			return m, nil
		}
		x, y = r.clamp(msg.X, msg.Y)
		y = max(y-1, 0)
	}
	if m.mode != modeDrawer {
		if msg.Button == tea.MouseLeft && msg.action == actPress {
			m.focusDrawer()
		}
		return m, nil
	}
	p := s.drawer.pane
	if wheelDir(msg.Button) != 0 && !p.MouseEnabled() {
		// The panel keeps no history of its own; the shell view is where scrollback lives.
		return m, nil
	}
	if m.remoteOwnsPointer(p, msg) {
		p.SendMouse(msg.report(), x, y)
		return m, nil
	}
	return m.mouseSelect(msg, x, y, p.View(), r)
}

// stepDrawer moves the panel's edge one step, reporting whether there was a panel to move.
func (m *model) stepDrawer(taller bool) bool {
	if m.drawerOnScreen() == 0 {
		return false
	}
	step := max(m.bodyHeight()*drawerStepPct/100, 1)
	if !taller {
		step = -step
	}
	m.resizeDrawer(m.drawerRows() + step)
	return true
}

// sizeDrawerKey is the keyboard while sizing: the resize keys and the arrows step the edge.
func (m *model) sizeDrawerKey(key string) bool {
	switch key {
	case "+", "=", "up", "k":
		return m.stepDrawer(true)
	case "-", "down", "j":
		return m.stepDrawer(false)
	}
	return false
}

// dragDrawerEdge moves the panel's top edge with the pointer; a press on that edge starts it.
func (m *model) dragDrawerEdge(msg mouseEvt) (tea.Model, tea.Cmd) {
	switch msg.action {
	case actRelease:
		m.resizingDrawer = false
	case actPress, actMotion:
		// The body's last row is 1+bodyHeight-1; the edge at y leaves the panel the rows below it.
		m.resizeDrawer(1 + m.bodyHeight() - msg.Y)
	}
	return m, nil
}

// renderDrawer draws the panel: a header naming where the shell is, then the shell.
func (m *model) renderDrawer(s *session, r rect) string {
	p := s.drawer.pane
	head := accentText.Render("terminal")
	if cwd := p.Cwd(); cwd != "" {
		head += dimStyle.Render(" · " + stripControl(cwd))
	}
	view := p.View()
	if m.mode == modeDrawer {
		view = m.selectedView(view)
	}
	content := truncate(head, max(r.innerW(), 1)) + "\n" + view
	return m.contentBox(m.mode == modeDrawer, r.innerW(), max(r.innerH(), 1), strings.TrimRight(content, "\n"))
}
