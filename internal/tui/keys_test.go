package tui

import (
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"hop/internal/config"
	"hop/internal/keys"
	"hop/internal/store"
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
	case "ctrl+k":
		return tea.KeyMsg{Type: tea.KeyCtrlK}
	case "ctrl+t":
		return tea.KeyMsg{Type: tea.KeyCtrlT}
	case `ctrl+\`:
		return tea.KeyMsg{Type: tea.KeyCtrlBackslash}
	case "space":
		return tea.KeyMsg{Type: tea.KeySpace, Runes: []rune{' '}}
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
	case "home":
		return tea.KeyMsg{Type: tea.KeyHome}
	case "end":
		return tea.KeyMsg{Type: tea.KeyEnd}
	case "pgup":
		return tea.KeyMsg{Type: tea.KeyPgUp}
	case "pgdown":
		return tea.KeyMsg{Type: tea.KeyPgDown}
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}
	case "esc":
		return tea.KeyMsg{Type: tea.KeyEsc}
	case "tab":
		return tea.KeyMsg{Type: tea.KeyTab}
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

// newNavModel builds a navigation-mode model with n hosts, listRows() == 15 and vim keys on.
func newNavModel(n int) *model {
	hosts := make([]store.Host, n)
	for i := range hosts {
		hosts[i] = store.Host{Alias: fmt.Sprintf("h%d", i), HostName: "example.test"}
	}
	m := &model{hosts: hosts, cfg: config.Config{VimKeys: true}, layout: layout{height: 20}}
	// applyFilter fills the filtered list and the drawn rows paging is measured in.
	m.applyFilter()
	return m
}

func TestNavVimMotions(t *testing.T) {
	cases := []struct {
		name string
		keys []string
		want int
	}{
		{"j moves down", []string{"j"}, 1},
		{"k clamps at top", []string{"k", "k"}, 0},
		{"j clamps at bottom", []string{"pgdown", "pgdown", "j"}, 29},
		{"pgdown pages", []string{"pgdown"}, 15},
		{"pgup pages back", []string{"pgdown", "pgdown", "pgup"}, 14},
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

func TestNavVimMotionsOffByDefault(t *testing.T) {
	for _, k := range []string{"j", "k", "G", "H", "M", "L", "ctrl+d", "ctrl+u", "ctrl+f"} {
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

	// With the setting off, the first g must not arm a chord a later g completes.
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

// The jumps and ctrl chords belong to the browser, not the list, vim keys on or off.
func TestNavJumpKeysAreUnbound(t *testing.T) {
	for _, k := range []string{"G", "H", "M", "L", "ctrl+d", "ctrl+u", "ctrl+f"} {
		t.Run(k, func(t *testing.T) {
			m := newNavModel(30)
			m.cursor = 5

			m.handleKey(key(t, k))

			if m.cursor != 5 {
				t.Fatalf("%q moved the cursor to %d; it is the browser's key, not the list's", k, m.cursor)
			}
		})
	}
}

// The first g must not arm a chord that swallows the key after it.
func TestNavHasNoGChord(t *testing.T) {
	m := newNavModel(30)
	m.cursor = 5

	m.handleKey(key(t, "g"))
	m.handleKey(key(t, "g"))
	if m.cursor != 5 {
		t.Fatalf("gg moved the cursor to %d", m.cursor)
	}

	m.handleKey(key(t, "g"))
	m.handleKey(key(t, "j"))
	if m.cursor != 6 {
		t.Fatalf("cursor = %d; the j after a g was swallowed by a chord that is not bound", m.cursor)
	}
}

// An empty host list must not drive the cursor negative.
func TestNavMotionsOnEmptyList(t *testing.T) {
	for _, k := range []string{"j", "k", "pgdown", "pgup"} {
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

// newPaneModel focuses a terminal pane with no live session, so only mode changes show.
func newPaneModel() *model {
	return &model{sessions: map[string]*session{}, layout: layout{height: 20}, focus: focus{active: "web1", mode: modeShell}}
}

func TestPaneDoubleEscLeaves(t *testing.T) {
	m := newPaneModel()

	m.handleKey(key(t, "esc"))
	if !m.focused() {
		t.Fatal("a single esc left the pane, want it forwarded to the shell")
	}
	if !m.reader.Pending() {
		t.Fatal("first esc did not arm the double-esc window")
	}

	m.handleKey(key(t, "esc"))
	if m.focused() {
		t.Fatal("double esc did not leave the pane")
	}
	if m.reader.Pending() {
		t.Fatal("lastEsc not reset on leaving the pane")
	}
}

func TestPaneSlowEscsStayInPane(t *testing.T) {
	m := newPaneModel()

	m.handleKey(key(t, "esc"))
	m.reader.Reset() // as if the user paused past the chord's window
	m.handleKey(key(t, "esc"))

	if !m.focused() {
		t.Fatal("two slow escs left the pane, want both forwarded to the shell")
	}
	if !m.reader.Pending() {
		t.Fatal("the second esc should re-arm the window")
	}
}

func TestPaneEscSequenceBrokenByOtherKey(t *testing.T) {
	m := newPaneModel()

	m.handleKey(key(t, "esc"))
	m.handleKey(key(t, "j"))
	if m.reader.Pending() {
		t.Fatal("an intervening key did not clear the pending esc")
	}

	m.handleKey(key(t, "esc"))
	if !m.focused() {
		t.Fatal("esc-j-esc left the pane, want it treated as two lone escs")
	}
}

// ctrl+o then o leaves the pane, and clears any half-finished esc chord.
func TestPaneCtrlOLeaves(t *testing.T) {
	m := newPaneModel()

	m.handleKey(key(t, "esc"))
	m.handleKey(key(t, "ctrl+o"))
	m.handleKey(runeKey('o'))

	if m.focused() {
		t.Fatal("ctrl+o did not leave the pane")
	}
	if m.reader.Pending() {
		t.Fatal("lastEsc not reset on leaving the pane")
	}
}

func TestBrowsingDoubleEscLeaves(t *testing.T) {
	m := newBrowseModel()

	m.handleKey(key(t, "esc"))
	if !m.browsing() {
		t.Fatal("a lone esc left the browser, want it to only arm the window")
	}

	m.handleKey(key(t, "esc"))
	if m.browsing() {
		t.Fatal("double esc did not leave the browser")
	}
	if m.reader.Pending() {
		t.Fatal("lastEsc not reset on leaving the browser")
	}
}

func TestBrowsingEscOtherEscIsNotAChord(t *testing.T) {
	m := newBrowseModel()

	m.handleKey(key(t, "esc"))
	m.handleKey(key(t, "j"))
	m.handleKey(key(t, "esc"))

	if !m.browsing() {
		t.Fatal("esc-j-esc left the browser, want it treated as two lone escs")
	}
}

func TestBrowsingSlowDoubleEscStays(t *testing.T) {
	m := newBrowseModel()

	m.handleKey(key(t, "esc"))
	m.reader.Reset() // as if the user paused past the chord's window
	m.handleKey(key(t, "esc"))

	if !m.browsing() {
		t.Fatal("a slow double esc left the browser, want the window to have expired")
	}
}

func TestBrowsingCtrlOLeaves(t *testing.T) {
	m := newBrowseModel()

	m.handleKey(key(t, "esc"))
	m.handleKey(key(t, "ctrl+o"))

	if m.browsing() {
		t.Fatal("ctrl+o did not leave the browser")
	}
	if m.reader.Pending() {
		t.Fatal("lastEsc not reset on leaving the browser")
	}
}

// newBrowseModel browses with no live session, so browser-bound keys are dropped.
func newBrowseModel() *model {
	return &model{sessions: map[string]*session{}, layout: layout{height: 20}, focus: focus{active: "web1", mode: modeBrowser}}
}

func TestFilterSwallowsMotionLetters(t *testing.T) {
	m := viewModel(120, 34)
	m.filtering = true
	m.filter = "web"
	m.applyFilter()
	before := m.cursor

	m.handleKey(key(t, "j"))

	if m.filter != "webj" {
		t.Fatalf("filter = %q, want %q — the j went to the list instead of the filter", m.filter, "webj")
	}
	if m.cursor != before {
		t.Fatalf("cursor = %d, want %d", m.cursor, before)
	}
}

func paneModeIf(cond bool, mode paneMode) paneMode {
	if cond {
		return mode
	}
	return modeList
}

func TestRebindingMovesAnAction(t *testing.T) {
	m := newNavModel(2)
	binds, errs := keys.New(map[string]string{
		string(keys.HostBrowser): "b", // sftp browser, was f
		string(keys.HostAdd):     "",  // unbound entirely
	})
	if len(errs) != 0 {
		t.Fatalf("keys.New: %v", errs)
	}
	m.binds = binds

	m.handleKey(key(t, "f"))
	if m.hostForm.open || m.mode == modeBrowser {
		t.Fatal("the default key still acted after being moved")
	}

	m.handleKey(key(t, "a"))
	if m.hostForm.open {
		t.Fatal("an unbound key still opened the host form")
	}

	// The footer follows the keyboard, not the defaults.
	_, extra, _ := m.footerHints()
	joined := strings.Join(extra, " ")
	if want := m.hint(keys.List, keys.HostBrowser, "sftp"); !strings.Contains(joined, want) {
		t.Fatalf("footer = %q, want it to name the rebound key as %q", joined, want)
	}
	if strings.Contains(joined, "add") {
		t.Fatalf("footer = %q, want no hint for the unbound key", joined)
	}
}
