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
// hit-testing, since the layout is a header, three side-by-side columns and a footer.
type zone int

const (
	zoneNone zone = iota
	zoneHeader
	zoneList
	zoneTree
	zonePane
	zoneFooter
)

// zoneAt names the region containing screen cell (x, y), from the same layout arithmetic
// View composes with. A collapsed column's outer width is 0, so the columns to its right
// simply start where it would have: the sidebar hidden gives its cells to the tree, and no
// tree column gives them all to the content area.
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
	switch {
	case m.frame.list.contains(x, y):
		return zoneList
	case m.frame.tree.contains(x, y):
		return zoneTree
	}
	// Everything else on a body row is the content area, including the blank column an
	// odd-width split leaves over: it is inside no box, so contentLocal will decline it,
	// but it is not somewhere else either.
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
	// The one card the wheel reaches: it is the only one long enough to be cut off by a
	// short window, and the wheel is what a pointer reaches for when it is.
	if m.help {
		switch msg.Button {
		case tea.MouseButtonWheelUp:
			m.helpScroll--
		case tea.MouseButtonWheelDown:
			m.helpScroll++
		}
		return m, nil
	}
	if m.capturing() {
		return m, nil
	}
	// A drag that ran off the pane is still that drag: it keeps its events wherever the
	// pointer went, or crossing into the sidebar would clear the selection halfway
	// through making it.
	if m.sel.dragging {
		return m.mousePane(msg)
	}
	switch m.zoneAt(msg.X, msg.Y) {
	case zoneList:
		return m.mouseList(msg)
	case zoneTree:
		return m.mouseTree(msg)
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

	case tea.MouseButtonRight:
		// The pointer's way to the context menu — the gesture every other program in the
		// terminal's window has taught, standing in for the space key exactly.
		if msg.Action != tea.MouseActionPress {
			return m, nil
		}
		return m.rightClickList(msg)
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

// rightClickList opens the context menu on the host that was clicked, standing the
// cursor on it first: the menu belongs to the selected host, so pointing at one and
// opening its menu are one gesture rather than two.
func (m *model) rightClickList(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	m.clearSelection()
	m.backToList()

	i, ok := m.listRowAt(msg.Y)
	if !ok {
		return m, nil
	}
	m.cursor = i
	m.openHostMenu()
	return m, nil
}

// listRowAt maps a screen row to an index into m.filtered, or false when the row holds
// no host. It runs renderList's bookkeeping backwards, then applies renderRows' scroll
// window.
func (m *model) listRowAt(y int) (int, bool) {
	first := m.listFirstRow()
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

// listFirstRow is the screen row the first host row is drawn on: the screen header, the
// sidebar's top border, its heading, and the filter prompt when there is one. Both the
// mouse (which maps a row back to a host) and the context menu (which anchors itself to
// one) need it, so it is one function rather than two copies of renderList's arithmetic.
func (m *model) listFirstRow() int {
	first := 2 + m.listTitleRows()
	if m.filtering || m.filter != "" {
		first++
	}
	return first
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
	m.reader.Reset()
}

// ---- the tree column ----

// mouseTree is the pointer over the SFTP column. A column that does not hold the keyboard
// takes it on a click — the pointer's way across the columns, standing in for tab and
// alt+t — and past that the browser answers for itself as it always has.
func (m *model) mouseTree(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	s := m.sessions[m.active]
	// A dropped session's column is a picture of a listing, on the same terms as its pane.
	if s == nil || s.browser == nil || s.dead {
		return m, nil
	}

	if !m.browsing() {
		// A click is the way in; the wheel is not, or a notch aimed at the file you are
		// reading would move the keyboard out of it.
		if msg.Button != tea.MouseButtonLeft || msg.Action != tea.MouseActionPress {
			return m, nil
		}
		m.clearSelection()
		m.focusTree()
		// And on through: the click that crossed into the column also stands on the row
		// it landed on, which is what clicking a host in the sidebar does.
	}

	x, y, ok := m.treeLocal(msg.X, msg.Y)
	if !ok {
		return m, nil
	}
	return m.mouseBrowser(s, msg, x, y)
}

// treeLocal maps a screen cell to one inside the tree column's content box. The browser's
// RowAt and Select are measured from its own top-left corner, so the column's border and
// the screen header have to come off the coordinate before it is asked anything — which
// is the whole of what "translate per column" means here.
func (m *model) treeLocal(x, y int) (int, int, bool) { return m.frame.tree.inner(x, y) }

// ---- the content area ----

// mousePane is the pointer over whatever the active session is showing in the content
// area. A column nothing has the keyboard in takes it on a click; past that, each mode
// answers for itself.
func (m *model) mousePane(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	s := m.sessions[m.active]
	// A dropped session's pane is a picture of a shell: it answers r, d and ctrl+o, and
	// nothing that would look like driving the far end.
	if m.active == "" || s == nil || s.dead {
		return m, nil
	}

	right, x, y, ok := m.contentLocal(msg.X, msg.Y)
	if !ok {
		// Off the pane with the button down: the drag continues at the edge it left by,
		// which is also what puts it on the edge row autoscroll watches.
		if !m.sel.dragging {
			return m, nil
		}
		right = s.focusedHalf()
		x, y = m.clampToPane(msg.X, msg.Y)
	}

	// The narrow-window fallback: with no column to put it in, the browser is the content
	// area, so the pointer over it is the pointer over the listing. Ahead of the focus
	// handling below, which would otherwise read a click on the listing as a click across
	// into a pane that is not there.
	if m.treeInline() && m.browsing() && s.browser != nil {
		return m.mouseBrowser(s, msg, x, y)
	}

	if m.listHasFocus() || m.browsing() {
		// The content area is on screen but the keyboard is elsewhere — in the host list,
		// or in the tree column beside it. A click is the way in, and it lands in the half
		// it was aimed at.
		if msg.Button == tea.MouseButtonLeft && msg.Action == tea.MouseActionPress {
			m.clickIntoPane(s, right)
		}
		return m, nil
	}

	// A click in the half that does not hold the keyboard moves the keyboard there first.
	// The halves are two panes and the pointer picks between them, exactly as it picks
	// between the columns.
	//
	// contentIsSplit, not splitOn: with one box on screen there is no other half to pick.
	if m.contentIsSplit() && right != s.focusedHalf() &&
		msg.Button == tea.MouseButtonLeft && msg.Action == tea.MouseActionPress {
		m.clearSelection()
		s.splitRight = right
	}

	switch {
	case m.editing() && s.editor() != nil:
		return m.mouseEditor(s, msg, x, y)
	case m.focused() && s.shell() != nil:
		return m.mouseShell(s, msg, x, y)
	}
	return m, nil
}

// contentLocal maps a screen cell into the content area: which half it landed in, and
// where inside that half's content box. (0, 0) is the tab strip's first column when there
// is a strip, and otherwise the emulated screen's origin.
//
// Unsplit there is one half and it is the left one, so the answer nobody has to look at is
// always false. Split, the two boxes sit side by side inside the same columns the one box
// had, and the divider they share belongs to neither.
func (m *model) contentLocal(x, y int) (bool, int, int, bool) {
	if lx, ly, ok := m.frame.left.inner(x, y); ok {
		return false, lx, ly, true
	}
	if lx, ly, ok := m.frame.right.inner(x, y); ok {
		return true, lx, ly, true
	}
	return false, 0, 0, false
}

// paneLocal maps a screen cell to one inside the content area's box, or reports false for
// a cell outside it — contentLocal for the callers that only ever ask about the half the
// keyboard is in.
func (m *model) paneLocal(x, y int) (int, int, bool) {
	_, lx, ly, ok := m.contentLocal(x, y)
	return lx, ly, ok
}

// clampToPane maps a screen cell to the nearest cell inside the focused half's content
// box — paneLocal for a pointer that has left it, which is where a drag off the edge
// lands.
func (m *model) clampToPane(x, y int) (int, int) {
	s := m.sessions[m.active]
	return m.frame.half(s != nil && s.splitRight).clamp(x, y)
}

// clickIntoPane gives the keyboard to what the content area is showing: the editor tab in
// the half that was clicked, the host's shell, or — on a window with no room for a
// column — the browser that is standing in the content area itself.
func (m *model) clickIntoPane(s *session, right bool) {
	// A click that crossed into another column is a click on something else, so whatever
	// double it was half of is spent. Without this, pointing at a file and then back at
	// the tree row you came from would open that row as though the detour never happened.
	m.chords.click = time.Time{}
	switch {
	case s.editorAt(right) != nil:
		// Only when there are two halves to choose between.
		if m.contentIsSplit() {
			s.splitRight = right
		}
		m.mode = modeEditor
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
	h := m.paneH
	if len(s.shells) > 1 {
		// A drag passing over the strip is still a drag: only a click that started
		// nowhere else gets to switch tabs.
		if y == 0 && !m.sel.dragging {
			if msg.Button == tea.MouseButtonLeft && msg.Action == tea.MouseActionPress {
				if i, ok := m.tabAt(shellTabNames(s), s.activeSh, x, m.paneW); ok {
					m.clearSelection()
					s.activeSh = i
				}
			}
			return m, nil
		}
		y--
		h--
	}
	p := s.shell().pane

	// A drag held against the top or bottom row scrolls the view under it, so a
	// selection is not limited to the screenful the button went down on.
	if m.sel.dragging && msg.Action == tea.MouseActionMotion {
		y = clamp(y, 0, max(h-1, 0))
		if cmd := m.dragAutoScroll(s, dragEdge(y, h)); cmd != nil {
			m.dragSelection(terminal.Cell{X: x, Y: y})
			return m, cmd
		}
	}

	// The wheel belongs to whoever is drawing the text. Paused in history that is hop
	// whatever the far end asked for — it is not being shown the live screen, so it is
	// not being pointed at — and on a live screen it is hop until a program asks.
	if dir := wheelDir(msg.Button); dir != 0 && (m.scrolling() || !p.MouseEnabled()) {
		return m.wheelShell(s, dir, x, y, h)
	}

	if m.scrolling() {
		// Anything else over the history is a drag over text that is not going anywhere.
		// frame.content, never a half: a shell is always drawn at the full content width.
		return m.mouseSelect(msg, x, y, p.ViewScrollback(), m.frame.content)
	}

	// A remote program that asked for the mouse keeps it, selection included: it has its
	// own, and two selections for one drag is worse than either.
	if p.MouseEnabled() {
		p.SendMouse(msg, x, y)
		return m, nil
	}
	return m.mouseSelect(msg, x, y, p.View(), m.frame.content)
}

// wheelDir reads a wheel notch as the direction it wants the view moved: -1 back into
// history, +1 toward the live bottom, 0 for anything that is not a vertical wheel. A
// pane has no columns to spare, so the horizontal wheel is left alone.
func wheelDir(b tea.MouseButton) int {
	switch b {
	case tea.MouseButtonWheelUp:
		return -1
	case tea.MouseButtonWheelDown:
		return 1
	}
	return 0
}

// wheelShell answers one wheel notch over a shell pane hop is scrolling itself.
//
// The notch walks the view through the shell's history and carries any selection with the
// text it was made on. While a drag is live it also moves the head to the cell under the
// pointer, so the wheel grows the selection: a span longer than one screenful no longer
// means holding the pointer against an edge row and waiting for the autoscroll.
//
// A full-screen program keeps no scrollback here, so its notch is translated into arrow
// keys instead — xterm's alternate-scroll, and the only way a wheel reaches a less or a
// vim that never asked for the mouse. Not while dragging: those keys would move the far
// end's cursor rather than hop's view, under a selection hop is still making.
func (m *model) wheelShell(s *session, dir, x, y, h int) (tea.Model, tea.Cmd) {
	p := s.shell().pane
	if !m.scrolling() && p.AltScreen() {
		if !m.sel.dragging {
			m.reportInput(p.SendKeys(wheelKeys(dir)))
		}
		return m, nil
	}
	if m.scrollShellBy(s, dir, wheelStep) == 0 {
		return m, nil
	}
	if m.sel.dragging {
		// The pointer did not move; the text moved under it, so the head is wherever the
		// pointer already was.
		m.dragSelection(terminal.Cell{X: x, Y: clamp(y, 0, max(h-1, 0))})
	}
	return m, nil
}

// wheelKeys is one wheel notch as the arrow keys a full-screen program understands: the
// same three lines the wheel moves everywhere else in hop.
func wheelKeys(dir int) []tea.KeyMsg {
	key := tea.KeyMsg{Type: tea.KeyUp}
	if dir > 0 {
		key = tea.KeyMsg{Type: tea.KeyDown}
	}
	msgs := make([]tea.KeyMsg, wheelStep)
	for i := range msgs {
		msgs[i] = key
	}
	return msgs
}

// ---- autoscroll while dragging ----

// dragEdge reports which way a drag at row y of an h-row content box wants the view to
// move: -1 while it is held on the top row, +1 on the bottom, 0 anywhere between.
func dragEdge(y, h int) int {
	switch {
	case y <= 0:
		return -1
	case y >= h-1:
		return 1
	}
	return 0
}

// dragAutoScroll keeps a drag going once it has reached an edge: it steps the view one
// line now and hands back the tick that steps it again, since a pointer held still sends
// no further motion. It returns nil when there is nothing left to scroll into — the top
// of history, or the live bottom — which is where the selection simply stops growing.
//
// The generation is bumped on every change of direction, so the pending tick of an edge
// the pointer has left is dropped rather than fighting the new one.
func (m *model) dragAutoScroll(s *session, dir int) tea.Cmd {
	if m.sel.edge == dir {
		// Already going this way (or going nowhere): the armed tick carries it.
		return nil
	}
	m.dragGen++
	m.sel.edge = dir
	if dir == 0 || !m.dragScrollStep(s, dir) {
		m.sel.edge = 0
		return nil
	}
	return dragScrollCmd(m.dragGen)
}

// dragScrollTick answers one autoscroll tick, re-arming itself while the pointer is
// still held against the edge it was armed for.
func (m *model) dragScrollTick(gen int) tea.Cmd {
	if gen != m.dragGen || !m.sel.dragging || m.sel.edge == 0 {
		return nil
	}
	if !m.dragScrollStep(m.sessions[m.active], m.sel.edge) {
		m.sel.edge = 0
		return nil
	}
	return dragScrollCmd(gen)
}

// dragScrollStep moves the focused shell's view one line in dir and reports whether it
// moved — one autoscroll step, on the same terms as a wheel notch.
func (m *model) dragScrollStep(s *session, dir int) bool {
	return m.scrollShellBy(s, dir, 1) != 0
}

// scrollShellBy moves the focused shell's view n lines in dir — negative back into
// history, positive toward the live bottom — and reports how many lines it moved, which
// is 0 when there was nothing there to move into.
//
// Whatever is selected travels with the text it was made on: the view moved n lines, so
// the cells the selection covers are n rows further down (or up) the screen than they
// were. It is the one place hop's view scrolls, so both the wheel and the edge autoscroll
// keep a selection the same way.
func (m *model) scrollShellBy(s *session, dir, n int) int {
	if s == nil || s.shell() == nil || dir == 0 || n <= 0 {
		return 0
	}
	p := s.shell().pane
	before := p.ScrollOffset()
	if dir < 0 {
		// Upward past the top of the live screen is what scrollback is for; a pane with
		// no history, or a full-screen program, has nowhere to go.
		if !m.scrolling() && !m.enterScrollback(s) {
			return 0
		}
		p.ScrollUp(n)
	} else {
		// Downward only matters while paused in history: the live screen is the bottom.
		if !m.scrolling() {
			return 0
		}
		p.ScrollDown(n)
	}
	moved := p.ScrollOffset() - before
	m.shiftSelection(moved)
	// Back at the live bottom is the way out of history, as it is for the keys. After the
	// selection has moved, since the snap exitScrollback does is already a no-op here.
	//
	// Unconditional, so a step that entered scrollback and then found nothing to scroll
	// into leaves the mode as it found it.
	if p.AtBottom() {
		m.exitScrollback()
	}
	return moved
}

// mouseSelect is the pointer over a pane's text with nothing else claiming it: press
// anchors a selection, motion drags it, release copies it. view is what the pane is
// showing, and is what the copy is read out of; box is the content box it was drawn in,
// which the selection keeps so that the rows it covers stay measured against the same
// width they were drawn at. Each caller knows its own box — the content area for a shell,
// one half for an editor — so nothing here has to work it back out.
func (m *model) mouseSelect(msg tea.MouseMsg, x, y int, view string, box rect) (tea.Model, tea.Cmd) {
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
		m.startSelection(c, box)
	case tea.MouseActionMotion:
		m.dragSelection(c)
	}
	return m, nil
}

// mouseEditor is the pointer over an editor tab: the strip switches tabs, and the editor
// gets the event when it has asked for the mouse. hop keeps no history for it, so one
// that has not asked is not scrolled.
func (m *model) mouseEditor(s *session, msg tea.MouseMsg, x, y int) (tea.Model, tea.Cmd) {
	// The half the keyboard is in, which mousePane has already moved to the half that was
	// clicked. Both halves draw the same names against a different open tab, so the strip
	// has to be measured for the one being pointed at.
	right := s.focusedHalf()
	if y == 0 {
		if msg.Button == tea.MouseButtonLeft && msg.Action == tea.MouseActionPress {
			if i, ok := m.tabAt(editorTabNames(s), s.editorIndex(right), x, m.contentW(s)); ok {
				m.clearSelection()
				s.setEditor(i)
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
	return m.mouseSelect(msg, x, y-1, p.View(), m.frame.half(right))
}

// browserZone is the region the listing is drawn in — its own column, or the content area
// on a window with no room for one. It is what the double-click chord is keyed to, so that
// a click in the tree and a click in the file beside it can never pair up into a double.
func (m *model) browserZone() zone {
	if !m.frame.tree.empty() {
		return zoneTree
	}
	return zonePane
}

// mouseBrowser is the pointer over the SFTP listing, wherever it is drawn: the wheel moves
// the cursor, a click stands on an entry, a second click opens it. The coordinates arrive
// already translated into the browser's own space by treeLocal or contentLocal.
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
		double := m.clickChord(m.browserZone(), i)
		b.Select(i)
		if double {
			// A file yields an OpenFileMsg the model answers, as enter does; a directory
			// is loaded in place and the command is nil.
			return m, b.Activate()
		}
	}
	return m, nil
}
