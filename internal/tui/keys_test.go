package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// key builds the tea.KeyMsg whose String() is name.
func key(t *testing.T, name string) tea.KeyMsg {
	t.Helper()
	switch name {
	case "ctrl+d":
		return tea.KeyMsg{Type: tea.KeyCtrlD}
	case "ctrl+u":
		return tea.KeyMsg{Type: tea.KeyCtrlU}
	case "ctrl+f":
		return tea.KeyMsg{Type: tea.KeyCtrlF}
	case "ctrl+b":
		return tea.KeyMsg{Type: tea.KeyCtrlB}
	case "up":
		return tea.KeyMsg{Type: tea.KeyUp}
	case "down":
		return tea.KeyMsg{Type: tea.KeyDown}
	case "left":
		return tea.KeyMsg{Type: tea.KeyLeft}
	case "right":
		return tea.KeyMsg{Type: tea.KeyRight}
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}
	case "esc":
		return tea.KeyMsg{Type: tea.KeyEsc}
	default:
		if len([]rune(name)) != 1 {
			t.Fatalf("key: unsupported name %q", name)
		}
		return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(name)}
	}
}

// newNavModel builds a model in navigation mode with n hosts in the filtered
// list and a viewport where listRows() == 15.
func newNavModel(n int) *model {
	filtered := make([]int, n)
	for i := range filtered {
		filtered[i] = i
	}
	return &model{filtered: filtered, height: 20}
}

func TestNavVimMotions(t *testing.T) {
	cases := []struct {
		name string
		keys []string
		want int
	}{
		{"j moves down", []string{"j"}, 1},
		{"k clamps at top", []string{"k", "k"}, 0},
		{"j clamps at bottom", []string{"G", "j"}, 29},
		{"G jumps to last", []string{"G"}, 29},
		{"gg jumps to first", []string{"G", "g", "g"}, 0},
		{"lone g is inert", []string{"j", "j", "g"}, 2},
		{"g then other key cancels", []string{"G", "g", "j", "g"}, 29},
		{"H jumps to first", []string{"G", "H"}, 0},
		{"L jumps to last", []string{"L"}, 29},
		{"M jumps to middle", []string{"M"}, 15},
		{"ctrl+d half page", []string{"ctrl+d"}, 7},
		{"ctrl+u half page back", []string{"G", "ctrl+u"}, 22},
		{"ctrl+f full page", []string{"ctrl+f"}, 15},
		{"ctrl+b full page back", []string{"G", "ctrl+b"}, 14},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := newNavModel(30)
			for _, k := range tc.keys {
				m.handleKey(key(t, k))
			}
			if m.cursor != tc.want {
				t.Fatalf("cursor = %d, want %d", m.cursor, tc.want)
			}
		})
	}
}

// An empty host list must not drive the cursor negative.
func TestNavMotionsOnEmptyList(t *testing.T) {
	for _, k := range []string{"G", "L", "M", "j", "ctrl+d", "ctrl+f"} {
		t.Run(k, func(t *testing.T) {
			m := newNavModel(0)
			m.handleKey(key(t, k))
			if m.cursor != 0 {
				t.Fatalf("cursor = %d, want 0", m.cursor)
			}
		})
	}
}

// left/h/esc are the back key in navigation mode: they drop the details view.
func TestNavBackKeys(t *testing.T) {
	for _, k := range []string{"esc", "left", "h"} {
		t.Run(k, func(t *testing.T) {
			// Navigation mode showing a host's details: active is set, but
			// neither browsing nor focused (those modes swallow keys first).
			m := newNavModel(30)
			m.active = "web1"
			m.status = "connected to web1"

			m.handleKey(key(t, k))

			if m.active != "" {
				t.Fatalf("active = %q, want empty", m.active)
			}
			if m.status != "" {
				t.Fatalf("status = %q, want empty", m.status)
			}
		})
	}
}

// enter/right/l are the forward key: with no host under the cursor they must be
// inert rather than panic.
func TestNavForwardKeysOnEmptyList(t *testing.T) {
	for _, k := range []string{"enter", "right", "l"} {
		t.Run(k, func(t *testing.T) {
			m := newNavModel(0)
			m.connecting = map[string]bool{}
			if _, cmd := m.handleKey(key(t, k)); cmd != nil {
				t.Fatal("got a connect command for an empty list, want none")
			}
		})
	}
}

// A "g" typed into the filter is literal text, not the start of a "gg" motion.
func TestFilterSwallowsG(t *testing.T) {
	m := newNavModel(30)
	m.filtering = true
	m.handleKey(key(t, "g"))

	if m.filter != "g" {
		t.Fatalf("filter = %q, want %q", m.filter, "g")
	}
	if m.pendingG {
		t.Fatal("pendingG set while filtering, want false")
	}
}
