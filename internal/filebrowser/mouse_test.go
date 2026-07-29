package filebrowser

import (
	"testing"

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

// Activate is enter by another name: a directory is loaded in place and yields no
// command, a file yields the OpenFileMsg the model turns into an editor tab.
func TestActivate(t *testing.T) {
	b, fc := newTestBrowser(0)
	fc.entries = []sftpx.Entry{{Name: "sub", IsDir: true}, {Name: "notes.txt", Size: 3}}
	b.entries = fc.entries

	b.Select(0)
	if cmd := b.Activate(); cmd != nil {
		t.Fatal("activating a directory returned a command, want it loaded in place")
	}
	if b.cwd != "/home/u/sub" {
		t.Fatalf("cwd = %q, want /home/u/sub", b.cwd)
	}

	b.cwd = "/home/u"
	b.entries = fc.entries
	b.Select(1)
	cmd := b.Activate()
	if cmd == nil {
		t.Fatal("activating a file returned no command, want an OpenFileMsg")
	}
	msg, ok := cmd().(OpenFileMsg)
	if !ok {
		t.Fatalf("activating a file yielded %T, want OpenFileMsg", cmd())
	}
	if msg.Path != "/home/u/notes.txt" {
		t.Fatalf("OpenFileMsg.Path = %q, want /home/u/notes.txt", msg.Path)
	}
}
