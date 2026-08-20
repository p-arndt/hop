package filebrowser

import (
	"fmt"
	"os"
	"os/exec"
	"path"
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
	case "esc":
		return tea.KeyMsg{Type: tea.KeyEscape}
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

// fakeClient serves a fixed listing for every directory and records the paths every
// mutating call was made with, so a test can assert what reached the server rather than
// only what the status line says.
type fakeClient struct {
	entries   []sftpx.Entry
	downloads [][2]string // {remote, local}
	uploads   [][2]string // {local, remote}
	mkdirs    []string
	removes   []string
	renames   [][2]string // {old, new}
	copies    [][2]string // {src, dstDir}
	moves     [][2]string // {src, dstDir}

	// Errors to return instead of succeeding, keyed by the operation name
	// ("upload", "mkdir", "remove", "rename", "download").
	errs map[string]error

	// steps are the running byte totals a transfer reports as it copies. Empty means a
	// copy that reports nothing, which is what most of these tests want.
	steps byteSteps

	// listErr fails every subsequent List, standing in for a connection lost partway
	// through a sequence rather than at its start.
	listErr error

	// lists counts the listings, which is how a test tells a directory that was read once
	// and cached from one that is re-read on every keypress.
	lists int

	// badName narrows errs to the entry of that name, so a test can fail the third file of
	// seven and then say what happened to the other four.
	badName string
}

// errFor is the scripted error for op, or nil — and nil for every entry but badName when
// one is set.
func (f *fakeClient) errFor(op, name string) error {
	err := f.errs[op]
	if err == nil || (f.badName != "" && f.badName != name) {
		return nil
	}
	return err
}

func (f *fakeClient) Home() (string, error) { return "/home/u", nil }

func (f *fakeClient) List(string) ([]sftpx.Entry, error) {
	f.lists++
	if f.listErr != nil {
		return nil, f.listErr
	}
	return f.entries, nil
}

func (f *fakeClient) DownloadProgress(remote, local string, progress func(int64)) (int64, error) {
	f.downloads = append(f.downloads, [2]string{remote, local})
	if err := f.errFor("download", path.Base(remote)); err != nil {
		return 0, err
	}
	// Create the local file like the real client would: the scratch fetch behind "o"
	// quarantines the copy afterwards, and the xattr call needs a file to land on.
	if err := os.WriteFile(local, nil, 0o644); err != nil {
		return 0, err
	}
	report(progress, f.steps)
	return f.steps.last(), nil
}

func (f *fakeClient) UploadProgress(local, remote string, progress func(int64)) (int64, error) {
	f.uploads = append(f.uploads, [2]string{local, remote})
	if err := f.errFor("upload", path.Base(remote)); err != nil {
		return 0, err
	}
	fi, err := os.Stat(local)
	if err != nil {
		return 0, err
	}
	if len(f.steps) > 0 {
		report(progress, f.steps)
		return f.steps.last(), nil
	}
	return fi.Size(), nil
}

// byteSteps is a scripted progress report: the running totals a copy publishes on its way
// through, standing in for the 32 KiB blocks the real counting writer reports.
type byteSteps []int64

func (s byteSteps) last() int64 {
	if len(s) == 0 {
		return 0
	}
	return s[len(s)-1]
}

// report replays steps into a progress callback, as sftpx does from inside io.Copy.
func report(progress func(int64), steps byteSteps) {
	if progress == nil {
		return
	}
	for _, n := range steps {
		progress(n)
	}
}

func (f *fakeClient) Mkdir(p string) error {
	f.mkdirs = append(f.mkdirs, p)
	return f.errs["mkdir"]
}

func (f *fakeClient) Remove(p string) error {
	f.removes = append(f.removes, p)
	return f.errFor("remove", path.Base(p))
}

func (f *fakeClient) Rename(oldp, newp string) error {
	f.renames = append(f.renames, [2]string{oldp, newp})
	return f.errs["rename"]
}

func (f *fakeClient) Copy(src, dstDir string, progress func(int64)) (int64, error) {
	f.copies = append(f.copies, [2]string{src, dstDir})
	if err := f.errFor("copy", path.Base(src)); err != nil {
		return 0, err
	}
	report(progress, f.steps)
	return f.steps.last(), nil
}

func (f *fakeClient) Move(src, dstDir string, _ func(int64)) error {
	f.moves = append(f.moves, [2]string{src, dstDir})
	return f.errFor("move", path.Base(src))
}

func (f *fakeClient) Close() error { return nil }

// plant gives a hand-built Browser the tree it would have had from load: dir as the root,
// ents as its rows. Every test here builds its Browser by hand — the point is usually the
// keyboard, not the listing — so this is the one place that knows how a root is put
// together.
func plant(b *Browser, dir string, ents []sftpx.Entry) *Browser {
	b.cwd = dir
	b.root = &node{e: sftpx.Entry{Name: path.Base(dir), IsDir: true}, path: dir, depth: -1, expanded: true}
	b.setKids(b.root, ents)
	b.rebuild()
	return b
}

// setName renames the entry on row i in place, which is how a test that is about what a
// name does to a key states its case without going through the server.
func setName(b *Browser, i int, name string) {
	n := b.rows[i]
	n.e.Name = name
	n.path = path.Join(path.Dir(n.path), name)
}

// rowNames is the visible tree as a list of names, indented so a test can assert on the
// shape of an expanded tree and not only on its contents.
func rowNames(b *Browser) []string {
	out := make([]string, len(b.rows))
	for i, n := range b.rows {
		out[i] = strings.Repeat("  ", n.depth) + n.e.Name
	}
	return out
}

// newTestBrowser builds a Browser over n synthetic entries with room for 10 content rows,
// rooted at /home/u. The vim motions are switched on, being what most of these test.
func newTestBrowser(n int) (*Browser, *fakeClient) {
	ents := make([]sftpx.Entry, n)
	for i := range ents {
		ents[i] = sftpx.Entry{Name: "f", Size: 1}
	}
	fc := &fakeClient{entries: ents}
	b := &Browser{
		client: fc,
		alias:  "web1",
		opts:   Options{VimKeys: true},
		w:      40,
		h:      13, // contentRows() == 10
	}
	return plant(b, "/home/u", ents), fc
}

// left, backspace and h are all strict "up a directory". None of them leaves the
// browser, and at the filesystem root they are no-ops rather than an exit.
func TestHandleUpKeys(t *testing.T) {
	for _, k := range []string{"left", "backspace", "h"} {
		t.Run(k, func(t *testing.T) {
			b, _ := newTestBrowser(3)

			b.Handle(key(t, k))
			if b.cwd != "/home" {
				t.Fatalf("cwd = %q, want the parent %q", b.cwd, "/home")
			}

			plant(b, "/", b.client.(*fakeClient).entries) // already at the top
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
	for _, k := range []string{"j", "k", "g", "G", "H", "M", "L", "ctrl+d", "ctrl+u", "ctrl+f"} {
		t.Run(k, func(t *testing.T) {
			b, _ := newTestBrowser(30)
			b.opts.VimKeys = false
			b.cursor = 5

			b.Handle(key(t, k))

			if b.cursor != 5 {
				t.Fatalf("%q moved the cursor to %d; with vim keys off it must do nothing", k, b.cursor)
			}
		})
	}

	// The "gg" chord must not be armed while the setting is off, or turning it on
	// would complete a motion the user began before they had the keys.
	t.Run("gg is not armed while off", func(t *testing.T) {
		b, _ := newTestBrowser(30)
		b.opts.VimKeys = false
		b.cursor = 5

		b.Handle(key(t, "g"))
		b.opts.VimKeys = true
		b.Handle(key(t, "g"))

		if b.cursor != 5 {
			t.Fatalf("cursor = %d; a g typed while off was completed by the first g typed after on", b.cursor)
		}
	})

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
	b := &Browser{
		client: fc,
		alias:  "web1",
		opts:   Options{DownloadDir: dl},
		tmpDir: tmp,
		w:      40,
		h:      13,
	}
	return plant(b, "/home/u", ents), fc, tmp, dl
}

// stubOpen swaps the default-app launcher for a command that starts and exits at once,
// recording the path it was handed, and restores the original when the test ends.
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
	// Everything the browser sends is addressed, so the model can route it by session.
	wrapped, ok := cmd().(Msg)
	if !ok {
		t.Fatalf("enter produced %T, want a filebrowser.Msg", cmd())
	}
	if wrapped.Alias != "web1" {
		t.Fatalf("Msg.Alias = %q, want the session the browser belongs to", wrapped.Alias)
	}
	msg, ok := wrapped.Body.(OpenFileMsg)
	if !ok {
		t.Fatalf("enter produced a %T body, want OpenFileMsg", wrapped.Body)
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
	// The fetch behind "o" is a transfer like any other, so the key hands back a command
	// rather than a finished download; drive runs it to completion the way the TUI does.
	drive(t, b, b.Handle(key(t, "o")))

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
	if b.note.err || b.note.text != "opened a.txt" {
		t.Fatalf("status = %q (err=%v), want %q", b.note.text, b.note.err, "opened a.txt")
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
	drive(t, b, b.Handle(key(t, "o")))

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
	drive(t, b, b.Handle(key(t, "d")))

	want := filepath.Join(dl, "a.txt")
	if len(fc.downloads) != 1 || fc.downloads[0][1] != want {
		t.Fatalf("downloads = %v, want a single one to %s", fc.downloads, want)
	}
	if *opened != "" {
		t.Fatalf("download launched the default app on %q", *opened)
	}
	if b.note.err || !strings.HasPrefix(b.note.text, "downloaded a.txt") {
		t.Fatalf("status = %q (err=%v), want a downloaded... message", b.note.text, b.note.err)
	}
}

// A server-supplied entry name must not place a download outside the chosen directory,
// address an NTFS stream, or name a device — for "d" and "o" alike, since both write
// locally.
func TestRejectsUnsafeRemoteNames(t *testing.T) {
	for _, name := range []string{
		`..`, `..\..\evil.exe`, `../../evil`, `sub/file`, `C:evil`,
		`ads.txt:Zone.Identifier`, `CON`, `NUL.txt`, "esc\x1b[2Jape", "",
	} {
		t.Run(name, func(t *testing.T) {
			for _, k := range []string{"d", "o"} {
				b, fc, _, _ := fileTestBrowser(t)
				opened, _ := stubOpen(t)
				setName(b, 1, name)
				b.cursor = 1

				b.Handle(key(t, k))

				if len(fc.downloads) != 0 {
					t.Fatalf("%q on %q downloaded to %v, want a refusal", k, name, fc.downloads)
				}
				if *opened != "" {
					t.Fatalf("%q on %q launched the default app on %q", k, name, *opened)
				}
				if !b.note.err {
					t.Fatalf("%q on %q: status = %q, want an error", k, name, b.note.text)
				}
			}
		})
	}
}

// A plain name — dots, spaces, unicode — must keep working.
func TestAcceptsOrdinaryNames(t *testing.T) {
	for _, name := range []string{"a.txt", "my report.pdf", "übersicht.md", "archive.tar.gz", "console.log"} {
		b, fc, _, dl := fileTestBrowser(t)
		setName(b, 1, name)
		b.cursor = 1

		drive(t, b, b.Handle(key(t, "d")))

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
	setName(b, 1, "evil\x1b]0;owned\x07\x9b31mname")
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

// executableName must flag exactly the names the OS default handler would run, judging by
// the last extension only, and see through the trailing dot or space Windows strips.
func TestExecutableName(t *testing.T) {
	cases := []struct {
		name string
		want bool
	}{
		{"evil.exe", true},
		{"Evil.EXE", true},     // the deny-list is case-insensitive
		{"note.pdf.hta", true}, // only the final extension counts
		{"evil.exe .", true},   // trailing dot and space are stripped by Windows
		{"setup.MSI", true},
		{"shortcut.lnk", true},
		{"invoices.terminal", true}, // macOS: Terminal profile runs its CommandString on open
		{"notes.pdf.Command", true},
		{"backup.scpt", true},
		{"photo.fileloc", true},
		{"report.desktop", true}, // Linux: xdg-open may honor the Exec= line
		{"report.pdf", false},
		{"main.go", false},
		{"README", false},         // no extension at all
		{"archive.tar.gz", false}, // .gz is not executable
	}
	for _, tc := range cases {
		if got := executableName(tc.name); got != tc.want {
			t.Errorf("executableName(%q) = %v, want %v", tc.name, got, tc.want)
		}
	}
}

// "o" on a file with an executable extension must refuse when the launch would reach the
// OS default handler, which would execute it. Nothing is fetched and nothing is launched.
func TestOpenInAppRefusesExecutable(t *testing.T) {
	b, fc, _, _ := fileTestBrowser(t)
	opened, _ := stubOpen(t)

	for _, name := range []string{"invoice.pdf.hta", "invoices-2026.terminal", "report.desktop"} {
		setName(b, 1, name)
		b.cursor = 1
		b.Handle(key(t, "o"))

		if len(fc.downloads) != 0 {
			t.Fatalf("o on %q fetched %v, want a refusal before any download", name, fc.downloads)
		}
		if *opened != "" {
			t.Fatalf("o on %q launched the default app on %q", name, *opened)
		}
		if !b.note.err {
			t.Fatalf("o on %q: status = %q, want an error", name, b.note.text)
		}
	}
}

// With an explicit OpenWith command the file is an argument to a program the
// user chose, not a ShellExecute target, so an executable extension is allowed.
func TestOpenInAppExecutableAllowedWithOpenWith(t *testing.T) {
	b, _, tmp, _ := fileTestBrowser(t)
	opened, with := stubOpen(t)

	b.SetOptions(Options{DownloadDir: b.opts.DownloadDir, OpenWith: "code -n"})
	setName(b, 1, "script.ps1")
	b.cursor = 1
	drive(t, b, b.Handle(key(t, "o")))

	if *with != "code -n" {
		t.Fatalf("openCmd got %q, want the configured %q", *with, "code -n")
	}
	if want := filepath.Join(tmp, "script.ps1"); *opened != want {
		t.Fatalf("opened %q, want %q", *opened, want)
	}
}

// "d" on the same executable-named file still downloads: saving a file is not
// executing it, so the guard applies only to "o".
func TestDownloadExecutableAllowed(t *testing.T) {
	b, fc, _, dl := fileTestBrowser(t)

	setName(b, 1, "invoice.pdf.hta")
	b.cursor = 1
	drive(t, b, b.Handle(key(t, "d")))

	want := filepath.Join(dl, "invoice.pdf.hta")
	if len(fc.downloads) != 1 || fc.downloads[0][1] != want {
		t.Fatalf("download of an executable = %v, want one to %s", fc.downloads, want)
	}
	if b.note.err {
		t.Fatalf("download of an executable failed: %q", b.note.text)
	}
}

// A reserved device name dressed up with the trailing dot/space or an extension
// Windows normalizes away must still be refused for "d" and "o" alike.
func TestRejectsNormalizedReservedNames(t *testing.T) {
	for _, name := range []string{"CON .txt", "con."} {
		t.Run(name, func(t *testing.T) {
			for _, k := range []string{"d", "o"} {
				b, fc, _, _ := fileTestBrowser(t)
				opened, _ := stubOpen(t)
				setName(b, 1, name)
				b.cursor = 1

				b.Handle(key(t, k))

				if len(fc.downloads) != 0 {
					t.Fatalf("%q on %q downloaded to %v, want a refusal", k, name, fc.downloads)
				}
				if *opened != "" {
					t.Fatalf("%q on %q launched the default app on %q", k, name, *opened)
				}
				if !b.note.err {
					t.Fatalf("%q on %q: status = %q, want an error", k, name, b.note.text)
				}
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

// pickyClient lists only the directories it knows, so a start directory can be
// made to fail the way a host's default directory does once it is renamed away.
type pickyClient struct {
	fakeClient
	ok map[string]bool
}

func (p *pickyClient) Upload(string, string) (int64, error) { return 0, nil }
func (p *pickyClient) Mkdir(string) error                   { return nil }
func (p *pickyClient) Remove(string) error                  { return nil }
func (p *pickyClient) Rename(string, string) error          { return nil }

func (p *pickyClient) List(dir string) ([]sftpx.Entry, error) {
	if !p.ok[dir] {
		return nil, fmt.Errorf("stat %s: no such file or directory", dir)
	}
	return p.entries, nil
}

// A start directory that lists is where the browser opens.
func TestNewOpensInTheStartDir(t *testing.T) {
	c := &pickyClient{ok: map[string]bool{"/srv/app": true, "/home/u": true}}
	b, err := New(c, "web1", "/srv/app", Options{DownloadDir: t.TempDir()}, 40, 13)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if b.Path() != "/srv/app" {
		t.Fatalf("cwd = %q, want /srv/app", b.Path())
	}
	if b.Status() != "" {
		t.Fatalf("status = %q, want none", b.Status())
	}
}

// One that does not — a default directory removed on the server — lands in the home
// directory instead, with the reason on the status line.
func TestNewFallsBackWhenTheStartDirIsGone(t *testing.T) {
	c := &pickyClient{ok: map[string]bool{"/home/u": true}}
	b, err := New(c, "web1", "/srv/gone", Options{DownloadDir: t.TempDir()}, 40, 13)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if b.Path() != "/home/u" {
		t.Fatalf("cwd = %q, want the home directory /home/u", b.Path())
	}
	if !strings.Contains(b.Status(), "/srv/gone") {
		t.Fatalf("status = %q, want it to name the directory that failed", b.Status())
	}
}

// With neither a listable start directory nor a listable home there is nothing to
// show, and New says so rather than handing back an empty browser.
func TestNewFailsWhenNothingLists(t *testing.T) {
	c := &pickyClient{ok: map[string]bool{}}
	if _, err := New(c, "web1", "/srv/gone", Options{DownloadDir: t.TempDir()}, 40, 13); err == nil {
		t.Fatal("New on a host that lists nothing: got nil error, want non-nil")
	}
}
