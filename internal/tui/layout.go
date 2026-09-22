package tui

// The body shows at most two boxes. With no host in front it is the host list beside the
// details; with one, it is that host's view: the shell view (one shell, full width) or the
// files view (the tree beside the open files). The host list then floats over the view
// instead of taking a column, so no pane is resized by going to it and back.

// sidebarWidth is the host list's preferred width, yielding to half of a narrow window.
const sidebarWidth = 32

// Bounds of the tree column, borders included; between them it is a quarter of the window.
const (
	treeColMin = 30
	treeColMax = 44
)

// minContentWidth is the floor the open files keep beside the tree; below it the tree
// column gives way. See roomForTree.
const minContentWidth = 62

// The terminal panel under the files: drawerPct of the body until the user drags it, never
// under drawerMinRows, and only while the files keep minFilesRows above it. Rows are outer.
const (
	drawerMinRows    = 6
	minFilesRows     = 8
	drawerDefaultPct = 45
	drawerStepPct    = 10
)

// minSplitHalf is the floor one half of a split content area keeps. See splitFits.
const minSplitHalf = 24

// chromeRows is what the header, status bar and footer cost the body.
const chromeRows = 3

// minPaneWidth is the inner width the content area is worth drawing at — not a promise.
const minPaneWidth = 10

// rect is a box on the screen in OUTER coordinates: x and y are its top-left cell, w and
// h include its border — the same unit listWidth and treeWidth speak in.
type rect struct{ x, y, w, h int }

// empty reports whether the box is not drawn at all; zero-width would still cost a border.
func (r rect) empty() bool { return r.w <= 0 || r.h <= 0 }

func (r rect) innerW() int { return max(r.w-2, 0) }
func (r rect) innerH() int { return max(r.h-2, 0) }

// contains counts the border: a click on a column's edge belongs to that column.
func (r rect) contains(x, y int) bool {
	return !r.empty() && x >= r.x && x < r.x+r.w && y >= r.y && y < r.y+r.h
}

// inner maps a screen cell into the box's content area, false on the border or outside.
func (r rect) inner(x, y int) (int, int, bool) {
	lx, ly := x-r.x-1, y-r.y-1
	if lx < 0 || ly < 0 || lx >= r.innerW() || ly >= r.innerH() {
		return 0, 0, false
	}
	return lx, ly, true
}

// clamp is inner for a pointer that has left the box — where a drag off the edge lands.
func (r rect) clamp(x, y int) (int, int) {
	return clamp(x-r.x-1, 0, max(r.innerW()-1, 0)), clamp(y-r.y-1, 0, max(r.innerH()-1, 0))
}

// frame is where every box of the body is, derived in recomputeLayout and read by the
// renderer and the pointer alike. Unsplit content lives in left, with right empty.
type frame struct {
	list, tree, content rect
	left, right         rect
	// drawer is the terminal panel under the content area, empty while it is not shown.
	drawer rect
}

func (f frame) half(right bool) rect {
	if right && !f.right.empty() {
		return f.right
	}
	return f.left
}

// recomputeLayout is safe to re-run whenever the columns could have moved — the tree
// column comes and goes with the active session's view, not only with a resize.
func (m *model) recomputeLayout() {
	if m.width <= 0 || m.height <= 0 {
		return
	}
	lw, tw, dh := m.listWidth(), m.treeWidth(), m.drawerOnScreen()
	// The content area has no floor of its own: one could only be honoured by drawing past
	// the right-hand edge, and listWidth has already yielded the sidebar to protect it.
	m.paneW = max(m.width-lw-tw-2, 1)
	m.paneH = max(m.bodyHeight()-2-dh, 3)
	// contentIsSplit, not splitOn: the frame says what is on SCREEN, and a split session
	// showing its shell shows one full-width box.
	m.frame = m.layout.buildFrame(lw, tw, dh, m.contentIsSplit())
	if m.sidebarFloats() {
		// Drawn over the view, so it is hit-tested ahead of it but measures nothing.
		m.frame.list = rect{x: 0, y: 1, w: m.sidebarPref(), h: m.bodyHeight()}
	}
}

func (l *layout) buildFrame(lw, tw, dh int, split bool) frame {
	bodyH := l.bodyHeight()
	ch := bodyH - dh
	f := frame{
		list:    rect{x: 0, y: 1, w: lw, h: bodyH},
		tree:    rect{x: lw, y: 1, w: tw, h: bodyH},
		content: rect{x: lw + tw, y: 1, w: l.paneW + 2, h: ch},
	}
	if dh > 0 {
		f.drawer = rect{x: lw + tw, y: 1 + ch, w: l.paneW + 2, h: dh}
	}

	if !split {
		f.left = f.content
		return f
	}
	// Both halves are the same width, so an odd content area leaves a blank column at the
	// right-hand edge that belongs to no box.
	hw := l.splitHalf() + 2
	f.left = rect{x: f.content.x, y: 1, w: hw, h: ch}
	f.right = rect{x: f.content.x + hw, y: 1, w: hw, h: ch}
	return f
}

// relayout: focusing a session is as much a layout change as a resize, since it decides
// whether there is a tree column.
func (m *model) relayout() {
	m.recomputeLayout()
	m.resizeAll()
	m.shape = m.layoutShape()
}

// layoutShape is everything a pane or browser size depends on besides the window.
type layoutShape struct {
	active  string
	files   bool
	list    bool
	browser bool
	editors int
	split   bool
	drawer  bool
}

func (m *model) layoutShape() layoutShape {
	sh := layoutShape{active: m.active, files: m.filesView(), list: m.sidebarOn(), drawer: m.drawerOnScreen() > 0}
	if s := m.sessions[m.active]; s != nil {
		sh.browser, sh.editors, sh.split = s.browser != nil, len(s.editors), s.split
	}
	return sh
}

// syncView runs after every message: it remembers which view the host in front is in, and
// relayouts when the shape of the screen changed without anyone saying so.
func (m *model) syncView() {
	if s := m.sessions[m.active]; s != nil {
		switch m.mode {
		case modeBrowser, modeEditor, modeDrawer:
			s.filesView = true
		case modeShell, modeScrollback:
			s.filesView = false
		}
		if m.mode == modeDrawer && !m.drawerShown(s) {
			// Shrunk below the panel's room, or its shell went: the keys go back above it.
			m.focusFiles()
		}
	}
	if m.ready && m.layoutShape() != m.shape {
		m.relayout()
	}
}

// bodyHeight is the rows left for the columns once the header and footer have theirs.
func (l *layout) bodyHeight() int {
	return max(l.height-chromeRows, 3)
}

// sidebarPref is the width the host list would like, before sidebarFits has its say.
func (l *layout) sidebarPref() int {
	return clamp(sidebarWidth, 16, max(l.width/2, 16))
}

// sidebarFits: below this the list yields entirely rather than shrink the content area.
func (l *layout) sidebarFits() bool {
	return l.width-l.sidebarPref() >= minPaneWidth+2
}

// sidebarOn: the host list is on screen exactly while it has the keyboard — as a column
// with no host in front, floating over the host's view otherwise.
func (m *model) sidebarOn() bool {
	return m.mode == modeList && m.sidebarFits()
}

// sidebarFloats: over a host's view the list is drawn on top, so the panes under it keep
// their size.
func (m *model) sidebarFloats() bool {
	return m.sidebarOn() && m.active != ""
}

// listWidth is the column the host list takes, 0 when it floats or is off screen.
func (m *model) listWidth() int {
	if !m.sidebarOn() || m.sidebarFloats() {
		return 0
	}
	return m.sidebarPref()
}

// filesView: browser and editor modes are the files view, a shell the shell view; the
// host list shows whichever the host was last in.
func (m *model) filesView() bool {
	s := m.sessions[m.active]
	if s == nil {
		return false
	}
	switch m.mode {
	case modeBrowser, modeEditor, modeDrawer:
		return true
	case modeShell, modeScrollback:
		return false
	}
	return s.filesView || s.shell() == nil
}

// fullH is the interior height of a box that has the whole body to itself.
func (l *layout) fullH() int { return max(l.bodyHeight()-2, 3) }

// drawerFits: the smallest panel still leaves the files their floor.
func (l *layout) drawerFits() bool {
	return l.bodyHeight()-drawerMinRows >= minFilesRows
}

// drawerRows is the panel's outer height whenever it is drawn: the user's share of the body,
// held between the panel's floor and the files'.
func (l *layout) drawerRows() int {
	pct := l.drawerPct
	if pct == 0 {
		pct = drawerDefaultPct
	}
	body := l.bodyHeight()
	return clamp(body*pct/100, drawerMinRows, max(body-minFilesRows, drawerMinRows))
}

// resizeDrawer sets the panel to rows, remembered as a share so a window resize keeps it.
func (m *model) resizeDrawer(rows int) {
	body := max(m.bodyHeight(), 1)
	rows = clamp(rows, drawerMinRows, max(body-minFilesRows, drawerMinRows))
	// Rounded up, so the share read back gives the same rows.
	m.drawerPct = (rows*100 + body - 1) / body
	m.relayout()
}

// drawerShown: open, running and with room — the one test every measurement of s asks.
func (m *model) drawerShown(s *session) bool {
	return s != nil && s.drawerOpen && s.drawer != nil && m.drawerFits()
}

// drawerOnScreen is the rows the panel takes on screen now: only in the files view.
func (m *model) drawerOnScreen() int {
	if !m.filesView() || !m.drawerShown(m.sessions[m.active]) {
		return 0
	}
	return m.drawerRows()
}

// filesH is the interior height the files of s get, the panel under them paid for.
func (m *model) filesH(s *session) int {
	if m.drawerShown(s) {
		return max(m.fullH()-m.drawerRows(), 1)
	}
	return m.fullH()
}

// drawerSize is the panel shell's size, the header row paid for. Its height does not depend
// on whether it is open, so hiding and showing it never resizes the shell.
func (m *model) drawerSize(s *session) (int, int) {
	w := max(m.width-2, 1)
	if m.hasColumn(s) {
		w = m.filesW(s)
	}
	return w, max(m.drawerRows()-3, 1)
}

// treeCol is the tree column's width, borders included, for the window as it is.
func (l *layout) treeCol() int {
	return clamp(l.width/4, treeColMin, treeColMax)
}

// roomForTree asks about the window alone, so a session off screen measures the same.
func (l *layout) roomForTree() bool {
	return l.width >= l.treeCol()+minContentWidth
}

// hasColumn: a browser earns its column only beside open files; alone it fills the view.
func (m *model) hasColumn(s *session) bool {
	return s != nil && s.browser != nil && len(s.editors) > 0 && !m.treeHidden && m.roomForTree()
}

// treeWidth is the tree column on screen now: only in the files view.
func (m *model) treeWidth() int {
	if !m.filesView() || !m.hasColumn(m.sessions[m.active]) {
		return 0
	}
	return m.treeCol()
}

func (m *model) hasTree() bool {
	s := m.sessions[m.active]
	return s != nil && s.browser != nil
}

// treeInline: the files view has a browser but no column for it, so it takes the content area.
func (m *model) treeInline() bool { return m.filesView() && m.hasTree() && m.treeWidth() == 0 }

// filesW is the width a session's open files get, whether or not the session is in front,
// so an editor keeps one size however the user moves around.
func (m *model) filesW(s *session) int {
	w := m.width - 2
	if s != nil && s.browser != nil && !m.treeHidden && m.roomForTree() {
		w -= m.treeCol()
	}
	return max(w, 1)
}

// toggleTree: hiding the column while the keyboard is in it is not a trap — treeInline
// puts the browser back in the content area.
func (m *model) toggleTree() {
	m.treeHidden = !m.treeHidden
	m.relayout()
}

// splitOn: every measurement of the content area asks this rather than s.split, so a
// window shrunk below the threshold shows one half and gets both back when it grows.
func (m *model) splitOn(s *session) bool {
	return s != nil && s.split && splitFitsIn(m.filesW(s))
}

func (m *model) contentW(s *session) int {
	if m.splitOn(s) {
		return splitHalfOf(m.filesW(s))
	}
	return m.filesW(s)
}

// splitHalf: both halves are the same width, which lets every editor tab be sized once.
func (l *layout) splitHalf() int { return splitHalfOf(l.paneW) }

func splitHalfOf(w int) int { return max((w-2)/2, 10) }

// splitFits: below it the split key opens an ordinary tab instead.
func (m *model) splitFits() bool { return splitFitsIn(m.filesW(m.sessions[m.active])) }

func splitFitsIn(w int) bool { return w+2 >= 2*minSplitHalf }

// editorSize takes the session, not m.active: resizeAll lays out off-screen editors.
func (m *model) editorSize(s *session) (int, int) {
	return m.contentW(s), max(m.filesH(s)-1, 1)
}

// browserSize answers for any session: in its column beside open files, else the whole view.
func (m *model) browserSize(s *session) (int, int) {
	if m.hasColumn(s) {
		return max(m.treeCol()-2, 10), m.fullH()
	}
	return max(m.width-2, 1), m.filesH(s)
}

// shellSize: a shell always has the whole width, so no view change resizes it. The tab
// strip costs a row only once there is a second shell.
func (m *model) shellSize(n int) (int, int) {
	h := m.fullH()
	if n > 1 {
		h--
	}
	return max(m.width-2, 1), max(h, 1)
}

// resizeShells: the shell count changes the tab strip, which resizes the panes.
func (m *model) resizeShells(s *session) {
	w, h := m.shellSize(len(s.shells))
	for _, sh := range s.shells {
		sh.pane.Resize(w, h)
	}
}

// resizeAll resizes hidden tabs too, which the split relies on since a tab may be shown
// in either half.
func (m *model) resizeAll() {
	for _, s := range m.sessions {
		m.resizeShells(s)
		if s.browser != nil {
			s.browser.Resize(m.browserSize(s))
		}
		ew, eh := m.editorSize(s)
		for _, e := range s.editors {
			e.pane.Resize(ew, eh)
		}
		if s.drawer != nil {
			s.drawer.pane.Resize(m.drawerSize(s))
		}
	}
}

// listRows mirrors renderList's bookkeeping and has to be kept in step with it.
func (m *model) listRows() int {
	r := m.height - 4 - m.listTitleRows()
	if m.filtering || m.filter != "" {
		r--
	}
	return max(r, 1)
}

// listTitleRows is what the sidebar's fixed title costs — none once sections carry it.
func (m *model) listTitleRows() int {
	if m.hasSections() {
		return 0
	}
	return 1
}
