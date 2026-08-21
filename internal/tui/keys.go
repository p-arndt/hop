package tui

import (
	"math"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"hop/internal/keys"
)

// handleKey routes a key to whichever mode owns the keyboard, most modal first.
func (m *model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// On Windows a paste arrives as a burst of plain keystrokes; a key that cannot be part
	// of one ends the burst here, before delivery. See paste.go.
	if m.pasteCoalesce {
		if m.takeKey(msg) {
			return m, m.pasteFlushCmd()
		}
		m.flushPaste()
	}

	// The modes below read the key's name, and a paste's name is the whole clipboard.
	if msg.Paste {
		return m.handlePaste(msg)
	}

	switch {
	case m.auth.open:
		// Above the help card: a card opened mid-handshake would hide the next challenge.
		return m.handleAuthKey(msg)
	case m.guidance.open:
		// Asked first, and the card it hands over to must not open behind it.
		return m.handleGuidanceKey(msg)
	case m.help:
		return m.handleHelpKey(msg)
	case m.hostKey.open:
		return m.handleHostKeyKey(msg)
	case m.confirm.open:
		return m.handleConfirmKey(msg)
	case m.palette.open:
		return m.handlePaletteKey(msg)
	case m.menu.open:
		return m.handleMenuKey(msg)
	case m.hostForm.open:
		return m.handleHostFormKey(msg)
	case m.importer.open:
		return m.handleImportKey(msg)
	case m.tunnels.open:
		return m.handleTunnelsKey(msg)
	case m.settings.open:
		return m.handleSettingsKey(msg)
	}

	// An open leader owns the keyboard outright, above even the global binds below.
	if m.leaderArmed() {
		return m.handleLeader(msg)
	}

	m.clearSelection()

	if a := m.binds.Action(keys.Global, msg.String(), m.cfg.VimKeys); a != keys.None {
		m.reader.Reset() // a key that is not an esc breaks a half-typed double-esc
		return m.doGlobal(a)
	}

	switch {
	// Above the three pane handlers: a drop kills shell, browser and editors at once.
	case m.active != "" && m.inPane() && m.activeDead():
		return m.handleDeadPaneKey(msg)

	case m.editing() && m.active != "":
		return m.handleEditorKey(msg)
	case m.browsing() && m.active != "":
		return m.handleBrowserKey(msg)
	case m.scrolling() && m.focused() && m.active != "":
		return m.handleScrollbackKey(msg)
	case m.focused() && m.active != "":
		return m.handleShellKey(msg)
	case m.filtering:
		return m.handleFilterKey(msg)
	}
	return m.handleNavKey(msg)
}

// doGlobal runs one of the bindings that belong to the window rather than to a mode.
func (m *model) doGlobal(a keys.Action) (tea.Model, tea.Cmd) {
	switch a {
	case keys.Sidebar:
		m.toggleSidebar()
	case keys.Mouse:
		return m, m.toggleMouse()
	}
	return m, nil
}

// ---- navigation ----

func (m *model) handleNavKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	key := msg.String()

	// A range rather than a binding, so it is read before the keyboard registry.
	if i, ok := listDigit(key); ok {
		h, ok := m.selectedHost()
		if !ok {
			return m, nil
		}
		return m.gotoShell(h.Alias, i)
	}

	return m.doList(m.reader.Read(m.binds, keys.List, key, m.cfg.VimKeys).Action)
}

// doList runs one host-list action, split from the key so the palette and menu can run it.
func (m *model) doList(a keys.Action) (tea.Model, tea.Cmd) {
	switch a {
	case keys.Up, keys.Down, keys.PageUp, keys.PageDown, keys.In, keys.Out:
		return m.move(a)

	case keys.Quit:
		m.closeAll()
		return m, tea.Quit

	case keys.Filter:
		m.filtering = true
		m.filter = ""
		m.applyFilter()

	case keys.Settings:
		m.openSettings()

	case keys.Help:
		m.openHelp()

	case keys.Palette:
		m.openPalette()

	case keys.Menu:
		m.openHostMenu()

	case keys.Back:
		// The list is the last level, so a second esc arrives as Quit above instead.
		m.leaveDetails()

	case keys.HostNewShell:
		h, ok := m.selectedHost()
		if !ok {
			return m, nil
		}
		return m, m.openShell(h, true)

	case keys.HostShell:
		h, ok := m.selectedHost()
		if !ok {
			return m, nil
		}
		s, live := m.sessions[h.Alias]
		switch {
		case live && s.dead:
			// Nothing on the other end: "focus the shell" means get it back.
			return m, m.reconnect(h)
		case live && s.shell() != nil:
			m.focusShell(h.Alias)
			return m, nil
		}
		m.setStatus(statusWarn, "no live session for %s", h.Alias)

	case keys.HostReconnec:
		return m, m.reconnectSelected()

	case keys.HostBrowser:
		h, ok := m.selectedHost()
		if !ok {
			return m, nil
		}
		return m, m.openBrowser(h)

	case keys.HostTunnels:
		h, ok := m.selectedHost()
		if !ok {
			return m, nil
		}
		return m, m.toggleTunnels(h)

	case keys.HostTunnelUI:
		h, ok := m.selectedHost()
		if !ok {
			return m, nil
		}
		m.openTunnels(h)

	case keys.HostVSCode:
		h, ok := m.selectedHost()
		if !ok {
			return m, nil
		}
		m.openVSCodeAt(h.Alias)

	case keys.HostDrop:
		h, ok := m.selectedHost()
		if !ok {
			return m, nil
		}
		m.disconnect(h.Alias)

	case keys.HostAdd:
		m.openHostFormAdd()

	case keys.HostImport:
		m.openImport(false)

	case keys.HostEdit:
		h, ok := m.selectedHost()
		if !ok {
			return m, nil
		}
		m.openHostFormEdit(h)

	case keys.HostPin:
		m.togglePin()

	case keys.HostPinUp:
		m.movePin(-1)

	case keys.HostPinDown:
		m.movePin(1)

	case keys.HostDelete:
		h, ok := m.selectedHost()
		if !ok {
			return m, nil
		}
		m.openConfirmDelete(h)
	}

	return m, nil
}

// listDigit recognises "1" … "9" in the host list and returns the shell it addresses.
func listDigit(key string) (int, bool) {
	if len(key) != 1 || key[0] < '1' || key[0] > '9' {
		return 0, false
	}
	return int(key[0] - '1'), true
}

// move applies a Scope List motion to the host list.
func (m *model) move(mo keys.Action) (tea.Model, tea.Cmd) {
	switch mo {
	case keys.Up:
		m.cursor--
	case keys.Down:
		m.cursor++
	case keys.PageDown:
		m.pageCursor(1)
	case keys.PageUp:
		m.pageCursor(-1)

	case keys.Out:
		m.leaveDetails()

	case keys.In:
		h, ok := m.selectedHost()
		if !ok {
			return m, nil
		}
		return m, m.openShell(h, false)
	}

	m.clampCursor()
	return m, nil
}

// pageCursor moves the cursor a screen down (delta 1) or up (-1), in row space including
// headings, since that is the space the screen is measured in.
func (m *model) pageCursor(delta int) {
	if len(m.rows) == 0 {
		return
	}
	target := clamp(m.cursorRow()+delta*m.listRows(), 0, len(m.rows)-1)

	back := -1 // back toward the cursor, so a page never overshoots
	if delta < 0 {
		back = 1
	}
	for i := target; i >= 0 && i < len(m.rows); i += back {
		if m.rows[i].heading == "" {
			m.cursor = m.rows[i].fi
			return
		}
	}
}

// leaveDetails backs out of the details/active view, to plain navigation.
func (m *model) leaveDetails() {
	m.active = ""
	m.mode = modeList
	m.clearStatus()
	m.relayout() // no active session means no tree column, so the columns move
}

// ---- filter entry ----

func (m *model) handleFilterKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.filtering = false
		m.filter = ""
		m.applyFilter()

	case "enter": // keeps the filter, hands the keyboard back to the list
		m.filtering = false
		m.applyFilter()

	case "up", "ctrl+p":
		m.cursor--
		m.clampCursor()

	case "down", "ctrl+n":
		m.cursor++
		m.clampCursor()

	case "backspace":
		if len(m.filter) > 0 {
			r := []rune(m.filter)
			m.filter = string(r[:len(r)-1])
		}
		m.applyFilter()

	case "ctrl+u":
		m.filter = ""
		m.applyFilter()

	default:
		if len(msg.Runes) > 0 {
			m.filter += string(msg.Runes)
			m.applyFilter()
		}
	}
	return m, nil
}

// ---- browser ----

// handleBrowserKey forwards everything to the file browser except the exits and cards.
func (m *model) handleBrowserKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// A browser waiting on an answer owns every key, including the ones hop keeps here.
	s := m.sessions[m.active]
	if s != nil && s.browser != nil && s.browser.Prompting() {
		m.reader.Reset()
		return m, s.browser.Handle(msg)
	}

	// Resolved once: resolving in both halves would give the layer two half-typed chords.
	res := m.reader.Read(m.binds, keys.Browser, msg.String(), m.cfg.VimKeys)

	if res.Pending && res.Action == keys.None {
		// First key of a chord; nothing downstream wants it while hop waits for the second.
		return m, nil
	}
	return m.doBrowser(res.Action)
}

// doBrowser runs hop's half of the browser layer; the rest is filebrowser.Do's.
func (m *model) doBrowser(a keys.Action) (tea.Model, tea.Cmd) {
	switch a {
	case keys.BrowserLeave:
		m.leaveBrowser()
		return m, nil

	case keys.BrowserSettings:
		m.openSettings()
		return m, nil

	case keys.BrowserPalette:
		m.openPalette()
		return m, nil

	case keys.BrowserHelp:
		m.openHelp()
		return m, nil

	case keys.BrowserFocusPane:
		m.focusContent()
		return m, nil

	case keys.BrowserSplit:
		return m, m.splitOpen()

	case keys.BrowserTree:
		m.toggleTree()
		return m, nil
	}

	if s := m.sessions[m.active]; s != nil && s.browser != nil {
		return m, s.browser.Do(a)
	}
	return m, nil
}

// ---- shell pane ----

// handleShellKey routes a key while a shell pane is focused; anything hop does not
// reserve is forwarded verbatim, arrows included.
func (m *model) handleShellKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	s := m.sessions[m.active]
	key := msg.String()

	if i, ok := altDigit(key); ok {
		if s != nil && i < len(s.shells) {
			s.activeSh = i
		}
		return m, nil
	}

	res := m.reader.Read(m.binds, keys.Pane, key, m.cfg.VimKeys)
	if handled, model, cmd := m.doPane(res.Action); handled {
		return model, cmd
	}

	// A first esc belongs to the shell; the second arrives as PaneLeave above.
	if s != nil && s.shell() != nil {
		m.reportInput(s.shell().pane.SendKey(msg))
	}
	return m, nil
}

// doPane runs one shell-pane action; "not mine" is an answer, since a pane forwards.
func (m *model) doPane(a keys.Action) (bool, tea.Model, tea.Cmd) {
	s := m.sessions[m.active]
	switch a {
	case keys.LeaderKey:
		m.armLeader()
		return true, m, nil

	case keys.PaneLeave:
		m.leavePane()
		return true, m, nil

	case keys.PaneNewShell:
		h, ok := m.hostByAlias(m.active)
		if !ok {
			return true, m, nil
		}
		return true, m, m.openShell(h, true)

	case keys.PaneNextTab:
		if s != nil {
			s.activeSh = cycle(s.activeSh, 1, len(s.shells))
		}
		return true, m, nil

	case keys.PanePrevTab:
		if s != nil {
			s.activeSh = cycle(s.activeSh, -1, len(s.shells))
		}
		return true, m, nil

	case keys.PaneScroll:
		// When enterScrollback declines, the key falls through to the shell.
		if m.enterScrollback(s) {
			s.shell().pane.ScrollUp(1)
			return true, m, nil
		}

	case keys.PaneScrollPg:
		if m.enterScrollback(s) {
			s.shell().pane.ScrollUp(m.scrollPage())
			return true, m, nil
		}
	}
	return false, m, nil
}

// ---- scrollback ----

// enterScrollback enters scrollback mode; the entry chords swallow their key only when
// it returns true.
func (m *model) enterScrollback(s *session) bool {
	if s == nil || s.shell() == nil {
		return false
	}
	p := s.shell().pane
	if p.AltScreen() || p.ScrollbackLen() == 0 {
		return false
	}
	m.mode = modeScrollback
	m.clearStatus()
	return true
}

// handleScrollbackKey drives the history viewport; an unused key snaps back to the live
// bottom and hands the keyboard to the shell.
func (m *model) handleScrollbackKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	s := m.sessions[m.active]
	if s == nil || s.shell() == nil {
		m.mode = modeShell
		return m, nil
	}
	p := s.shell().pane

	switch m.reader.Read(m.binds, keys.Scrollback, msg.String(), m.cfg.VimKeys).Action {
	case keys.ScrollUp:
		p.ScrollUp(1)

	case keys.ScrollDown:
		p.ScrollDown(1)
		if p.AtBottom() {
			m.exitScrollback()
		}

	case keys.ScrollPageUp:
		p.ScrollUp(m.scrollPage())

	case keys.ScrollPageDown:
		p.ScrollDown(m.scrollPage())
		if p.AtBottom() {
			m.exitScrollback()
		}

	case keys.ScrollHalfUp:
		p.ScrollUp(m.scrollHalf())

	case keys.ScrollHalfDown:
		p.ScrollDown(m.scrollHalf())
		if p.AtBottom() {
			m.exitScrollback()
		}

	case keys.ScrollTop:
		p.ScrollToTop()

	case keys.ScrollBottom:
		p.ScrollToBottom()
		m.exitScrollback()

	case keys.ScrollHelp:
		m.openHelp()

	case keys.ScrollLeave:
		// Only leaves scrollback; a second one then leaves the pane.
		m.exitScrollback()

	default:
		// Any other key means you want to type: leave history and forward it.
		m.exitScrollback()
		if s := m.sessions[m.active]; s != nil && s.shell() != nil {
			m.reportInput(s.shell().pane.SendKey(msg))
		}
	}
	return m, nil
}

func (m *model) scrollPage() int { return max(m.paneH-1, 1) }
func (m *model) scrollHalf() int { return max(m.paneH/2, 1) }

// exitScrollback returns to the live shell, snapping the viewport back to the bottom.
func (m *model) exitScrollback() {
	if m.mode == modeScrollback {
		m.mode = modeShell
	}
	if s := m.sessions[m.active]; s != nil && s.shell() != nil {
		s.shell().pane.ScrollToBottom()
	}
}

// ---- editor pane ----

// handleEditorKey routes a key while an editor tab is shown.
func (m *model) handleEditorKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	s := m.sessions[m.active]
	if s == nil || s.editor() == nil {
		m.mode = modeList
		return m, nil
	}

	key := msg.String()

	if i, ok := altDigit(key); ok {
		if i < len(s.editors) {
			s.setEditor(i)
		}
		return m, nil
	}

	if handled, model, cmd := m.doEditor(m.reader.Read(m.binds, keys.Editor, key, m.cfg.VimKeys).Action); handled {
		return model, cmd
	}

	// A first esc is still forwarded; the second arrives as EditorLeave above.
	m.reportInput(s.editor().pane.SendKey(msg))
	return m, nil
}

// doEditor runs one editor-tab action, reporting whether the layer owns it.
func (m *model) doEditor(a keys.Action) (bool, tea.Model, tea.Cmd) {
	s := m.sessions[m.active]
	if s == nil {
		return false, m, nil
	}
	switch a {
	case keys.LeaderKey:
		m.armLeader()
		return true, m, nil

	case keys.EditorLeave:
		m.leaveEditor()
		return true, m, nil

	case keys.EditorNextTab:
		// The half the keyboard is in cycles, not the left one.
		s.setEditor(cycle(s.editorIndex(s.focusedHalf()), 1, len(s.editors)))
		return true, m, nil

	case keys.EditorPrevTab:
		s.setEditor(cycle(s.editorIndex(s.focusedHalf()), -1, len(s.editors)))
		return true, m, nil

	case keys.EditorFocusTree:
		// With no browser open the key falls through to the remote editor.
		if m.focusTree() {
			return true, m, nil
		}

	case keys.EditorUnsplit:
		// s.split, not m.splitOn: a too-narrow window still springs back into a split.
		if s.split {
			s.collapseSplit()
			// Pane sizes and hit-test boxes were derived from there being two halves.
			m.relayout()
			return true, m, nil
		}

	case keys.BrowserTree:
		m.toggleTree()
		return true, m, nil
	}
	return false, m, nil
}

// focusContent moves the keyboard to the content area, reporting whether it could.
func (m *model) focusContent() bool {
	s := m.sessions[m.active]
	if s == nil || s.dead {
		return false
	}
	switch {
	case s.editor() != nil:
		m.mode = modeEditor
	case s.shell() != nil:
		m.mode = modeShell
	default:
		return false
	}
	m.clearStatus()
	return true
}

// focusTree moves the keyboard into the SFTP column, reporting whether there is one.
func (m *model) focusTree() bool {
	s := m.sessions[m.active]
	if s == nil || s.browser == nil {
		return false
	}
	m.mode = modeBrowser
	m.clearStatus()
	m.reader.Reset()
	return true
}

// ---- help card ----

// openHelp raises the key card; renderHelp picks the section from m.mode.
func (m *model) openHelp() {
	m.help = true
	m.helpScroll = 0
	m.clearStatus()
}

// handleHelpKey keeps the help card modal, swallowing every key.
func (m *model) handleHelpKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc", "q", "?", "ctrl+o", "enter":
		m.help = false
	case "up", "k":
		m.helpScroll--
	case "down", "j":
		m.helpScroll++
	case "pgup":
		m.helpScroll -= m.helpPage()
	case "pgdown", " ":
		m.helpScroll += m.helpPage()
	case "home", "g":
		m.helpScroll = 0
	case "end", "G":
		// Past the end on purpose: fitHelp clamps it to the last page.
		m.helpScroll = math.MaxInt32
	}
	return m, nil
}

// ---- leaving a mode ----

// leavePane returns from a focused terminal pane to navigation mode.
func (m *model) leavePane() {
	m.exitScrollback() // snaps the pane's offset back while m.active is still set
	m.mode = modeList
	m.clearStatus()
	m.reader.Reset()
}

// armLeader opens the leader on the pane the keyboard is in. No timer starts.
func (m *model) armLeader() {
	m.chords.leaderAlias = m.active
}

// leaderArmed reports whether the leader is waiting for its second key.
func (m *model) leaderArmed() bool { return m.chords.leaderAlias != "" }

// disarmLeader closes the leader and returns whose pane it was open on.
func (m *model) disarmLeader() string {
	alias := m.chords.leaderAlias
	m.chords.leaderAlias = ""
	return alias
}

// handleLeader answers the key after the leader; an unknown one is swallowed, not sent on.
func (m *model) handleLeader(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	alias := m.disarmLeader()
	editing := m.editing()
	key := msg.String()

	if isTabDigit(key) {
		m.selectTab(alias, int(key[0]-'1'), editing)
		return m, nil
	}

	return m.doLeader(m.binds.Action(keys.Leader, key, m.cfg.VimKeys), alias, editing)
}

// doLeader runs one leader action; alias/editing are settled at close, as the palette can
// run a leader row long after.
func (m *model) doLeader(a keys.Action, alias string, editing bool) (tea.Model, tea.Cmd) {
	switch a {
	case keys.LeaderOut:
		if editing {
			m.leaveEditor()
		} else {
			m.leavePane()
		}
		return m, nil

	case keys.LeaderVSCode:
		if editing {
			break
		}
		// Leaving is part of it: VS Code takes over.
		m.leavePane()
		m.openVSCodeAt(alias)
		return m, nil

	case keys.LeaderPalette:
		m.openPalette()
		return m, nil

	case keys.LeaderHelp:
		m.openHelp()
		return m, nil

	case keys.LeaderShell:
		if editing {
			break
		}
		h, ok := m.hostByAlias(alias)
		if !ok {
			return m, nil
		}
		return m, m.openShell(h, true)
	}

	return m, nil
}

// isTabDigit reports whether key names a tab. 0 is not one: it opens a new shell.
func isTabDigit(key string) bool {
	return len(key) == 1 && key[0] >= '1' && key[0] <= '9'
}

// selectTab moves to tab i of alias; not focusShell, which would resize every shell.
func (m *model) selectTab(alias string, i int, editing bool) {
	s := m.sessions[alias]
	if s == nil || s.dead {
		return
	}
	if editing {
		if i < len(s.editors) {
			s.setEditor(i)
		}
		return
	}
	if i < len(s.shells) {
		s.activeSh = i
	}
}

// gotoShell focuses shell i of alias from the host list.
func (m *model) gotoShell(alias string, i int) (tea.Model, tea.Cmd) {
	s := m.sessions[alias]
	if s == nil || s.dead || i >= len(s.shells) {
		return m, nil
	}
	s.activeSh = i
	m.focusShell(alias)
	return m, nil
}

// leaveBrowser hands the keyboard back to the host list; the column stays on screen.
func (m *model) leaveBrowser() {
	m.mode = modeList
	m.clearStatus()
	m.reader.Reset()
}

// leaveEditor hands the keyboard to the tree column, or the host list if it is gone.
func (m *model) leaveEditor() {
	m.mode = modeList
	m.clearStatus()
	m.reader.Reset()
	if s := m.sessions[m.active]; s != nil && s.browser != nil {
		m.mode = modeBrowser
	}
}

// leaveAll drops every pane mode, handing the keyboard back to the host list.
func (m *model) leaveAll() {
	m.active = ""
	m.mode = modeList
	m.relayout()
}

// ---- key helpers ----

// cycle moves i by delta among n tabs, wrapping at both ends.
func cycle(i, delta, n int) int {
	if n < 2 {
		return i
	}
	return ((i+delta)%n + n) % n
}

// altDigit recognises alt+1 … alt+9 and returns the tab index it addresses.
func altDigit(key string) (int, bool) {
	n, ok := strings.CutPrefix(key, "alt+")
	if !ok || len(n) != 1 || n[0] < '1' || n[0] > '9' {
		return 0, false
	}
	return int(n[0] - '1'), true
}

// chordState holds hop's half-typed gestures: the leader and the pointer's double-click.
type chordState struct {
	// leaderAlias is whose pane the leader was opened in, "" when closed. No timestamp: it
	// waits for its second key however long that takes.
	leaderAlias string

	// click is when the most recent click landed; clickZone/clickID are what it landed on.
	click     time.Time
	clickZone zone
	clickID   int
}
