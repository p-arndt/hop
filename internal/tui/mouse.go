package tui

import (
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// Mouse support, and the one rule behind all of it: the pointer never does
// anything the keyboard cannot. Every gesture below is an existing binding reached
// by pointing at the thing instead of naming it — a click in the sidebar is ctrl+o,
// a double-click on a host is enter, the wheel over a shell is shift+↑, the wheel
// over the browser is j/k. Nothing is mouse-only, so a session over a link that
// eats mouse reports (or a terminal with them switched off) loses no capability.
//
// The one place the pointer is *forwarded* rather than translated is a remote
// program that has asked for it — vim with `set mouse=a`, htop, less. hop honours
// that ask the same way it honours a key: see terminal.Pane.MouseEnabled.
//
// The cards are deliberately keyboard-only. They are modal and they are small, the
// keys are named along their foot, and a click that fell through one onto the list
// behind it would be exactly the trap handleKey's ordering exists to prevent.

// doubleClickWindow is how long after a click a second one on the same row counts
// as "open this" rather than two independent clicks. It is the same window the
// double-esc and the vs-code chord use, for the same reason: long enough to be a
// deliberate double-tap, short enough that two considered clicks stay separate.
const doubleClickWindow = 400 * time.Millisecond

// wheelStep is how many lines one notch of the wheel moves a view that scrolls —
// a shell's history, the browser's listing. It is the customary three, so a gesture
// covers ground at the rate every other program has taught the hand to expect.
//
// The host list is the exception: it does not scroll (every host is on screen), so
// there the wheel steps the *selection*, and it steps it one host at a time —
// three-at-a-time on a nine-host list is a cursor that jumps rather than moves.
const wheelStep = 3

// zone is the part of the screen a mouse event landed in. It is the whole of hop's
// hit-testing: the layout is a header, two side-by-side boxes and a footer, and
// which box was pointed at decides who the event belongs to.
type zone int

const (
	zoneNone zone = iota
	zoneHeader
	zoneList
	zonePane
	zoneFooter
)

// zoneAt names the region containing screen cell (x, y). It derives from the same
// layout arithmetic View composes with: the header owns the first row, the footer
// the row after the body, and the body is split at the sidebar's outer edge — which
// is 0 while the sidebar is collapsed, so the pane then owns the whole width.
func (m *model) zoneAt(x, y int) zone {
	if x < 0 || y < 0 || x >= m.width || y >= m.height {
		return zoneNone
	}
	switch {
	case y == 0:
		return zoneHeader
	case y >= 1+m.bodyHeight():
		return zoneFooter
	}
	if w := m.listWidth(); w > 0 && x < w {
		return zoneList
	}
	return zonePane
}

// handleMouse routes a mouse event to whichever region it landed in, in the same
// order of modality handleKey uses: a card takes everything (by swallowing it), then
// the two boxes of the body.
func (m *model) handleMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	if m.modalCard() != "" {
		return m, nil
	}
	switch m.zoneAt(msg.X, msg.Y) {
	case zoneList:
		return m.mouseList(msg)
	case zonePane:
		return m.mousePane(msg)
	}
	return m, nil
}

// clickChord reports whether this click completes a double-click on the same thing
// in the same region, and arms the window when it does not. A double is spent once
// claimed, so a third click in a fast sequence starts a fresh pair rather than
// opening a second time.
//
// id identifies what was clicked — the host's index in the filtered list, the
// browser entry's index in the listing — and deliberately not the screen row it was
// drawn on. Both of those lists re-scroll around the cursor a click has just moved,
// so the row under the pointer can hold a different thing by the time the second
// click arrives; keying on the row would then open something nobody pointed at.
func (m *model) clickChord(z zone, id int) bool {
	double := !m.lastClick.IsZero() && z == m.lastClickZone && id == m.lastClickID &&
		time.Since(m.lastClick) <= doubleClickWindow
	if double {
		m.lastClick = time.Time{}
		return true
	}
	m.lastClick, m.lastClickZone, m.lastClickID = time.Now(), z, id
	return false
}

// ---- the host list ----

// mouseList is the sidebar's share of the pointer: the wheel steps the selection,
// a click stands on a host, and a second click connects to it.
func (m *model) mouseList(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	switch msg.Button {
	case tea.MouseButtonWheelUp:
		m.cursor--
		m.clampCursor()

	case tea.MouseButtonWheelDown:
		m.cursor++
		m.clampCursor()

	case tea.MouseButtonLeft:
		// The press, not the release: a click acts where it lands, and waiting for
		// the release would put the action a gesture behind the finger.
		if msg.Action != tea.MouseActionPress {
			return m, nil
		}
		return m.clickList(msg)
	}
	return m, nil
}

// clickList stands the cursor on the host that was clicked, and connects to it on a
// double-click — enter, by pointing. A click in the sidebar is also a click *away*
// from whatever pane holds the keyboard, so it hands the keyboard back first: the
// list you just pointed at is the thing that should answer the next key.
func (m *model) clickList(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	m.backToList()

	i, ok := m.listRowAt(msg.Y)
	if !ok {
		return m, nil
	}
	double := m.clickChord(zoneList, i)
	m.cursor = i
	if !double {
		return m, nil
	}
	h, ok := m.selectedHost()
	if !ok {
		return m, nil
	}
	return m, m.openShell(h, false)
}

// listRowAt maps a screen row to an index into m.filtered, or false when the row
// holds no host: the sidebar's border, its HOSTS heading, the filter prompt, or the
// blank space under a short list.
//
// It runs renderList's bookkeeping backwards — the header row, the box's top
// border, the heading, and the filter prompt when there is one — and then the same
// scroll window renderRows draws with, so the host that answers is the host under
// the pointer.
func (m *model) listRowAt(y int) (int, bool) {
	// The screen's header row, then the sidebar's top border, then its heading.
	first := 3
	if m.filtering || m.filter != "" {
		first++
	}
	rows := m.listRows()
	if y < first || y >= first+rows {
		return 0, false
	}
	i := m.listStart(rows) + (y - first)
	if i < 0 || i >= len(m.filtered) {
		return 0, false
	}
	return i, true
}

// backToList hands the keyboard back to the host list from whichever pane holds it
// — what ctrl+o does from a shell, and what pointing at the sidebar means. The
// active session is kept, so its pane stays on screen behind the list's cursor;
// only the keyboard moves.
func (m *model) backToList() {
	if m.listHasFocus() {
		return
	}
	m.exitScrollback()
	m.focused, m.browsing, m.editing = false, false, false
	m.clearStatus()
	m.lastEsc = time.Time{}
}

// ---- the right pane ----

// mousePane is the pointer over whatever the active session is showing. A pane
// nothing has the keyboard in takes it on a click; past that, each of the three
// pane modes answers for itself.
func (m *model) mousePane(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	s := m.sessions[m.active]
	// A dropped session's pane is a picture of a shell, not a shell: it answers r,
	// d and ctrl+o, and nothing that would look like driving the far end.
	if m.active == "" || s == nil || s.dead {
		return m, nil
	}

	x, y, ok := m.paneLocal(msg.X, msg.Y)
	if !ok {
		return m, nil
	}

	if m.listHasFocus() {
		// The pane is on screen but the list has the keyboard (you came back with
		// ctrl+o). A click is the way in — the pointer's s, or f.
		if msg.Button == tea.MouseButtonLeft && msg.Action == tea.MouseActionPress {
			m.clickIntoPane(s)
		}
		return m, nil
	}

	switch {
	case m.editing && s.editor() != nil:
		return m.mouseEditor(s, msg, x, y)
	case m.browsing && s.browser != nil:
		return m.mouseBrowser(s, msg, x, y)
	case m.focused && s.shell() != nil:
		return m.mouseShell(s, msg, x, y)
	}
	return m, nil
}

// paneLocal maps a screen cell to one inside the right pane's content box, or
// reports false for a cell outside it. (0, 0) is the top-left cell of the content —
// the tab strip's first column when there is a strip, and otherwise the emulated
// screen's own origin.
func (m *model) paneLocal(x, y int) (int, int, bool) {
	// The sidebar's outer width, then the pane's own left border; the screen header,
	// then the pane's top border.
	lx, ly := x-m.listWidth()-1, y-2
	if lx < 0 || ly < 0 || lx >= m.paneW || ly >= m.paneH {
		return 0, 0, false
	}
	return lx, ly, true
}

// clickIntoPane gives the keyboard to what the pane is showing: its shell, or the
// browser on a session that has no shell of its own.
func (m *model) clickIntoPane(s *session) {
	switch {
	case s.shell() != nil:
		m.focusShell(m.active)
	case s.browser != nil:
		m.browsing = true
		m.focused = false
		m.editing = false
	}
}

// mouseShell is the pointer over a focused shell pane. The tab strip is hop's own
// row and answers a click on it; below that, a remote program that asked for the
// mouse gets the event verbatim, and one that did not leaves the wheel to hop's
// scrollback — the pointer's shift+↑.
func (m *model) mouseShell(s *session, msg tea.MouseMsg, x, y int) (tea.Model, tea.Cmd) {
	if len(s.shells) > 1 {
		if y == 0 {
			if msg.Button == tea.MouseButtonLeft && msg.Action == tea.MouseActionPress {
				if i, ok := m.tabAt(shellTabNames(s), s.activeSh, x); ok {
					s.activeSh = i
				}
			}
			return m, nil
		}
		y--
	}
	p := s.shell().pane

	// Paused in history, the wheel drives the history — whatever the far end has
	// asked for. It is not being shown the live screen, so it is not being pointed at.
	if m.scrolling {
		switch msg.Button {
		case tea.MouseButtonWheelUp:
			p.ScrollUp(wheelStep)
		case tea.MouseButtonWheelDown:
			p.ScrollDown(wheelStep)
			if p.AtBottom() {
				m.exitScrollback()
			}
		}
		return m, nil
	}

	if p.MouseEnabled() {
		p.SendMouse(msg, x, y)
		return m, nil
	}

	// Nothing asked for the mouse, so the wheel is hop's: back into the shell's
	// scrollback, on the same terms as the entry chord (nothing to show, or a
	// full-screen program owning the screen, and the gesture is spent doing nothing).
	if msg.Button == tea.MouseButtonWheelUp && m.enterScrollback(s) {
		p.ScrollUp(wheelStep)
	}
	return m, nil
}

// mouseEditor is the pointer over an editor tab: the strip switches tabs, and the
// editor itself gets the event when it has asked for the mouse (vim's `set
// mouse=a`). An editor that has not asked is left alone rather than scrolled by
// hop, which keeps no history for it — the pane is the program's own screen.
func (m *model) mouseEditor(s *session, msg tea.MouseMsg, x, y int) (tea.Model, tea.Cmd) {
	if y == 0 {
		if msg.Button == tea.MouseButtonLeft && msg.Action == tea.MouseActionPress {
			if i, ok := m.tabAt(editorTabNames(s), s.activeEd, x); ok {
				s.activeEd = i
			}
		}
		return m, nil
	}
	s.editor().pane.SendMouse(msg, x, y-1)
	return m, nil
}

// mouseBrowser is the pointer over the SFTP listing: the wheel moves the cursor,
// a click stands on an entry, and a second click opens it — enter, by pointing.
func (m *model) mouseBrowser(s *session, msg tea.MouseMsg, _, y int) (tea.Model, tea.Cmd) {
	b := s.browser
	switch msg.Button {
	case tea.MouseButtonWheelUp:
		b.Scroll(-wheelStep)

	case tea.MouseButtonWheelDown:
		b.Scroll(wheelStep)

	case tea.MouseButtonLeft:
		if msg.Action != tea.MouseActionPress {
			return m, nil
		}
		i, ok := b.RowAt(y)
		if !ok {
			return m, nil
		}
		double := m.clickChord(zonePane, i)
		b.Select(i)
		if double {
			// Opening a file yields an OpenFileMsg the model answers, exactly as enter
			// does; a directory is loaded in place and the command is nil.
			return m, b.Activate()
		}
	}
	return m, nil
}
