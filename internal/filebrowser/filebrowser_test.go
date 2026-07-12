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
// enough for 10 content rows, rooted at /home/u. The vim motions are switched on:
// they are what most of these tests are about, and they are off by default.
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
		opts:    Options{VimKeys: true},
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

// With the setting off the vim motions are not bound, so "h" is not a way out of a
// directory and "j" is not a way down a list. The arrows and backspace still are.
func TestVimMotionsOffByDefault(t *testing.T) {
	for _, k := range []string{"j", "k", "g", "G", "H", "M", "L", "ctrl+d", "ctrl+u", "ctrl+f", "ctrl+b"} {
		t.Run(k, func(t *testing.T) {
			b, _ := newTestBrowser(30)
			b.opts.VimKeys = false
			b.cursor = 5

			b.Handle(key(t, k))

			if b.cursor != 5 {
				t.Fatalf("%q moved the cursor to %d; with vim keys off it must do nothing", k, b.cursor)
			}
			if b.pendingG {
				t.Fatalf("%q armed a pending gg with vim keys off", k)
			}
		})
	}

	t.Run("h does not leave the directory", func(t *testing.T) {
		b, _ := newTestBrowser(3)
		b.opts.VimKeys = false

		b.Handle(key(t, "h"))
		if b.cwd != "/home/u" {
			t.Fatalf("cwd = %q; with vim keys off, h must not walk up", b.cwd)
		}

		b.Handle(key(t, "left"))
		if b.cwd != "/home" {
			t.Fatalf("cwd = %q; the arrow must still walk up", b.cwd)
		}
	})

	t.Run("arrows still move", func(t *testing.T) {
		b, _ := newTestBrowser(30)
		b.opts.VimKeys = false

		b.Handle(key(t, "down"))
		if b.cursor != 1 {
			t.Fatalf("cursor = %d, want down to have moved it to 1", b.cursor)
		}
	})
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
		client:  fc,
		cwd:     "/home/u",
		entries: ents,
		opts:    Options{DownloadDir: dl},
		tmpDir:  tmp,
		w:       40,
		h:       13,
	}, fc, tmp, dl
}

// stubOpen swaps the default-app launcher for a command that starts and exits
// immediately (the test binary itself, told to run no tests), recording the path
// it was handed. It restores the original when the test ends.
func stubOpen(t *testing.T) (opened, openedWith *string) {
	t.Helper()
	var p, with string
	orig := openCmd
	openCmd = func(w, path string) *exec.Cmd {
		with, p = w, path
		return exec.Command(os.Args[0], "-test.run=^$")
	}
	t.Cleanup(func() { openCmd = orig })
	return &p, &with
}

// Enter on a file asks the model to open it, and touches no local disk at all:
// the editor runs on the remote host, against the real file.
func TestEnterAsksToOpenFile(t *testing.T) {
	b, fc, tmp, dl := fileTestBrowser(t)

	b.cursor = 1 // a.txt
	cmd := b.Handle(key(t, "enter"))
	if cmd == nil {
		t.Fatal("enter returned no tea.Cmd, want an OpenFileMsg for the model")
	}
	msg, ok := cmd().(OpenFileMsg)
	if !ok {
		t.Fatalf("enter produced %T, want OpenFileMsg", cmd())
	}
	if msg.Path != "/home/u/a.txt" || msg.Name != "a.txt" {
		t.Fatalf("OpenFileMsg = %+v, want {/home/u/a.txt a.txt}", msg)
	}
	if len(fc.downloads) != 0 {
		t.Fatalf("enter downloaded %v, want nothing fetched", fc.downloads)
	}
	for _, dir := range []string{tmp, dl} {
		if entries, _ := os.ReadDir(dir); len(entries) != 0 {
			t.Fatalf("%s is not empty: %v", dir, entries)
		}
	}
}

// Enter on a directory still navigates into it, and asks for nothing.
func TestEnterOnDirNavigates(t *testing.T) {
	b, fc, _, _ := fileTestBrowser(t)

	b.cursor = 0 // sub/
	if cmd := b.Handle(key(t, "enter")); cmd != nil {
		t.Fatal("entering a directory returned a tea.Cmd, want pure navigation")
	}

	if b.cwd != "/home/u/sub" {
		t.Fatalf("cwd = %q, want %q", b.cwd, "/home/u/sub")
	}
	if len(fc.downloads) != 0 {
		t.Fatalf("entering a directory fetched %v", fc.downloads)
	}
}

// "o" fetches into the scratch dir — not the download dir — and hands that copy
// to the OS default app. On a directory it does nothing at all.
func TestOpenInAppKey(t *testing.T) {
	b, fc, tmp, dl := fileTestBrowser(t)
	opened, _ := stubOpen(t)

	b.cursor = 1 // a.txt
	if cmd := b.Handle(key(t, "o")); cmd != nil {
		t.Fatal("o returned a tea.Cmd; the default-app launch must not suspend the TUI")
	}

	want := filepath.Join(tmp, "a.txt")
	if len(fc.downloads) != 1 {
		t.Fatalf("downloads = %v, want exactly one", fc.downloads)
	}
	if got := fc.downloads[0]; got[0] != "/home/u/a.txt" || got[1] != want {
		t.Fatalf("download = %v, want {/home/u/a.txt %s}", got, want)
	}
	if *opened != want {
		t.Fatalf("opened %q, want %q", *opened, want)
	}
	if b.statusErr || b.status != "opened a.txt" {
		t.Fatalf("status = %q (err=%v), want %q", b.status, b.statusErr, "opened a.txt")
	}
	if entries, _ := os.ReadDir(dl); len(entries) != 0 {
		t.Fatalf("download dir is not empty: %v", entries)
	}

	b.cursor = 0 // sub/
	b.Handle(key(t, "o"))
	if len(fc.downloads) != 1 {
		t.Fatalf("downloads = %v, want only the file", fc.downloads)
	}
}

// With an "open with" setting, "o" uses that command instead of the desktop
// default — flags and all.
func TestOpenWithOverride(t *testing.T) {
	b, _, tmp, _ := fileTestBrowser(t)
	opened, with := stubOpen(t)

	b.SetOptions(Options{DownloadDir: b.opts.DownloadDir, OpenWith: "code -n"})
	b.cursor = 1 // a.txt
	b.Handle(key(t, "o"))

	if *with != "code -n" {
		t.Fatalf("openCmd got %q, want the configured %q", *with, "code -n")
	}
	if want := filepath.Join(tmp, "a.txt"); *opened != want {
		t.Fatalf("opened %q, want %q", *opened, want)
	}
}

// "d" is the only key that writes to the download dir, and it launches nothing.
func TestDownloadKey(t *testing.T) {
	b, fc, _, dl := fileTestBrowser(t)
	opened, _ := stubOpen(t)

	b.cursor = 1 // a.txt
	b.Handle(key(t, "d"))

	want := filepath.Join(dl, "a.txt")
	if len(fc.downloads) != 1 || fc.downloads[0][1] != want {
		t.Fatalf("downloads = %v, want a single one to %s", fc.downloads, want)
	}
	if *opened != "" {
		t.Fatalf("download launched the default app on %q", *opened)
	}
	if b.statusErr || !strings.HasPrefix(b.status, "downloaded a.txt") {
		t.Fatalf("status = %q (err=%v), want a downloaded... message", b.status, b.statusErr)
	}
}

// A server-supplied entry name must not be able to place a download outside
// the chosen directory (path separators, ".."), address an NTFS stream (":"),
// or name a device (CON) — for "d" and "o" alike: both write locally.
func TestRejectsUnsafeRemoteNames(t *testing.T) {
	for _, name := range []string{
		`..`, `..\..\evil.exe`, `../../evil`, `sub/file`, `C:evil`,
		`ads.txt:Zone.Identifier`, `CON`, `NUL.txt`, "esc\x1b[2Jape", "",
	} {
		t.Run(name, func(t *testing.T) {
			for _, k := range []string{"d", "o"} {
				b, fc, _, _ := fileTestBrowser(t)
				opened, _ := stubOpen(t)
				b.entries[1].Name = name
				b.cursor = 1

				b.Handle(key(t, k))

				if len(fc.downloads) != 0 {
					t.Fatalf("%q on %q downloaded to %v, want a refusal", k, name, fc.downloads)
				}
				if *opened != "" {
					t.Fatalf("%q on %q launched the default app on %q", k, name, *opened)
				}
				if !b.statusErr {
					t.Fatalf("%q on %q: status = %q, want an error", k, name, b.status)
				}
			}
		})
	}
}

// A plain name — dots, spaces, unicode — must keep working.
func TestAcceptsOrdinaryNames(t *testing.T) {
	for _, name := range []string{"a.txt", "my report.pdf", "übersicht.md", "archive.tar.gz", "console.log"} {
		b, fc, _, dl := fileTestBrowser(t)
		b.entries[1].Name = name
		b.cursor = 1

		b.Handle(key(t, "d"))

		want := filepath.Join(dl, name)
		if len(fc.downloads) != 1 || fc.downloads[0][1] != want {
			t.Fatalf("download of %q = %v, want one to %s", name, fc.downloads, want)
		}
	}
}

// Control characters in remote-supplied strings are display-stripped, so a file
// name cannot smuggle an escape sequence into the user's terminal.
func TestViewStripsControlCharacters(t *testing.T) {
	b, _, _, _ := fileTestBrowser(t)
	b.entries[1].Name = "evil\x1b]0;owned\x07\x9b31mname"
	b.cwd = "/home/u\x1b[2J"

	view := b.View()
	for _, bad := range []string{"\x1b]", "\x1b[2J", "\x07", "\x9b"} {
		if strings.Contains(view, bad) {
			t.Fatalf("View() leaked control sequence %q", bad)
		}
	}
	if !strings.Contains(view, "evil") || !strings.Contains(view, "name") {
		t.Fatalf("View() lost the printable part of the name:\n%s", view)
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
