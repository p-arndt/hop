package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// box is one drawn box: its border columns, its zone, and which half of a split it is.
type box struct {
	lo, hi int
	z      zone
	right  bool
}

// layoutCase is one window size crossed with one arrangement of the columns.
type layoutCase struct {
	name  string
	w, h  int
	setup func(t *testing.T, m *model)
}

// ---- the arrangements ----

// withTree gives the active session a browser, which is what puts the tree column on screen.
func withTree(t *testing.T, m *model) {
	t.Helper()
	m.sessions["web1"] = &session{browser: fakeBrowser(t, "/srv")}
	m.active, m.mode = "web1", modeBrowser
	m.relayout()
}

// withShell is a session that costs no tree column: the content area takes all the list leaves.
func withShell(t *testing.T, m *model) {
	t.Helper()
	s := &session{shells: []*shellTab{{id: 1, pane: fakePane()}}}
	t.Cleanup(s.closeShells)
	m.sessions["web1"] = s
	m.active, m.mode = "web1", modeShell
	m.relayout()
}

// withEditors opens two files, optionally split across the content area and beside a tree column.
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

// withSplitShell: a split session with the keyboard in its shell, so the screen is not split.
func withSplitShell(t *testing.T, m *model) {
	t.Helper()
	s := &session{
		shells: []*shellTab{{id: 1, pane: fakePane()}},
		editors: []*editorTab{
			{id: 1, name: "a.conf", path: "/etc/a.conf", pane: fakePane()},
			{id: 2, name: "b.conf", path: "/etc/b.conf", pane: fakePane()},
		},
	}
	t.Cleanup(s.closeShells)
	t.Cleanup(s.closeEditors)
	s.openSplit()
	s.splitEd = 1
	m.sessions["web1"] = s
	m.active, m.mode = "web1", modeShell
	m.relayout()
}

// treeThreshold is the width at which three columns first fit with the host list open.
const treeThreshold = sidebarWidth + treeColWidth + minContentWidth

// splitThreshold is the width at which the content area first halves with no sidebar or tree.
const splitThreshold = 2 * minSplitHalf

// layoutCases crosses every column state with the widths where the layout changes its mind.
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
		{"three columns", 200, 60, withTree},
		{"three columns, sidebar collapsed", 200, 60, hide(withTree)},
		{"tree collapsed", 200, 60, func(t *testing.T, m *model) {
			withTree(t, m)
			m.toggleTree()
		}},
		{"no browser on the session", 200, 60, withShell},
		{"no session at all", 200, 60, func(*testing.T, *model) {}},

		{"one column short of three", treeThreshold - 1, 34, withTree},
		{"exactly three columns' worth", treeThreshold, 34, withTree},
		{"one column over", treeThreshold + 1, 34, withTree},
		{"collapsed, one short", treeColWidth + minContentWidth - 1, 34, hide(withTree)},
		{"collapsed, exactly", treeColWidth + minContentWidth, 34, hide(withTree)},
		{"the classic 80 columns, inline", 80, 24, withTree},

		{"split beside the tree", 200, 60, editors(true, true)},
		{"split with no tree", 200, 60, editors(false, true)},
		{"split, sidebar collapsed", 200, 60, hide(editors(true, true))},
		{"split, odd content width", 201, 60, editors(true, true)},
		{"unsplit editors", 200, 60, editors(true, false)},
		{"shell focused in a split session", 200, 20, withSplitShell},
		{"shell focused in a split session, sidebar collapsed", 200, 20, hide(withSplitShell)},

		{"one column short of a split", splitThreshold - 1, 20, hide(editors(false, true))},
		{"exactly a split's worth", splitThreshold, 20, hide(editors(false, true))},
		{"one column over a split", splitThreshold + 1, 20, hide(editors(false, true))},
		{"split beside the host list", sidebarWidth + splitThreshold, 24, editors(false, true)},

		{"a tiny window", 40, 10, withTree},
		{"a tiny window, no session", 40, 10, func(*testing.T, *model) {}},
		{"a tiny window, sidebar collapsed", 40, 10, hide(withShell)},
	}
}

// ---- reading the frame ----

// frameOf renders the model and hands back the screen with the styling stripped.
func frameOf(m *model) []string {
	lines := strings.Split(m.View(), "\n")
	for i, ln := range lines {
		lines[i] = ansi.Strip(ln)
	}
	return lines
}

// drawnBoxes reads the boxes off the first body row, which holds nothing but their tops.
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

// wantBoxes is where the layout arithmetic says the boxes are, in outer coordinates.
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
	// contentIsSplit, not splitOn: what is drawn is what the renderer's switch decides.
	if m.contentIsSplit() {
		w := m.splitHalf()
		return append(boxes,
			box{lo: base, hi: base + w + 1, z: zonePane},
			box{lo: base + w + 2, hi: base + 2*w + 3, z: zonePane, right: true})
	}
	return append(boxes, box{lo: base, hi: base + m.paneW + 1, z: zonePane})
}

// layoutBoxes is the drawn boxes labelled with what the layout says each one is.
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

// boxAt returns the box containing screen column x, or false for a column no box covers.
func boxAt(boxes []box, x int) (box, bool) {
	for _, b := range boxes {
		if x >= b.lo && x <= b.hi {
			return b, true
		}
	}
	return box{}, false
}

// ---- the frame is additive ----

// Every line is exactly as wide as the window, and there are exactly as many as it is tall.
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

// Regression: the list's floor of 16 and the pane's of 10 once drew a 20-column window 28 cells wide.
func TestVeryNarrowWindowsStillFitTheirTerminal(t *testing.T) {
	// Three is the floor: two borders plus a column to draw in.
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

// The sidebar is what gives way, and only when it has to.
func TestTheSidebarYieldsBeforeTheFrameOverruns(t *testing.T) {
	for _, c := range []struct {
		w        int
		wantList int
	}{
		{27, 0}, // 16 + 12 needs 28; one short, so the list goes
		{28, 16},
		{40, 20},
	} {
		m := viewModel(c.w, 12)
		withShell(t, m)
		m.recomputeLayout()
		if got := m.frame.list.w; got != c.wantList {
			t.Errorf("at %d columns the list is %d wide, want %d", c.w, got, c.wantList)
		}
		if got := m.frame.content.x + m.frame.content.w; got != c.w {
			t.Errorf("at %d columns the content box ends at %d, want the window edge", c.w, got)
		}
	}
}

// The boxes the renderer drew are the boxes the layout describes, in the same order.
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

// Every body cell reports the zone of its box; anything no box covers falls to the content area.
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

// treeLocal answers for the inside of the tree column's box and nowhere else.
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
					// The interior is the box less its borders; the listing's rows are paneH.
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

// contentLocal answers for the inside of a content box, names which half, and refuses the divider.
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

// The two columns of divider between the halves belong to neither half.
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
	if right, _, _, ok := m.contentLocal(base+w, 3); !ok || right {
		t.Fatalf("the column before the divider is (%v, %v), want the left half", right, ok)
	}
	if right, _, _, ok := m.contentLocal(base+w+3, 3); !ok || !right {
		t.Fatalf("the column after the divider is (%v, %v), want the right half", right, ok)
	}
}

// Below the width that can pay for the host list, hop behaves as though it were collapsed.
func TestTooNarrowForTheSidebarReadsAsCollapsed(t *testing.T) {
	for _, w := range []int{24, 27} {
		m := viewModel(w, 12)
		withShell(t, m)

		if m.sidebarOn() {
			t.Fatalf("at %d columns the sidebar reports itself on screen", w)
		}
		screen := ansi.Strip(m.View())
		if strings.Contains(screen, "HOSTS") {
			t.Fatalf("at %d columns the host list is drawn after all", w)
		}
		// Asked of the hint, not the rendered row: a footer this narrow has no room to print it.
		if got := m.sidebarHint(); got != "" {
			t.Fatalf("at %d columns the footer offers %q for a list that cannot come back", w, got)
		}
		before := m.sidebarHidden
		m.handleKey(toggleKey())
		if m.sidebarHidden != before {
			t.Fatalf("at %d columns ctrl+b flipped sidebarHidden with nothing to show for it", w)
		}
		if got := m.listWidth(); got != 0 {
			t.Fatalf("at %d columns the list is %d columns wide, want 0", w, got)
		}
	}
}

// The threshold is a threshold: one column over it the list is back as the user left it.
func TestTheSidebarComesBackWhenTheWindowCanPayForIt(t *testing.T) {
	m := viewModel(24, 12)
	withShell(t, m)
	if m.sidebarOn() {
		t.Fatal("24 columns cannot pay for the sidebar")
	}

	m.update(tea.WindowSizeMsg{Width: 28, Height: 12})

	if !m.sidebarOn() {
		t.Fatal("28 columns can pay for the sidebar and it did not come back")
	}
	if got := m.listWidth(); got != 16 {
		t.Fatalf("the restored list is %d columns wide, want its floor of 16", got)
	}
	if !strings.Contains(ansi.Strip(m.View()), "HOSTS") {
		t.Fatal("the restored list is not on screen")
	}
}
