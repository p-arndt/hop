package tui

import (
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"hop/internal/config"
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
	case "shift+up":
		return tea.KeyMsg{Type: tea.KeyShiftUp}
	case "shift+down":
		return tea.KeyMsg{Type: tea.KeyShiftDown}
	case "left":
		return tea.KeyMsg{Type: tea.KeyLeft}
	case "right":
		return tea.KeyMsg{Type: tea.KeyRight}
	case "pgup":
		return tea.KeyMsg{Type: tea.KeyPgUp}
	case "pgdown":
		return tea.KeyMsg{Type: tea.KeyPgDown}
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}
	case "esc":
		return tea.KeyMsg{Type: tea.KeyEsc}
	case "backspace":
		return tea.KeyMsg{Type: tea.KeyBackspace}
	case "ctrl+c":
		return tea.KeyMsg{Type: tea.KeyCtrlC}
	default:
		if len([]rune(name)) != 1 {
			t.Fatalf("key: unsupported name %q", name)
		}
		return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(name)}
	}
}

// newNavModel builds a model in navigation mode with n hosts in the filtered
// list and a viewport where listRows() == 15. The vim motions are switched on:
// they are what most of these tests are about, and they are off by default.
func newNavModel(n int) *model {
	filtered := make([]int, n)
	for i := range filtered {
		filtered[i] = i
	}
	return &model{filtered: filtered, height: 20, cfg: config.Config{VimKeys: true}}
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

// With the setting off, every vim motion is inert: the letters are not bound to
// anything, so a user who never asked for vim cannot move (or leave) by typing one.
func TestNavVimMotionsOffByDefault(t *testing.T) {
	for _, k := range []string{"j", "k", "G", "H", "M", "L", "ctrl+d", "ctrl+u", "ctrl+f", "ctrl+b"} {
		t.Run(k, func(t *testing.T) {
			m := newNavModel(30)
			m.cfg.VimKeys = false
			m.cursor = 5
			m.active = "web"

			m.handleKey(key(t, k))

			if m.cursor != 5 {
				t.Fatalf("%q moved the cursor to %d; with vim keys off it must do nothing", k, m.cursor)
			}
			if m.active != "web" {
				t.Fatalf("%q left the active host; with vim keys off it must do nothing", k)
			}
		})
	}

	// "gg" is two keys, and with the setting off the first must not quietly arm a
	// chord that a later "g" completes — least of all after the setting is switched
	// back on, which would make turning it on jump the cursor by itself.
	m := newNavModel(30)
	m.cfg.VimKeys = false
	m.cursor = 5

	m.handleKey(key(t, "g"))
	m.handleKey(key(t, "g"))
	if m.cursor != 5 {
		t.Fatalf("gg jumped to %d with vim keys off", m.cursor)
	}

	m.handleKey(key(t, "g"))
	m.cfg.VimKeys = true
	m.handleKey(key(t, "g"))
	if m.cursor != 5 {
		t.Fatalf("a g typed while off was completed by the first g typed after on: cursor = %d", m.cursor)
	}
}

// Turning vim keys off must not cost you a way to move: the arrows, enter and the
// page keys are bound either way.
func TestNavPlainKeysWorkWithoutVim(t *testing.T) {
	m := newNavModel(30)
	m.cfg.VimKeys = false

	m.handleKey(key(t, "down"))
	if m.cursor != 1 {
		t.Fatalf("cursor = %d, want down to have moved it to 1", m.cursor)
	}

	m.handleKey(key(t, "pgdown"))
	if want := 1 + m.listRows(); m.cursor != want {
		t.Fatalf("cursor = %d, want pgdown to have paged to %d", m.cursor, want)
	}

	m.handleKey(key(t, "up"))
	if want := m.listRows(); m.cursor != want {
		t.Fatalf("cursor = %d, want up to have moved it to %d", m.cursor, want)
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

// The browser leaves on the same two chords the pane does: a fast double esc...
func TestBrowsingDoubleEscLeaves(t *testing.T) {
	m := newBrowseModel()

	m.handleKey(key(t, "esc"))
	if !m.browsing {
		t.Fatal("a lone esc left the browser, want it to only arm the window")
	}

	m.handleKey(key(t, "esc"))
	if m.browsing {
		t.Fatal("double esc did not leave the browser")
	}
	if !m.lastEsc.IsZero() {
		t.Fatal("lastEsc not reset on leaving the browser")
	}
}

// ...and any other key in between breaks the chord, as in the pane.
func TestBrowsingEscOtherEscIsNotAChord(t *testing.T) {
	m := newBrowseModel()

	m.handleKey(key(t, "esc"))
	m.handleKey(key(t, "j"))
	m.handleKey(key(t, "esc"))

	if !m.browsing {
		t.Fatal("esc-j-esc left the browser, want it treated as two lone escs")
	}
}

// A slow second esc is two lone escapes, not a chord.
func TestBrowsingSlowDoubleEscStays(t *testing.T) {
	m := newBrowseModel()

	m.handleKey(key(t, "esc"))
	m.lastEsc = time.Now().Add(-2 * doubleEscWindow)
	m.handleKey(key(t, "esc"))

	if !m.browsing {
		t.Fatal("a slow double esc left the browser, want the window to have expired")
	}
}

func TestBrowsingCtrlOLeaves(t *testing.T) {
	m := newBrowseModel()

	m.handleKey(key(t, "esc"))
	m.handleKey(key(t, "ctrl+o"))

	if m.browsing {
		t.Fatal("ctrl+o did not leave the browser")
	}
	if !m.lastEsc.IsZero() {
		t.Fatal("lastEsc not reset on leaving the browser")
	}
}

// newBrowseModel builds a model in browsing mode with no live session, so keys
// that would reach the browser are simply dropped.
func newBrowseModel() *model {
	return &model{active: "web1", browsing: true, sessions: map[string]*session{}, height: 20}
}

// A "g" typed into the filter is literal text, not the start of a "gg" motion — so
// it must not leave a chord armed for the next "g" to complete once the filter is
// applied and the list has the keyboard back.
func TestFilterSwallowsG(t *testing.T) {
	m := newNavModel(30)
	m.filtering = true
	m.cursor = 5

	m.handleKey(key(t, "g"))
	if m.filter != "g" {
		t.Fatalf("filter = %q, want %q", m.filter, "g")
	}

	m.handleKey(key(t, "enter")) // apply the filter, back to the list
	m.cursor = 5                 // applyFilter may have clamped it
	m.handleKey(key(t, "g"))

	if m.cursor != 5 {
		t.Fatalf("cursor = %d; the g typed into the filter armed a gg the list completed", m.cursor)
	}
}
