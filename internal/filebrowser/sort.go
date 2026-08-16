package filebrowser

import (
	"cmp"
	"slices"
	"strings"
	"time"

	"hop/internal/sftpx"
)

// sortMode is the order the listing is shown in. sortName is the zero value because it
// is what sftpx.Client.List already returns: a browser that has never seen "s" shows the
// server's listing untouched.
type sortMode int

const (
	sortName sortMode = iota
	sortSize
	sortMTime

	// sortModes is the number of modes "s" cycles through, kept next to them so adding a
	// mode does not silently leave it unreachable.
	sortModes = 3
)

// String names the mode for the status line, in the words the user thinks in rather than
// the identifier.
func (m sortMode) String() string {
	switch m {
	case sortSize:
		return "size"
	case sortMTime:
		return "modified"
	default:
		return "name"
	}
}

// now is the clock modTimeCol reads to decide whether a timestamp is old enough to lose
// its time of day. A variable so tests do not have to be run in a particular year.
var now = time.Now

// cycleSort advances to the next order and re-sorts the current listing.
//
// The cursor is carried to wherever its entry ended up rather than left on its old index.
// Every destructive key here — "d", "o" — acts on whatever the cursor stands on, so a
// sort that shuffled a different file under it would make the next keystroke a surprise.
func (b *Browser) cycleSort() {
	// Remember the entry before the order changes; afterwards its index means nothing.
	// An empty listing yields the zero Entry, whose name matches nothing — focus then
	// leaves the cursor where it is, which is the right answer for a listing with no rows.
	under, _ := b.selected()

	b.sortBy = (b.sortBy + 1) % sortModes
	b.entries = b.applySort(b.entries)

	b.focus(under.Name)
	b.ok("sorted by " + b.sortBy.String())
}

// applySort returns ents ordered by the browser's current mode, directories first in
// every mode — a listing whose directories are scattered through it by size or age is
// hard to navigate, and navigation is what this pane is for.
//
// The caller's slice is left alone: load hands over the slice the client returned, and
// re-sorting it in place would reorder a listing the client may still be caching.
func (b *Browser) applySort(ents []sftpx.Entry) []sftpx.Entry {
	// The lowercased name is the tie-break in every mode, so it is folded once per entry
	// rather than inside the comparator. A comparator that lowercases both operands does
	// it O(n log n) times, and the directory size is the remote host's choice — /usr/bin
	// or a log directory turns a few thousand entries into a hundred thousand full-string
	// scans for a sort that should cost one pass.
	keyed := make([]keyedEntry, len(ents))
	for i, e := range ents {
		keyed[i] = keyedEntry{e: e, fold: strings.ToLower(e.Name)}
	}

	mode := b.sortBy
	slices.SortFunc(keyed, func(a, c keyedEntry) int {
		if a.e.IsDir != c.e.IsDir {
			if a.e.IsDir {
				return -1
			}
			return 1
		}
		switch mode {
		case sortSize:
			if n := cmp.Compare(c.e.Size, a.e.Size); n != 0 { // largest first
				return n
			}
		case sortMTime:
			if n := cmp.Compare(c.e.ModTime, a.e.ModTime); n != 0 { // newest first
				return n
			}
		}
		// Every mode ends here, which is what makes each of them a total order — a
		// refresh must not reshuffle rows of equal size or age. The raw name breaks a
		// tie between names differing only in case, so even those have one fixed order.
		if n := cmp.Compare(a.fold, c.fold); n != 0 {
			return n
		}
		return cmp.Compare(a.e.Name, c.e.Name)
	})

	out := make([]sftpx.Entry, len(keyed))
	for i, k := range keyed {
		out[i] = k.e
	}
	return out
}

// keyedEntry is an entry beside its case-folded name, so the fold is paid for once per
// entry instead of once per comparison.
type keyedEntry struct {
	e    sftpx.Entry
	fold string
}

// modTimeCol renders e's modification time for the row's right-hand column, or "" when
// the server did not report one.
//
// The two formats are ls -l's: a timestamp from the current year keeps its time of day,
// an older one trades it for the year. Both are twelve cells wide, so the column stays
// straight down the listing whichever side of new year an entry falls on.
func (b *Browser) modTimeCol(e sftpx.Entry) string {
	if e.ModTime == 0 {
		return ""
	}
	t := time.Unix(e.ModTime, 0)
	if t.Year() == now().Year() {
		return t.Format("Jan 02 15:04")
	}
	return t.Format("Jan 02  2006")
}
