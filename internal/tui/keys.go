package tui

import (
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"hop/internal/action"
	"hop/internal/keymap"
)

// doubleEscWindow is how long after an esc a second esc counts as "leave the
// pane" rather than two independent escapes bound for the remote shell. Long
// enough for a deliberate double-tap, short enough that two considered presses
// (say, in vim) stay independent.
const doubleEscWindow = 400 * time.Millisecond

// handleKey routes a key to whichever mode currently owns the keyboard. The order
// is the order of modality: the modal cards take everything, then the panes that
// forward to a remote program, then the filter, then plain navigation.
func (m *model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case m.help:
		return m.handleHelpKey(msg)
	case m.confirm.open:
		return m.handleConfirmKey(msg)
	case m.hostForm.open:
		return m.handleHostFormKey(msg)
	case m.settings.open:
		return m.handleSettingsKey(msg)
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
	// exist. What is left below is the list's own keyboard: the commands.
	if mo := m.keys.Motion(key, m.cfg.VimKeys); mo != keymap.None {
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
		if s, live := m.sessions[h.Alias]; live && s.shell() != nil {
			m.focusShell(h.Alias)
			return m, nil
		}
		m.setStatus(statusWarn, "no live session for %s", h.Alias)

	case "f":
		h, ok := m.selectedHost()
		if !ok {
			return m, nil
		}
		return m, m.openBrowser(h)

	case "o":
		h, ok := m.selectedHost()
		if !ok {
			return m, nil
		}
		if err := action.OpenVSCodeRemote(h.Alias, ""); err != nil {
			m.setStatus(statusErr, "vscode: %v", err)
		} else {
			m.setStatus(statusOK, "opening VS Code remote → %s", h.Alias)
		}

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

	case "e":
		// Edit the host under the cursor: the same form, pre-filled.
		h, ok := m.selectedHost()
		if !ok {
			return m, nil
		}
		m.openHostFormEdit(h)

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
// The list does not scroll — every host is on screen — so the screen-relative
// motions land on the same rows as the absolute ones. They are honoured anyway,
// rather than dropped, so that H/M/L mean in the list what they mean in the browser
// and a habit formed in one still works in the other.
func (m *model) move(mo keymap.Motion) (tea.Model, tea.Cmd) {
	switch mo {
	case keymap.Up:
		m.cursor--
	case keymap.Down:
		m.cursor++
	case keymap.Top, keymap.ScreenTop:
		m.cursor = 0
	case keymap.Bottom, keymap.ScreenBot:
		m.cursor = len(m.filtered) - 1
	case keymap.ScreenMid:
		m.cursor = len(m.filtered) / 2
	case keymap.HalfDown:
		m.cursor += m.halfPage()
	case keymap.HalfUp:
		m.cursor -= m.halfPage()
	case keymap.PageDown:
		m.cursor += m.listRows()
	case keymap.PageUp:
		m.cursor -= m.listRows()

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
// shell, alt+←/→ and alt+1..9 to switch between the ones already open — and ←, but
// only where the shell has no use for it (see the left case). Everything else is
// forwarded verbatim.
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
		m.leavePane()
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

	case "left":
		// Back to the host list — the same key that backs out of a host in the list
		// and out of a directory in the browser, so "left is back" holds everywhere
		// hop is the one listening. Where the shell wants the key it does not get
		// taken, and this falls through to forward it like any other.
		if s != nil && backsOut(s.shell()) {
			m.leavePane()
			return m, nil
		}

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

// backsOut reports whether ← is hop's to take from this shell rather than the
// shell's to keep: it is hop's only where the shell has nothing for it to do — a bare
// input line, and no full-screen program on the alt screen.
//
// At a bare prompt ← is already a no-op: there is nothing to the left of the prompt
// for the cursor to move over, so taking the key costs the shell nothing. Over a
// half-typed command it moves the readline cursor, and inside vim or htop it belongs
// to the program — in both, it stays the shell's.
//
// The footer asks this too, on every frame, so what it offers is what the very next
// ← will actually do.
func backsOut(sh *shellTab) bool {
	return sh != nil && sh.pane.LineEmpty() && !sh.pane.AltScreen()
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

	case "pgup", "ctrl+b", "shift+pgup":
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
