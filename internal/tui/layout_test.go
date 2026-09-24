package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// box is one drawn box: where it is, its zone, and which half of a split it is.
type box struct {
	r     rect
	z     zone
	right bool
}

// layoutCase is one window size crossed with one arrangement of the columns.
type layoutCase struct {
	name  string
	w, h  int
	setup func(t *testing.T, m *model)
}

// ---- the arrangements ----

// withTree gives the active session a browser and an open file, the keyboard in the tree
// box of the editor tab.
func withTree(t *testing.T, m *model) {
	t.Helper()
	s := &session{browser: fakeBrowser(t, "/srv"), editors: []*editorTab{{id: 1, name: "a.conf", path: "/etc/a.conf", pane: fakePane()}}}
	t.Cleanup(s.closeEditors)
	s.front = tabEditor
	m.sessions["web1"] = s
	m.active, m.mode = "web1", modeBrowser
	m.relayout()
}

// withFiles is the files tab: the tree in the sidebar beside a preview when there is room,
// else the tree in the content area.
func withFiles(t *testing.T, m *model) {
	t.Helper()
	s := &session{browser: fakeBrowser(t, "/srv"), shells: []*shellTab{{id: 1, pane: fakePane()}}}
	t.Cleanup(s.closeShells)
	s.front = tabFiles
	m.sessions["web1"] = s
	m.active, m.mode = "web1", modeBrowser
	m.relayout()
}

// withShell is a session with no browser: the content area is its shell.
func withShell(t *testing.T, m *model) {
	t.Helper()
	s := &session{shells: []*shellTab{{id: 1, pane: fakePane()}}}
	t.Cleanup(s.closeShells)
	m.sessions["web1"] = s
	m.active, m.mode = "web1", modeShell
	m.relayout()
}

// withEditors opens two files, optionally split across the content area and with a browser.
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

// dockThreshold is the width at which the sidebar first docks beside the content.
const dockThreshold = sidebarWidth + minContentWidth

// splitThreshold is the width at which the content area first halves with no sidebar.
const splitThreshold = 2 * minSplitHalf

// layoutCases crosses every box state with the widths where the layout changes its mind.
func layoutCases() []layoutCase {
	editors := func(tree, split bool) func(*testing.T, *model) {
		return func(t *testing.T, m *model) { withEditors(t, m, tree, split) }
	}
	inSidebar := func(setup func(*testing.T, *model)) func(*testing.T, *model) {
		return func(t *testing.T, m *model) {
			setup(t, m)
			m.toSidebar()
		}
	}
	return []layoutCase{
		{"tree under the hosts, beside a file", 200, 60, withTree},
		{"tree hidden", 200, 60, func(t *testing.T, m *model) {
			withTree(t, m)
			m.toggleTree()
		}},
		{"no browser on the session", 200, 60, withShell},
		{"no session at all", 200, 60, func(*testing.T, *model) {}},
		{"the files tab and its preview", 200, 60, withFiles},
		{"the files tab, too narrow to dock", dockThreshold - 1, 34, withFiles},

		{"one column short of docking", dockThreshold - 1, 34, withTree},
		{"exactly docking's worth", dockThreshold, 34, withTree},
		{"one column over", dockThreshold + 1, 34, withTree},
		{"the classic 80 columns", 80, 24, withTree},
		{"the classic 80 columns, keyboard in the floating sidebar", 80, 24, inSidebar(withShell)},
		{"docked, keyboard in the sidebar", 120, 34, inSidebar(withTree)},

		{"split beside the tree", 200, 60, editors(true, true)},
		{"split with no tree", 200, 60, editors(false, true)},
		{"split, odd content width", 201, 60, editors(true, true)},
		{"unsplit editors", 200, 60, editors(true, false)},
		{"shell focused in a split session", 200, 20, withSplitShell},

		{"one column short of a split", splitThreshold + 1, 20, editors(false, true)},
		{"exactly a split's worth", splitThreshold + 2, 20, editors(false, true)},

		{"a tiny window", 40, 10, withTree},
		{"a tiny window, no session", 40, 10, func(*testing.T, *model) {}},
	}
}

// ---- reading the frame ----

// frameOf renders the model and hands back the screen with the styling stripped.
func frameOf(m *model) [][]rune {
	lines := strings.Split(m.View().Content, "\n")
	out := make([][]rune, len(lines))
	for i, ln := range lines {
		out[i] = []rune(ansi.Strip(ln))
	}
	return out
}

// boxes is every box the frame says is drawn, with the zone the pointer should find there,
// on top first: a floating sidebar is over the content.
func boxes(m *model) []box {
	var out []box
	add := func(r rect, z zone, right bool) {
		if !r.empty() {
			out = append(out, box{r: r, z: z, right: right})
		}
	}
	add(m.frame.list, zoneList, false)
	add(m.frame.tree, zoneTree, false)
	add(m.frame.drawer, zoneDrawer, false)
	add(m.frame.left, zonePane, false)
	if !m.frame.right.empty() {
		add(m.frame.right, zonePane, true)
	}
	return out
}

// boxAt returns the top box containing the cell, or false for a cell no box covers.
func boxAt(bs []box, x, y int) (box, bool) {
	for _, b := range bs {
		if b.r.contains(x, y) {
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

			lines := strings.Split(m.View().Content, "\n")
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

		for i, ln := range strings.Split(m.View().Content, "\n") {
			if got := lipgloss.Width(ln); got != w {
				t.Fatalf("a %d-column window renders line %d at %d cells, want %d", w, i, got, w)
			}
		}
	}
}

// Where the sidebar floats, it is what gives way, and only when it has to; the content box
// always reaches the window's edge.
func TestTheFloatingSidebarYieldsBeforeTheFrameOverruns(t *testing.T) {
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
		m.toSidebar()
		if got := m.frame.list.w; got != c.wantList {
			t.Errorf("at %d columns the list is %d wide, want %d", c.w, got, c.wantList)
		}
		if got := m.frame.content.x + m.frame.content.w; got != c.w {
			t.Errorf("at %d columns the content box ends at %d, want the window edge", c.w, got)
		}
	}
}

// Every box the frame names is drawn where it says: its four corners are on screen at its
// four corners. The frame is what the pointer reads, so this is drawing and clicks agreeing.
func TestDrawnBoxesMatchTheLayout(t *testing.T) {
	for _, c := range layoutCases() {
		t.Run(c.name, func(t *testing.T) {
			m := viewModel(c.w, c.h)
			c.setup(t, m)
			screen := frameOf(m)

			for _, b := range boxes(m) {
				r := b.r
				for _, corner := range []struct {
					x, y int
					want rune
				}{
					{r.x, r.y, '╭'}, {r.x + r.w - 1, r.y, '╮'},
					{r.x, r.y + r.h - 1, '╰'}, {r.x + r.w - 1, r.y + r.h - 1, '╯'},
				} {
					if got := screen[corner.y][corner.x]; got != corner.want {
						t.Fatalf("%dx%d: the %v box %+v has %q at (%d, %d), want %q:\n%s",
							c.w, c.h, b.z, r, got, corner.x, corner.y, corner.want, string(screen[corner.y]))
					}
				}
			}
		})
	}
}

// ---- the renderer and the hit-testing agree ----

// Every body cell reports the zone of the box drawn there; a cell no box covers falls to the
// content area.
func TestZoneAtMatchesTheDrawnBoxes(t *testing.T) {
	for _, c := range layoutCases() {
		t.Run(c.name, func(t *testing.T) {
			m := viewModel(c.w, c.h)
			c.setup(t, m)
			bs := boxes(m)

			for y := 0; y < m.bodyHeight(); y++ {
				for x := 0; x < m.width; x++ {
					want := zonePane
					if b, ok := boxAt(bs, x, y); ok {
						want = b.z
					}
					if got := m.zoneAt(x, y); got != want {
						t.Fatalf("%dx%d: zoneAt(%d, %d) = %v, want %v", c.w, c.h, x, y, got, want)
					}
				}
			}
			if got := m.zoneAt(0, m.bodyHeight()); got != zoneFooter {
				t.Fatalf("the row under the body is %v, want zoneFooter", got)
			}
			if got := m.zoneAt(m.width, 0); got != zoneNone {
				t.Fatalf("a column past the window is %v, want zoneNone", got)
			}
		})
	}
}

// treeLocal answers for the inside of the tree box and nowhere else.
func TestTreeLocalCoversTheTreeBoxInterior(t *testing.T) {
	for _, c := range layoutCases() {
		t.Run(c.name, func(t *testing.T) {
			m := viewModel(c.w, c.h)
			c.setup(t, m)
			tree := m.frame.tree

			for y := 0; y < m.bodyHeight(); y++ {
				for x := 0; x < m.width; x++ {
					lx, ly, ok := m.treeLocal(x, y)
					want := !tree.empty() && x > tree.x && x < tree.x+tree.w-1 && y > tree.y && y < tree.y+tree.h-1
					if ok != want {
						t.Fatalf("%dx%d: treeLocal(%d, %d) ok = %v, want %v", c.w, c.h, x, y, ok, want)
					}
					if ok && (lx != x-tree.x-1 || ly != y-tree.y-1) {
						t.Fatalf("%dx%d: treeLocal(%d, %d) = (%d, %d), want (%d, %d)",
							c.w, c.h, x, y, lx, ly, x-tree.x-1, y-tree.y-1)
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

			for y := 0; y < m.bodyHeight(); y++ {
				for x := 0; x < m.width; x++ {
					gotRight, lx, ly, ok := m.contentLocal(x, y)

					var in box
					inside := false
					for _, b := range boxes(m) {
						r := b.r
						if b.z == zonePane && x > r.x && x < r.x+r.w-1 && y > r.y && y < r.y+r.h-1 {
							in, inside = b, true
						}
					}
					if ok != inside {
						t.Fatalf("%dx%d: contentLocal(%d, %d) ok = %v, want %v", c.w, c.h, x, y, ok, inside)
					}
					if ok && (gotRight != in.right || lx != x-in.r.x-1 || ly != y-in.r.y-1) {
						t.Fatalf("%dx%d: contentLocal(%d, %d) = (%v, %d, %d), want (%v, %d, %d)",
							c.w, c.h, x, y, gotRight, lx, ly, in.right, x-in.r.x-1, y-in.r.y-1)
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

	base, w := m.frame.content.x, m.splitHalf()
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

// Below the width that can pay for even a floating sidebar, there is none to show.
func TestTooNarrowForTheSidebarReadsAsCollapsed(t *testing.T) {
	for _, w := range []int{24, 27} {
		m := viewModel(w, 12)
		withShell(t, m)
		m.toSidebar()

		if m.sidebarOn() {
			t.Fatalf("at %d columns the sidebar reports itself on screen", w)
		}
		if screen := ansi.Strip(m.View().Content); strings.Contains(screen, "HOSTS") {
			t.Fatalf("at %d columns the host list is drawn after all", w)
		}
	}
}

// The threshold is a threshold: one column over it the floating sidebar is back.
func TestTheSidebarComesBackWhenTheWindowCanPayForIt(t *testing.T) {
	m := viewModel(24, 12)
	withShell(t, m)
	m.toSidebar()
	if m.sidebarOn() {
		t.Fatal("24 columns cannot pay for the sidebar")
	}

	m.update(tea.WindowSizeMsg{Width: 28, Height: 12})
	m.recomputeLayout()

	if !m.sidebarOn() || !m.frame.floats {
		t.Fatal("28 columns can pay for a floating sidebar and it did not come back")
	}
	if got := m.frame.list.w; got != 16 {
		t.Fatalf("the restored list is %d columns wide, want its floor of 16", got)
	}
	if !strings.Contains(ansi.Strip(m.View().Content), "HOSTS") {
		t.Fatal("the restored list is not on screen")
	}
}
