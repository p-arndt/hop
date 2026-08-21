package tui

import (
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"hop/internal/terminal"
)

const doubleClickWindow = 400 * time.Millisecond

// wheelStep is how many lines one wheel notch moves a scrolling view; the host list steps
// the selection instead, since it does not scroll.
const wheelStep = 3

// zone is the part of the screen a mouse event landed in.
type zone int

const (
	zoneNone zone = iota
	zoneHeader
	zoneList
	zoneTree
	zonePane
	zoneFooter
)

// zoneAt must mirror the layout arithmetic View composes with; a collapsed column has
// outer width 0, so its cells fall to the column on its right.
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
	// odd-width split leaves over.
	return zonePane
}

// handleMouse routes a mouse event to whichever region it landed in.
func (m *model) handleMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	model, cmd := m.routeMouse(msg)

	// A release ends the drag wherever it lands, or a gesture that ran off the pane would
	// leave a drag live for the next release to finish.
	if msg.Action == tea.MouseActionRelease && m.sel.dragging {
		m.endSelection(m.dragView())
	}
	return model, cmd
}

func (m *model) routeMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
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
	// A live drag keeps its events wherever the pointer went, or crossing into the sidebar
	// would clear the selection halfway through making it.
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

// dragView is what a drag in progress is selecting out of.
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

// clickChord reports whether this click completes a double-click, arming the window when
// it does not. Keyed on id rather than screen row, since the first click re-scrolls both
// lists around the cursor.
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
		if msg.Action != tea.MouseActionPress {
			return m, nil
		}
		return m.clickList(msg)

	case tea.MouseButtonRight:
		if msg.Action != tea.MouseActionPress {
			return m, nil
		}
		return m.rightClickList(msg)
	}
	return m, nil
}

// clickList stands the cursor on the clicked host, connecting on a double-click.
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

// rightClickList opens the context menu on the clicked host, standing the cursor on it
// first since the menu acts on the selected host.
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

// listRowAt maps a screen row to an index into m.filtered, or false when the row holds no
// host.
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

// listFirstRow is the screen row the first host row is drawn on; must track renderList's
// arithmetic, and is shared with the context menu's anchoring.
func (m *model) listFirstRow() int {
	first := 2 + m.listTitleRows()
	if m.filtering || m.filter != "" {
		first++
	}
	return first
}

// backToList hands the keyboard back to the host list, keeping the active session on
// screen.
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

func (m *model) mouseTree(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	s := m.sessions[m.active]
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
		// And on through: the click also stands on the row it landed on.
	}

	x, y, ok := m.treeLocal(msg.X, msg.Y)
	if !ok {
		return m, nil
	}
	return m.mouseBrowser(s, msg, x, y)
}

// treeLocal maps a screen cell into the tree column's content box, which is the space the
// browser's RowAt and Select are measured in.
func (m *model) treeLocal(x, y int) (int, int, bool) { return m.frame.tree.inner(x, y) }

// ---- the content area ----

func (m *model) mousePane(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	s := m.sessions[m.active]
	if m.active == "" || s == nil || s.dead {
		return m, nil
	}

	right, x, y, ok := m.contentLocal(msg.X, msg.Y)
	if !ok {
		// Off the pane with the button down: continue the drag at the edge it left by,
		// which is the edge row autoscroll watches.
		if !m.sel.dragging {
			return m, nil
		}
		right = s.focusedHalf()
		x, y = m.clampToPane(msg.X, msg.Y)
	}

	// With no column for it the browser is the content area. Must come before the focus
	// handling below, which would read a click on the listing as a click into a pane that
	// is not there.
	if m.treeInline() && m.browsing() && s.browser != nil {
		return m.mouseBrowser(s, msg, x, y)
	}

	if m.listHasFocus() || m.browsing() {
		if msg.Button == tea.MouseButtonLeft && msg.Action == tea.MouseActionPress {
			m.clickIntoPane(s, right)
		}
		return m, nil
	}

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
// where inside that half's box. (0, 0) is the tab strip when there is one.
func (m *model) contentLocal(x, y int) (bool, int, int, bool) {
	if lx, ly, ok := m.frame.left.inner(x, y); ok {
		return false, lx, ly, true
	}
	if lx, ly, ok := m.frame.right.inner(x, y); ok {
		return true, lx, ly, true
	}
	return false, 0, 0, false
}

// paneLocal is contentLocal for callers that only ask about the focused half.
func (m *model) paneLocal(x, y int) (int, int, bool) {
	_, lx, ly, ok := m.contentLocal(x, y)
	return lx, ly, ok
}

// clampToPane maps a screen cell to the nearest one inside the focused half's box, which
// is where a drag off the edge lands.
func (m *model) clampToPane(x, y int) (int, int) {
	s := m.sessions[m.active]
	return m.frame.half(s != nil && s.splitRight).clamp(x, y)
}

// clickIntoPane gives the keyboard to what the content area is showing.
func (m *model) clickIntoPane(s *session, right bool) {
	// A click that crossed columns spends any pending double, or pointing at a file and
	// back at the tree row you came from would open that row.
	m.chords.click = time.Time{}
	switch {
	case s.editorAt(right) != nil:
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

// mouseShell is the pointer over a focused shell pane.
func (m *model) mouseShell(s *session, msg tea.MouseMsg, x, y int) (tea.Model, tea.Cmd) {
	h := m.paneH
	if len(s.shells) > 1 {
		// A drag passing over the strip is still a drag.
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

	// A drag held against the top or bottom row scrolls the view under it.
	if m.sel.dragging && msg.Action == tea.MouseActionMotion {
		y = clamp(y, 0, max(h-1, 0))
		if cmd := m.dragAutoScroll(s, dragEdge(y, h)); cmd != nil {
			m.dragSelection(terminal.Cell{X: x, Y: y})
			return m, cmd
		}
	}

	// Paused in history the wheel is hop's whatever the far end asked for: the live screen
	// is not what is being pointed at.
	if dir := wheelDir(msg.Button); dir != 0 && (m.scrolling() || !p.MouseEnabled()) {
		return m.wheelShell(s, dir, x, y, h)
	}

	if m.scrolling() {
		// frame.content, never a half: a shell is always drawn at the full content width.
		return m.mouseSelect(msg, x, y, p.ViewScrollback(), m.frame.content)
	}

	// A remote program that asked for the mouse keeps it, selection included: two
	// selections for one drag is worse than either.
	if p.MouseEnabled() {
		p.SendMouse(msg, x, y)
		return m, nil
	}
	return m.mouseSelect(msg, x, y, p.View(), m.frame.content)
}

// wheelDir reads a wheel notch as -1 back into history, +1 toward the live bottom, 0 for
// anything that is not a vertical wheel.
func wheelDir(b tea.MouseButton) int {
	switch b {
	case tea.MouseButtonWheelUp:
		return -1
	case tea.MouseButtonWheelDown:
		return 1
	}
	return 0
}

// wheelShell answers one wheel notch over a shell pane hop scrolls itself. A full-screen
// program keeps no scrollback, so its notch becomes arrow keys (xterm alternate-scroll) —
// but not mid-drag, where those keys would move the far end's cursor instead of the view.
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
		// The pointer did not move; the text moved under it.
		m.dragSelection(terminal.Cell{X: x, Y: clamp(y, 0, max(h-1, 0))})
	}
	return m, nil
}

// wheelKeys is one wheel notch as the arrow keys a full-screen program understands.
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

// dragEdge reports which way a drag at row y of an h-row box wants the view moved.
func dragEdge(y, h int) int {
	switch {
	case y <= 0:
		return -1
	case y >= h-1:
		return 1
	}
	return 0
}

// dragAutoScroll steps the view once and returns the tick that repeats it, since a pointer
// held still sends no further motion. The generation is bumped on every direction change
// so a stale tick is dropped rather than fighting the new one.
func (m *model) dragAutoScroll(s *session, dir int) tea.Cmd {
	if m.sel.edge == dir {
		// Already going this way (or nowhere): the armed tick carries it.
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

// dragScrollTick answers one autoscroll tick, re-arming while the pointer is still held.
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

func (m *model) dragScrollStep(s *session, dir int) bool {
	return m.scrollShellBy(s, dir, 1) != 0
}

// scrollShellBy moves the focused shell's view n lines in dir and reports how far it
// moved. The one place hop's view scrolls, so it is also where a selection is carried
// along with the text it was made on.
func (m *model) scrollShellBy(s *session, dir, n int) int {
	if s == nil || s.shell() == nil || dir == 0 || n <= 0 {
		return 0
	}
	p := s.shell().pane
	before := p.ScrollOffset()
	if dir < 0 {
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
	// After the selection has moved, and unconditional so a step that entered scrollback
	// then found nothing to scroll into leaves the mode as it found it.
	if p.AtBottom() {
		m.exitScrollback()
	}
	return moved
}

// mouseSelect is the pointer over a pane's text: press anchors, motion drags, release
// copies. box is the content box the view was drawn in, which the selection keeps so its
// rows stay measured against the same width.
func (m *model) mouseSelect(msg tea.MouseMsg, x, y int, view string, box rect) (tea.Model, tea.Cmd) {
	c := terminal.Cell{X: x, Y: y}
	// A release ends the drag whatever button it names: not every terminal says which one
	// came up, and a drag that never ends never copies.
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

// mouseEditor is the pointer over an editor tab. hop keeps no history for it, so an editor
// that has not asked for the mouse is not scrolled.
func (m *model) mouseEditor(s *session, msg tea.MouseMsg, x, y int) (tea.Model, tea.Cmd) {
	// Both halves draw the same names against a different open tab, so the strip has to be
	// measured for the half being pointed at.
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
	return m.mouseSelect(msg, x, y-1, p.View(), m.frame.half(right))
}

// browserZone is the region the listing is drawn in; the double-click chord is keyed to
// it so a click in the tree and one in the file beside it cannot pair up.
func (m *model) browserZone() zone {
	if !m.frame.tree.empty() {
		return zoneTree
	}
	return zonePane
}

// mouseBrowser expects coordinates already translated into the browser's own space by
// treeLocal or contentLocal.
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
			// A file yields an OpenFileMsg the model answers; a directory loads in place.
			return m, b.Activate()
		}
	}
	return m, nil
}
