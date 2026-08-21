package tui

// The body is three columns: the host list, the SFTP tree, and the content area. Each of
// the first two collapses entirely rather than shrinking to a sliver.

// sidebarWidth is the host list's preferred width, yielding to half of a narrow window.
const sidebarWidth = 32

// treeColWidth is the SFTP column's preferred width, borders included; it does not grow.
const treeColWidth = 34

// minContentWidth is the floor the content area keeps while the tree has a column; with
// treeColWidth it is the threshold below which the browser goes inline. See treeWidth.
const minContentWidth = 62

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
}

func (f frame) half(right bool) rect {
	if right && !f.right.empty() {
		return f.right
	}
	return f.left
}

// recomputeLayout is safe to re-run whenever the columns could have moved — the tree
// column comes and goes with the active session's browser, not only with a resize.
func (m *model) recomputeLayout() {
	if m.width <= 0 || m.height <= 0 {
		return
	}
	lw, tw := m.listWidth(), m.treeWidth()
	// The content area has no floor of its own: one could only be honoured by drawing past
	// the right-hand edge, and listWidth has already yielded the sidebar to protect it.
	m.paneW = max(m.width-lw-tw-2, 1)
	m.paneH = max(m.bodyHeight()-2, 3)
	// contentIsSplit, not splitOn: the frame says what is on SCREEN, and a split session
	// showing its shell shows one full-width box.
	m.frame = m.layout.buildFrame(lw, tw, m.contentIsSplit())
}

func (l *layout) buildFrame(lw, tw int, split bool) frame {
	bodyH := l.bodyHeight()
	f := frame{
		list:    rect{x: 0, y: 1, w: lw, h: bodyH},
		tree:    rect{x: lw, y: 1, w: tw, h: bodyH},
		content: rect{x: lw + tw, y: 1, w: l.paneW + 2, h: bodyH},
	}

	if !split {
		f.left = f.content
		return f
	}
	// Both halves are the same width, so an odd content area leaves a blank column at the
	// right-hand edge that belongs to no box.
	hw := l.splitHalf() + 2
	f.left = rect{x: f.content.x, y: 1, w: hw, h: bodyH}
	f.right = rect{x: f.content.x + hw, y: 1, w: hw, h: bodyH}
	return f
}

// relayout: focusing a session is as much a layout change as a resize, since it decides
// whether there is a tree column.
func (m *model) relayout() {
	m.recomputeLayout()
	m.resizeAll()
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

// sidebarOn: the two ways to be off — collapsed, unaffordable — must not be told apart
// elsewhere.
func (l *layout) sidebarOn() bool {
	return !l.sidebarHidden && l.sidebarFits()
}

// listWidth is 0 while the sidebar is off screen; every other size derives from it.
func (l *layout) listWidth() int {
	if !l.sidebarOn() {
		return 0
	}
	return l.sidebarPref()
}

// treeWidth tests the width left after the host list, so collapsing the sidebar is what
// buys the column on a middling terminal.
func (m *model) treeWidth() int {
	if m.treeHidden || !m.hasTree() || !m.roomForTree() {
		return 0
	}
	return treeColWidth
}

// roomForTree asks about the window alone, not about what any session holds, so
// browserSize can answer for an off-screen browser and still agree with treeWidth.
func (m *model) roomForTree() bool {
	return m.width-m.listWidth() >= treeColWidth+minContentWidth
}

func (m *model) hasTree() bool {
	s := m.sessions[m.active]
	return s != nil && s.browser != nil
}

// treeInline is the narrow-window fallback; see treeWidth for the threshold.
func (m *model) treeInline() bool { return m.hasTree() && m.treeWidth() == 0 }

func (m *model) toggleSidebar() {
	// Flipping here would change nothing on screen and would discard the preference the
	// window growing back is supposed to restore. sidebarHint stays silent to match.
	if !m.sidebarFits() {
		return
	}
	m.sidebarHidden = !m.sidebarHidden
	m.relayout()
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
	return s != nil && s.split && m.splitFits()
}

func (m *model) contentW(s *session) int {
	if m.splitOn(s) {
		return m.splitHalf()
	}
	return m.paneW
}

// splitHalf: both halves are the same width, which lets every editor tab be sized once.
func (l *layout) splitHalf() int {
	return max((l.paneW-2)/2, 10)
}

// splitFits: below it the split key opens an ordinary tab instead.
func (l *layout) splitFits() bool {
	return l.paneW+2 >= 2*minSplitHalf
}

// editorSize takes the session, not m.active: resizeAll lays out off-screen editors.
func (m *model) editorSize(s *session) (int, int) {
	return m.contentW(s), max(m.paneH-1, 1)
}

// browserSize answers for any session, so it tests the window directly rather than
// asking treeWidth (which is about the session on screen).
func (m *model) browserSize() (int, int) {
	if m.treeHidden || !m.roomForTree() {
		return m.paneW, m.paneH
	}
	return max(treeColWidth-2, 10), m.paneH
}

// shellSize: the tab strip costs a row only once there is a second shell.
func (m *model) shellSize(n int) (int, int) {
	h := m.paneH
	if n > 1 {
		h--
	}
	return m.paneW, max(h, 1)
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
	bw, bh := m.browserSize()
	for _, s := range m.sessions {
		m.resizeShells(s)
		if s.browser != nil {
			s.browser.Resize(bw, bh)
		}
		ew, eh := m.editorSize(s)
		for _, e := range s.editors {
			e.pane.Resize(ew, eh)
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
