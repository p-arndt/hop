package tui

import (
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"hop/internal/terminal"
)

// Mouse support, under one rule: the pointer never does anything the keyboard cannot.
// Every gesture is an existing binding reached by pointing — a click in the sidebar is
// ctrl+o, a double-click on a host is enter, the wheel over a shell is shift+↑ — so a
// terminal with mouse reports off loses no capability.
//
// The pointer is forwarded rather than translated only to a remote program that asked
// for it: see terminal.Pane.MouseEnabled. The cards are keyboard-only, since a click
// falling through onto the list behind is the trap handleKey's ordering prevents.

// doubleClickWindow is how long after a click a second one on the same row counts as
// "open this" rather than two independent clicks — the same window the double-esc uses.
const doubleClickWindow = 400 * time.Millisecond

// wheelStep is how many lines one notch of the wheel moves a view that scrolls: the
// customary three. The host list is the exception — it does not scroll, so there the
// wheel steps the selection, one host at a time.
const wheelStep = 3

// zone is the part of the screen a mouse event landed in — the whole of hop's
// hit-testing, since the layout is a header, two side-by-side boxes and a footer.
type zone int

const (
	zoneNone zone = iota
	zoneHeader
	zoneList
	zonePane
	zoneFooter
)

// zoneAt names the region containing screen cell (x, y), from the same layout
// arithmetic View composes with. The sidebar's outer edge is 0 while it is collapsed,
// so the pane then owns the whole width.
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

// handleMouse routes a mouse event to whichever region it landed in, in handleKey's
// order of modality: a card swallows everything, then the two boxes of the body.
func (m *model) handleMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	model, cmd := m.routeMouse(msg)

	// A button that came up ends the drag it started, wherever it came up. A gesture
	// that ran off the edge of the pane never reaches mouseSelect, and a drag left live
	// would make the next release finish a gesture nobody was making.
	if msg.Action == tea.MouseActionRelease && m.sel.dragging {
		m.endSelection(m.dragView())
	}
	return model, cmd
}

// routeMouse hands the event to whichever region it landed in.
func (m *model) routeMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
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

// dragView is what a drag in progress is selecting out of: the focused shell's live
// screen, or its history while the pane is paused in it. An empty view is a session
// that has gone since the button went down, which copies nothing.
func (m *model) dragView() string {
	s := m.sessions[m.active]
	if s == nil || s.shell() == nil {
		return ""
	}
	p := s.shell().pane
	if m.scrolling() {
		return p.ViewScrollback()
	}
	return p.View()
}

// clickChord reports whether this click completes a double-click on the same thing in
// the same region, arming the window when it does not. A double is spent once claimed.
//
// id is what was clicked, not the screen row: both lists re-scroll around the cursor the
// first click just moved, so keying on the row would open something nobody pointed at.
func (m *model) clickChord(z zone, id int) bool {
	double := !m.chords.click.IsZero() && z == m.chords.clickZone && id == m.chords.clickID &&
		time.Since(m.chords.click) <= doubleClickWindow
	if double {
		m.chords.click = time.Time{}
		return true
	}
	m.chords.click, m.chords.clickZone, m.chords.clickID = time.Now(), z, id
	return false
}

// ---- the host list ----

// mouseList is the sidebar's share of the pointer: the wheel steps the selection, a
// click stands on a host, a second click connects.
func (m *model) mouseList(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	switch msg.Button {
	case tea.MouseButtonWheelUp:
		m.clearSelection()
		m.cursor--
		m.clampCursor()

	case tea.MouseButtonWheelDown:
		m.clearSelection()
		m.cursor++
		m.clampCursor()

	case tea.MouseButtonLeft:
		// The press, not the release: a click acts where it lands.
		if msg.Action != tea.MouseActionPress {
			return m, nil
		}
		return m.clickList(msg)
	}
	return m, nil
}

// clickList stands the cursor on the host that was clicked, and connects on a
// double-click. A click in the sidebar is also a click away from whatever pane holds
// the keyboard, so it hands the keyboard back first.
func (m *model) clickList(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	m.clearSelection()
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

// listRowAt maps a screen row to an index into m.filtered, or false when the row holds
// no host. It runs renderList's bookkeeping backwards, then applies renderRows' scroll
// window.
func (m *model) listRowAt(y int) (int, bool) {
	// The screen header, the sidebar's top border, then its heading.
	first := 2 + m.listTitleRows()
	if m.filtering || m.filter != "" {
		first++
	}
	rows := m.listRows()
	if y < first || y >= first+rows {
		return 0, false
	}
	i := m.listStart(rows) + (y - first)
	if i < 0 || i >= len(m.rows) {
		return 0, false
	}
	if m.rows[i].heading != "" {
		return 0, false
	}
	return m.rows[i].fi, true
}

// backToList hands the keyboard back to the host list — what ctrl+o does from a shell.
// The active session is kept, so its pane stays on screen; only the keyboard moves.
func (m *model) backToList() {
	if m.listHasFocus() {
		return
	}
	m.exitScrollback()
	m.mode = modeList
	m.clearStatus()
	m.chords.esc = time.Time{}
}

// ---- the right pane ----

// mousePane is the pointer over whatever the active session is showing. A pane nothing
// has the keyboard in takes it on a click; past that, each pane mode answers for itself.
func (m *model) mousePane(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	s := m.sessions[m.active]
	// A dropped session's pane is a picture of a shell: it answers r, d and ctrl+o, and
	// nothing that would look like driving the far end.
	if m.active == "" || s == nil || s.dead {
		return m, nil
	}

	x, y, ok := m.paneLocal(msg.X, msg.Y)
	if !ok {
		return m, nil
	}

	if m.listHasFocus() {
		// The pane is on screen but the list has the keyboard. A click is the way in.
		if msg.Button == tea.MouseButtonLeft && msg.Action == tea.MouseActionPress {
			m.clickIntoPane(s)
		}
		return m, nil
	}

	switch {
	case m.editing() && s.editor() != nil:
		return m.mouseEditor(s, msg, x, y)
	case m.browsing() && s.browser != nil:
		return m.mouseBrowser(s, msg, x, y)
	case m.focused() && s.shell() != nil:
		return m.mouseShell(s, msg, x, y)
	}
	return m, nil
}

// paneLocal maps a screen cell to one inside the right pane's content box, or reports
// false for a cell outside it. (0, 0) is the tab strip's first column when there is a
// strip, and otherwise the emulated screen's origin.
func (m *model) paneLocal(x, y int) (int, int, bool) {
	// The sidebar's outer width and the pane's left border; the screen header and the
	// pane's top border.
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
		m.mode = modeBrowser
	}
}

// mouseShell is the pointer over a focused shell pane. The tab strip answers a click on
// it; below that, a remote program that asked for the mouse gets the event verbatim, and
// one that did not leaves the wheel to hop's scrollback.
func (m *model) mouseShell(s *session, msg tea.MouseMsg, x, y int) (tea.Model, tea.Cmd) {
	if len(s.shells) > 1 {
		if y == 0 {
			if msg.Button == tea.MouseButtonLeft && msg.Action == tea.MouseActionPress {
				if i, ok := m.tabAt(shellTabNames(s), s.activeSh, x); ok {
					m.clearSelection()
					s.activeSh = i
				}
			}
			return m, nil
		}
		y--
	}
	p := s.shell().pane

	// Paused in history, the wheel drives the history whatever the far end asked for: it
	// is not being shown the live screen, so it is not being pointed at.
	if m.scrolling() {
		switch msg.Button {
		case tea.MouseButtonWheelUp:
			m.clearSelection()
			p.ScrollUp(wheelStep)
			return m, nil
		case tea.MouseButtonWheelDown:
			m.clearSelection()
			p.ScrollDown(wheelStep)
			if p.AtBottom() {
				m.exitScrollback()
			}
			return m, nil
		}
		// Anything else over the history is a drag over text that is not going anywhere.
		return m.mouseSelect(msg, x, y, p.ViewScrollback())
	}

	// A remote program that asked for the mouse keeps it, selection included: it has its
	// own, and two selections for one drag is worse than either.
	if p.MouseEnabled() {
		p.SendMouse(msg, x, y)
		return m, nil
	}

	// Nothing asked for the mouse, so the wheel is hop's: into the shell's scrollback, on
	// the same terms as the entry chord.
	if msg.Button == tea.MouseButtonWheelUp {
		m.clearSelection()
		if m.enterScrollback(s) {
			p.ScrollUp(wheelStep)
		}
		return m, nil
	}
	if msg.Button == tea.MouseButtonWheelDown {
		return m, nil
	}
	return m.mouseSelect(msg, x, y, p.View())
}

// mouseSelect is the pointer over a pane's text with nothing else claiming it: press
// anchors a selection, motion drags it, release copies it. view is what the pane is
// showing, and is what the copy is read out of.
func (m *model) mouseSelect(msg tea.MouseMsg, x, y int, view string) (tea.Model, tea.Cmd) {
	c := terminal.Cell{X: x, Y: y}
	// A release ends the drag whatever button it names: not every terminal says which
	// one came up, and a drag that never ends is a highlight that never copies.
	if msg.Action == tea.MouseActionRelease && m.sel.dragging {
		m.dragSelection(c)
		m.endSelection(view)
		return m, nil
	}
	if msg.Button != tea.MouseButtonLeft {
		return m, nil
	}
	switch msg.Action {
	case tea.MouseActionPress:
		m.startSelection(c)
	case tea.MouseActionMotion:
		m.dragSelection(c)
	}
	return m, nil
}

// mouseEditor is the pointer over an editor tab: the strip switches tabs, and the editor
// gets the event when it has asked for the mouse. hop keeps no history for it, so one
// that has not asked is not scrolled.
func (m *model) mouseEditor(s *session, msg tea.MouseMsg, x, y int) (tea.Model, tea.Cmd) {
	if y == 0 {
		if msg.Button == tea.MouseButtonLeft && msg.Action == tea.MouseActionPress {
			if i, ok := m.tabAt(editorTabNames(s), s.activeEd, x); ok {
				m.clearSelection()
				s.activeEd = i
			}
		}
		return m, nil
	}
	p := s.editor().pane
	if p.MouseEnabled() {
		p.SendMouse(msg, x, y-1)
		return m, nil
	}
	// An editor that has not asked is a screen full of text like any other.
	return m.mouseSelect(msg, x, y-1, p.View())
}

// mouseBrowser is the pointer over the SFTP listing: the wheel moves the cursor, a
// click stands on an entry, a second click opens it.
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
			// A file yields an OpenFileMsg the model answers, as enter does; a directory
			// is loaded in place and the command is nil.
			return m, b.Activate()
		}
	}
	return m, nil
}
