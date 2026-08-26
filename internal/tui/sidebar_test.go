package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"hop/internal/keys"
)

// toggleKey is the sidebar chord as the terminal delivers it: a control byte.
func toggleKey() tea.KeyPressMsg {
	return tea.KeyPressMsg{Code: 'b', Mod: tea.ModCtrl}
}

func TestSidebarToggleKey(t *testing.T) {
	if got := toggleKey().String(); got != keys.Defaults().Keycap(keys.Sidebar) {
		t.Fatalf("the test's key renders as %q, want %q", got, keys.Defaults().Keycap(keys.Sidebar))
	}
}

// Collapsing gives every sidebar column to the pane; toggling back restores them.
func TestSidebarToggleGivesColumnsToThePane(t *testing.T) {
	m := viewModel(120, 34)
	wide, listW := m.paneW, m.listWidth()
	if listW == 0 {
		t.Fatal("the sidebar starts collapsed, want it open")
	}

	m.handleKey(toggleKey())

	if !m.sidebarHidden {
		t.Fatal("ctrl+b did not collapse the sidebar")
	}
	if got := m.listWidth(); got != 0 {
		t.Fatalf("collapsed listWidth = %d, want 0", got)
	}
	if want := wide + listW; m.paneW != want {
		t.Fatalf("collapsed paneW = %d, want %d — the pane did not take the freed columns", m.paneW, want)
	}

	m.handleKey(toggleKey())

	if m.sidebarHidden {
		t.Fatal("a second ctrl+b did not bring the sidebar back")
	}
	if m.paneW != wide || m.listWidth() != listW {
		t.Fatalf("restored paneW/listWidth = %d/%d, want %d/%d", m.paneW, m.listWidth(), wide, listW)
	}
}

func TestSidebarCollapsedRendersNoHostList(t *testing.T) {
	m := viewModel(120, 34)
	if !strings.Contains(m.View().Content, "HOSTS") {
		t.Fatal("the open sidebar does not render its HOSTS title; the test is not looking at the right thing")
	}

	m.handleKey(toggleKey())

	if strings.Contains(m.View().Content, "HOSTS") {
		t.Fatal("the collapsed sidebar is still on screen")
	}
	if !strings.Contains(m.View().Content, "show hosts") {
		t.Fatal("the footer does not say how to bring the sidebar back")
	}
}

// The toggle is layout state: a resize while collapsed keeps it collapsed.
func TestSidebarSurvivesResize(t *testing.T) {
	m := viewModel(120, 34)
	m.handleKey(toggleKey())

	m.update(tea.WindowSizeMsg{Width: 100, Height: 30})

	if !m.sidebarHidden {
		t.Fatal("a resize brought the sidebar back")
	}
	if want := 100 - 2; m.paneW != want {
		t.Fatalf("paneW = %d after resizing while collapsed, want %d", m.paneW, want)
	}
}

// The toggle is held in every mode below the cards, the focused shell included.
func TestSidebarTogglesFromEveryMode(t *testing.T) {
	modes := map[string]func(m *model){
		"navigation": func(m *model) {},
		"shell":      func(m *model) { m.active, m.mode = "web1", modeShell },
		"browser":    func(m *model) { m.active, m.mode = "web1", modeBrowser },
		"editor":     func(m *model) { m.active, m.mode = "web1", modeEditor },
		"filter":     func(m *model) { m.filtering = true },
		"scrollback": func(m *model) { m.active, m.mode = "web1", modeScrollback },
	}
	for name, setup := range modes {
		t.Run(name, func(t *testing.T) {
			m := viewModel(120, 34)
			setup(m)

			m.handleKey(toggleKey())

			if !m.sidebarHidden {
				t.Fatalf("ctrl+b did not collapse the sidebar in %s mode", name)
			}
			if m.focused() != (name == "shell" || name == "scrollback") ||
				m.browsing() != (name == "browser") || m.editing() != (name == "editor") {
				t.Fatalf("ctrl+b changed the mode in %s", name)
			}
		})
	}
}

func TestSidebarToggleIsSwallowedByCards(t *testing.T) {
	cards := map[string]func(m *model){
		"help":     func(m *model) { m.help = true },
		"settings": func(m *model) { m.openSettings() },
		"add host": func(m *model) { m.openHostFormAdd() },
		"import":   func(m *model) { m.openImport(false) },
	}
	for name, open := range cards {
		t.Run(name, func(t *testing.T) {
			m := viewModel(120, 34)
			open(m)

			m.handleKey(toggleKey())

			if m.sidebarHidden {
				t.Fatalf("ctrl+b reached past the %s card", name)
			}
		})
	}
}

func TestSidebarToggleBreaksTheEscChord(t *testing.T) {
	m := newPaneModel()

	m.handleKey(key(t, "esc"))
	m.handleKey(toggleKey())
	if m.reader.Pending() {
		t.Fatal("ctrl+b left the pending esc armed")
	}

	m.handleKey(key(t, "esc"))
	if !m.focused() {
		t.Fatal("esc-ctrl+b-esc left the pane, want it treated as two lone escs")
	}
}

// columnModel is statusModel with an SFTP browser open, which puts the tree column up.
func columnModel(t *testing.T, w, h int) (*model, *session) {
	t.Helper()
	m, s := statusModel(t, w, h)
	s.browser = fakeBrowser(t, "/srv")
	m.mode = modeBrowser
	m.relayout()
	return m, s
}

// The tree column is all or nothing: its preferred width, or two columns instead.
func TestColumnWidthsAcrossTerminalWidths(t *testing.T) {
	cases := []struct {
		name    string
		w       int
		hideBar bool
		tree    int
		paneW   int
		inline  bool
	}{
		{"a wide terminal holds all three", 200, false, treeColWidth, 200 - sidebarWidth - treeColWidth - 2, false},
		{"exactly at the threshold", sidebarWidth + treeColWidth + minContentWidth, false,
			treeColWidth, minContentWidth - 2, false},
		{"one column short of it falls back", sidebarWidth + treeColWidth + minContentWidth - 1, false,
			0, sidebarWidth + treeColWidth + minContentWidth - 1 - sidebarWidth - 2, true},
		{"the classic 80 columns falls back", 80, false, 0, 80 - sidebarWidth - 2, true},
		// The threshold is measured against what is left after the sidebar.
		{"collapsing the sidebar buys the column", 120, true, treeColWidth, 120 - treeColWidth - 2, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m, _ := columnModel(t, c.w, 34)
			if c.hideBar {
				m.toggleSidebar()
			}
			if got := m.treeWidth(); got != c.tree {
				t.Fatalf("treeWidth = %d, want %d", got, c.tree)
			}
			if m.paneW != c.paneW {
				t.Fatalf("paneW = %d, want %d", m.paneW, c.paneW)
			}
			if got := m.treeInline(); got != c.inline {
				t.Fatalf("treeInline = %v, want %v", got, c.inline)
			}
		})
	}
}

// With no browser there is no tree column: the content area takes the whole row.
func TestNoBrowserCostsNoColumn(t *testing.T) {
	m, s := statusModel(t, 200, 34)
	m.relayout()
	if m.treeWidth() != 0 || m.hasTree() {
		t.Fatalf("treeWidth = %d, hasTree = %v; a session with no browser has no column",
			m.treeWidth(), m.hasTree())
	}
	if want := 200 - sidebarWidth - 2; m.paneW != want {
		t.Fatalf("paneW = %d, want the whole row (%d)", m.paneW, want)
	}

	s.browser = fakeBrowser(t, "/srv")
	m.relayout()
	if m.treeWidth() != treeColWidth {
		t.Fatalf("treeWidth = %d after a browser opened, want %d", m.treeWidth(), treeColWidth)
	}
}

// The tree column collapses and restores on the same terms the host list does.
func TestTreeColumnToggleGivesColumnsToTheContent(t *testing.T) {
	m, _ := columnModel(t, 200, 34)
	wide, treeW := m.paneW, m.treeWidth()
	if treeW == 0 {
		t.Fatal("the column starts collapsed, want it open")
	}

	m.toggleTree()

	if got := m.treeWidth(); got != 0 {
		t.Fatalf("collapsed treeWidth = %d, want 0", got)
	}
	if want := wide + treeW; m.paneW != want {
		t.Fatalf("collapsed paneW = %d, want %d — the content did not take the freed columns", m.paneW, want)
	}
	// Collapsed, the browser falls back into the content area.
	if !m.treeInline() {
		t.Fatal("a collapsed column left the browser with nowhere to be drawn")
	}

	m.toggleTree()

	if m.paneW != wide || m.treeWidth() != treeW {
		t.Fatalf("restored paneW/treeWidth = %d/%d, want %d/%d", m.paneW, m.treeWidth(), wide, treeW)
	}
}

// Both halves of a split are the same width; the odd column is given up.
func TestSplitHalvesTheContentArea(t *testing.T) {
	m, s := columnModel(t, 200, 34)
	full, _ := m.editorSize(s)
	if full != m.paneW {
		t.Fatalf("an unsplit content area gives an editor %d columns, want the whole %d", full, m.paneW)
	}

	s.openSplit()
	half, _ := m.editorSize(s)
	if want := m.splitHalf(); half != want {
		t.Fatalf("a split gives an editor %d columns, want %d", half, want)
	}
	if 2*(half+2) > m.paneW+2 {
		t.Fatalf("two %d-wide halves do not fit the %d-column content area", half+2, m.paneW+2)
	}
}

func TestSplitRefusesANarrowContentArea(t *testing.T) {
	m, _ := columnModel(t, 200, 34)
	if !m.splitFits() {
		t.Fatal("a 200-column window cannot be split, so this test proves nothing")
	}
	m.paneW = 2*minSplitHalf - 3
	if m.splitFits() {
		t.Fatalf("a %d-column content area claims to fit two halves of %d", m.paneW, minSplitHalf)
	}
}
