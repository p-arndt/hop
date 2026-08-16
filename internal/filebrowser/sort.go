package filebrowser

import (
	"sort"
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
	under, hadEntry := b.selected()

	b.sortBy = (b.sortBy + 1) % sortModes
	b.entries = b.applySort(b.entries)

	if hadEntry {
		for i, e := range b.entries {
			if e.Name == under.Name {
				b.cursor = i
				break
			}
		}
	}
	b.clampScroll()
	b.ok("sorted by " + b.sortBy.String())
}

// applySort returns ents ordered by the browser's current mode, directories first in
// every mode — a listing whose directories are scattered through it by size or age is
// hard to navigate, and navigation is what this pane is for.
//
// The caller's slice is left alone: load hands over the slice the client returned, and
// re-sorting it in place would reorder a listing the client may still be caching.
func (b *Browser) applySort(ents []sftpx.Entry) []sftpx.Entry {
	out := make([]sftpx.Entry, len(ents))
	copy(out, ents)

	mode := b.sortBy
	sort.Slice(out, func(i, j int) bool {
		a, c := out[i], out[j]
		if a.IsDir != c.IsDir {
			return a.IsDir
		}
		switch mode {
		case sortSize:
			if a.Size != c.Size {
				return a.Size > c.Size
			}
		case sortMTime:
			if a.ModTime != c.ModTime {
				return a.ModTime > c.ModTime
			}
		}
		return nameLess(a.Name, c.Name)
	})
	return out
}

// nameLess orders two entry names the way a person reads a directory: case-insensitively,
// falling back to the raw bytes so that names differing only in case still have one fixed
// order. Every mode ends here, which is what makes each of them a total order — a refresh
// must not reshuffle rows of equal size or age.
func nameLess(a, b string) bool {
	la, lb := strings.ToLower(a), strings.ToLower(b)
	if la != lb {
		return la < lb
	}
	return a < b
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
