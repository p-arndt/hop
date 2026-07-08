package filebrowser

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"hop/internal/sftpx"
)

// key builds the tea.KeyMsg whose String() is name. Only the keys the browser
// binds are covered.
func key(t *testing.T, name string) tea.KeyMsg {
	t.Helper()
	switch name {
	case "left":
		return tea.KeyMsg{Type: tea.KeyLeft}
	case "backspace":
		return tea.KeyMsg{Type: tea.KeyBackspace}
	case "up":
		return tea.KeyMsg{Type: tea.KeyUp}
	case "down":
		return tea.KeyMsg{Type: tea.KeyDown}
	case "ctrl+d":
		return tea.KeyMsg{Type: tea.KeyCtrlD}
	case "ctrl+u":
		return tea.KeyMsg{Type: tea.KeyCtrlU}
	case "ctrl+f":
		return tea.KeyMsg{Type: tea.KeyCtrlF}
	case "ctrl+b":
		return tea.KeyMsg{Type: tea.KeyCtrlB}
	default:
		if len([]rune(name)) != 1 {
			t.Fatalf("key: unsupported name %q", name)
		}
		return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(name)}
	}
}

// fakeClient serves a fixed listing for every directory.
type fakeClient struct {
	entries []sftpx.Entry
}

func (f *fakeClient) Home() (string, error) { return "/home/u", nil }

func (f *fakeClient) List(string) ([]sftpx.Entry, error) { return f.entries, nil }

func (f *fakeClient) Download(string, string) (int64, error) { return 0, nil }
func (f *fakeClient) Close() error                           { return nil }

// newTestBrowser builds a Browser over n synthetic entries with a viewport tall
// enough for 10 content rows, rooted at /home/u.
func newTestBrowser(n int) (*Browser, *fakeClient) {
	ents := make([]sftpx.Entry, n)
	for i := range ents {
		ents[i] = sftpx.Entry{Name: "f", Size: 1}
	}
	fc := &fakeClient{entries: ents}
	return &Browser{
		client:  fc,
		cwd:     "/home/u",
		root:    "/home/u",
		entries: ents,
		w:       40,
		h:       13, // contentRows() == 10
	}, fc
}

func TestHandleLeftDismissesAtTop(t *testing.T) {
	cases := []struct {
		name string
		cwd  string
		root string
		want bool
	}{
		{"at start dir", "/home/u", "/home/u", true},
		{"at filesystem root", "/", "/home/u", true},
		{"above start dir", "/home", "/home/u", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			b, _ := newTestBrowser(3)
			b.cwd, b.root = tc.cwd, tc.root

			got := b.Handle(key(t, "left"))
			if got != tc.want {
				t.Fatalf("Handle(left) = %v, want %v", got, tc.want)
			}
			if !tc.want && b.cwd != "/" {
				t.Fatalf("cwd = %q, want the parent %q", b.cwd, "/")
			}
		})
	}
}

// Backspace and h are strict "up a directory": they must never dismiss, so the
// user can bump against the top without falling out of the browser.
func TestHandleUpKeysNeverDismiss(t *testing.T) {
	for _, k := range []string{"backspace", "h"} {
		t.Run(k, func(t *testing.T) {
			b, _ := newTestBrowser(3)
			b.cwd, b.root = "/", "/" // already at the top
			if b.Handle(key(t, k)) {
				t.Fatalf("Handle(%q) = true, want no dismiss", k)
			}
			if b.cwd != "/" {
				t.Fatalf("cwd = %q, want %q", b.cwd, "/")
			}
		})
	}
}

func TestVimMotions(t *testing.T) {
	cases := []struct {
		name    string
		keys    []string
		entries int
		want    int
	}{
		{"j moves down", []string{"j"}, 30, 1},
		{"k clamps at top", []string{"k", "k"}, 30, 0},
		{"j clamps at bottom", []string{"G", "j"}, 30, 29},
		{"G jumps to last", []string{"G"}, 30, 29},
		{"gg jumps to first", []string{"G", "g", "g"}, 30, 0},
		{"lone g is inert", []string{"j", "j", "g"}, 30, 2},
		{"g then other key cancels", []string{"G", "g", "j", "g"}, 30, 29},
		{"ctrl+d half page", []string{"ctrl+d"}, 30, 5},
		{"ctrl+u half page back", []string{"G", "ctrl+u"}, 30, 24},
		{"ctrl+f full page", []string{"ctrl+f"}, 30, 10},
		{"ctrl+b full page back", []string{"G", "ctrl+b"}, 30, 19},
		{"G on empty list stays 0", []string{"G"}, 0, 0},
		{"gg on empty list stays 0", []string{"g", "g"}, 0, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			b, _ := newTestBrowser(tc.entries)
			for _, k := range tc.keys {
				if b.Handle(key(t, k)) {
					t.Fatalf("Handle(%q) unexpectedly dismissed", k)
				}
			}
			if b.cursor != tc.want {
				t.Fatalf("cursor = %d, want %d", b.cursor, tc.want)
			}
		})
	}
}

// TestScreenMotions checks H/M/L land inside the visible window, not the whole
// list, which is what makes them differ from gg/G once the list scrolls.
func TestScreenMotions(t *testing.T) {
	cases := []struct {
		key  string
		want int
	}{
		{"H", 20}, // top of the window
		{"M", 25}, // middle of the window
		{"L", 29}, // bottom of the window
	}
	for _, tc := range cases {
		t.Run(tc.key, func(t *testing.T) {
			b, _ := newTestBrowser(30)
			b.Handle(key(t, "G")) // cursor 29, scroll 20, window rows 10
			if b.scroll != 20 {
				t.Fatalf("setup: scroll = %d, want 20", b.scroll)
			}
			b.Handle(key(t, tc.key))
			if b.cursor != tc.want {
				t.Fatalf("cursor = %d, want %d", b.cursor, tc.want)
			}
			if b.scroll != 20 {
				t.Fatalf("scroll moved to %d, want 20", b.scroll)
			}
		})
	}
}

// TestCursorStaysVisible is the invariant every motion must preserve.
func TestCursorStaysVisible(t *testing.T) {
	b, _ := newTestBrowser(100)
	for _, k := range []string{"G", "ctrl+u", "ctrl+u", "j", "ctrl+f", "k", "g", "g", "ctrl+d"} {
		b.Handle(key(t, k))
		if b.cursor < b.scroll || b.cursor >= b.scroll+b.contentRows() {
			t.Fatalf("after %q: cursor %d outside window [%d,%d)",
				k, b.cursor, b.scroll, b.scroll+b.contentRows())
		}
	}
}
