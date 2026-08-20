package filebrowser

import (
	"testing"

	"hop/internal/keys"
	"hop/internal/sftpx"
)

// RowAt runs View's layout backwards: row 0 is the path header, row 1 the rule, and
// the entries start at row 2. The rows past the listing — the padding under a short
// directory and the status line — hold no entry, and must say so rather than clamp
// onto the last one.
func TestRowAt(t *testing.T) {
	b, _ := newTestBrowser(3) // contentRows() == 10

	cases := []struct {
		row   int
		want  int
		found bool
	}{
		{0, 0, false},  // the path header
		{1, 0, false},  // the rule
		{2, 0, true},   // the first entry
		{4, 2, true},   // the last of three
		{5, 0, false},  // padding under a short listing
		{12, 0, false}, // the status line
		{-1, 0, false},
	}
	for _, c := range cases {
		got, ok := b.RowAt(c.row)
		if ok != c.found || (ok && got != c.want) {
			t.Errorf("RowAt(%d) = %d, %v; want %d, %v", c.row, got, ok, c.want, c.found)
		}
	}
}

// A scrolled listing maps rows through the same window it is drawn with: the top
// row is the first *visible* entry, not the first entry.
func TestRowAtScrolled(t *testing.T) {
	b, _ := newTestBrowser(30)
	b.Select(25) // scrolls the window down to keep the cursor visible

	if b.scroll == 0 {
		t.Fatal("selecting the 26th of 30 entries did not scroll the window")
	}
	got, ok := b.RowAt(2)
	if !ok || got != b.scroll {
		t.Fatalf("RowAt(2) = %d, %v; want the first visible entry %d", got, ok, b.scroll)
	}
}

// Select stands on an entry as a click does, and clamps rather than refusing an
// index off either end — the same contract every other move here has.
func TestSelectClamps(t *testing.T) {
	b, _ := newTestBrowser(5)

	b.Select(3)
	if b.cursor != 3 {
		t.Fatalf("cursor = %d, want 3", b.cursor)
	}
	b.Select(99)
	if b.cursor != 4 {
		t.Fatalf("cursor = %d after selecting past the end, want the last entry 4", b.cursor)
	}
	b.Select(-4)
	if b.cursor != 0 {
		t.Fatalf("cursor = %d after selecting before the start, want 0", b.cursor)
	}
}

// The wheel moves the cursor, not the window alone: whatever is under it is what
// "d" would download. It stops at both ends of the listing.
func TestScroll(t *testing.T) {
	b, _ := newTestBrowser(30)

	b.Scroll(3)
	if b.cursor != 3 {
		t.Fatalf("cursor = %d after one notch down, want 3", b.cursor)
	}
	b.Scroll(-9)
	if b.cursor != 0 {
		t.Fatalf("cursor = %d after scrolling back past the top, want 0", b.cursor)
	}
	b.Scroll(999)
	if b.cursor != 29 {
		t.Fatalf("cursor = %d after scrolling past the end, want the last entry 29", b.cursor)
	}
	if b.cursor < b.scroll || b.cursor >= b.scroll+b.contentRows() {
		t.Fatalf("cursor %d scrolled out of the window [%d, %d)", b.cursor, b.scroll, b.scroll+b.contentRows())
	}
}

// Activate is enter by another name: a directory opens in place and yields no command, a
// file yields the OpenFileMsg the model turns into an editor tab.
func TestActivate(t *testing.T) {
	b, fc := newTestBrowser(0)
	fc.entries = []sftpx.Entry{{Name: "sub", IsDir: true}, {Name: "notes.txt", Size: 3}}
	plant(b, "/home/u", fc.entries)

	b.Select(0)
	if cmd := b.Activate(); cmd != nil {
		t.Fatal("activating a directory returned a command, want it opened in place")
	}
	// Opening a directory is being inside it: that is what the current directory means
	// once the listing is a tree, and it is what "m" and "u" then act on.
	if b.cwd != "/home/u/sub" {
		t.Fatalf("cwd = %q, want /home/u/sub", b.cwd)
	}
	if !b.rows[0].expanded {
		t.Fatal("the directory did not open")
	}

	// The rows below it are its contents, indented, with the parent's own siblings after.
	want := []string{"sub", "  sub", "  notes.txt", "notes.txt"}
	if got := rowNames(b); !equalRows(got, want) {
		t.Fatalf("rows = %v, want %v", got, want)
	}

	b.Select(3) // the top-level notes.txt, below the open directory
	cmd := b.Activate()
	if cmd == nil {
		t.Fatal("activating a file returned no command, want an OpenFileMsg")
	}
	wrapped, ok := cmd().(Msg)
	if !ok {
		t.Fatalf("activating a file yielded %T, want a filebrowser.Msg", cmd())
	}
	msg, ok := wrapped.Body.(OpenFileMsg)
	if !ok {
		t.Fatalf("activating a file yielded a %T body, want OpenFileMsg", wrapped.Body)
	}
	if msg.Path != "/home/u/notes.txt" {
		t.Fatalf("OpenFileMsg.Path = %q, want /home/u/notes.txt", msg.Path)
	}

	// And a file inside the open directory is the one at its own depth, not the one that
	// shares its name at the top level — the row index is the whole of the address.
	b.Select(2)
	inner := b.Activate()
	if inner == nil {
		t.Fatal("activating the nested file returned no command")
	}
	if got := inner().(Msg).Body.(OpenFileMsg).Path; got != "/home/u/sub/notes.txt" {
		t.Fatalf("nested OpenFileMsg.Path = %q, want /home/u/sub/notes.txt", got)
	}
}

// equalRows compares two row listings.
func equalRows(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

// treeBrowser opens a browser whose first entry is a directory, and expands it, so the
// pointer tests run against a tree rather than a flat listing. The client lists the same
// three entries for every directory, which is all a row-index test needs.
func treeBrowser(t *testing.T) (*Browser, *fakeClient) {
	t.Helper()
	b, fc := newTestBrowser(0)
	fc.entries = []sftpx.Entry{
		{Name: "sub", IsDir: true},
		{Name: "a.txt", Size: 1},
		{Name: "b.txt", Size: 2},
	}
	plant(b, "/home/u", fc.entries)
	b.Select(0)
	b.Activate() // opens sub, so the rows are sub, sub/*, a.txt, b.txt
	if len(b.rows) != 6 {
		t.Fatalf("setup: %d rows, want 6", len(b.rows))
	}
	return b, fc
}

// The flat row index is the mouse's whole vocabulary, and an open directory must not
// change that: row 2 of the view is row 2 of the flattened tree, whichever directory that
// row's entry happens to live in.
func TestRowAtInAnOpenTree(t *testing.T) {
	b, _ := treeBrowser(t)

	for row, want := range map[int]string{
		2: "/home/u/sub",
		3: "/home/u/sub/sub",
		5: "/home/u/sub/b.txt",
		6: "/home/u/a.txt",
		7: "/home/u/b.txt",
	} {
		i, ok := b.RowAt(row)
		if !ok {
			t.Fatalf("RowAt(%d) found nothing in a tree of %d rows", row, len(b.rows))
		}
		if got := b.rows[i].path; got != want {
			t.Fatalf("RowAt(%d) = row %d (%s), want %s", row, i, got, want)
		}
	}
	// Past the last row of the tree there is still nothing, as under a short listing.
	if _, ok := b.RowAt(8); ok {
		t.Fatalf("RowAt(8) found an entry in a tree of %d rows", len(b.rows))
	}
}

// Select and Scroll address the same flattened rows, so a click and a wheel notch inside
// an open subdirectory land on the entry that was drawn there.
func TestSelectAndScrollInAnOpenTree(t *testing.T) {
	b, _ := treeBrowser(t)

	b.Select(2)
	if got := b.rows[b.cursor].path; got != "/home/u/sub/a.txt" {
		t.Fatalf("Select(2) stood on %s, want /home/u/sub/a.txt", got)
	}
	// The cursor is inside the open directory, so that is the current directory.
	if b.cwd != "/home/u/sub" {
		t.Fatalf("cwd = %q, want /home/u/sub", b.cwd)
	}

	b.Scroll(2)
	if got := b.rows[b.cursor].path; got != "/home/u/a.txt" {
		t.Fatalf("two notches on stood on %s, want /home/u/a.txt", got)
	}
	if b.cwd != "/home/u" {
		t.Fatalf("cwd = %q after leaving the open directory, want /home/u", b.cwd)
	}

	b.Scroll(999)
	if b.cursor != len(b.rows)-1 {
		t.Fatalf("cursor = %d after scrolling past the end, want %d", b.cursor, len(b.rows)-1)
	}
}

// Closing a directory takes its rows away, and the cursor comes back to the directory
// itself rather than being clamped onto whatever slid into its index.
func TestCollapseBringsTheCursorBack(t *testing.T) {
	b, _ := treeBrowser(t)
	b.Select(2) // sub/a.txt

	// Left is one key with three meanings, taken in order: from inside an open directory
	// it steps out to the directory itself, and from there it closes it.
	b.Do(keys.Out)
	if got := b.rows[b.cursor].path; got != "/home/u/sub" {
		t.Fatalf("cursor stands on %s after stepping out, want the directory /home/u/sub", got)
	}
	b.Do(keys.Out)
	if b.rows[0].expanded {
		t.Fatal("the directory is still open")
	}
	if len(b.rows) != 3 {
		t.Fatalf("%d rows after the close, want the three top-level ones", len(b.rows))
	}
}
