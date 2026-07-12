package tui

import (
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"hop/internal/action"
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
	case m.settings.open:
		return m.handleSettingsKey(msg)
	case m.editing && m.active != "":
		return m.handleEditorKey(msg)
	case m.browsing && m.active != "":
		return m.handleBrowserKey(msg)
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

	// Complete or abandon a pending "gg".
	if m.pendingG {
		m.pendingG = false
		if key == "g" {
			m.cursor = 0
			return m, nil
		}
	}

	switch key {
	case "q", "ctrl+c":
		m.closeAll()
		return m, tea.Quit

	case "up", "k":
		m.cursor--
		m.clampCursor()

	case "down", "j":
		m.cursor++
		m.clampCursor()

	case "g":
		m.pendingG = true

	case "G", "L":
		m.cursor = len(m.filtered) - 1
		m.clampCursor()

	case "H":
		m.cursor = 0
		m.clampCursor()

	case "M":
		m.cursor = len(m.filtered) / 2
		m.clampCursor()

	case "ctrl+d":
		m.cursor += m.halfPage()
		m.clampCursor()

	case "ctrl+u":
		m.cursor -= m.halfPage()
		m.clampCursor()

	case "ctrl+f", "pgdown":
		m.cursor += m.listRows()
		m.clampCursor()

	case "ctrl+b", "pgup":
		m.cursor -= m.listRows()
		m.clampCursor()

	case "/":
		m.filtering = true
		m.filter = ""
		m.applyFilter()

	case ",":
		m.openSettings()

	case "?":
		m.help = true
		m.clearStatus()

	case "esc", "left", "h":
		// Back: leave the details/active view, back to plain navigation.
		m.active = ""
		m.browsing = false
		m.clearStatus()

	case "enter", "right", "l":
		// Forward: connect to the selected host, mirroring the browser's
		// enter/right/l "descend into this thing".
		h, ok := m.selectedHost()
		if !ok {
			return m, nil
		}
		return m, m.openShell(h, false)

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
	}

	return m, nil
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

// handleShellKey routes a key while a shell pane is focused. The remote shell
// owns every key, arrows included, so hop reserves only ctrl+o, a double esc, and
// — when the host has more than one shell — alt+←/→ and alt+1..9 to switch
// between them. Everything else is forwarded verbatim.
//
// The alt chords are deliberately fewer than the editor's: readline binds the
// alt+letters (alt+l downcases a word, alt+b walks one back), so alt+h/alt+l are
// not taken here the way they are in an editor.
func (m *model) handleShellKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	s := m.sessions[m.active]
	key := msg.String()

	switch key {
	case "ctrl+o":
		m.leavePane()
		return m, nil

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
