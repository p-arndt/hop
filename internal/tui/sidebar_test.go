package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// With no host in front the list is a column beside the details.
func TestLandingShowsTheListAsAColumn(t *testing.T) {
	m := viewModel(120, 34)

	if m.listWidth() == 0 || m.sidebarFloats() {
		t.Fatalf("listWidth = %d, floats = %v; want a column", m.listWidth(), m.sidebarFloats())
	}
	if want := 120 - m.sidebarPref() - 2; m.paneW != want {
		t.Fatalf("paneW = %d, want %d", m.paneW, want)
	}
}

// A focused shell has the whole width, with or without a browser open on the host.
func TestShellViewIsFullWidth(t *testing.T) {
	for _, withBrowser := range []bool{false, true} {
		m, s := statusModel(t, 160, 45)
		if withBrowser {
			s.browser = fakeBrowser(t, "/srv")
		}
		m.mode = modeShell
		m.relayout()

		if m.listWidth() != 0 || m.treeWidth() != 0 || !m.frame.list.empty() {
			t.Fatalf("browser=%v: list %d, tree %d; the shell view shows neither",
				withBrowser, m.listWidth(), m.treeWidth())
		}
		if w, _ := m.shellSize(1); m.paneW != 158 || w != 158 {
			t.Fatalf("browser=%v: paneW = %d, shell width = %d, want 158", withBrowser, m.paneW, w)
		}
	}
}

// Going to the host list and back must not resize a live shell: a remote vim would reflow.
func TestTheListFloatsOverTheShell(t *testing.T) {
	m, s := statusModel(t, 120, 34)
	m.mode = modeShell
	m.relayout()
	before := m.paneW

	m.leavePane()
	m.syncView()

	if !m.sidebarFloats() || m.frame.list.empty() {
		t.Fatal("the host list is not drawn over the shell")
	}
	if m.paneW != before {
		t.Fatalf("paneW went %d → %d on leaving the shell", before, m.paneW)
	}
	if w, _ := m.shellSize(len(s.shells)); w != before {
		t.Fatalf("the shell was resized to %d, want %d", w, before)
	}
	screen := m.View().Content
	if !strings.Contains(screen, "HOSTS") {
		t.Fatal("the floating list is not on screen")
	}
	if m.zoneAt(1, 3) != zoneList || m.zoneAt(110, 3) != zonePane {
		t.Fatal("the pointer does not find the list over the shell, or the shell beside it")
	}
}

// The files view is the tree beside the open files.
func TestFilesViewPutsTheTreeBesideTheFiles(t *testing.T) {
	m, s := statusModel(t, 160, 45)
	s.browser = fakeBrowser(t, "/srv")
	s.editors = []*editorTab{{id: 1, name: "a", path: "/srv/a", pane: fakePane()}}
	m.mode = modeEditor
	m.relayout()

	if got, want := m.treeWidth(), m.treeCol(); got != want || want != 40 {
		t.Fatalf("treeWidth = %d, treeCol = %d, want 40 at 160 columns", got, want)
	}
	if ew, _ := m.editorSize(s); ew != 160-40-2 || m.paneW != ew {
		t.Fatalf("editor width = %d, paneW = %d, want %d", ew, m.paneW, 160-40-2)
	}
	if bw, _ := m.browserSize(s); bw != 40-2 {
		t.Fatalf("browser width = %d, want the column's interior", bw)
	}
}

// At the window most people have, the tree still fits beside the file.
func TestFilesViewHasAColumnAt120(t *testing.T) {
	m, s := statusModel(t, 120, 34)
	s.browser = fakeBrowser(t, "/srv")
	s.editors = []*editorTab{{id: 1, name: "a", path: "/srv/a", pane: fakePane()}}
	m.mode = modeBrowser
	m.relayout()

	if m.treeWidth() != treeColMin || m.treeInline() {
		t.Fatalf("treeWidth = %d, inline = %v; want a %d-column tree", m.treeWidth(), m.treeInline(), treeColMin)
	}
}

// Alone, the browser fills the view: a column beside nothing is wasted.
func TestALoneBrowserFillsTheView(t *testing.T) {
	m, s := statusModel(t, 160, 45)
	s.browser = fakeBrowser(t, "/srv")
	m.mode = modeBrowser
	m.relayout()

	if m.treeWidth() != 0 || !m.treeInline() || !m.browserInContent(s) {
		t.Fatal("a browser with no open file is not drawn in the content area")
	}
	if bw, _ := m.browserSize(s); bw != 158 {
		t.Fatalf("browser width = %d, want 158", bw)
	}
}

// The host list remembers which view a host was in, and draws that view under itself.
func TestTheListShowsTheViewTheHostWasIn(t *testing.T) {
	m, s := statusModel(t, 160, 45)
	s.browser = fakeBrowser(t, "/srv")
	m.mode = modeBrowser
	m.syncView()

	m.leaveBrowser()
	m.syncView()

	if !m.filesView() || !m.browserInContent(s) {
		t.Fatal("leaving the browser for the list swapped the files view for the shell")
	}

	m.focusShell("web1")
	m.syncView()
	m.leavePane()
	m.syncView()
	if m.filesView() {
		t.Fatal("leaving the shell for the list showed the files view")
	}
}

// Below the tree's floor plus the files' floor the column gives way.
func TestNarrowFilesViewDropsTheColumn(t *testing.T) {
	m, s := statusModel(t, treeColMin+minContentWidth-1, 34)
	s.browser = fakeBrowser(t, "/srv")
	s.editors = []*editorTab{{id: 1, name: "a", path: "/srv/a", pane: fakePane()}}
	m.mode = modeBrowser
	m.relayout()

	if m.treeWidth() != 0 || !m.treeInline() {
		t.Fatalf("treeWidth = %d at %d columns, want the browser inline", m.treeWidth(), m.width)
	}
}

func TestTreeColumnToggleGivesColumnsToTheFiles(t *testing.T) {
	m, s := statusModel(t, 200, 34)
	s.browser = fakeBrowser(t, "/srv")
	s.editors = []*editorTab{{id: 1, name: "a", path: "/srv/a", pane: fakePane()}}
	m.mode = modeEditor
	m.relayout()
	wide, treeW := m.paneW, m.treeWidth()

	m.toggleTree()

	if m.treeWidth() != 0 || m.paneW != wide+treeW {
		t.Fatalf("collapsed treeWidth/paneW = %d/%d, want 0/%d", m.treeWidth(), m.paneW, wide+treeW)
	}

	m.toggleTree()

	if m.paneW != wide || m.treeWidth() != treeW {
		t.Fatalf("restored paneW/treeWidth = %d/%d, want %d/%d", m.paneW, m.treeWidth(), wide, treeW)
	}
}

// Both halves of a split are the same width; the odd column is given up.
func TestSplitHalvesTheFiles(t *testing.T) {
	m, s := statusModel(t, 200, 34)
	s.browser = fakeBrowser(t, "/srv")
	s.editors = []*editorTab{{id: 1, pane: fakePane()}, {id: 2, pane: fakePane()}}
	m.mode = modeEditor
	m.relayout()
	full, _ := m.editorSize(s)
	if full != m.paneW {
		t.Fatalf("an unsplit files view gives an editor %d columns, want the whole %d", full, m.paneW)
	}

	s.openSplit()
	half, _ := m.editorSize(s)
	if want := splitHalfOf(full); half != want {
		t.Fatalf("a split gives an editor %d columns, want %d", half, want)
	}
	if 2*(half+2) > full+2 {
		t.Fatalf("two %d-wide halves do not fit %d columns", half+2, full+2)
	}
}

func TestSplitRefusesANarrowFilesView(t *testing.T) {
	if !splitFitsIn(2*minSplitHalf - 2) {
		t.Fatal("exactly two halves' worth does not fit")
	}
	if splitFitsIn(2*minSplitHalf - 3) {
		t.Fatal("one column short still claims to fit two halves")
	}
}

// ctrl+b is no longer hop's: a shell pane hands it to the remote, where tmux wants it.
func TestCtrlBReachesTheRemote(t *testing.T) {
	m := newPaneModel()

	m.handleKey(tea.KeyPressMsg{Code: 'b', Mod: tea.ModCtrl})

	if !m.focused() || m.listWidth() != 0 {
		t.Fatal("ctrl+b did something in hop")
	}
}

// Leaving a pane hands the keyboard to the host list, so the list has to be on screen.
func TestLeavingAPaneShowsTheList(t *testing.T) {
	leaves := map[string]func(m *model){
		"pane":    (*model).leavePane,
		"browser": (*model).leaveBrowser,
		"details": (*model).leaveDetails,
		"all":     (*model).leaveAll,
	}
	for name, leave := range leaves {
		t.Run(name, func(t *testing.T) {
			m := viewModel(120, 34)
			m.active, m.mode = "web1", modeShell

			leave(m)

			if m.mode != modeList || !m.sidebarOn() {
				t.Fatalf("leave %s: mode %v, list on %v; want the list on screen", name, m.mode, m.sidebarOn())
			}
		})
	}
}

// Too narrow to hold the list there is nothing to show, and the keys stay quiet.
func TestNarrowWindowLeavesTheListOff(t *testing.T) {
	m := viewModel(24, 24)
	if m.sidebarFits() {
		t.Fatal("a 24-column window fits the sidebar, so this test proves nothing")
	}
	m.active, m.mode = "web1", modeShell

	m.leavePane()

	if m.sidebarOn() {
		t.Fatal("the list claims to be on screen in a window too narrow for it")
	}
	m.cursor = 1
	m.handleKey(key(t, "e"))
	m.handleKey(key(t, "down"))
	if m.hostForm.open || m.cursor != 1 {
		t.Fatalf("form=%v cursor=%d, want no key to reach the off-screen list", m.hostForm.open, m.cursor)
	}
}

// columnModel is statusModel with an SFTP browser open and the keyboard in it.
func columnModel(t *testing.T, w, h int) (*model, *session) {
	t.Helper()
	m, s := statusModel(t, w, h)
	s.browser = fakeBrowser(t, "/srv")
	m.mode = modeBrowser
	m.relayout()
	return m, s
}
