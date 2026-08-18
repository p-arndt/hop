package tui

import (
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"hop/internal/keys"
)

// The keyboard itself lives in internal/keys: every binding below is resolved to an
// action there, so the switches in this file say what hop does rather than which key did
// it, and a user's config.json can move the key without touching any of them.
//
// The rationale each binding was chosen for stayed with the binding, in that registry.

// handleKey routes a key to whichever mode owns the keyboard, in order of modality:
// modal cards, sidebar toggle, panes that forward to a remote program, filter, then
// plain navigation.
func (m *model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// On Windows a paste arrives as a burst of ordinary keystrokes; a key that cannot
	// be part of one ends the burst here, before it is delivered. See paste.go.
	if m.pasteCoalesce {
		if m.takeKey(msg) {
			return m, m.pasteFlushCmd()
		}
		m.flushPaste()
	}

	// Above every mode: the modes below read the key's name, and a paste's name is the
	// whole clipboard.
	if msg.Paste {
		return m.handlePaste(msg)
	}

	switch {
	case m.auth.open:
		// Above even the help card: a dial is parked on this one inside the SSH
		// handshake, so a help card opened while connecting would hide the challenge
		// that arrives next.
		return m.handleAuthKey(msg)
	case m.guidance.open:
		// The first-run question, above the rest: it is asked before hop has a screen
		// worth acting on, and the card it hands over to must not open behind it.
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

	// While the leader is open hop owns the keyboard outright, above even ctrl+b and
	// ctrl+g.
	if m.leaderArmed() {
		return m.handleLeader(msg)
	}

	// Any key ends a selection; the key itself still means whatever it means below.
	m.clearSelection()

	// The two bindings hop holds in every mode below the cards: they belong to the
	// window, not to whatever owns the keyboard.
	if a := m.binds.Action(keys.Global, msg.String(), m.cfg.VimKeys); a != keys.None {
		m.reader.Reset() // a key that is not an esc breaks a half-typed double-esc
		return m.doGlobal(a)
	}

	switch {
	// A dropped pane takes the reconnect key and the ways out, and forwards nothing.
	// Above the three pane handlers because a drop kills shell, browser and editors at
	// once.
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

// doGlobal runs one of the two bindings that belong to the window rather than to a mode.
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

	// A digit addresses a shell by its number — a range rather than a binding, so it is
	// read before the keyboard is consulted and is not in the registry. A host with no
	// session, or no shell at that position, ignores it.
	if i, ok := listDigit(key); ok {
		h, ok := m.selectedHost()
		if !ok {
			return m, nil
		}
		return m.gotoShell(h.Alias, i)
	}

	return m.doList(m.reader.Read(m.binds, keys.List, key, m.cfg.VimKeys).Action)
}

// doList runs one host-list action. Split from the key that resolved to it so the palette
// and the context menu can run a row directly: before this they replayed the row's key
// through handleKey, which meant only a key hop bound could be an action at all, and a
// rebound key had to be replayed as whatever the user had chosen.
func (m *model) doList(a keys.Action) (tea.Model, tea.Cmd) {
	switch a {
	// Motions: the list binds the steps, the pages and in/out, and leaves the jumps and
	// the ctrl chords to the browser (see the registry).
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
		// Every action in one searchable list, each row carrying the key that does it —
		// the way in for a keyboard nobody has learnt yet. See actions.go.
		m.openPalette()

	case keys.Menu:
		// The same list, narrowed to the host under the cursor and anchored to its row.
		m.openHostMenu()

	case keys.Back:
		// In the list, back is one level out and the list is the last level: the first
		// esc drops the host you were reading about, and the second — within the window
		// the Reader holds — leaves hop. That second one arrives as Quit above.
		m.leaveDetails()

	case keys.HostNewShell:
		// Another shell on the same host, alongside the ones already open.
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
		// Reconnect a dropped session, putting back the shells and browser it held. Bound
		// in the list as well as in the dead pane: a drop is as often noticed by the red
		// dot in the sidebar.
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
		// In the directory the host's shell is standing in, when it has one.
		m.openVSCodeAt(h.Alias)

	case keys.HostDrop:
		h, ok := m.selectedHost()
		if !ok {
			return m, nil
		}
		m.disconnect(h.Alias)

	case keys.HostAdd:
		// Add a host from scratch — the in-app twin of `hop add`.
		m.openHostFormAdd()

	case keys.HostImport:
		// Import (or re-import) an OpenSSH config — the in-app twin of `hop import`. It
		// upserts per host, so it stays bound once the list is full.
		m.openImport(false)

	case keys.HostEdit:
		// Edit the host under the cursor: the same form, pre-filled.
		h, ok := m.selectedHost()
		if !ok {
			return m, nil
		}
		m.openHostFormEdit(h)

	case keys.HostPin:
		// Pin the host under the cursor to the PINNED section, or take it back out.
		m.togglePin()

	case keys.HostPinUp:
		m.movePin(-1)

	case keys.HostPinDown:
		m.movePin(1)

	case keys.HostDelete:
		// Delete the host under the cursor, behind a confirmation. Disconnect is its own
		// key.
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

// move applies a motion to the host list. Only Scope List's motions arrive here: step,
// page and in/out. The jumps and ctrl chords are the browser's — the list does not
// scroll, and the letters are worth more to it as commands.
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
		// Descend into the host under the cursor: connect to it.
		h, ok := m.selectedHost()
		if !ok {
			return m, nil
		}
		return m, m.openShell(h, false)
	}

	m.clampCursor()
	return m, nil
}

// pageCursor moves the cursor a screen down (delta 1) or up (-1). It works in row
// space, headings included, because that is the space the screen is measured in. A
// page that ends on a heading gives the row back toward where the cursor came from.
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
}

// ---- filter entry ----

func (m *model) handleFilterKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.filtering = false
		m.filter = ""
		m.applyFilter()

	case "enter":
		// Keep the filter, hand the keyboard back to the list.
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

// handleBrowserKey forwards everything to the file browser except the exits and the
// settings key, leaving the arrows as pure motion.
func (m *model) handleBrowserKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// A browser waiting on an answer owns every key, including the ones hop keeps for
	// itself here: while a filename is being typed a "," is a comma, not the settings
	// popover, and esc cancels the question rather than leaving the browser.
	s := m.sessions[m.active]
	if s != nil && s.browser != nil && s.browser.Prompting() {
		m.reader.Reset()
		return m, s.browser.Handle(msg)
	}

	// One keyboard, resolved once: the exits and the cards below are hop's half of the
	// Browser layer, everything else is the browser's own and goes to Do. Resolving in
	// both places would give the layer two half-typed chords.
	res := m.reader.Read(m.binds, keys.Browser, msg.String(), m.cfg.VimKeys)

	if res.Pending && res.Action == keys.None {
		// The first key of a chord — an esc, or the first g of a gg. Nothing downstream
		// wants it while hop waits for the second.
		return m, nil
	}
	return m.doBrowser(res.Action)
}

// doBrowser runs one action of the browser layer: hop's half here, the browser's own in
// filebrowser.Do. The split is where the behaviour lives, not what the user sees — to
// them it is one keyboard, which is why it is one layer.
func (m *model) doBrowser(a keys.Action) (tea.Model, tea.Cmd) {
	switch a {
	case keys.BrowserLeave:
		m.leaveBrowser()
		return m, nil

	case keys.BrowserSettings:
		// Reachable from the browser too, where the editor and download settings are
		// felt. The browser binds no "," of its own.
		m.openSettings()
		return m, nil

	case keys.BrowserPalette:
		// The browser's own keyboard, searchable. As with the settings and the card, the
		// browser binds no ctrl+k of its own, so the palette is free to take it.
		m.openPalette()
		return m, nil

	case keys.BrowserHelp:
		m.openHelp()
		return m, nil
	}

	if s := m.sessions[m.active]; s != nil && s.browser != nil {
		return m, s.browser.Do(a)
	}
	return m, nil
}

// ---- shell pane ----

// handleShellKey routes a key while a shell pane is focused. The remote shell owns
// nearly every key, so hop reserves only ctrl+o, a double esc, shift+←/→ to switch
// shells, shift+↑/pgup for scrollback, and ctrl+b for the sidebar (taken in handleKey).
// Everything else is forwarded verbatim, arrows included.
//
// Tab selection is shift+←/→ because a stock macOS terminal never sends the alt+←/→
// meta escape; the alt chords stay bound as aliases. Going to a shell by number is the
// leader — see handleLeader.
func (m *model) handleShellKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	s := m.sessions[m.active]
	key := msg.String()

	// alt+1 … alt+9 jump straight to a shell — a range rather than a binding, and read
	// before the keyboard for the same reason the list's digits are.
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

	// A first esc is still forwarded below: a lone esc belongs to the shell, and a stray
	// extra one is harmless. The second one arrives as PaneLeave above.
	if s != nil && s.shell() != nil {
		m.reportInput(s.shell().pane.SendKey(msg))
	}
	return m, nil
}

// doPane runs one shell-pane action, reporting whether it was one this layer owns — a
// pane forwards everything else to the remote program, so "not mine" is an answer rather
// than a no-op.
func (m *model) doPane(a keys.Action) (bool, tea.Model, tea.Cmd) {
	s := m.sessions[m.active]
	switch a {
	case keys.LeaderKey:
		// Open the leader and wait. See handleLeader.
		m.armLeader()
		return true, m, nil

	case keys.PaneLeave:
		m.leavePane()
		return true, m, nil

	case keys.PaneNewShell:
		// Another shell on the host you are already in — the list's "S" without the trip
		// back. A second channel on the connection hop holds, so there is no handshake.
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
		// Enter scrollback and step one line up. When enterScrollback declines, the key
		// falls through to the shell.
		if m.enterScrollback(s) {
			s.shell().pane.ScrollUp(1)
			return true, m, nil
		}

	case keys.PaneScrollPg:
		// The same entry, a page at a time.
		if m.enterScrollback(s) {
			s.shell().pane.ScrollUp(m.scrollPage())
			return true, m, nil
		}
	}
	return false, m, nil
}

// ---- scrollback ----

// enterScrollback puts the focused shell pane into scrollback mode, or reports that
// there is nothing to scroll and leaves the mode untouched — the entry chords only
// swallow their key when it returns true. A full-screen program owns its own scrolling
// and keeps no scrollback here.
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

// handleScrollbackKey drives the history viewport while a shell pane is scrolled back.
// The ways out — and any key the viewport has no use for — snap back to the live bottom
// and hand the keyboard back to the shell. Scrolling to the bottom is itself a way out.
func (m *model) handleScrollbackKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	s := m.sessions[m.active]
	if s == nil || s.shell() == nil {
		// The session went away under us; there is nothing left to scroll.
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
		// The footer names the card in every mode hop owns the keyboard in, and this is
		// one: scrollback forwards nothing until you leave it.
		m.openHelp()

	case keys.ScrollLeave:
		// The deliberate ways out. The leader key only leaves scrollback, back to the
		// live shell; a second one then leaves the pane. None of these reach the shell.
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

// scrollPage / scrollHalf are the page and half-page steps for scrollback mode.
func (m *model) scrollPage() int { return max(m.paneH-1, 1) }
func (m *model) scrollHalf() int { return max(m.paneH/2, 1) }

// exitScrollback returns from scrollback mode to the live shell, snapping the viewport
// back to the bottom.
func (m *model) exitScrollback() {
	if m.mode == modeScrollback {
		m.mode = modeShell
	}
	if s := m.sessions[m.active]; s != nil && s.shell() != nil {
		s.shell().pane.ScrollToBottom()
	}
}

// ---- editor pane ----

// handleEditorKey routes a key while an editor tab is shown. The editor owns nearly
// every key; hop reserves ctrl+o, a double esc, and the tab-switch chords.
func (m *model) handleEditorKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	s := m.sessions[m.active]
	if s == nil || s.editor() == nil {
		// Every tab closed while we were in editing mode; there is nothing to show.
		m.mode = modeList
		return m, nil
	}

	key := msg.String()

	// alt+1 … alt+9 jump straight to a tab, ignoring one that is not open.
	if i, ok := altDigit(key); ok {
		if i < len(s.editors) {
			s.activeEd = i
		}
		return m, nil
	}

	if handled, model, cmd := m.doEditor(m.reader.Read(m.binds, keys.Editor, key, m.cfg.VimKeys).Action); handled {
		return model, cmd
	}

	// As in a shell pane, a first esc is still forwarded to the editor: the second one
	// arrives as EditorLeave above.
	m.reportInput(s.editor().pane.SendKey(msg))
	return m, nil
}

// doEditor runs one editor-tab action, reporting whether the layer owns it. As with a
// shell pane, everything it does not own belongs to the remote editor.
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
		s.activeEd = cycle(s.activeEd, 1, len(s.editors))
		return true, m, nil

	case keys.EditorPrevTab:
		s.activeEd = cycle(s.activeEd, -1, len(s.editors))
		return true, m, nil
	}
	return false, m, nil
}

// ---- help card ----

// openHelp raises the key card. Every mode's way in goes through here, so the card is
// entered the same way from all of them — and so the section it opens on is decided in
// one place (see renderHelp, which reads m.mode).
func (m *model) openHelp() {
	m.help = true
	m.clearStatus()
}

// handleHelpKey keeps the help card modal: it swallows every key, and the usual ways
// out close it.
func (m *model) handleHelpKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc", "q", "?", "ctrl+o", "enter":
		m.help = false
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

// handleLeader answers the key that followed the leader. Nothing here leaves except
// "o": every other chord acts where you already are.
//
// A key that names no chord closes the leader and is swallowed rather than passed to
// the remote, which would otherwise act on the tail of an abandoned chord.
func (m *model) handleLeader(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	alias := m.disarmLeader()
	editing := m.editing()
	key := msg.String()

	// A digit picks a tab: a range, read before the keyboard, as everywhere else.
	if isTabDigit(key) {
		m.selectTab(alias, int(key[0]-'1'), editing)
		return m, nil
	}

	return m.doLeader(m.binds.Action(keys.Leader, key, m.cfg.VimKeys), alias, editing)
}

// doLeader runs one leader action on the pane it was opened over. alias is whose pane
// that was and editing whether it holds an editor tab, both settled when the leader
// closed rather than read again here: the palette can run a leader row too, and by then
// the leader is long shut.
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
		// This directory in VS Code Remote. Leaving is part of it: VS Code takes over.
		m.leavePane()
		m.openVSCodeAt(alias)
		return m, nil

	case keys.LeaderPalette:
		// The palette from inside a pane. Behind the leader for the reason the card is:
		// a bare ctrl+k in a shell is a keystroke the remote program is owed.
		m.openPalette()
		return m, nil

	case keys.LeaderHelp:
		// The way to the card from a pane. A bare "?" cannot be it: in a shell or an
		// editor that key is text, and the remote is owed it.
		m.openHelp()
		return m, nil

	case keys.LeaderShell:
		// Another shell on this host. Not for editor tabs: those are opened by file.
		if editing {
			break
		}
		h, ok := m.hostByAlias(alias)
		if !ok {
			return m, nil
		}
		return m, m.openShell(h, true)
	}

	// Anything else: the leader is closed and the key is spent on closing it.
	return m, nil
}

// isTabDigit reports whether key names a tab. 0 is not one: it opens a new shell.
func isTabDigit(key string) bool {
	return len(key) == 1 && key[0] >= '1' && key[0] <= '9'
}

// selectTab moves to tab i of alias without touching the focus; a tab that is not open
// leaves the selection alone. Deliberately not focusShell: that re-runs resizeShells,
// sending a window-change down every shell of the host for no change in size.
func (m *model) selectTab(alias string, i int, editing bool) {
	s := m.sessions[alias]
	if s == nil || s.dead {
		return
	}
	if editing {
		if i < len(s.editors) {
			s.activeEd = i
		}
		return
	}
	if i < len(s.shells) {
		s.activeSh = i
	}
}

// gotoShell focuses shell i of alias from the host list. Unlike selectTab it does
// focus: the digits in the list are a way in.
func (m *model) gotoShell(alias string, i int) (tea.Model, tea.Cmd) {
	s := m.sessions[alias]
	if s == nil || s.dead || i >= len(s.shells) {
		return m, nil
	}
	s.activeSh = i
	m.focusShell(alias)
	return m, nil
}

// leaveBrowser returns from the file browser to navigation mode.
func (m *model) leaveBrowser() {
	m.mode = modeList
	m.clearStatus()
	m.reader.Reset()
}

// leaveEditor returns from the editor tabs to the browser the file was opened from (or
// to navigation, when that browser is gone). The editors keep running; closing one is
// the editor's own business.
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

// chordState is what is left of hop's half-typed gestures once the keyboard's own
// sequences moved into keys.Reader: the leader, which is a mode rather than a pending
// keystroke, and the pointer's double-click. Both share the failure mode the Reader was
// built for — armed by one event, resolved by another, and firing again on the next
// event if they are not spent when they resolve.
type chordState struct {
	// leaderAlias is whose pane the leader was opened in, and "" when it is closed. No
	// timestamp: the leader waits for its second key however long that takes.
	leaderAlias string

	// click is when the most recent click landed; clickZone/clickID are what it landed
	// on. A second click on the same thing inside doubleClickWindow means "open this".
	// See clickChord.
	click     time.Time
	clickZone zone
	clickID   int
}
