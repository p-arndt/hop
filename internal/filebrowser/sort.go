package filebrowser

import (
	"cmp"
	"slices"
	"strings"
	"time"

	"hop/internal/sftpx"
)

// sortMode is the order the listing is shown in; sortName is the zero value because it is what List already returns.
type sortMode int

const (
	sortName sortMode = iota
	sortSize
	sortMTime

	// sortModes is the number of modes "s" cycles through.
	sortModes = 3
)

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

// now is the clock modTimeCol reads — a variable so tests need not run in a particular year.
var now = time.Now

// cycleSort advances to the next order, carrying the cursor to wherever its entry ended up.
func (b *Browser) cycleSort() {
	at := ""
	if n := b.cur(); n != nil {
		at = n.path
	}

	b.sortBy = (b.sortBy + 1) % sortModes
	b.resort(b.root)
	b.rebuild()

	b.focusPath(at)
	b.ok("sorted by " + b.sortBy.String())
}

// applySort returns ents ordered by the current mode, directories first, leaving the caller's slice alone.
func (b *Browser) applySort(ents []sftpx.Entry) []sftpx.Entry {
	// Fold once per entry, not inside the comparator: a big directory would otherwise pay O(n log n) full-string lowerings.
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
		// Every mode ends here, which is what makes each a total order: a refresh must not reshuffle equal rows.
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

// keyedEntry is an entry beside its case-folded name.
type keyedEntry struct {
	e    sftpx.Entry
	fold string
}

// modTimeCol renders e's mtime in ls -l's two formats, both twelve cells wide so the column stays straight.
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
