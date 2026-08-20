package filebrowser

import (
	"strings"
	"testing"
	"time"

	"hop/internal/sftpx"
)

// sortFixture is a listing built to pin every rule at once: two directories, files whose
// size order and age order both disagree with the name order, and a pair tied on size and
// on mtime so the name tie-break is exercised.
func sortFixture() []sftpx.Entry {
	return []sftpx.Entry{
		{Name: "beta.txt", Size: 300, ModTime: 100},
		{Name: "src", IsDir: true, Size: 4096, ModTime: 900},
		{Name: "Alpha.txt", Size: 300, ModTime: 100},
		{Name: "zeta.log", Size: 10, ModTime: 500},
		{Name: "docs", IsDir: true, Size: 4096, ModTime: 50},
		{Name: "gamma.bin", Size: 900, ModTime: 20},
	}
}

// sortBrowser is a browser over the fixture, sized like newTestBrowser's.
func sortBrowser() *Browser {
	ents := sortFixture()
	b := &Browser{
		client: &fakeClient{entries: ents},
		opts:   Options{VimKeys: true},
		w:      40,
		h:      13,
	}
	return plant(b, "/home/u", ents)
}

// rowEntries is the visible tree as the entries it was built from, which is what the sort
// tests assert an order over.
func rowEntries(b *Browser) []sftpx.Entry {
	out := make([]sftpx.Entry, len(b.rows))
	for i, n := range b.rows {
		out[i] = n.e
	}
	return out
}

func names(ents []sftpx.Entry) []string {
	out := make([]string, len(ents))
	for i, e := range ents {
		out[i] = e.Name
	}
	return out
}

func TestApplySortOrders(t *testing.T) {
	for _, tc := range []struct {
		mode sortMode
		want []string
	}{
		{sortName, []string{"docs", "src", "Alpha.txt", "beta.txt", "gamma.bin", "zeta.log"}},
		// Directories keep their own block and sort by name inside it, being all of one
		// size; the tied 300-byte pair falls back to the name order.
		{sortSize, []string{"docs", "src", "gamma.bin", "Alpha.txt", "beta.txt", "zeta.log"}},
		{sortMTime, []string{"src", "docs", "zeta.log", "Alpha.txt", "beta.txt", "gamma.bin"}},
	} {
		b := sortBrowser()
		b.sortBy = tc.mode
		got := names(b.applySort(rowEntries(b)))
		if strings.Join(got, ",") != strings.Join(tc.want, ",") {
			t.Errorf("sort by %s: got %v, want %v", tc.mode, got, tc.want)
		}
	}
}

// applySort must not reorder the slice it is handed: load passes the client's listing
// straight through.
func TestApplySortCopies(t *testing.T) {
	b := sortBrowser()
	b.sortBy = sortSize
	in := sortFixture()
	before := names(in)
	b.applySort(in)
	if got := names(in); strings.Join(got, ",") != strings.Join(before, ",") {
		t.Errorf("applySort mutated its argument: got %v, want %v", got, before)
	}
}

// Equal rows must land in the same place every time, or a refresh would reshuffle them
// under the cursor.
func TestSortIsStableAcrossRuns(t *testing.T) {
	for _, mode := range []sortMode{sortName, sortSize, sortMTime} {
		b := sortBrowser()
		b.sortBy = mode
		first := strings.Join(names(b.applySort(sortFixture())), ",")
		// Feed it a differently-ordered input of the same entries.
		shuffled := sortFixture()
		for i, j := 0, len(shuffled)-1; i < j; i, j = i+1, j-1 {
			shuffled[i], shuffled[j] = shuffled[j], shuffled[i]
		}
		second := strings.Join(names(b.applySort(shuffled)), ",")
		if first != second {
			t.Errorf("sort by %s is not deterministic: %v vs %v", mode, first, second)
		}
	}
}

func TestSortKeyCycles(t *testing.T) {
	b := sortBrowser()
	for _, want := range []sortMode{sortSize, sortMTime, sortName} {
		b.Handle(key(t, "s"))
		if b.sortBy != want {
			t.Fatalf("after s: got %s, want %s", b.sortBy, want)
		}
		if !strings.Contains(b.note.text, want.String()) || b.note.err {
			t.Errorf("status %q does not name %s", b.note.text, want)
		}
	}
}

// The cursor follows its entry through a re-sort. "d" and "o" act on whatever it stands
// on, so this is a safety property rather than a nicety.
func TestSortKeepsCursorOnSameEntry(t *testing.T) {
	b := sortBrowser() // planted in name order, as load leaves it
	for i, n := range b.rows {
		if n.e.Name == "zeta.log" {
			b.cursor = i
		}
	}

	b.Handle(key(t, "s")) // size
	if got := b.rows[b.cursor].e.Name; got != "zeta.log" {
		t.Fatalf("cursor moved to %q after sorting by size", got)
	}
	b.Handle(key(t, "s")) // mtime
	if got := b.rows[b.cursor].e.Name; got != "zeta.log" {
		t.Fatalf("cursor moved to %q after sorting by mtime", got)
	}
}

// An empty listing has nothing to keep the cursor on; cycling must still work.
func TestSortEmptyListing(t *testing.T) {
	b := sortBrowser()
	plant(b, "/home/u", nil)
	b.Handle(key(t, "s"))
	if b.sortBy != sortSize || b.cursor != 0 {
		t.Errorf("empty listing: sortBy=%s cursor=%d", b.sortBy, b.cursor)
	}
}

func TestModTimeColumn(t *testing.T) {
	fixed := time.Date(2026, time.August, 16, 12, 0, 0, 0, time.Local)
	old := now
	now = func() time.Time { return fixed }
	t.Cleanup(func() { now = old })

	b := sortBrowser()

	thisYear := time.Date(2026, time.March, 4, 9, 5, 0, 0, time.Local)
	if got, want := b.modTimeCol(sftpx.Entry{ModTime: thisYear.Unix()}), "Mar 04 09:05"; got != want {
		t.Errorf("current-year mtime: got %q, want %q", got, want)
	}

	lastYear := time.Date(2021, time.December, 31, 23, 59, 0, 0, time.Local)
	if got, want := b.modTimeCol(sftpx.Entry{ModTime: lastYear.Unix()}), "Dec 31  2021"; got != want {
		t.Errorf("older mtime: got %q, want %q", got, want)
	}

	// Both formats must occupy the same column width.
	if a, c := b.modTimeCol(sftpx.Entry{ModTime: thisYear.Unix()}), b.modTimeCol(sftpx.Entry{ModTime: lastYear.Unix()}); len(a) != len(c) {
		t.Errorf("mtime formats differ in width: %q vs %q", a, c)
	}

	// An unreported time gets no column at all rather than the epoch.
	if got := b.modTimeCol(sftpx.Entry{Name: "f"}); got != "" {
		t.Errorf("unknown mtime: got %q, want empty", got)
	}
}
