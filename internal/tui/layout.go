package tui

// The screen is at most two columns: the sidebar on the left and the content area beside it.
// The sidebar is the host list, with the host in front opened out into its tabs and, under a
// files tab or an editor tab, the file tree in a box of its own below the hosts. What the
// content area shows is decided by one thing, the tab in front: a shell tab is one shell, an
// editor tab is the open file, the files tab is a preview of the file under the tree's
// cursor. The terminal panel may sit under an editor or the files tab. With no host in front
// the content area is the recent places and the details of the host under the cursor.
//
// The sidebar's width is the window's business alone. Where the window cannot pay for it the
// sidebar is not a column at all, and floats over the content while it has the keyboard, so
// no pane is ever resized by the keyboard moving.

// sidebarWidth is the sidebar's width, borders included.
const sidebarWidth = 32

// minContentWidth is the floor the content area keeps beside the sidebar; below it the
// sidebar stops being a column. See docked.
const minContentWidth = 62

// The two boxes of the sidebar: the hosts box gives the tree box everything past
// hostsBoxPct of the body it does not need, and neither goes under its floor. Rows are outer.
const (
	hostsBoxMin = 5
	treeBoxMin  = 6
	hostsBoxPct = 40
)

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

// footerRows is what the footer costs the body; there is no header.
const footerRows = 1

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
	// list is the sidebar's hosts box: a column, or floating over the content. tree is the
	// file tree's box under it, empty while it is not shown.
	list, tree, content rect
	left, right         rect
	// drawer is the terminal panel under the content area, empty while it is not shown.
	drawer rect
	// floats says list is drawn over the content rather than beside it.
	floats bool
}

func (f frame) half(right bool) rect {
	if right && !f.right.empty() {
		return f.right
	}
	return f.left
}

// recomputeLayout is safe to re-run whenever the boxes could have moved — the tree box comes
// and goes with the tab in front, and the hosts box with the rows it holds.
func (m *model) recomputeLayout() {
	if m.width <= 0 || m.height <= 0 {
		return
	}
	lw, dh := m.listWidth(), m.drawerOnScreen()
	// The content area has no floor of its own: one could only be honoured by drawing past
	// the right-hand edge, and listWidth has already yielded the sidebar to protect it.
	m.paneW = max(m.width-lw-2, 1)
	m.paneH = max(m.bodyHeight()-2-dh, 3)
	// contentIsSplit, not splitOn: the frame says what is on SCREEN, and a split session
	// showing a shell tab shows one full-width box.
	m.frame = m.layout.buildFrame(lw, m.hostsBoxH(m.treeBoxOn()), dh, m.contentIsSplit())
	if m.sidebarFloats() {
		// Drawn over the content, so it is hit-tested ahead of it but measures nothing.
		m.frame.list = rect{x: 0, y: 0, w: m.sidebarPref(), h: m.bodyHeight()}
		m.frame.floats = true
	}
}

func (l *layout) buildFrame(lw, hostsH, dh int, split bool) frame {
	bodyH := l.bodyHeight()
	ch := bodyH - dh
	f := frame{
		list:    rect{x: 0, y: 0, w: lw, h: min(hostsH, bodyH)},
		content: rect{x: lw, y: 0, w: l.paneW + 2, h: ch},
	}
	if lw > 0 && hostsH < bodyH {
		f.tree = rect{x: 0, y: hostsH, w: lw, h: bodyH - hostsH}
	}
	if dh > 0 {
		f.drawer = rect{x: lw, y: ch, w: l.paneW + 2, h: dh}
	}

	if !split {
		f.left = f.content
		return f
	}
	// Both halves are the same width, so an odd content area leaves a blank column at the
	// right-hand edge that belongs to no box.
	hw := l.splitHalf() + 2
	f.left = rect{x: f.content.x, y: 0, w: hw, h: ch}
	f.right = rect{x: f.content.x + hw, y: 0, w: hw, h: ch}
	return f
}

// relayout: focusing a session is as much a layout change as a resize, since it decides
// whether there is a tree box.
func (m *model) relayout() {
	m.recomputeLayout()
	m.resizeAll()
	m.shape = m.layoutShape()
}

// layoutShape is everything a pane or browser size depends on besides the window.
type layoutShape struct {
	active  string
	front   tabKind
	list    int
	hosts   int
	dead    bool
	browser bool
	editors int
	split   bool
	drawer  bool
}

func (m *model) layoutShape() layoutShape {
	sh := layoutShape{active: m.active, front: m.front(), list: m.listWidth(),
		hosts: m.hostsBoxH(true), drawer: m.drawerOnScreen() > 0}
	if s := m.sessions[m.active]; s != nil {
		sh.dead, sh.browser, sh.editors, sh.split = s.dead, s.browser != nil, len(s.editors), s.split
	}
	return sh
}

// syncView runs after every message: it settles which tab the host in front shows, and
// relayouts when the shape of the screen changed without anyone saying so.
func (m *model) syncView() {
	if s := m.sessions[m.active]; s != nil {
		s.front = m.frontOf(s)
		if m.mode == modeDrawer && !m.drawerShown(s) {
			// Shrunk below the panel's room, or its shell went: the keys go back above it.
			m.focusFiles()
		}
	}
	if m.ready && m.layoutShape() != m.shape {
		m.relayout()
	}
}

// frontOf is the tab s shows. For the host in front the keyboard decides, where it can: a
// shell's keys mean its shell tab, an editor's its editor tab, and the browser's the files
// tab — unless the browser is the tree box of the editor tab in front. The panel sits under
// whichever of the files and an editor was in front, and the sidebar changes nothing. What
// no longer exists gives way to the first tab there is: a shell, the files, an editor.
func (m *model) frontOf(s *session) tabKind {
	if s == nil {
		return tabNone
	}
	f := s.front
	if m.active != "" && s == m.sessions[m.active] {
		switch m.mode {
		case modeShell, modeScrollback:
			f = tabShell
		case modeEditor:
			f = tabEditor
		case modeBrowser:
			if f != tabEditor || s.editor() == nil || !m.treeInSidebar() {
				f = tabFiles
			}
		case modeDrawer:
			if f != tabFiles && f != tabEditor {
				f = tabEditor
			}
		}
	}
	has := func(k tabKind) bool {
		switch k {
		case tabShell:
			return s.shell() != nil
		case tabFiles:
			return s.browser != nil
		case tabEditor:
			return s.editor() != nil
		}
		return false
	}
	if has(f) {
		return f
	}
	order := []tabKind{tabShell, tabFiles, tabEditor}
	if f == tabEditor || f == tabFiles {
		// A panel under the files stays under the files.
		order = []tabKind{tabEditor, tabFiles, tabShell}
	}
	for _, k := range order {
		if has(k) {
			return k
		}
	}
	return tabNone
}

// front is the tab filling the content area now, tabNone with no host in front. The
// keyboard being in the sidebar does not change it.
func (m *model) front() tabKind {
	if m.active == "" {
		return tabNone
	}
	return m.frontOf(m.sessions[m.active])
}

// bodyHeight is the rows left for the boxes once the footer has its row.
func (l *layout) bodyHeight() int {
	return max(l.height-footerRows, 3)
}

// docked: the window pays for the sidebar as a column and still leaves the content its floor,
// and the user has not hidden it. It asks about the window and that choice alone, so no pane
// is sized by where the keyboard is.
func (l *layout) docked() bool {
	return !l.sidebarHidden && l.width >= sidebarWidth+minContentWidth
}

// paneLeft is the column the sidebar takes from every pane: all of it while docked, none
// while it only floats.
func (l *layout) paneLeft() int {
	if l.docked() {
		return sidebarWidth
	}
	return 0
}

// sidebarPref is the width the sidebar takes where it is not docked, yielding to half of a
// narrow window.
func (l *layout) sidebarPref() int {
	return clamp(sidebarWidth, 16, max(l.width/2, 16))
}

// sidebarFits: below this the sidebar yields entirely rather than shrink the content area.
func (l *layout) sidebarFits() bool {
	return l.width-l.sidebarPref() >= minPaneWidth+2
}

// listWidth is the column the sidebar takes: docked, or — with no host in front, where no
// pane can be reflowed — whatever a narrow window can spare. 0 while it floats or is off.
func (m *model) listWidth() int {
	switch {
	case m.docked():
		return sidebarWidth
	case m.active == "" && m.sidebarFits():
		return m.sidebarPref()
	}
	return 0
}

// sidebarFloats: a narrow window shows the sidebar only while it has the keyboard, drawn
// over the host in front so the panes under it keep their size.
func (m *model) sidebarFloats() bool {
	return m.mode == modeList && m.listWidth() == 0 && m.sidebarFits()
}

// sidebarOn: the sidebar is on screen, as a column or floating.
func (m *model) sidebarOn() bool {
	return m.listWidth() > 0 || m.sidebarFloats()
}

// sidebarChrome is the rows of the hosts box above its rows: the title while there are no
// sections to carry one, and the filter prompt.
func (m *model) sidebarChrome() int {
	n := m.listTitleRows()
	if m.filtering || m.filter != "" {
		n++
	}
	return n
}

// hostsBoxH is the hosts box's outer height. With the tree box under it, it takes what its
// rows need, up to hostsBoxPct of the body and never so much the tree goes under its floor;
// alone it takes the body.
func (m *model) hostsBoxH(withTree bool) int {
	body := m.bodyHeight()
	if !withTree {
		return body
	}
	want := max(len(m.rows), 1) + m.sidebarChrome() + 2
	return clamp(want, hostsBoxMin, max(min(body*hostsBoxPct/100, body-treeBoxMin), hostsBoxMin))
}

// treeBoxFits: the body holds both of the sidebar's boxes at their floors.
func (l *layout) treeBoxFits() bool {
	return l.bodyHeight() >= hostsBoxMin+treeBoxMin
}

// treeInSidebar: a browser is drawn in the sidebar's tree box rather than the content area,
// whenever its files are in front. It asks about the window, never the keyboard or the
// session, so a browser is sized the same before it lands as after.
func (m *model) treeInSidebar() bool {
	return !m.treeHidden && m.docked() && m.treeBoxFits()
}

// treeBoxOn: the tree box is on screen now, under the files tab or an editor tab.
func (m *model) treeBoxOn() bool {
	s := m.sessions[m.active]
	f := m.front()
	return (f == tabFiles || f == tabEditor) && !s.dead && s.browser != nil && m.treeInSidebar()
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

// drawerOnScreen is the rows the panel takes on screen now: only under the files tab or an
// editor tab.
func (m *model) drawerOnScreen() int {
	s := m.sessions[m.active]
	if f := m.front(); f != tabFiles && f != tabEditor || s.dead || !m.drawerShown(s) {
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

// drawerSize is the panel shell's size, the header row paid for. It sits under the width the
// files get, and its height does not depend on whether it is open, so neither hiding it nor
// moving between the files tab and an editor tab resizes its shell.
func (m *model) drawerSize(s *session) (int, int) {
	return m.filesW(s), max(m.drawerRows()-3, 1)
}

// previewOn: the files tab gives the content area to a preview while its tree is in the
// sidebar; with no tree box, the tree fills the tab instead.
func (m *model) previewOn() bool {
	return m.front() == tabFiles && m.treeBoxOn()
}

// filesW is the width a session's open files get: the content area's, which the window
// alone decides, so an editor keeps one size however the user moves around.
func (m *model) filesW(s *session) int {
	return max(m.width-m.paneLeft()-2, 1)
}

// toggleTree: hiding the tree box while the keyboard is in it is not a trap — the keyboard
// goes to the editor, or, on the files tab, the tree takes the content area.
func (m *model) toggleTree() {
	m.treeHidden = !m.treeHidden
	s := m.sessions[m.active]
	if m.treeHidden && m.browsing() && s != nil && s.front == tabEditor && s.editor() != nil {
		m.mode = modeEditor
	}
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

// browserSize answers for any session and any tab in front: the tree box's interior, else
// the content area above the panel.
func (m *model) browserSize(s *session) (int, int) {
	if m.treeInSidebar() {
		return max(sidebarWidth-2, 10), max(m.bodyHeight()-m.hostsBoxH(true)-2, 1)
	}
	return m.filesW(s), m.filesH(s)
}

// shellSize: a shell tab always has the whole content area, whose width the window alone
// decides, so no change of tab, host or focus resizes it — only the window does.
func (m *model) shellSize() (int, int) {
	return max(m.width-m.paneLeft()-2, 1), max(m.fullH(), 1)
}

func (m *model) resizeShells(s *session) {
	w, h := m.shellSize()
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

// listRows is the hosts box's rows for hosts and tabs, its title and filter paid for. It
// reads the frame, which the drawing reads too; before there is one it asks the layout.
func (m *model) listRows() int {
	h := m.frame.list.h
	if m.frame.list.empty() {
		h = m.hostsBoxH(m.treeBoxOn())
	}
	return max(h-2-m.sidebarChrome(), 1)
}

// listTitleRows is what the sidebar's fixed title costs — none once sections carry it.
func (m *model) listTitleRows() int {
	if m.hasSections() {
		return 0
	}
	return 1
}
