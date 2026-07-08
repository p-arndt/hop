package tui

import (
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// key builds the tea.KeyMsg whose String() is name.
func key(t *testing.T, name string) tea.KeyMsg {
	t.Helper()
	switch name {
	case "ctrl+o":
		return tea.KeyMsg{Type: tea.KeyCtrlO}
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

// newPaneModel builds a model focused on a terminal pane. The session map is
// empty, so key forwarding is a no-op and we can assert purely on mode changes.
func newPaneModel() *model {
	return &model{
		active:   "web1",
		focused:  true,
		sessions: map[string]*session{},
		height:   20,
	}
}

func TestPaneDoubleEscLeaves(t *testing.T) {
	m := newPaneModel()

	m.handleKey(key(t, "esc"))
	if !m.focused {
		t.Fatal("a single esc left the pane, want it forwarded to the shell")
	}
	if m.lastEsc.IsZero() {
		t.Fatal("first esc did not arm the double-esc window")
	}

	m.handleKey(key(t, "esc"))
	if m.focused {
		t.Fatal("double esc did not leave the pane")
	}
	if !m.lastEsc.IsZero() {
		t.Fatal("lastEsc not reset on leaving the pane")
	}
}

// Two escs further apart than the window are two independent escapes for the
// remote shell, not a "leave" chord.
func TestPaneSlowEscsStayInPane(t *testing.T) {
	m := newPaneModel()

	m.handleKey(key(t, "esc"))
	m.lastEsc = time.Now().Add(-2 * doubleEscWindow) // as if the user paused
	m.handleKey(key(t, "esc"))

	if !m.focused {
		t.Fatal("two slow escs left the pane, want both forwarded to the shell")
	}
	if m.lastEsc.IsZero() {
		t.Fatal("the second esc should re-arm the window")
	}
}

// An intervening key breaks the sequence: esc, j, esc is not a double-esc.
func TestPaneEscSequenceBrokenByOtherKey(t *testing.T) {
	m := newPaneModel()

	m.handleKey(key(t, "esc"))
	m.handleKey(key(t, "j"))
	if !m.lastEsc.IsZero() {
		t.Fatal("an intervening key did not clear the pending esc")
	}

	m.handleKey(key(t, "esc"))
	if !m.focused {
		t.Fatal("esc-j-esc left the pane, want it treated as two lone escs")
	}
}

// ctrl+o still leaves the pane, and clears any half-finished esc chord.
func TestPaneCtrlOLeaves(t *testing.T) {
	m := newPaneModel()

	m.handleKey(key(t, "esc"))
	m.handleKey(key(t, "ctrl+o"))

	if m.focused {
		t.Fatal("ctrl+o did not leave the pane")
	}
	if !m.lastEsc.IsZero() {
		t.Fatal("lastEsc not reset on leaving the pane")
	}
}

// The double-esc chord belongs to the terminal pane only; in the browser, esc is
// the browser's own key and must not pop the mode.
func TestBrowsingEscIsNotADoubleEscChord(t *testing.T) {
	m := &model{active: "web1", browsing: true, sessions: map[string]*session{}, height: 20}

	m.handleKey(key(t, "esc"))
	m.handleKey(key(t, "esc"))

	if !m.browsing {
		t.Fatal("double esc left the browser, want the chord to be pane-only")
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
