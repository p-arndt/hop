package filebrowser

import (
	"errors"
	"strings"
	"testing"

	"hop/internal/keys"
	"hop/internal/sftpx"
)

// The tree and the multi-selection, which are one subject: what a key acts on is the
// marked set, and what can be marked is whatever the tree is currently showing.

// dirClient lists a different set of entries per directory, which the flat fakeClient
// cannot — a tree test needs /a to hold something other than what its parent holds.
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

// treeFixture is /home/u holding one directory and two files, with two more files inside
// the directory. It is the smallest shape that has a nesting to get wrong.
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

// A directory is listed on the first open and never again: the contents are kept, so
// closing and re-opening it is free. A pane that re-listed on every twisty would put a
// round trip behind a cursor key.
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

// A directory that will not list stays shut with the reason on the status line. Drawing
// it open and empty would claim it has no contents, which is a different fact.
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

// Enter on a file is still an OpenFileMsg, and it names the file's real path — which,
// inside an open directory, is not a name joined to the current directory.
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

// Backspace re-roots the tree above wherever it is rooted, whatever the cursor is doing
// three levels down — it is how the visible tree grows upwards.
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

// A refresh re-lists the whole tree in place: the directories the user has open stay
// open, and the cursor stays on the row it was on. Snapping every directory shut because
// one file changed somewhere would throw away the view being worked in.
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

// Marks survive a refresh where the entry is still there, and go where it is not. A mark
// is a path, and a listing is the only thing that can say whether it still means anything.
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

// Space marks the entry and steps down, which is what makes marking a run of files one
// held key rather than two alternating ones. It stops at the last row rather than
// wrapping onto an entry it has already marked.
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

	// Space on a marked row takes the mark off again.
	b.Select(2)
	b.Do(keys.BrowserMark)
	if b.marks["/home/u/b.txt"] {
		t.Fatalf("marks = %v, want b.txt unmarked", b.marks)
	}
}

// "a" works on the current directory, not on the screen: an open subdirectory's rows
// belong to that subdirectory, and marking a parent must not sweep them up.
func TestMarkAllTakesTheCurrentDirectory(t *testing.T) {
	b, _ := treeFixture(t)
	b.Do(keys.In) // open src; the cursor is on it, so /home/u/src is the current directory

	b.Do(keys.BrowserMarkAll)
	if len(b.marks) != 2 || !b.marks["/home/u/src/main.go"] || !b.marks["/home/u/src/util.go"] {
		t.Fatalf("marks = %v, want the two entries of the open directory", b.marks)
	}

	// Again, and they all come off.
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

// With nothing marked an operation acts on the cursor's entry, which is what keeps a
// browser nobody has pressed space in behaving exactly as it did before.
func TestTargetsFallsBackToTheCursor(t *testing.T) {
	b, _ := treeFixture(t)
	b.Select(1) // a.txt

	got := b.targets()
	if len(got) != 1 || got[0].path != "/home/u/a.txt" {
		t.Fatalf("targets = %v, want the entry under the cursor", nodePaths(got))
	}

	// Marked entries are answered in tree order however they were marked, and a mark
	// inside a closed directory still counts — hiding a file is not unmarking it.
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

// reveal walks the tree by path segment, so a bare prefix test would let a sibling in:
// with root /home/u, /home/user-data starts with the root's path but is not inside it.
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

// The target is an aim at a directory, and a deleted directory is not one. Without this
// the footer keeps pointing at it and "c" fails with a raw server error instead.
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
