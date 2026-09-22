package filebrowser

import (
	"strings"
	"testing"

	"hop/internal/keys"
	"hop/internal/sftpx"
)

func TestGoToReRootsTheTreeAtTheDirectory(t *testing.T) {
	b, _ := treeFixture(t)
	b.Select(2) // b.txt, so the cursor has somewhere to be reset from

	if !b.GoTo("/home/u/src") {
		t.Fatalf("GoTo refused a listable directory: %s", b.note.text)
	}
	if b.rootPath() != "/home/u/src" || b.Path() != "/home/u/src" {
		t.Fatalf("root = %q, path = %q; want both /home/u/src", b.rootPath(), b.Path())
	}
	if want := []string{"main.go", "util.go"}; !equalRows(rowNames(b), want) || b.cursor != 0 {
		t.Fatalf("rows = %v, cursor = %d; want %v from the top", rowNames(b), b.cursor, want)
	}
}

// A directory the browser cannot list leaves it where it was, saying why.
func TestGoToAnUnlistableDirectoryStaysPut(t *testing.T) {
	b, _ := treeFixture(t)

	if b.GoTo("/root") {
		t.Fatal("GoTo reported success for a directory that does not list")
	}
	if b.rootPath() != "/home/u" || len(b.rows) != 3 {
		t.Fatalf("root = %q with %d rows; want the old tree kept", b.rootPath(), len(b.rows))
	}
	if !b.note.err || !strings.Contains(b.note.text, "/root") {
		t.Fatalf("note = %q (err=%v), want the listing error", b.note.text, b.note.err)
	}
}

func TestCursorDirIsTheDirectoryPointedAt(t *testing.T) {
	b, _ := treeFixture(t)

	if got := b.CursorDir(); got != "/home/u/src" {
		t.Fatalf("on a closed directory CursorDir = %q, want the directory itself", got)
	}
	b.Select(1) // a.txt
	if got := b.CursorDir(); got != "/home/u" {
		t.Fatalf("on a top-level file CursorDir = %q, want the root", got)
	}
	b.Select(0)
	b.Do(keys.In) // open src
	b.Select(2)   // src/util.go
	if got := b.CursorDir(); got != "/home/u/src" {
		t.Fatalf("on a nested file CursorDir = %q, want its parent", got)
	}
}

func TestCursorDirInAnEmptyDirectoryIsTheRoot(t *testing.T) {
	c := &dirClient{dirs: map[string][]sftpx.Entry{"/srv/empty": {}}}
	b := &Browser{client: c, alias: "web1", w: 40, h: 13}
	if !b.load("/srv/empty") {
		t.Fatalf("load: %s", b.note.text)
	}
	if got := b.CursorDir(); got != "/srv/empty" {
		t.Fatalf("CursorDir = %q, want /srv/empty", got)
	}
}
