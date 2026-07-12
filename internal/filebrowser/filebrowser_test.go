package filebrowser

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"hop/internal/sftpx"
)

// key builds the tea.KeyMsg whose String() is name. Only the keys the browser
// binds are covered.
func key(t *testing.T, name string) tea.KeyMsg {
	t.Helper()
	switch name {
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}
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

// fakeClient serves a fixed listing for every directory and records the paths
// Download was called with.
type fakeClient struct {
	entries   []sftpx.Entry
	downloads [][2]string // {remote, local}
}

func (f *fakeClient) Home() (string, error) { return "/home/u", nil }

func (f *fakeClient) List(string) ([]sftpx.Entry, error) { return f.entries, nil }

func (f *fakeClient) Download(remote, local string) (int64, error) {
	f.downloads = append(f.downloads, [2]string{remote, local})
	return 0, nil
}
func (f *fakeClient) Close() error { return nil }

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
		entries: ents,
		w:       40,
		h:       13, // contentRows() == 10
	}, fc
}

// left, backspace and h are all strict "up a directory". None of them leaves the
// browser, and at the filesystem root they are no-ops rather than an exit.
func TestHandleUpKeys(t *testing.T) {
	for _, k := range []string{"left", "backspace", "h"} {
		t.Run(k, func(t *testing.T) {
			b, _ := newTestBrowser(3)
			b.cwd = "/home/u"

			b.Handle(key(t, k))
			if b.cwd != "/home" {
				t.Fatalf("cwd = %q, want the parent %q", b.cwd, "/home")
			}

			b.cwd = "/" // already at the top
			b.Handle(key(t, k))
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
				b.Handle(key(t, k))
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

// fileTestBrowser builds a Browser over one directory and one file, with the
// scratch and download directories pointed at throwaway locations.
func fileTestBrowser(t *testing.T) (*Browser, *fakeClient, string, string) {
	t.Helper()
	ents := []sftpx.Entry{
		{Name: "sub", IsDir: true},
		{Name: "a.txt", Size: 12},
	}
	fc := &fakeClient{entries: ents}
	tmp, dl := t.TempDir(), t.TempDir()
	return &Browser{
		client:      fc,
		cwd:         "/home/u",
		entries:     ents,
		downloadDir: dl,
		tmpDir:      tmp,
		w:           40,
		h:           13,
	}, fc, tmp, dl
}

// stubExec swaps the two launchers for commands that start and exit immediately
// (the test binary itself, told to run no tests), recording the path each was
// handed. It restores the originals when the test ends.
func stubExec(t *testing.T) (opened, edited *string) {
	t.Helper()
	var o, e string
	origOpen, origEditor := openCmd, editorCmd
	openCmd = func(p string) *exec.Cmd { o = p; return exec.Command(os.Args[0], "-test.run=^$") }
	editorCmd = func(p string) *exec.Cmd { e = p; return exec.Command(os.Args[0], "-test.run=^$") }
	t.Cleanup(func() { openCmd, editorCmd = origOpen, origEditor })
	return &o, &e
}

// Enter on a file fetches it into the scratch dir — not the download dir — and
// returns a command, because the terminal editor takes over the terminal.
func TestEnterEditsFile(t *testing.T) {
	b, fc, tmp, dl := fileTestBrowser(t)
	_, edited := stubExec(t)

	b.cursor = 1 // a.txt
	if cmd := b.Handle(key(t, "enter")); cmd == nil {
		t.Fatal("enter returned no tea.Cmd; the editor needs the terminal to itself")
	}

	want := filepath.Join(tmp, "a.txt")
	if len(fc.downloads) != 1 {
		t.Fatalf("downloads = %v, want exactly one", fc.downloads)
	}
	if got := fc.downloads[0]; got[0] != "/home/u/a.txt" || got[1] != want {
		t.Fatalf("download = %v, want {/home/u/a.txt %s}", got, want)
	}
	if *edited != want {
		t.Fatalf("edited %q, want %q", *edited, want)
	}
	if entries, _ := os.ReadDir(dl); len(entries) != 0 {
		t.Fatalf("download dir is not empty: %v", entries)
	}

	b.EditFinished(EditFinishedMsg{Name: "a.txt"})
	if b.statusErr || b.status != "edited a.txt" {
		t.Fatalf("status = %q (err=%v), want %q", b.status, b.statusErr, "edited a.txt")
	}
}

// Enter on a directory still navigates into it, and starts nothing.
func TestEnterOnDirNavigates(t *testing.T) {
	b, fc, _, _ := fileTestBrowser(t)
	_, edited := stubExec(t)

	b.cursor = 0 // sub/
	if cmd := b.Handle(key(t, "enter")); cmd != nil {
		t.Fatal("entering a directory returned a tea.Cmd, want pure navigation")
	}

	if b.cwd != "/home/u/sub" {
		t.Fatalf("cwd = %q, want %q", b.cwd, "/home/u/sub")
	}
	if len(fc.downloads) != 0 || *edited != "" {
		t.Fatalf("entering a directory fetched %v / edited %q", fc.downloads, *edited)
	}
}

// "o" hands the scratch copy to the OS default app without suspending the TUI.
// On a directory it does nothing at all.
func TestOpenInAppKey(t *testing.T) {
	b, fc, tmp, _ := fileTestBrowser(t)
	opened, _ := stubExec(t)

	b.cursor = 1 // a.txt
	if cmd := b.Handle(key(t, "o")); cmd != nil {
		t.Fatal("o returned a tea.Cmd; the default-app launch must not suspend the TUI")
	}
	want := filepath.Join(tmp, "a.txt")
	if *opened != want {
		t.Fatalf("opened %q, want %q", *opened, want)
	}
	if b.statusErr || b.status != "opened a.txt" {
		t.Fatalf("status = %q (err=%v), want %q", b.status, b.statusErr, "opened a.txt")
	}

	b.cursor = 0 // sub/
	b.Handle(key(t, "o"))
	if len(fc.downloads) != 1 {
		t.Fatalf("downloads = %v, want only the file", fc.downloads)
	}
}

// "d" is the only key that writes to the download dir, and it launches nothing.
func TestDownloadKey(t *testing.T) {
	b, fc, _, dl := fileTestBrowser(t)
	opened, edited := stubExec(t)

	b.cursor = 1 // a.txt
	b.Handle(key(t, "d"))

	want := filepath.Join(dl, "a.txt")
	if len(fc.downloads) != 1 || fc.downloads[0][1] != want {
		t.Fatalf("downloads = %v, want a single one to %s", fc.downloads, want)
	}
	if *opened != "" || *edited != "" {
		t.Fatalf("download launched something: opened=%q edited=%q", *opened, *edited)
	}
	if b.statusErr || !strings.HasPrefix(b.status, "downloaded a.txt") {
		t.Fatalf("status = %q (err=%v), want a downloaded... message", b.status, b.statusErr)
	}
}

// With no $EDITOR set, the fallback must be a terminal editor whenever one exists
// on PATH — landing on a GUI (notepad) would defeat the point of editing in place.
func TestLookEditorPrefersTerminalEditors(t *testing.T) {
	var onPath []string
	for _, ed := range terminalEditors {
		if _, err := exec.LookPath(ed); err == nil {
			onPath = append(onPath, ed)
		}
	}
	if len(onPath) == 0 {
		t.Skip("no terminal editor on PATH")
	}
	if got := lookEditor(); got != onPath[0] {
		t.Fatalf("lookEditor() = %q, want the highest-priority one on PATH, %q", got, onPath[0])
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
