package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// The frame is the one thing every other test takes for granted: three side-by-side
// boxes whose outer widths add up to the window, and a pointer that lands in the same
// box the renderer drew. Both halves of that are arithmetic scattered across layout.go,
// view.go and mouse.go, and the only way they can be kept in step is to measure the
// frame that was actually drawn rather than to ask the same functions twice.
//
// So these tests read the boxes off the rendered screen — the first body row is nothing
// but the tops of the boxes, so every ╭ opens one and every ╮ closes it — and then hold
// zoneAt, treeLocal and contentLocal to the boxes found there. They are characterization
// tests: what they pin down is what hop draws today, so that a refactor which replaces
// the scattered border arithmetic with a single rect type has something to be wrong
// against.

// box is one of the body's boxes as the frame shows it: the screen columns of its left
// and right border, the zone the pointer is supposed to report inside it, and — for the
// content area drawn as two halves — which half it is.
type box struct {
	lo, hi int
	z      zone
	right  bool
}

// layoutCase is one window size crossed with one arrangement of the columns. setup runs
// on a freshly laid-out model and puts whatever the case is about on screen.
type layoutCase struct {
	name  string
	w, h  int
	setup func(t *testing.T, m *model)
}

// ---- the arrangements ----

// withTree gives the active session a browser, which is what puts the tree column on
// screen — or, on a window too narrow for three columns, what puts the browser inside
// the content area instead. See treeWidth.
func withTree(t *testing.T, m *model) {
	t.Helper()
	m.sessions["web1"] = &session{browser: fakeBrowser(t, "/srv")}
	m.active, m.mode = "web1", modeBrowser
	m.relayout()
}

// withShell is a session that costs no tree column at all: the content area takes
// everything the host list leaves, which is every screen hop drew before the column
// existed.
func withShell(t *testing.T, m *model) {
	t.Helper()
	s := &session{shells: []*shellTab{{id: 1, pane: fakePane()}}}
	t.Cleanup(s.closeShells)
	m.sessions["web1"] = s
	m.active, m.mode = "web1", modeShell
	m.relayout()
}

// withEditors opens two files on the active session, split across the content area when
// split is set and beside a tree column when tree is. The split is asked for rather than
// asserted: splitOn refuses it on a window too narrow to hold two halves, and a case
// straddling that threshold is exactly what wants testing.
func withEditors(t *testing.T, m *model, tree, split bool) {
	t.Helper()
	s := &session{editors: []*editorTab{
		{id: 1, name: "a.conf", path: "/etc/a.conf", pane: fakePane()},
		{id: 2, name: "b.conf", path: "/etc/b.conf", pane: fakePane()},
	}}
	t.Cleanup(s.closeEditors)
	if tree {
		s.browser = fakeBrowser(t, "/srv")
	}
	if split {
		s.openSplit()
		s.splitEd = 1
	}
	m.sessions["web1"] = s
	m.active, m.mode = "web1", modeEditor
	m.relayout()
}

// treeThreshold is the window width at which three columns first fit with the host list
// open: the two constants plus the sidebar the test is measured against. Cases sit one
// column below it, exactly on it and one above, since that is where treeWidth flips
// between a column of its own and the inline fallback.
const treeThreshold = sidebarWidth + treeColWidth + minContentWidth

// splitThreshold is the window width at which the content area first halves with the
// host list collapsed and no tree column: splitFits wants paneW+2 ≥ 2*minSplitHalf, and
// paneW is the window less the content box's own two border columns.
const splitThreshold = 2 * minSplitHalf

// layoutCases is the grid: every state a column can be in, crossed with the widths where
// the layout changes its mind.
func layoutCases() []layoutCase {
	hide := func(f func(t *testing.T, m *model)) func(*testing.T, *model) {
		return func(t *testing.T, m *model) {
			f(t, m)
			m.toggleSidebar()
		}
	}
	editors := func(tree, split bool) func(*testing.T, *model) {
		return func(t *testing.T, m *model) { withEditors(t, m, tree, split) }
	}
	return []layoutCase{
		// The three columns, with and without the host list beside them.
		{"three columns", 200, 60, withTree},
		{"three columns, sidebar collapsed", 200, 60, hide(withTree)},
		// The column is collapsible on its own terms, and a session with no browser
		// never had one — two different routes to the same two-column screen.
		{"tree collapsed", 200, 60, func(t *testing.T, m *model) {
			withTree(t, m)
			m.toggleTree()
		}},
		{"no browser on the session", 200, 60, withShell},
		{"no session at all", 200, 60, func(*testing.T, *model) {}},

		// The narrow-terminal threshold, from both sides and exactly on it.
		{"one column short of three", treeThreshold - 1, 34, withTree},
		{"exactly three columns' worth", treeThreshold, 34, withTree},
		{"one column over", treeThreshold + 1, 34, withTree},
		// Measured against what is left after the host list, so collapsing it moves the
		// threshold by the sidebar's width — the trade the user makes by collapsing it.
		{"collapsed, one short", treeColWidth + minContentWidth - 1, 34, hide(withTree)},
		{"collapsed, exactly", treeColWidth + minContentWidth, 34, hide(withTree)},
		{"the classic 80 columns, inline", 80, 24, withTree},

		// The content area drawn as two halves, beside a tree column and without one.
		{"split beside the tree", 200, 60, editors(true, true)},
		{"split with no tree", 200, 60, editors(false, true)},
		{"split, sidebar collapsed", 200, 60, hide(editors(true, true))},
		// An odd content width cannot be halved evenly; the odd column is left blank at
		// the right-hand edge, which is a cell the pointer can still land on.
		{"split, odd content width", 201, 60, editors(true, true)},
		{"unsplit editors", 200, 60, editors(true, false)},

		// The split threshold, from both sides and exactly on it. The host list is
		// collapsed and there is no browser, so the window is the content area plus its
		// two border columns and the arithmetic is visible in the case's width.
		{"one column short of a split", splitThreshold - 1, 20, hide(editors(false, true))},
		{"exactly a split's worth", splitThreshold, 20, hide(editors(false, true))},
		{"one column over a split", splitThreshold + 1, 20, hide(editors(false, true))},
		// And with the host list open, which is the same threshold moved along by it.
		{"split beside the host list", sidebarWidth + splitThreshold, 24, editors(false, true)},

		// An absurd terminal, which still has to add up.
		{"a tiny window", 40, 10, withTree},
		{"a tiny window, no session", 40, 10, func(*testing.T, *model) {}},
		{"a tiny window, sidebar collapsed", 40, 10, hide(withShell)},
	}
}

// ---- reading the frame ----

// frameOf renders the model and hands back the screen as lines with the styling stripped,
// which is what the cell arithmetic below indexes into.
func frameOf(m *model) []string {
	lines := strings.Split(m.View(), "\n")
	for i, ln := range lines {
		lines[i] = ansi.Strip(ln)
	}
	return lines
}

// drawnBoxes reads the body's boxes off the frame itself. The first body row is nothing
// but the tops of the boxes — lipgloss draws them with a rounded border, so the row holds
// only ╭, ─ and ╮ — which makes it an account of the layout that does not go through any
// of the arithmetic being tested.
func drawnBoxes(t *testing.T, frame []string) []box {
	t.Helper()
	var boxes []box
	open := -1
	for i, r := range []rune(frame[1]) {
		switch r {
		case '╭':
			if open >= 0 {
				t.Fatalf("a box opened at column %d while one was still open at %d:\n%s", i, open, frame[1])
			}
			open = i
		case '╮':
			if open < 0 {
				t.Fatalf("a box closed at column %d with none open:\n%s", i, frame[1])
			}
			boxes = append(boxes, box{lo: open, hi: i})
			open = -1
		}
	}
	if open >= 0 {
		t.Fatalf("the box opened at column %d never closed:\n%s", open, frame[1])
	}
	return boxes
}

// wantBoxes is where the layout arithmetic says the boxes are: the host list, the tree
// column, and the content area as one box or as two halves sharing the columns the one
// box had. Outer coordinates throughout — lo and hi are border columns.
func wantBoxes(m *model) []box {
	var boxes []box
	base := 0
	if lw := m.listWidth(); lw > 0 {
		boxes = append(boxes, box{lo: 0, hi: lw - 1, z: zoneList})
		base = lw
	}
	if tw := m.treeWidth(); tw > 0 {
		boxes = append(boxes, box{lo: base, hi: base + tw - 1, z: zoneTree})
		base += tw
	}
	if s := m.sessions[m.active]; m.splitOn(s) {
		w := m.splitHalf()
		return append(boxes,
			box{lo: base, hi: base + w + 1, z: zonePane},
			box{lo: base + w + 2, hi: base + 2*w + 3, z: zonePane, right: true})
	}
	return append(boxes, box{lo: base, hi: base + m.paneW + 1, z: zonePane})
}

// layoutBoxes is the drawn boxes labelled with what each one is: the frame says where
// they are, the layout says which is which, and TestDrawnBoxesMatchTheLayout is what
// makes pairing them up by position legitimate.
func layoutBoxes(t *testing.T, m *model) []box {
	t.Helper()
	drawn, want := drawnBoxes(t, frameOf(m)), wantBoxes(m)
	if len(drawn) != len(want) {
		t.Fatalf("the frame holds %d boxes, want %d (%v vs %v)", len(drawn), len(want), drawn, want)
	}
	for i := range drawn {
		drawn[i].z, drawn[i].right = want[i].z, want[i].right
	}
	return drawn
}

// boxAt returns the box containing screen column x, or false for a column no box covers —
// the odd column an odd-width split leaves blank at the right-hand edge.
func boxAt(boxes []box, x int) (box, bool) {
	for _, b := range boxes {
		if x >= b.lo && x <= b.hi {
			return b, true
		}
	}
	return box{}, false
}

// ---- the frame is additive ----

// Every line of the screen is exactly as wide as the window and there are exactly as
// many as it is tall — not "at most", which is what the older tests settle for: a frame
// a column short is a gap down the right-hand edge, and the columns adding up is the
// whole premise of the hit-testing below. This is what clampLines and fitLines exist to
// protect, and what a rect type would have to keep true.
func TestFrameIsExactlyTheWindow(t *testing.T) {
	for _, c := range layoutCases() {
		t.Run(c.name, func(t *testing.T) {
			m := viewModel(c.w, c.h)
			c.setup(t, m)

			lines := strings.Split(m.View(), "\n")
			if len(lines) != c.h {
				t.Fatalf("%dx%d: the screen is %d lines, want %d", c.w, c.h, len(lines), c.h)
			}
			for i, ln := range lines {
				if got := lipgloss.Width(ln); got != c.w {
					t.Fatalf("%dx%d: line %d is %d cells wide, want exactly %d:\n%q",
						c.w, c.h, i, got, c.w, ln)
				}
			}
		})
	}
}

// TODO(frame): the frame stops adding up on a window narrower than 28 columns, and this
// test says so rather than pretending otherwise. Both column widths have a floor —
// listWidth clamps to 16 and recomputeLayout clamps paneW to 10 — and neither floor
// knows about the other or about the window, so below 16+10+2 the two boxes are wider
// than the terminal they are drawn in and every body line overruns it. Expected: the
// frame is the window at every size, the columns yielding entirely as they do at every
// other threshold in this file. Actual: a 20-column window is drawn 28 cells wide, which
// is the one case TestFrameIsExactlyTheWindow would fail on, so it has no case that
// small. The fix belongs with whatever replaces the border arithmetic — a rect that
// cannot be wider than its parent, and it no longer is.
//
// This test used to assert the defect: the sidebar's floor of 16 and the content area's
// of 10 were independent of each other and of the window, so a 20-column terminal was
// drawn 28 cells wide — and a frame wider than the terminal scrolls hop's own header off
// the top of itself. listWidth now yields the sidebar entirely rather than overrun, on
// the same terms as the tree column, and the content area has no floor left to break.
func TestVeryNarrowWindowsStillFitTheirTerminal(t *testing.T) {
	// Three is the real floor: a box cannot be narrower than its two borders plus a
	// column to draw in. No terminal is that small; the arithmetic simply has an end.
	for w := 3; w <= 40; w++ {
		m := viewModel(w, 12)
		withShell(t, m)

		for i, ln := range strings.Split(m.View(), "\n") {
			if got := lipgloss.Width(ln); got != w {
				t.Fatalf("a %d-column window renders line %d at %d cells, want %d", w, i, got, w)
			}
		}
	}
}

// The sidebar is what gives way, and only when it has to: the window that can hold both
// keeps both, and the one that cannot loses the list rather than the file.
func TestTheSidebarYieldsBeforeTheFrameOverruns(t *testing.T) {
	for _, c := range []struct {
		w        int
		wantList int
	}{
		{27, 0},  // 16 + 12 needs 28; one short, so the list goes
		{28, 16}, // exactly enough for both floors
		{40, 20}, // half the window, which is the ordinary clamp
	} {
		m := viewModel(c.w, 12)
		withShell(t, m)
		m.recomputeLayout()
		if got := m.fr.list.w; got != c.wantList {
			t.Errorf("at %d columns the list is %d wide, want %d", c.w, got, c.wantList)
		}
		if got := m.fr.content.x + m.fr.content.w; got != c.w {
			t.Errorf("at %d columns the content box ends at %d, want the window edge", c.w, got)
		}
	}
}

// The boxes the renderer drew are the boxes the layout arithmetic describes: same left
// border, same right border, in the same order across the row. Everything else here is
// measured against the drawn boxes, so this is the test that makes the rest mean
// anything.
func TestDrawnBoxesMatchTheLayout(t *testing.T) {
	for _, c := range layoutCases() {
		t.Run(c.name, func(t *testing.T) {
			m := viewModel(c.w, c.h)
			c.setup(t, m)

			drawn, want := drawnBoxes(t, frameOf(m)), wantBoxes(m)
			if len(drawn) != len(want) {
				t.Fatalf("%dx%d: the frame holds %d boxes, want %d (%v vs %v)",
					c.w, c.h, len(drawn), len(want), drawn, want)
			}
			for i := range want {
				if drawn[i].lo != want[i].lo || drawn[i].hi != want[i].hi {
					t.Fatalf("%dx%d: box %d is drawn at columns %d..%d, want %d..%d",
						c.w, c.h, i, drawn[i].lo, drawn[i].hi, want[i].lo, want[i].hi)
				}
			}
		})
	}
}

// ---- the renderer and the hit-testing agree ----

// Every cell of the body reports the zone of the box it was drawn in. The header and the
// two rows below the body are the frame's own, and everything a box does not cover — the
// blank column an odd-width split leaves over — falls to the content area, which is what
// zoneAt's final fallthrough means.
func TestZoneAtMatchesTheDrawnBoxes(t *testing.T) {
	for _, c := range layoutCases() {
		t.Run(c.name, func(t *testing.T) {
			m := viewModel(c.w, c.h)
			c.setup(t, m)
			boxes := layoutBoxes(t, m)

			for y := 1; y <= m.bodyHeight(); y++ {
				for x := 0; x < m.width; x++ {
					want := zonePane
					if b, ok := boxAt(boxes, x); ok {
						want = b.z
					}
					if got := m.zoneAt(x, y); got != want {
						t.Fatalf("%dx%d: zoneAt(%d, %d) = %v, want %v — the cell was drawn in a %v box",
							c.w, c.h, x, y, got, want, want)
					}
				}
			}
			// The rows above and below the body belong to the chrome whatever the
			// columns are doing, and a cell off the screen belongs to nothing.
			if got := m.zoneAt(0, 0); got != zoneHeader {
				t.Fatalf("the top row is %v, want zoneHeader", got)
			}
			if got := m.zoneAt(0, m.bodyHeight()+1); got != zoneFooter {
				t.Fatalf("the row under the body is %v, want zoneFooter", got)
			}
			if got := m.zoneAt(m.width, 1); got != zoneNone {
				t.Fatalf("a column past the window is %v, want zoneNone", got)
			}
		})
	}
}

// treeLocal answers for the inside of the tree column's box and nowhere else: a cell on
// its border, in another column, or below the listing is not a row of the tree, and
// saying otherwise would open the file the pointer happens to be level with.
func TestTreeLocalCoversTheTreeBoxInterior(t *testing.T) {
	for _, c := range layoutCases() {
		t.Run(c.name, func(t *testing.T) {
			m := viewModel(c.w, c.h)
			c.setup(t, m)
			boxes := layoutBoxes(t, m)

			tree, hasTree := box{}, false
			for _, b := range boxes {
				if b.z == zoneTree {
					tree, hasTree = b, true
				}
			}

			for y := 1; y <= m.bodyHeight(); y++ {
				for x := 0; x < m.width; x++ {
					lx, ly, ok := m.treeLocal(x, y)
					// The interior is the box less its two border columns, and the
					// listing's rows are the box less its two border rows — which is
					// paneH, the same count the browser was resized to.
					want := hasTree && x > tree.lo && x < tree.hi && y >= 2 && y-2 < m.paneH
					if ok != want {
						t.Fatalf("%dx%d: treeLocal(%d, %d) ok = %v, want %v", c.w, c.h, x, y, ok, want)
					}
					if !ok {
						continue
					}
					if lx != x-tree.lo-1 || ly != y-2 {
						t.Fatalf("%dx%d: treeLocal(%d, %d) = (%d, %d), want (%d, %d)",
							c.w, c.h, x, y, lx, ly, x-tree.lo-1, y-2)
					}
				}
			}
		})
	}
}

// contentLocal answers for the inside of a content box, names which half it was, and
// refuses everything else. The two halves share a divider two columns wide — the right
// border of one box and the left border of the next — and it belongs to neither: a drag
// started on it would anchor a selection in a pane nobody pointed at.
func TestContentLocalCoversTheContentBoxInteriors(t *testing.T) {
	for _, c := range layoutCases() {
		t.Run(c.name, func(t *testing.T) {
			m := viewModel(c.w, c.h)
			c.setup(t, m)
			boxes := layoutBoxes(t, m)

			for y := 1; y <= m.bodyHeight(); y++ {
				for x := 0; x < m.width; x++ {
					gotRight, lx, ly, ok := m.contentLocal(x, y)

					var in box
					inside := false
					for _, b := range boxes {
						if b.z == zonePane && x > b.lo && x < b.hi {
							in, inside = b, true
						}
					}
					inside = inside && y >= 2 && y-2 < m.paneH
					if ok != inside {
						t.Fatalf("%dx%d: contentLocal(%d, %d) ok = %v, want %v", c.w, c.h, x, y, ok, inside)
					}
					if !ok {
						continue
					}
					if gotRight != in.right || lx != x-in.lo-1 || ly != y-2 {
						t.Fatalf("%dx%d: contentLocal(%d, %d) = (%v, %d, %d), want (%v, %d, %d)",
							c.w, c.h, x, y, gotRight, lx, ly, in.right, x-in.lo-1, y-2)
					}
				}
			}
		})
	}
}

// The divider between two halves is two columns of border and belongs to neither half —
// stated on its own, because it is the one place in the body where two boxes touch and
// the arithmetic that puts the right half three columns past the left is the easiest in
// the file to get wrong by one.
func TestSplitDividerBelongsToNeitherHalf(t *testing.T) {
	m := viewModel(200, 60)
	withEditors(t, m, true, true)
	if !m.splitOn(m.sessions[m.active]) {
		t.Fatal("the content area is not split; the test is not looking at a divider")
	}

	base, w := m.listWidth()+m.treeWidth(), m.splitHalf()
	for _, x := range []int{base + w + 1, base + w + 2} {
		if _, _, _, ok := m.contentLocal(x, 3); ok {
			t.Fatalf("column %d is on the divider, and contentLocal claimed it", x)
		}
	}
	// And the cells either side of it are the two halves, which is what makes those two
	// a divider rather than a gap.
	if right, _, _, ok := m.contentLocal(base+w, 3); !ok || right {
		t.Fatalf("the column before the divider is (%v, %v), want the left half", right, ok)
	}
	if right, _, _, ok := m.contentLocal(base+w+3, 3); !ok || !right {
		t.Fatalf("the column after the divider is (%v, %v), want the right half", right, ok)
	}
}
