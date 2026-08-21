package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
)

func TestFooterShowsUpdateHint(t *testing.T) {
	m := viewModel(120, 34)
	if got := m.renderFooter(); strings.Contains(got, "available") {
		t.Errorf("footer advertised an update before any check: %q", got)
	}

	m.update(updateAvailableMsg{latest: "9.9.9"})
	got := m.renderFooter()
	if !strings.Contains(got, "9.9.9") {
		t.Errorf("footer = %q, want it to mention 9.9.9", got)
	}
	if !strings.Contains(got, "hop self-update") {
		t.Errorf("footer = %q, want it to name the update command", got)
	}
}

// The hint leads the legend so narrow-window truncation cannot drop it.
func TestUpdateHintSurvivesNarrowWindow(t *testing.T) {
	m := viewModel(60, 16)
	m.update(updateAvailableMsg{latest: "9.9.9"})
	got := m.renderFooter()
	if !strings.Contains(got, "9.9.9") {
		t.Errorf("footer on a narrow window = %q, want it to still mention 9.9.9", got)
	}
	if w := lipgloss.Width(got); w > 60 {
		t.Errorf("footer width = %d, must not exceed the window (60)", w)
	}
}

// An empty version means no update, a failed check, or one that was disabled.
func TestNoUpdateHintWhenCurrent(t *testing.T) {
	m := viewModel(120, 34)
	before := m.renderFooter()
	m.update(updateAvailableMsg{latest: ""})
	if got := m.renderFooter(); got != before {
		t.Errorf("footer changed on an empty update result:\n got %q\nwant %q", got, before)
	}
}

func TestNoUpdateHintWhileFocused(t *testing.T) {
	m := viewModel(120, 34)
	m.update(updateAvailableMsg{latest: "9.9.9"})
	m.active, m.mode = "web1", modeShell
	if got := m.renderFooter(); strings.Contains(got, "9.9.9") {
		t.Errorf("focused footer should stay a key legend, got %q", got)
	}
}
