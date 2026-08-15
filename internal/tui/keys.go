package tui

import (
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"hop/internal/keymap"
)

// doubleEscWindow is how long after an esc a second esc counts as "leave the pane"
// rather than two escapes bound for the remote shell.
const doubleEscWindow = 400 * time.Millisecond

// leaderKey opens hop's keyboard inside a pane. It arms with no timeout: the footer
// lists what can follow and hop waits for the next key. Leaving is leaderKey then "o";
// esc esc still leaves in one gesture.
const leaderKey = "ctrl+o"

// toggleSidebarKey collapses the host list and brings it back. ctrl+b rather than the
// mnemonic alt+b: stock macOS terminals do not send alt+letter as a meta escape. The
// price is that a remote tmux never sees its prefix while hop holds it.
const toggleSidebarKey = "ctrl+b"

// toggleMouseKey hands mouse reporting back to the terminal and takes it again. hop
// selects text itself (see selection.go); this is for what that does not cover —
// dragging across sidebar and pane, or a terminal feature that wants the pointer.
// ctrl+g for the same reason ctrl+b is the sidebar, and it is the least spoken-for
// control byte at a shell prompt.
const toggleMouseKey = "ctrl+g"

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
	case m.help:
		return m.handleHelpKey(msg)
	case m.hostKey.open:
		return m.handleHostKeyKey(msg)
	case m.confirm.open:
		return m.handleConfirmKey(msg)
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
	switch msg.String() {
	case toggleSidebarKey:
		m.toggleSidebar()
		m.chords.esc = time.Time{} // any non-esc breaks a half-typed double-esc
		return m, nil
	case toggleMouseKey:
		m.chords.esc = time.Time{}
		return m, m.toggleMouse()
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

// ---- navigation ----

func (m *model) handleNavKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	key := msg.String()

	// Motions first, through the shared keymap — which also drops the vim keys when the
	// setting is off. Scope List is the step and page keys, without the jumps and ctrl
	// chords the browser keeps.
	if mo := m.keys.Motion(keymap.List, key, m.cfg.VimKeys); mo != keymap.None {
		return m.move(mo)
	}

	switch key {
	case "q", "ctrl+c":
		m.closeAll()
		return m, tea.Quit

	case "/":
		m.filtering = true
		m.filter = ""
		m.applyFilter()

	case ",":
		m.openSettings()

	case "?":
		m.openHelp()

	case "ctrl+o":
		// Nothing to go back from: the list is where back leads.

	case "esc":
		// Not a motion: esc is the browser's double-tap chord, so the keymap leaves it
		// to the mode that owns it. In the list it does the same as everywhere else —
		// one level out — and the list is the last level: the first esc drops the host
		// you were reading about, a second one inside the window leaves hop. The window
		// is what keeps a stray esc from quitting.
		if m.escChord() {
			m.closeAll()
			return m, tea.Quit
		}
		m.leaveDetails()

	case "S":
		// Another shell on the same host, alongside the ones already open.
		h, ok := m.selectedHost()
		if !ok {
			return m, nil
		}
		return m, m.openShell(h, true)

	case "1", "2", "3", "4", "5", "6", "7", "8", "9":
		// Straight into a numbered shell of the host under the cursor — "s", aimed. A
		// host with no session, or no shell at that position, ignores it.
		h, ok := m.selectedHost()
		if !ok {
			return m, nil
		}
		return m.gotoShell(h.Alias, int(key[0]-'1'))

	case "s":
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

	case "r":
		// Reconnect a dropped session, putting back the shells and browser it held. Bound
		// in the list as well as in the dead pane: a drop is as often noticed by the red
		// dot in the sidebar.
		return m, m.reconnectSelected()

	case "f":
		h, ok := m.selectedHost()
		if !ok {
			return m, nil
		}
		return m, m.openBrowser(h)

	case "t":
		h, ok := m.selectedHost()
		if !ok {
			return m, nil
		}
		return m, m.toggleTunnels(h)

	case "T":
		h, ok := m.selectedHost()
		if !ok {
			return m, nil
		}
		m.openTunnels(h)

	case "o":
		h, ok := m.selectedHost()
		if !ok {
			return m, nil
		}
		// In the directory the host's shell is standing in, when it has one.
		m.openVSCodeAt(h.Alias)

	case "d":
		h, ok := m.selectedHost()
		if !ok {
			return m, nil
		}
		m.disconnect(h.Alias)

	case "a":
		// Add a host from scratch — the in-app twin of `hop add`.
		m.openHostFormAdd()

	case "i":
		// Import (or re-import) an OpenSSH config — the in-app twin of `hop import`. It
		// upserts per host, so it stays bound once the list is full.
		m.openImport(false)

	case "e":
		// Edit the host under the cursor: the same form, pre-filled.
		h, ok := m.selectedHost()
		if !ok {
			return m, nil
		}
		m.openHostFormEdit(h)

	case "p":
		// Pin the host under the cursor to the PINNED section, or take it back out.
		m.togglePin()

	case "K":
		// Move a pinned host up its section, "J" down. Shifted j/k: the step keys with
		// "and take the host with you" on them, and they arrive on a stock macOS terminal.
		m.movePin(-1)

	case "J":
		m.movePin(1)

	case "x":
		// Delete the host under the cursor, behind a confirmation. 'd' already disconnects.
		h, ok := m.selectedHost()
		if !ok {
			return m, nil
		}
		m.openConfirmDelete(h)
	}

	return m, nil
}

// move applies a motion to the host list. Only Scope List's motions arrive here: step,
// page and in/out. The jumps and ctrl chords are the browser's — the list does not
// scroll, and the letters are worth more to it as commands.
func (m *model) move(mo keymap.Motion) (tea.Model, tea.Cmd) {
	switch mo {
	case keymap.Up:
		m.cursor--
	case keymap.Down:
		m.cursor++
	case keymap.PageDown:
		m.pageCursor(1)
	case keymap.PageUp:
		m.pageCursor(-1)

	case keymap.Out:
		m.leaveDetails()

	case keymap.In:
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
	switch key := msg.String(); key {
	case "ctrl+o":
		m.leaveBrowser()
		return m, nil

	case ",":
		// Reachable from the browser too, where the editor and download settings are
		// felt. The browser binds no ",".
		m.openSettings()
		return m, nil

	case "?":
		// As in the list: the browser binds no "?" of its own, so the card is free to
		// take it, and the footer is free to name it.
		m.openHelp()
		return m, nil

	case "esc":
		// Nothing downstream wants an esc here, so swallow the first and arm the window.
		if m.escChord() {
			m.leaveBrowser()
		}
		return m, nil

	default:
		// Any other key breaks the sequence, so esc-j-esc is not a double.
		m.chords.esc = time.Time{}
	}

	if s := m.sessions[m.active]; s != nil && s.browser != nil {
		return m, s.browser.Handle(msg)
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

	switch key {
	case leaderKey:
		// Open the leader and wait. See handleLeader.
		m.armLeader()
		return m, nil

	case "alt+0":
		// Another shell on the host you are already in — "S" without the trip back to the
		// list. A second channel on the connection hop holds, so there is no handshake.
		h, ok := m.hostByAlias(m.active)
		if !ok {
			return m, nil
		}
		return m, m.openShell(h, true)

	case "shift+right", "alt+right":
		if s != nil {
			s.activeSh = cycle(s.activeSh, 1, len(s.shells))
		}
		return m, nil

	case "shift+left", "alt+left":
		if s != nil {
			s.activeSh = cycle(s.activeSh, -1, len(s.shells))
		}
		return m, nil

	case "shift+up":
		// Enter scrollback and step one line up. When enterScrollback declines, the key
		// falls through to the shell.
		if m.enterScrollback(s) {
			s.shell().pane.ScrollUp(1)
			return m, nil
		}

	case "shift+pgup":
		// The same entry, a page at a time.
		if m.enterScrollback(s) {
			s.shell().pane.ScrollUp(m.scrollPage())
			return m, nil
		}
	}

	// alt+1 … alt+9 jump straight to a shell, ignoring one that is not open.
	if i, ok := altDigit(key); ok {
		if s != nil && i < len(s.shells) {
			s.activeSh = i
		}
		return m, nil
	}

	if key == "esc" {
		// A second esc inside the window leaves the pane. The first is still forwarded
		// below: a lone esc belongs to the shell, and a stray extra one is harmless.
		if m.escChord() {
			m.leavePane()
			return m, nil
		}
	} else {
		// Any other key breaks the sequence, so esc-j-esc is not a double.
		m.chords.esc = time.Time{}
	}

	if s != nil && s.shell() != nil {
		s.shell().pane.SendKey(msg)
	}
	return m, nil
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

	switch msg.String() {
	case "up", "k", "shift+up":
		p.ScrollUp(1)

	case "down", "j", "shift+down":
		p.ScrollDown(1)
		if p.AtBottom() {
			m.exitScrollback()
		}

	case "pgup", "shift+pgup":
		// No ctrl+b partner for the ctrl+f below: handleKey takes it for the sidebar.
		p.ScrollUp(m.scrollPage())

	case "pgdown", "ctrl+f", "shift+pgdown":
		p.ScrollDown(m.scrollPage())
		if p.AtBottom() {
			m.exitScrollback()
		}

	case "ctrl+u":
		p.ScrollUp(m.scrollHalf())

	case "ctrl+d":
		p.ScrollDown(m.scrollHalf())
		if p.AtBottom() {
			m.exitScrollback()
		}

	case "g", "home":
		p.ScrollToTop()

	case "G", "end":
		p.ScrollToBottom()
		m.exitScrollback()

	case "?":
		// The footer names ? in every mode hop owns the keyboard in, and this is one:
		// scrollback forwards nothing until you leave it.
		m.openHelp()

	case "q", "esc", "enter", "i", "ctrl+o", "left", "right":
		// The deliberate ways out. ctrl+o only leaves scrollback, back to the live shell;
		// a second one then leaves the pane. None of these reach the shell.
		m.exitScrollback()

	default:
		// Any other key means you want to type: leave history and forward it.
		m.exitScrollback()
		if s := m.sessions[m.active]; s != nil && s.shell() != nil {
			s.shell().pane.SendKey(msg)
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

	switch key {
	case leaderKey:
		m.armLeader()
		return m, nil

	case "shift+right", "alt+right", "alt+l":
		s.activeEd = cycle(s.activeEd, 1, len(s.editors))
		return m, nil

	case "shift+left", "alt+left", "alt+h":
		s.activeEd = cycle(s.activeEd, -1, len(s.editors))
		return m, nil
	}

	// alt+1 … alt+9 jump straight to a tab, ignoring one that is not open.
	if i, ok := altDigit(key); ok {
		if i < len(s.editors) {
			s.activeEd = i
		}
		return m, nil
	}

	if key == "esc" {
		// As in a shell pane, the first esc is still forwarded to the editor.
		if m.escChord() {
			m.leaveEditor()
			return m, nil
		}
	} else {
		m.chords.esc = time.Time{}
	}

	s.editor().pane.SendKey(msg)
	return m, nil
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

// escChord reports whether this esc completes a double-esc within the window, and
// arms the window when it does not.
func (m *model) escChord() bool {
	if !m.chords.esc.IsZero() && time.Since(m.chords.esc) <= doubleEscWindow {
		return true
	}
	m.chords.esc = time.Now()
	return false
}

// leavePane returns from a focused terminal pane to navigation mode.
func (m *model) leavePane() {
	m.exitScrollback() // snaps the pane's offset back while m.active is still set
	m.mode = modeList
	m.clearStatus()
	m.chords.esc = time.Time{}
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

	switch key := msg.String(); {
	case key == "o":
		// Out.
		if editing {
			m.leaveEditor()
		} else {
			m.leavePane()
		}
		return m, nil

	case key == "c" && !editing:
		// This directory in VS Code Remote. Leaving is part of it: VS Code takes over.
		m.leavePane()
		m.openVSCodeAt(alias)
		return m, nil

	case key == "?":
		// The way to the card from a pane. A bare "?" cannot be it: in a shell or an
		// editor that key is text, and the remote is owed it.
		m.openHelp()
		return m, nil

	case key == "0" && !editing:
		// Another shell on this host. No 0 for editor tabs: those are opened by file.
		h, ok := m.hostByAlias(alias)
		if !ok {
			return m, nil
		}
		return m, m.openShell(h, true)

	case isTabDigit(key):
		m.selectTab(alias, int(key[0]-'1'), editing)
		return m, nil
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
	m.chords.esc = time.Time{}
}

// leaveEditor returns from the editor tabs to the browser the file was opened from (or
// to navigation, when that browser is gone). The editors keep running; closing one is
// the editor's own business.
func (m *model) leaveEditor() {
	m.mode = modeList
	m.clearStatus()
	m.chords.esc = time.Time{}
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

// chordState is every half-typed key sequence hop is holding. They share a failure
// mode: each is armed by one event and resolved by another, and must be spent when it
// resolves — a chord left armed fires again on the next keystroke.
type chordState struct {
	// esc is when the most recent esc was forwarded to the focused pane. A second esc
	// within doubleEscWindow leaves the pane. Zero means none is pending.
	esc time.Time

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
