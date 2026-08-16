package filebrowser

import "hop/internal/sftpx"

// sortMode is the order the listing is held in. STUB — the sort toggle is not built yet.
type sortMode int

const (
	sortName sortMode = iota
)

// cycleSort advances the sort order. STUB.
func (b *Browser) cycleSort() {}

// applySort returns ents in the browser's sort order. STUB: the client already sorts
// directories-first by name, so this is currently a pass-through.
func (b *Browser) applySort(ents []sftpx.Entry) []sftpx.Entry { return ents }

// modTimeCol is the mtime column for e, or "" when the browser does not show one. STUB.
func (b *Browser) modTimeCol(e sftpx.Entry) string { return "" }
