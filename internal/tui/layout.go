package tui

// The body is three columns: the host list, the SFTP tree, and the content area the
// shells and the editors are drawn in. Each of the first two is collapsible and each
// yields entirely rather than shrinking to a sliver — a column too narrow to read is
// worse than no column, because it still costs the one beside it.

// sidebarWidth is the host list's preferred width, yielding to half of a narrow window.
const sidebarWidth = 32

// treeColWidth is the SFTP column's preferred width, borders included. Thirty-four leaves
// thirty-two for the listing, which is what a filename plus the size and date the browser
// draws beside it wants; below that the names start being elided at the point they stop
// telling two files apart. It does not grow with the window: the column is a tree, and a
// wide terminal is better spent on the file.
const treeColWidth = 34

// minContentWidth is the outer width the content area will not go below while the tree
// has a column of its own — sixty-two, so a remote editor gets sixty columns inside it.
// Sixty is the floor a file is readable at once vim's own gutter has taken its share, and
// an editor is what the tree column exists to open.
//
// The two constants together are the narrow-terminal threshold: with fewer than
// treeColWidth+minContentWidth columns left after the host list, hop draws the layout it
// had before the column existed — the browser takes the whole content area while it holds
// the keyboard — rather than three columns none of which can be worked in. See treeWidth.
const minContentWidth = 62

// minSplitHalf is the outer width one half of a split content area will not go below.
// Twenty-two columns of text is already a poor place to read a file; it is offered
// because half a wide window is still worth having, and refused below this because two
// unreadable halves are worse than one readable pane. See splitFits.
const minSplitHalf = 24

// chromeRows is what the header, status bar and footer cost the body.
const chromeRows = 3

// minPaneWidth is the inner width the content area is worth drawing at. It is not a
// promise the layout can always keep — see listWidth, which yields the whole sidebar
// rather than let the content box run off the right-hand edge to honour it.
const minPaneWidth = 10

// rect is a box on the screen in OUTER coordinates: x and y are its top-left cell, w and
// h include its border. That is the unit listWidth and treeWidth have always spoken in,
// so a rect can be built from them without an adjustment nobody can see.
//
// It exists because the same border arithmetic used to be written out at every consumer —
// the renderer, three translations in the pointer handling, and the selection's width —
// each correct only relative to its own starting point, and none of them checked against
// the others. A rect answers "is this cell mine" and "where is it inside me" once.
type rect struct{ x, y, w, h int }

// empty reports whether the box is not drawn at all. A collapsed column is empty rather
// than zero-width-but-present: a zero-width box would still cost the two columns its
// border takes, which is why collapsing has always meant returning 0 here.
func (r rect) empty() bool { return r.w <= 0 || r.h <= 0 }

// innerW and innerH are the box less its border — the size the content drawn in it gets.
func (r rect) innerW() int { return max(r.w-2, 0) }
func (r rect) innerH() int { return max(r.h-2, 0) }

// contains reports whether screen cell (x, y) is anywhere in the box, border included.
// This is the question the zone hit-testing asks, which is why it counts the border: a
// click on a column's edge belongs to that column, not to its neighbour.
func (r rect) contains(x, y int) bool {
	return !r.empty() && x >= r.x && x < r.x+r.w && y >= r.y && y < r.y+r.h
}

// inner maps a screen cell to one inside the box's content area, or reports false for a
// cell on the border or outside. This is the question the panes ask: an emulated screen
// is addressed from its own top-left corner and knows nothing of the box around it.
func (r rect) inner(x, y int) (int, int, bool) {
	lx, ly := x-r.x-1, y-r.y-1
	if lx < 0 || ly < 0 || lx >= r.innerW() || ly >= r.innerH() {
		return 0, 0, false
	}
	return lx, ly, true
}

// clamp maps a screen cell to the nearest cell inside the box's content area — inner for
// a pointer that has left it, which is where a drag off the edge lands.
func (r rect) clamp(x, y int) (int, int) {
	return clamp(x-r.x-1, 0, max(r.innerW()-1, 0)), clamp(y-r.y-1, 0, max(r.innerH()-1, 0))
}

// frame is where every box of the body is, for one drawn screen. It is derived in
// recomputeLayout and read by the renderer and the pointer alike, so the two cannot
// disagree about what is on screen or how wide it is.
//
// left and right are the halves of the content area. Unsplit there is one half and it is
// left, with right empty — so "is the content split" is the same question as "is there a
// right half", asked of the layout rather than of the session.
type frame struct {
	list, tree, content rect
	left, right         rect
}

// half returns the content box for one side, which is left when the content is not split.
func (f frame) half(right bool) rect {
	if right && !f.right.empty() {
		return f.right
	}
	return f.left
}

// recomputeLayout derives the column inner sizes from the window size. It is cheap and
// derives everything from state, so it is safe to run again whenever the columns could
// have moved — which is not only a resize: the tree column comes and goes with the active
// session's browser.
func (m *model) recomputeLayout() {
	if m.width <= 0 || m.height <= 0 {
		return
	}
	lw, tw := m.listWidth(), m.treeWidth()
	// listWidth and treeWidth are outer widths, borders included; the content area gets
	// the rest of the row, less the two columns its own border takes. It has no floor of
	// its own: a floor here could only be honoured by drawing past the right-hand edge,
	// and listWidth has already given up the sidebar to keep this from going small.
	m.paneW = max(m.width-lw-tw-2, 1)
	m.paneH = max(m.bodyHeight()-2, 3)
	m.fr = m.buildFrame(lw, tw)
}

// buildFrame places the boxes of the body along the row. The columns are laid left to
// right from widths that are already settled, so the frame cannot disagree with them —
// which is the whole point of deriving it in one place.
func (m *model) buildFrame(lw, tw int) frame {
	bodyH := m.bodyHeight()
	f := frame{
		list:    rect{x: 0, y: 1, w: lw, h: bodyH},
		tree:    rect{x: lw, y: 1, w: tw, h: bodyH},
		content: rect{x: lw + tw, y: 1, w: m.paneW + 2, h: bodyH},
	}

	if !m.splitOn(m.sessions[m.active]) {
		f.left = f.content
		return f
	}
	// Two boxes inside the columns the one box had. Both halves are the same width, so
	// the odd column an odd-width content area leaves over stays blank at the right-hand
	// edge — it belongs to no box, and the pointer is told so.
	hw := m.splitHalf() + 2
	f.left = rect{x: f.content.x, y: 1, w: hw, h: bodyH}
	f.right = rect{x: f.content.x + hw, y: 1, w: hw, h: bodyH}
	return f
}

// relayout re-derives the columns and tells every live pane the size it now has. It is
// what a window resize does, and what anything that moves the columns has to do too:
// which session is active decides whether there is a tree column at all, so focusing one
// is as much a layout change as resizing the window is.
func (m *model) relayout() {
	m.recomputeLayout()
	m.resizeAll()
}

// bodyHeight is the rows left for the columns once the header and footer have theirs.
func (m *model) bodyHeight() int {
	return max(m.height-chromeRows, 3)
}

// listWidth is the outer width of the host list, borders included, or 0 while the sidebar
// is collapsed — which is the whole of what collapsing means, since every other size here
// derives from it.
func (m *model) listWidth() int {
	if m.sidebarHidden {
		return 0
	}
	w := clamp(sidebarWidth, 16, max(m.width/2, 16))
	// The floor of 16 is a preference, not a promise. It is independent of the window, so
	// on a terminal narrower than that floor plus a content box the two together used to
	// come to more cells than there were — and the frame was drawn wider than the
	// terminal, which scrolls hop's own header off the top of itself.
	//
	// So the list yields entirely, on the same terms as the tree column: a column too
	// narrow to work in is worse than no column, and of the two the content area is the
	// one that cannot be given up.
	if m.width-w < minPaneWidth+2 {
		return 0
	}
	return w
}

// treeWidth is the outer width of the SFTP column, borders included, or 0 when there is
// no column to draw: the active session has no browser, the column is collapsed, or the
// window cannot hold three columns at once. As with listWidth, returning 0 is the whole
// of what collapsing means.
//
// The window test is against what is left after the host list rather than against the
// whole width, so collapsing the sidebar on a middling terminal is what buys the column —
// which is the trade the user is making by collapsing it.
func (m *model) treeWidth() int {
	if m.treeHidden || !m.hasTree() || !m.roomForTree() {
		return 0
	}
	return treeColWidth
}

// roomForTree reports whether the window can hold the SFTP column and a content area
// worth reading beside it. It asks about the window alone — not about what any session is
// holding — which is what lets browserSize answer for a browser that is not on screen, or
// does not exist yet, and still agree with treeWidth about how wide it will be.
func (m *model) roomForTree() bool {
	return m.width-m.listWidth() >= treeColWidth+minContentWidth
}

// hasTree reports whether the active session has a browser to put in the column. A
// session without one costs no columns at all: the content area takes the whole row,
// which is every screen hop drew before the column existed.
func (m *model) hasTree() bool {
	s := m.sessions[m.active]
	return s != nil && s.browser != nil
}

// treeInline reports whether the browser has to share the content area rather than having
// a column of its own — the narrow-window fallback, and the only case in which a file and
// its tree cannot be on screen together. See treeWidth for the threshold.
func (m *model) treeInline() bool { return m.hasTree() && m.treeWidth() == 0 }

// toggleSidebar hides or restores the host list and re-lays out what the change resizes.
// The remote programs are told their new size here rather than on the next window event,
// so a full-screen editor reflows the moment the columns arrive.
func (m *model) toggleSidebar() {
	m.sidebarHidden = !m.sidebarHidden
	m.relayout()
}

// toggleTree hides or restores the SFTP column, on the same terms as the host list: the
// column is a place to stand, and a file being read at full width does not need one beside
// it. Hiding it while the keyboard is in it is not a trap — treeInline then puts the
// browser back in the content area, which is the pre-column screen.
func (m *model) toggleTree() {
	m.treeHidden = !m.treeHidden
	m.relayout()
}

// splitOn reports whether s's content is drawn as two halves right now: it was split, and
// the window is still wide enough to hold two of them. A window shrunk below the threshold
// shows the focused half alone rather than two boxes that overrun the row, and gets both
// back when it grows again — the split is layout, so a narrow moment does not discard it.
//
// Every measurement of the content area asks this rather than s.split, so the renderer,
// the sizes the editors are told and the pointer's hit-testing cannot disagree about how
// many boxes are on screen.
func (m *model) splitOn(s *session) bool {
	return s != nil && s.split && m.splitFits()
}

// contentW is the inner width of one content box: the whole content area, or half of it
// while that session's content is drawn as two.
func (m *model) contentW(s *session) int {
	if m.splitOn(s) {
		return m.splitHalf()
	}
	return m.paneW
}

// splitHalf is the inner width of one half of a split content area. Both halves are the
// same width, which is what lets every editor tab — the hidden ones included — be sized
// once: a tab switched into either half is already laid out for it. The odd column an
// odd-width content area leaves over stays blank at the right-hand edge rather than
// making one half wider than the other for the sake of it.
func (m *model) splitHalf() int {
	return max((m.paneW-2)/2, 10)
}

// splitFits reports whether the content area can be halved and leave two halves worth
// reading. Below it the split key opens an ordinary tab and says so.
func (m *model) splitFits() bool {
	return m.paneW+2 >= 2*minSplitHalf
}

// editorSize is the terminal size an editor pane gets: its content box less the tab
// strip. It takes the session rather than reading m.active, because resizeAll lays out
// the editors of sessions that are not on screen and the split is per session.
func (m *model) editorSize(s *session) (int, int) {
	return m.contentW(s), max(m.paneH-1, 1)
}

// browserSize is the terminal size a session's browser gets: the inside of the tree
// column, or the whole content area on a window with no room for a column.
//
// Like editorSize it answers for any session rather than the active one, so a browser
// switched to later is already the right size. That is why it tests the window against
// the threshold directly instead of asking treeWidth, which is about the session on
// screen now.
func (m *model) browserSize() (int, int) {
	if m.treeHidden || !m.roomForTree() {
		return m.paneW, m.paneH
	}
	return max(treeColWidth-2, 10), m.paneH
}

// shellSize is the terminal size a shell pane gets on a host with n shells open. A lone
// shell gets the whole content area: the tab strip appears — and costs a row — only once
// there is a second shell. Shells never split; the split is the file browser's, and what
// it opens is files.
func (m *model) shellSize(n int) (int, int) {
	h := m.paneH
	if n > 1 {
		h--
	}
	return m.paneW, max(h, 1)
}

// resizeShells re-lays out every shell of s for the current pane size and tab strip. It
// runs whenever the shell count changes, since the strip appearing resizes the panes.
func (m *model) resizeShells(s *session) {
	w, h := m.shellSize(len(s.shells))
	for _, sh := range s.shells {
		sh.pane.Resize(w, h)
	}
}

// resizeAll re-lays out every live pane, browser and editor for the current window.
// Hidden tabs are resized too, so one switched to later is already laid out — which the
// split relies on, since both halves are the same width and a tab may be shown in either.
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

// listRows approximates the rows visible in the list pane — the step pgup/pgdn move by.
// It mirrors renderList's bookkeeping: the body less the border, the HOSTS title unless
// the sections have replaced it, and the filter prompt when there is one.
func (m *model) listRows() int {
	r := m.height - 4 - m.listTitleRows()
	if m.filtering || m.filter != "" {
		r--
	}
	return max(r, 1)
}

// listTitleRows is what the sidebar's fixed title costs: one row, or none once the
// section headings scroll with the list and carry the counts.
func (m *model) listTitleRows() int {
	if m.hasSections() {
		return 0
	}
	return 1
}
