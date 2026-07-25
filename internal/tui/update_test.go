package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
)

// The footer mentions a newer release once the startup check reports one, and
// says nothing at all when hop is current.
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

// The hint is news, not a key, so it leads the legend — otherwise the
// truncation that trims a long legend on a narrow window would be exactly what
// drops it.
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

// An empty version — no update, or a check that failed or was disabled — must
// leave the footer exactly as it was.
func TestNoUpdateHintWhenCurrent(t *testing.T) {
	m := viewModel(120, 34)
	before := m.renderFooter()
	m.update(updateAvailableMsg{latest: ""})
	if got := m.renderFooter(); got != before {
		t.Errorf("footer changed on an empty update result:\n got %q\nwant %q", got, before)
	}
}

// The pane-focused footer is the remote shell's key legend; an update hint has
// no business competing with it while keystrokes are going to another machine.
func TestNoUpdateHintWhileFocused(t *testing.T) {
	m := viewModel(120, 34)
	m.update(updateAvailableMsg{latest: "9.9.9"})
	m.active, m.focused = "web1", true
	if got := m.renderFooter(); strings.Contains(got, "9.9.9") {
		t.Errorf("focused footer should stay a key legend, got %q", got)
	}
}
