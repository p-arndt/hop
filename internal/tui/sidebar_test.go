package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// toggleKey is the sidebar chord as the terminal delivers it: a control byte, not
// the meta escape an alt+letter would need the terminal to be configured for. See
// toggleSidebarKey.
func toggleKey() tea.KeyMsg {
	return tea.KeyMsg{Type: tea.KeyCtrlB}
}

func TestSidebarToggleKey(t *testing.T) {
	if got := toggleKey().String(); got != toggleSidebarKey {
		t.Fatalf("the test's key renders as %q, want %q", got, toggleSidebarKey)
	}
}

// Collapsed, the sidebar costs nothing: its width is zero and every column it held
// goes to the pane. Toggling back restores exactly what was there before.
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

// The host list is not drawn narrow while collapsed — it is not drawn at all.
func TestSidebarCollapsedRendersNoHostList(t *testing.T) {
	m := viewModel(120, 34)
	if !strings.Contains(m.View(), "HOSTS") {
		t.Fatal("the open sidebar does not render its HOSTS title; the test is not looking at the right thing")
	}

	m.handleKey(toggleKey())

	if strings.Contains(m.View(), "HOSTS") {
		t.Fatal("the collapsed sidebar is still on screen")
	}
	if !strings.Contains(m.View(), "show hosts") {
		t.Fatal("the footer does not say how to bring the sidebar back")
	}
}

// A window resize while collapsed keeps it collapsed, and the pane still gets the
// whole window — the toggle is layout state, not a one-off adjustment that the next
// WindowSizeMsg undoes.
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

// The toggle is the one binding hop holds in every mode below the cards: the point
// of it is the focused shell, where the remote program owns nearly every key.
func TestSidebarTogglesFromEveryMode(t *testing.T) {
	modes := map[string]func(m *model){
		"navigation": func(m *model) {},
		"shell":      func(m *model) { m.active, m.focused = "web1", true },
		"browser":    func(m *model) { m.active, m.browsing = "web1", true },
		"editor":     func(m *model) { m.active, m.editing = "web1", true },
		"filter":     func(m *model) { m.filtering = true },
		"scrollback": func(m *model) { m.active, m.focused, m.scrolling = "web1", true, true },
	}
	for name, setup := range modes {
		t.Run(name, func(t *testing.T) {
			m := viewModel(120, 34)
			setup(m)

			m.handleKey(toggleKey())

			if !m.sidebarHidden {
				t.Fatalf("ctrl+b did not collapse the sidebar in %s mode", name)
			}
			// Nothing else moved: the key is layout, not a way out of the mode.
			if m.focused != (name == "shell" || name == "scrollback") ||
				m.browsing != (name == "browser") || m.editing != (name == "editor") {
				t.Fatalf("ctrl+b changed the mode in %s", name)
			}
		})
	}
}

// A card takes every key while it is up, the toggle included: the sidebar is behind
// it, and a card that answers to some keys but not others is a card you cannot trust.
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

// The toggle breaks a half-typed double-esc, as every other non-esc key does.
func TestSidebarToggleBreaksTheEscChord(t *testing.T) {
	m := newPaneModel()

	m.handleKey(key(t, "esc"))
	m.handleKey(toggleKey())
	if !m.chords.esc.IsZero() {
		t.Fatal("ctrl+b left the pending esc armed")
	}

	m.handleKey(key(t, "esc"))
	if !m.focused {
		t.Fatal("esc-ctrl+b-esc left the pane, want it treated as two lone escs")
	}
}
