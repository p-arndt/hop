package filebrowser

import (
	"errors"
	"strings"
	"testing"

	"hop/internal/keys"
	"hop/internal/sftpx"
)

// dirClient lists a different set of entries per directory, which the flat fakeClient cannot.
type dirClient struct {
	fakeClient
	dirs map[string][]sftpx.Entry
}

func (c *dirClient) List(dir string) ([]sftpx.Entry, error) {
	c.lists++
	if c.listErr != nil {
		return nil, c.listErr
	}
	ents, ok := c.dirs[dir]
	if !ok {
		return nil, errors.New("no such directory: " + dir)
	}
	return ents, nil
}

// dir is shorthand for a directory entry.
func dir(name string) sftpx.Entry { return sftpx.Entry{Name: name, IsDir: true} }

// treeFixture is /home/u holding one directory and two files, with two files inside it.
func treeFixture(t *testing.T) (*Browser, *dirClient) {
	t.Helper()
	c := &dirClient{dirs: map[string][]sftpx.Entry{
		"/home/u":     {dir("src"), {Name: "a.txt", Size: 1}, {Name: "b.txt", Size: 2}},
		"/home/u/src": {{Name: "main.go", Size: 3}, {Name: "util.go", Size: 4}},
	}}
	b := &Browser{client: c, alias: "web1", opts: Options{VimKeys: true, DownloadDir: t.TempDir()}, w: 40, h: 13}
	if !b.load("/home/u") {
		t.Fatalf("load: %s", b.note.text)
	}
	c.lists = 0
	return b, c
}

func TestExpandListsOnceAndCaches(t *testing.T) {
	b, c := treeFixture(t)

	b.Do(keys.In) // open src
	if c.lists != 1 {
		t.Fatalf("%d listings on the first open, want 1", c.lists)
	}
	want := []string{"src", "  main.go", "  util.go", "a.txt", "b.txt"}
	if got := rowNames(b); !equalRows(got, want) {
		t.Fatalf("rows = %v, want %v", got, want)
	}

	b.Do(keys.In) // close it again
	b.Do(keys.In) // and re-open
	if c.lists != 1 {
		t.Fatalf("%d listings after a close and a re-open, want the cached contents", c.lists)
	}
	if got := rowNames(b); !equalRows(got, want) {
		t.Fatalf("rows = %v after re-opening, want %v", got, want)
	}
}

func TestExpandReportsAListingFailure(t *testing.T) {
	b, c := treeFixture(t)
	c.listErr = errors.New("permission denied")

	b.Do(keys.In)

	if b.rows[0].expanded {
		t.Fatal("a directory that failed to list was opened anyway")
	}
	if !b.note.err || !strings.Contains(b.note.text, "permission denied") {
		t.Fatalf("status = %q (err=%v), want the listing error", b.note.text, b.note.err)
	}
}

// The message names the file's real path, not a name joined to the current directory.
func TestInOnANestedFileOpensIt(t *testing.T) {
	b, _ := treeFixture(t)
	b.Do(keys.In) // open src
	b.Select(1)   // src/main.go

	cmd := b.Do(keys.In)
	if cmd == nil {
		t.Fatal("In on a nested file produced no command")
	}
	got := cmd().(Msg).Body.(OpenFileMsg)
	if got.Path != "/home/u/src/main.go" {
		t.Fatalf("OpenFileMsg.Path = %q, want /home/u/src/main.go", got.Path)
	}
}

func TestBrowserUpReRootsTheTree(t *testing.T) {
	c := &dirClient{dirs: map[string][]sftpx.Entry{
		"/home/u":     {dir("src")},
		"/home/u/src": {{Name: "main.go"}},
		"/home":       {dir("u")},
	}}
	b := &Browser{client: c, alias: "web1", w: 40, h: 13}
	if !b.load("/home/u") {
		t.Fatalf("load: %s", b.note.text)
	}
	b.Do(keys.In) // open src, so the cursor's directory is /home/u/src
	b.Select(1)

	b.Do(keys.BrowserUp)

	if b.rootPath() != "/home" {
		t.Fatalf("root = %q, want /home", b.rootPath())
	}
	if b.cwd != "/home" {
		t.Fatalf("cwd = %q, want /home", b.cwd)
	}
}

func TestRefreshKeepsTheTreeOpen(t *testing.T) {
	b, c := treeFixture(t)
	b.Do(keys.In) // open src
	b.Select(2)   // src/util.go

	c.dirs["/home/u/src"] = append(c.dirs["/home/u/src"], sftpx.Entry{Name: "new.go", Size: 5})
	b.Do(keys.BrowserRefresh)

	want := []string{"src", "  main.go", "  new.go", "  util.go", "a.txt", "b.txt"}
	if got := rowNames(b); !equalRows(got, want) {
		t.Fatalf("rows = %v, want %v", got, want)
	}
	if got := b.rows[b.cursor].path; got != "/home/u/src/util.go" {
		t.Fatalf("cursor stands on %s after the refresh, want the row it was on", got)
	}
}

// Marks survive where the entry is still there, and go where it is not.
func TestMarksSurviveARefresh(t *testing.T) {
	b, c := treeFixture(t)
	b.Do(keys.In) // open src

	b.Select(1)
	b.Do(keys.BrowserMark) // src/main.go
	b.Select(3)
	b.Do(keys.BrowserMark) // a.txt
	if len(b.marks) != 2 {
		t.Fatalf("marks = %v, want two", b.marks)
	}

	// main.go is still there; a.txt is gone from the server.
	c.dirs["/home/u"] = []sftpx.Entry{dir("src"), {Name: "b.txt", Size: 2}}
	b.Do(keys.BrowserRefresh)

	if !b.marks["/home/u/src/main.go"] {
		t.Fatalf("marks = %v, want the surviving entry still marked", b.marks)
	}
	if b.marks["/home/u/a.txt"] {
		t.Fatalf("marks = %v, want the deleted entry dropped", b.marks)
	}
}

// It stops at the last row rather than wrapping onto an entry it has already marked.
func TestMarkAdvancesTheCursor(t *testing.T) {
	b, _ := treeFixture(t)

	b.Do(keys.BrowserMark)
	if b.cursor != 1 {
		t.Fatalf("cursor = %d after marking, want it advanced to 1", b.cursor)
	}
	b.Do(keys.BrowserMark)
	b.Do(keys.BrowserMark)
	if b.cursor != 2 {
		t.Fatalf("cursor = %d at the last row, want it to stop there rather than wrap", b.cursor)
	}
	if len(b.marks) != 3 {
		t.Fatalf("marks = %v, want all three rows marked", b.marks)
	}
	if !strings.Contains(b.note.text, "3 marked") {
		t.Fatalf("status = %q, want the count", b.note.text)
	}

	b.Select(2)
	b.Do(keys.BrowserMark)
	if b.marks["/home/u/b.txt"] {
		t.Fatalf("marks = %v, want b.txt unmarked", b.marks)
	}
}

// "a" works on the current directory, not on the screen.
func TestMarkAllTakesTheCurrentDirectory(t *testing.T) {
	b, _ := treeFixture(t)
	b.Do(keys.In) // open src; the cursor is on it, so /home/u/src is the current directory

	b.Do(keys.BrowserMarkAll)
	if len(b.marks) != 2 || !b.marks["/home/u/src/main.go"] || !b.marks["/home/u/src/util.go"] {
		t.Fatalf("marks = %v, want the two entries of the open directory", b.marks)
	}

	b.Do(keys.BrowserMarkAll)
	if len(b.marks) != 0 {
		t.Fatalf("marks = %v, want the second press to clear them", b.marks)
	}

	// From a top-level file the current directory is the root, so "a" takes its three rows.
	b.Select(3) // a.txt
	b.Do(keys.BrowserMarkAll)
	if len(b.marks) != 3 || b.marks["/home/u/src/main.go"] {
		t.Fatalf("marks = %v, want the three entries of the root and nothing nested", b.marks)
	}
}

// With nothing marked an operation acts on the cursor's entry.
func TestTargetsFallsBackToTheCursor(t *testing.T) {
	b, _ := treeFixture(t)
	b.Select(1) // a.txt

	got := b.targets()
	if len(got) != 1 || got[0].path != "/home/u/a.txt" {
		t.Fatalf("targets = %v, want the entry under the cursor", nodePaths(got))
	}

	// A mark inside a closed directory still counts: hiding a file is not unmarking it.
	b.Select(0)
	b.Do(keys.In) // open src
	b.Select(2)
	b.Do(keys.BrowserMark) // src/util.go
	b.Select(0)
	b.Do(keys.In) // close src again

	got = b.targets()
	if len(got) != 1 || got[0].path != "/home/u/src/util.go" {
		t.Fatalf("targets = %v, want the mark inside the closed directory", nodePaths(got))
	}
}

// nodePaths is what a failure message says about a set of nodes.
func nodePaths(ns []*node) []string {
	out := make([]string, len(ns))
	for i, n := range ns {
		out[i] = n.path
	}
	return out
}

// A bare prefix test would let /home/user-data in under root /home/u.
func TestRevealDoesNotWalkIntoASibling(t *testing.T) {
	c := &dirClient{dirs: map[string][]sftpx.Entry{
		"/home/u":          {dir("ser-data")},
		"/home/u/ser-data": {{Name: "x.txt", Size: 1}},
	}}
	b := &Browser{client: c, alias: "web1", opts: Options{DownloadDir: t.TempDir()}, w: 40, h: 13}
	if !b.load("/home/u") {
		t.Fatalf("load: %s", b.note.text)
	}

	b.reveal("/home/user-data/x.txt")

	if got := rowNames(b); len(got) != 1 || strings.TrimSpace(got[0]) != "ser-data" {
		t.Fatalf("rows = %v, want the sibling path to have expanded nothing", got)
	}
}

// Otherwise the footer keeps pointing at it and "c" fails with a raw server error.
func TestTargetIsDroppedWhenItsDirectoryGoes(t *testing.T) {
	c := &dirClient{dirs: map[string][]sftpx.Entry{
		"/home/u":     {dir("sub"), {Name: "a.txt", Size: 1}},
		"/home/u/sub": {},
	}}
	b := &Browser{client: c, alias: "web1", opts: Options{DownloadDir: t.TempDir()}, w: 40, h: 13}
	if !b.load("/home/u") {
		t.Fatalf("load: %s", b.note.text)
	}

	b.Select(0)
	b.Do(keys.BrowserTarget)
	if b.target != "/home/u/sub" {
		t.Fatalf("target = %q, want it set", b.target)
	}

	c.dirs["/home/u"] = []sftpx.Entry{{Name: "a.txt", Size: 1}}
	delete(c.dirs, "/home/u/sub")
	b.refresh()

	if b.target != "" {
		t.Fatalf("target = %q, want it dropped with the directory", b.target)
	}
}
