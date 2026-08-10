package tui

import (
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"hop/internal/keymap"
)

// doubleEscWindow is how long after an esc a second esc counts as "leave the
// pane" rather than two independent escapes bound for the remote shell. Long
// enough for a deliberate double-tap, short enough that two considered presses
// (say, in vim) stay independent.
const doubleEscWindow = 400 * time.Millisecond

// toggleSidebarKey collapses the host list and brings it back. The mnemonic key
// would be alt+b (sideBar), but a terminal only sends alt+letter as a meta escape
// when it is configured to — on macOS it is not, by default, so the key arrives as a
// plain "b" hop never sees. ctrl+b is sent as a control byte by every terminal there
// is, and it is the same key tmux and screen use for "talk to the multiplexer, not
// to the program inside it", which is exactly what this is.
//
// The price is that a remote tmux never sees its prefix while hop holds it. That is
// the deal every multiplexer makes with the one above it; ctrl+o still leaves the
// pane, and nothing else is taken.
const toggleSidebarKey = "ctrl+b"

// toggleMouseKey switches hop's mouse reporting off and on again without opening
// the settings card — the escape hatch for the one thing hop cannot give back
// while it is reading the mouse.
//
// hop selects text itself (see selection.go), so this is not the way to copy out
// of a pane. It is for the selections hop's own does not cover: dragging across
// the sidebar *and* a pane, picking a URL out of the footer, or handing the window
// to a terminal feature — a click-to-open, a search overlay — that wants the
// pointer. Off, the terminal is in charge of the mouse again exactly as it was
// before hop started; the same key puts hop back in charge.
//
// ctrl+g for the same reason ctrl+b is the sidebar: on macOS a terminal does not
// send alt+<letter> by default, so an alt mnemonic never arrives. ctrl+g is the
// least spoken-for control byte there is — readline's abort, which is idle at a
// prompt — and it is taken from the remote program the way ctrl+o and ctrl+b are.
const toggleMouseKey = "ctrl+g"

// handleKey routes a key to whichever mode currently owns the keyboard. The order
// is the order of modality: the modal cards take everything, then the sidebar
// toggle, then the panes that forward to a remote program, then the filter, then
// plain navigation.
func (m *model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// Before anything else, because it is about what this key *is* rather than what
	// it means: on Windows a paste arrives as a burst of ordinary keystrokes, so the
	// ones that could be part of one are held for a few milliseconds and sent as a
	// paste if they turn out to be. Any key that is not held ends the burst, and ends
	// it here so that the keys typed before it are delivered before it. See paste.go.
	if m.pasteCoalesce {
		if m.takeKey(msg) {
			return m, m.pasteFlushCmd()
		}
		m.flushPaste()
	}

	// A paste is answered above every mode, because every mode below reads the key's
	// name and a paste's name is the whole clipboard. See handlePaste.
	if msg.Paste {
		return m.handlePaste(msg)
	}

	switch {
	case m.auth.open:
		// First, above even the help card: this one has a dial parked on it inside
		// the SSH handshake, so it is the most modal thing hop has. The order
		// matters — '?' is bound in navigation mode and a dial takes long enough to
		// press it, so a help card opened while connecting would otherwise be
		// drawn over the challenge that arrives next, leaving the dial waiting on
		// a card nobody can see or reach until the code has expired.
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

	// A key is the end of a selection: the highlight is a moment, and the screen
	// under it is about to change. The key itself is not spent doing this — it goes
	// on to mean whatever it means below.
	m.clearSelection()

	// The two bindings hop holds in every mode below the cards. They are layout and
	// device, not navigation: they belong to the window rather than to whatever owns
	// the keyboard, so they are answered here instead of being repeated in four
	// handlers — and they are reserved from the remote program the same way ctrl+o is.
	// A card is the exception: each takes every key while it is up.
	switch msg.String() {
	case toggleSidebarKey:
		m.toggleSidebar()
		// Any key that is not an esc breaks a half-typed double-esc, this one included.
		m.lastEsc = time.Time{}
		return m, nil
	case toggleMouseKey:
		m.lastEsc = time.Time{}
		return m, m.toggleMouse()
	}

	switch {
	// A pane whose connection has dropped is showing a picture, not a program: it
	// takes the reconnect key and the ways out, and forwards nothing. This sits above
	// the three pane handlers rather than inside each of them, because a dead
	// connection kills the shell, the browser and the editors at once.
	case m.active != "" && (m.focused || m.browsing || m.editing) && m.activeDead():
		return m.handleDeadPaneKey(msg)

	case m.editing && m.active != "":
		return m.handleEditorKey(msg)
	case m.browsing && m.active != "":
		return m.handleBrowserKey(msg)
	case m.scrolling && m.focused && m.active != "":
		return m.handleScrollbackKey(msg)
	case m.focused && m.active != "":
		return m.handleShellKey(msg)
	case m.filtering:
		return m.handleFilterKey(msg)
	}
	return m.handleNavKey(msg)
}

// ---- navigation ----

func (m *model) handleNavKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	key := msg.String()

	// The motions first, resolved through the shared keymap — which is also what
	// drops the vim keys when the setting is off, so this switch never learns they
	// exist. Scope List asks for the list's share of that keyboard: the step and page
	// keys, without the jumps and ctrl chords the browser keeps. What is left below is
	// the list's own keyboard: the commands.
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
		m.help = true
		m.clearStatus()

	case "ctrl+o":
		// The second half of the chord the pane armed on its way out: ctrl+o ctrl+o
		// opens VS Code Remote on the directory the shell you just left was standing in.
		// A ctrl+o that arrives on its own — nothing was left, or the window has passed
		// — does nothing, which is what it did before.
		if alias, ok := m.vscodeChord(); ok {
			m.openVSCodeAt(alias)
		}

	case "esc":
		// The one way back that is not a motion: esc is the browser's double-tap
		// chord, so the keymap leaves it to the mode that owns it.
		m.leaveDetails()

	case "S":
		// Another shell on the same host, alongside the ones already open.
		h, ok := m.selectedHost()
		if !ok {
			return m, nil
		}
		return m, m.openShell(h, true)

	case "s":
		h, ok := m.selectedHost()
		if !ok {
			return m, nil
		}
		s, live := m.sessions[h.Alias]
		switch {
		case live && s.dead:
			// The shell is there to focus, but nothing is on the other end of it. What
			// "focus the shell" means on a dropped session is: get the shell back.
			return m, m.reconnect(h)
		case live && s.shell() != nil:
			m.focusShell(h.Alias)
			return m, nil
		}
		m.setStatus(statusWarn, "no live session for %s", h.Alias)

	case "r":
		// Reconnect a session whose connection dropped, putting back the shells and the
		// browser it was holding. It is bound in the list as well as in the dead pane
		// itself, because a drop you notice by the red dot in the sidebar is as likely
		// as one you notice by the pane going still.
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
		// In the directory the host's shell is standing in, when it has one — the
		// list is where you land after ctrl+o, so 'o' here still means "open what I
		// was just looking at".
		m.openVSCodeAt(h.Alias)

	case "d":
		h, ok := m.selectedHost()
		if !ok {
			return m, nil
		}
		m.disconnect(h.Alias)

	case "a":
		// Add a host from scratch — the in-app twin of `hop add`, so a new server
		// does not send you back to the command line.
		m.openHostFormAdd()

	case "i":
		// Import (or re-import) an OpenSSH config — the in-app twin of `hop import`.
		// It is an upsert per host, so it is as much "sync my ssh config" as it is a
		// first-run step, which is why it stays bound once the list is full.
		m.openImport(false)

	case "e":
		// Edit the host under the cursor: the same form, pre-filled.
		h, ok := m.selectedHost()
		if !ok {
			return m, nil
		}
		m.openHostFormEdit(h)

	case "p":
		// Pin the host under the cursor to the PINNED section at the top of the list,
		// or take it back out.
		m.togglePin()

	case "K":
		// Move a pinned host up its section, and "J" down. Shifted j/k rather than a
		// ctrl or alt chord: they are the step keys with "and take the host with you"
		// on them, and they arrive intact on a default macOS terminal, which alt
		// chords do not.
		m.movePin(-1)

	case "J":
		m.movePin(1)

	case "x":
		// Delete the host under the cursor — behind a confirmation, since there is no
		// undo. 'x' rather than a second life for 'd', which already disconnects.
		h, ok := m.selectedHost()
		if !ok {
			return m, nil
		}
		m.openConfirmDelete(h)
	}

	return m, nil
}

// move applies a motion to the host list.
//
// Only the motions Scope List binds can arrive here: step, page and in/out. The
// jumps (gg/G/H/M/L) and the ctrl chords are the browser's — the list does not
// scroll, every host being on screen, so they landed a keypress or two from where
// j and k already were, and the letters are worth more to the list as commands.
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
		// Descend into the host under the cursor: connect to it, which is what the
		// browser's In does to a directory.
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
// space, where the section headings are rows too, because that is the space the
// screen is measured in: paging by a count of *hosts* would step over the headings
// as well and land a host or two past what was ever on screen. A page that ends on
// a heading gives the row back toward where the cursor came from.
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
	m.browsing = false
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
		// Keep the filter, hand the keyboard back to the list — so the very next
		// key can be the enter that connects to the one host still on screen.
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

// handleBrowserKey forwards everything to the file browser except the exits and
// the settings key. The browser itself never asks to be dismissed, so arrows stay
// pure motion.
func (m *model) handleBrowserKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch key := msg.String(); key {
	case "ctrl+o":
		m.leaveBrowser()
		return m, nil

	case ",":
		// Settings are reachable from the browser too — it is where the editor and
		// download settings are felt, so it is where you notice you want to change
		// them. The browser binds no ",".
		m.openSettings()
		return m, nil

	case "esc":
		// Unlike the focused pane, nothing downstream wants an esc: the browser
		// ignores it. So swallow the first one and only arm the window.
		if m.escChord() {
			m.leaveBrowser()
		}
		return m, nil

	default:
		// Any other key breaks the sequence, so esc-j-esc is not a double.
		m.lastEsc = time.Time{}
	}

	if s := m.sessions[m.active]; s != nil && s.browser != nil {
		return m, s.browser.Handle(msg)
	}
	return m, nil
}

// ---- shell pane ----

// handleShellKey routes a key while a shell pane is focused. The remote shell owns
// nearly every key, so hop reserves only ctrl+o, a double esc, alt+0 to open another
// shell, alt+←/→ and alt+1..9 to switch between the ones already open, and ctrl+b for
// the sidebar (taken before this, in handleKey). Everything
// else is forwarded verbatim — including ←, which the shell needs for readline (and
// for the alt+b/alt+f word motions built on it) and full-screen programs need for
// navigation. Leaving the pane is ctrl+o or a double esc.
//
// The alt chords are deliberately fewer than the editor's: readline binds the
// alt+letters (alt+l downcases a word, alt+b walks one back), so alt+h/alt+l are
// not taken here the way they are in an editor. The digits are the exception hop
// already makes — which is why "another shell" is alt+0 and not alt+n: it costs
// the shell nothing that alt+1..9 has not already cost it.
func (m *model) handleShellKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	s := m.sessions[m.active]
	key := msg.String()

	switch key {
	case "ctrl+o":
		// Leave the pane, and arm the second half of the "vs code here" chord: another
		// ctrl+o now opens VS Code on the directory this shell is in. It is a chord
		// rather than a key of its own because the shell owns every plain key, and the
		// alt namespace in here is tab selection — see handleNavKey for the other half.
		alias := m.active
		m.leavePane()
		m.armVSCodeChord(alias)
		return m, nil

	case "alt+0":
		// Another shell on the host you are already in — S in the host list, without
		// going back to the list for it. It is a second channel on the connection hop
		// holds, so there is no handshake, and it lands focused (see shellLanded).
		h, ok := m.hostByAlias(m.active)
		if !ok {
			return m, nil
		}
		return m, m.openShell(h, true)

	case "alt+right":
		if s != nil {
			s.activeSh = cycle(s.activeSh, 1, len(s.shells))
		}
		return m, nil

	case "alt+left":
		if s != nil {
			s.activeSh = cycle(s.activeSh, -1, len(s.shells))
		}
		return m, nil

	case "shift+up":
		// Enter scrollback and step one line up. When there is nothing scrolled off
		// (or a full-screen program owns the screen), enterScrollback declines and we
		// do not return — the key falls through to the shell like any other.
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
		// A second esc inside the window leaves the pane. The *first* esc is
		// still forwarded below, because a lone esc belongs to the shell
		// (it drops vim out of insert mode) and we cannot know a second one
		// is coming without swallowing it. A stray extra esc is harmless:
		// in vim's normal mode it is a no-op.
		if m.escChord() {
			m.leavePane()
			return m, nil
		}
	} else {
		// Any other key breaks the sequence, so esc-j-esc is not a double.
		m.lastEsc = time.Time{}
	}

	if s != nil && s.shell() != nil {
		s.shell().pane.SendKey(msg)
	}
	return m, nil
}

// ---- scrollback ----

// enterScrollback puts the focused shell pane into scrollback mode, or reports
// that there is nothing to scroll and leaves the mode untouched. It is the guard
// the entry chords lean on: they only commit — and only swallow the key — when it
// returns true.
//
// A full-screen program (vim/htop/less) owns its own scrolling and keeps no
// scrollback here; and with nothing scrolled off there is nothing to show. In
// either case the key is better spent on the shell.
func (m *model) enterScrollback(s *session) bool {
	if s == nil || s.shell() == nil {
		return false
	}
	p := s.shell().pane
	if p.AltScreen() || p.ScrollbackLen() == 0 {
		return false
	}
	m.scrolling = true
	m.clearStatus()
	return true
}

// handleScrollbackKey drives the history viewport while a shell pane is scrolled
// back. The motion keys move within the scrollback; the ways out (and any key the
// viewport has no use for) snap back to the live bottom and hand the keyboard back
// to the shell. Reaching the bottom by scrolling is itself a way out: the point of
// scrollback is to look at what went by, so arriving at the live tail means you are
// done looking.
func (m *model) handleScrollbackKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	s := m.sessions[m.active]
	if s == nil || s.shell() == nil {
		// The session went away under us; there is nothing left to scroll.
		m.scrolling = false
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
		// No ctrl+b partner for the ctrl+f below: it is the sidebar toggle in every
		// mode, and handleKey takes it before scrollback ever sees it.
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

	case "q", "esc", "enter", "i", "ctrl+o", "left", "right":
		// The deliberate ways out. ctrl+o here only leaves scrollback, back to the
		// live shell — a second ctrl+o then leaves the pane, the consistent "back one
		// level" the rest of hop keeps to. None of these reach the shell.
		m.exitScrollback()

	default:
		// Any other key — a letter, most likely — means you are done reading and want
		// to type: leave history and forward the key so it starts the line.
		m.exitScrollback()
		if s := m.sessions[m.active]; s != nil && s.shell() != nil {
			s.shell().pane.SendKey(msg)
		}
	}
	return m, nil
}

// scrollPage / scrollHalf are the page and half-page steps for scrollback mode,
// tracking the pane height so they move by roughly what you can see.
func (m *model) scrollPage() int { return max(m.paneH-1, 1) }
func (m *model) scrollHalf() int { return max(m.paneH/2, 1) }

// exitScrollback returns from scrollback mode to the live shell, snapping the
// viewport back to the bottom so the next entry starts fresh at the prompt.
func (m *model) exitScrollback() {
	m.scrolling = false
	if s := m.sessions[m.active]; s != nil && s.shell() != nil {
		s.shell().pane.ScrollToBottom()
	}
}

// ---- editor pane ----

// handleEditorKey routes a key while an editor tab is shown. The editor is a
// full-screen terminal program, so it owns nearly every key — hop reserves only
// ctrl+o, a double esc, and the alt chords that switch tabs. Alt is free to take
// because neither vim nor nano binds it.
func (m *model) handleEditorKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	s := m.sessions[m.active]
	if s == nil || s.editor() == nil {
		// Every tab closed while we were in editing mode; there is nothing to show.
		m.editing = false
		return m, nil
	}

	key := msg.String()

	switch key {
	case "ctrl+o":
		m.leaveEditor()
		return m, nil

	case "alt+right", "alt+l":
		s.activeEd = cycle(s.activeEd, 1, len(s.editors))
		return m, nil

	case "alt+left", "alt+h":
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
		// As in a shell pane, the first esc is still forwarded — it belongs to the
		// editor, and we cannot know a second one is coming without swallowing it.
		if m.escChord() {
			m.leaveEditor()
			return m, nil
		}
	} else {
		m.lastEsc = time.Time{}
	}

	s.editor().pane.SendKey(msg)
	return m, nil
}

// ---- help card ----

// handleHelpKey keeps the help card modal: it swallows every key, and any of the
// usual ways out closes it.
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
	if !m.lastEsc.IsZero() && time.Since(m.lastEsc) <= doubleEscWindow {
		return true
	}
	m.lastEsc = time.Now()
	return false
}

// leavePane returns from a focused terminal pane to navigation mode.
func (m *model) leavePane() {
	m.exitScrollback() // resets scrolling + the pane's offset while m.active is still set
	m.focused = false
	m.clearStatus()
	m.lastEsc = time.Time{}
}

// armVSCodeChord remembers that a shell pane was just left with ctrl+o, so a second
// ctrl+o — in the list, where the first one lands — opens VS Code on the directory
// that shell was standing in. See handleNavKey.
func (m *model) armVSCodeChord(alias string) {
	m.leftPane, m.leftPaneAlias = time.Now(), alias
}

// vscodeChord reports whether this ctrl+o completes the chord: it is the second one,
// inside the window, and the shell it was armed for is still there to be asked. The
// arm is spent either way, so a third press is not a third open.
func (m *model) vscodeChord() (string, bool) {
	alias := m.leftPaneAlias
	armed := !m.leftPane.IsZero() && time.Since(m.leftPane) <= doubleEscWindow
	m.leftPane, m.leftPaneAlias = time.Time{}, ""
	return alias, armed && alias != ""
}

// leaveBrowser returns from the file browser to navigation mode.
func (m *model) leaveBrowser() {
	m.browsing = false
	m.clearStatus()
	m.lastEsc = time.Time{}
}

// leaveEditor returns from the editor tabs to the browser the file was opened
// from (or to navigation, when that browser is gone). The editors keep running:
// the tabs are still open when you come back, cursor where you left it. Closing
// one is the editor's own business — quit it and its tab goes with it.
func (m *model) leaveEditor() {
	m.editing = false
	m.clearStatus()
	m.lastEsc = time.Time{}
	if s := m.sessions[m.active]; s != nil && s.browser != nil {
		m.browsing = true
	}
}

// leaveAll drops every pane mode, handing the keyboard back to the host list.
func (m *model) leaveAll() {
	m.active = ""
	m.focused = false
	m.browsing = false
	m.editing = false
	m.scrolling = false
}

// ---- key helpers ----

// cycle moves i by delta among n tabs, wrapping at both ends. Fewer than two tabs
// means there is nowhere to go.
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
