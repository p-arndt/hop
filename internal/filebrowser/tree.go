package filebrowser

import (
	"path"
	"strings"

	"hop/internal/sftpx"
)

// The listing is a tree, not a directory. A pane this narrow is used as a sidebar next to
// the file being edited, and walking in and out of directories to find a sibling three
// levels away costs more keystrokes than the edit does — so a directory opens in place,
// under the row it was on, and the rows above and below stay where they were.
//
// The whole of the tree lives in nodes, but nothing outside this file addresses one by
// walking parents and children: View, RowAt, Select, Scroll and every motion work on
// b.rows, the flattened list of what is currently visible, indexed from zero. That is the
// invariant the mouse depends on — a click is a row number and nothing else — and the
// reason the tree could be added without touching a line of the pointer handling.

// node is one entry in the tree: the listing row it came from, where it lives, and — for
// a directory — whether it is open and what is inside it.
//
// Children are read on the first expand and kept afterwards, so re-opening a directory is
// free and a collapse loses nothing. loaded is what says a nil kids slice means "empty
// directory" rather than "not looked at yet"; without it every collapsed empty directory
// would re-list on each keypress.
type node struct {
	e      sftpx.Entry
	path   string // absolute remote path
	parent *node
	depth  int // 0 for the root's own children, which are the top-level rows

	expanded bool
	loaded   bool
	kids     []*node
}

func newNode(parent *node, e sftpx.Entry) *node {
	return &node{e: e, path: path.Join(parent.path, e.Name), parent: parent, depth: parent.depth + 1}
}

// setKids replaces n's children with ents, carrying over the expansion — and the
// already-read contents — of every child that is still there under the same name.
//
// Carrying it over is the whole point of the function: a refresh after a rename or an
// upload re-lists the tree, and a user watching three open directories must not have them
// all snap shut because one file changed somewhere below.
func (b *Browser) setKids(n *node, ents []sftpx.Entry) {
	old := make(map[string]*node, len(n.kids))
	for _, k := range n.kids {
		old[k.e.Name] = k
	}

	sorted := b.applySort(ents)
	kids := make([]*node, len(sorted))
	for i, e := range sorted {
		k := newNode(n, e)
		// Only a directory that is still a directory keeps its state: a name that was a
		// directory and is now a file has nothing to carry over, and the cached children
		// would describe something that no longer exists.
		if p, ok := old[e.Name]; ok && p.e.IsDir && e.IsDir {
			k.expanded, k.loaded, k.kids = p.expanded, p.loaded, p.kids
			for _, gk := range k.kids {
				gk.parent = k
			}
		}
		kids[i] = k
	}
	n.kids = kids
	n.loaded = true
}

// rebuild flattens the visible tree into b.rows.
func (b *Browser) rebuild() {
	b.rows = b.rows[:0]
	if b.root != nil {
		b.appendRows(b.root)
	}
	b.clampScroll()
}

// appendRows adds n's open descendants in the order they are drawn.
func (b *Browser) appendRows(n *node) {
	for _, k := range n.kids {
		b.rows = append(b.rows, k)
		if k.e.IsDir && k.expanded {
			b.appendRows(k)
		}
	}
}

// walk calls fn for every node under n, open or not. Marks are held for the whole tree
// rather than for one directory, so the operations have to see the nodes a collapsed
// directory is hiding as well as the rows on screen.
func walk(n *node, fn func(*node)) {
	for _, k := range n.kids {
		fn(k)
		walk(k, fn)
	}
}

// loadKids lists n and hangs the result off it, reporting whether the listing worked. A
// failure leaves the node closed with the reason on the status line: half-opening a
// directory that could not be read would draw it as empty, which is a different fact.
func (b *Browser) loadKids(n *node) bool {
	ents, err := b.client.List(n.path)
	if err != nil {
		b.fail(err)
		return false
	}
	b.setKids(n, ents)
	b.pruneMarks()
	b.pruneTarget()
	return true
}

func (b *Browser) expand(n *node) {
	if n == nil || !n.e.IsDir || n.expanded {
		return
	}
	if !n.loaded && !b.loadKids(n) {
		return
	}
	n.expanded = true
	b.rebuild()
}

// collapse shuts a directory node. The cursor is pulled back onto the directory itself when it was standing on something
// inside: the row it was on is about to stop existing, and clamping it into whatever
// slides into that index would leave the next keystroke aimed at a file nobody chose.
func (b *Browser) collapse(n *node) {
	if n == nil || !n.e.IsDir || !n.expanded {
		return
	}
	n.expanded = false

	at := n.path
	if cur := b.cur(); cur != nil && !under(n, cur) {
		at = cur.path
	}
	b.rebuild()
	b.focusPath(at)
}

// under reports whether c is n itself or somewhere inside it.
func under(n, c *node) bool {
	for p := c; p != nil; p = p.parent {
		if p == n {
			return true
		}
	}
	return false
}

// cur is the node under the cursor, or nil in an empty tree.
func (b *Browser) cur() *node {
	if b.cursor < 0 || b.cursor >= len(b.rows) {
		return nil
	}
	return b.rows[b.cursor]
}

// cwdNode is the node standing for the current directory: the directory under the cursor
// when it is open — being inside a directory is what having it open means — and otherwise
// the directory the cursor's row lives in. See syncCwd.
func (b *Browser) cwdNode() *node {
	n := b.cur()
	if n == nil {
		return b.root
	}
	if n.e.IsDir && n.expanded {
		return n
	}
	return n.parent
}

// syncCwd re-points b.cwd at the directory the cursor is in.
//
// "The current directory" has to keep meaning something once the listing is a tree: other
// packages read Path(), and "m" and "u" need a directory to act in. The answer that
// matches what the eye sees is the directory the cursor is inside — an open directory the
// cursor stands on, or the parent of any other row — so walking down a tree moves the
// current directory exactly as walking into one used to.
func (b *Browser) syncCwd() {
	if n := b.cwdNode(); n != nil {
		b.cwd = n.path
	}
}

// focusPath stands the cursor on the row for p, and leaves it where it is when p is not
// on screen — the entry may have been deleted, renamed by the server to something else,
// or sit inside a directory that has since been closed.
func (b *Browser) focusPath(p string) {
	for i, n := range b.rows {
		if n.path == p {
			b.cursor = i
			break
		}
	}
	b.clampScroll()
}

// reveal opens every directory between the root and p and stands the cursor on it, so an
// entry created or renamed deeper in the tree is shown rather than only re-listed.
func (b *Browser) reveal(p string) {
	// The separator matters: a bare prefix test makes /ab/c look like it is under /a, and
	// reveal would then walk into a sibling. descends applies the same rule to marks.
	if b.root == nil || !descends(p, b.root.path) {
		return
	}
	n := b.root
	for _, seg := range strings.Split(strings.Trim(strings.TrimPrefix(p, b.root.path), "/"), "/") {
		next := childNamed(n, seg)
		if seg == "" || next == nil || next.path == p || !next.e.IsDir {
			break
		}
		if !next.loaded && !b.loadKids(next) {
			break
		}
		next.expanded = true
		n = next
	}
	b.rebuild()
	b.focusPath(p)
}

// nodeAt finds the node for the absolute path p anywhere in the part of the tree that has
// been read, or nil. It is how an operation that captured a directory when it opened its
// prompt finds that directory again once the answer comes in — the cursor may have moved
// somewhere else entirely in between.
func (b *Browser) nodeAt(p string) *node {
	if b.root == nil {
		return nil
	}
	if b.root.path == p {
		return b.root
	}
	var found *node
	walk(b.root, func(n *node) {
		if n.path == p {
			found = n
		}
	})
	return found
}

func childNamed(n *node, name string) *node {
	for _, k := range n.kids {
		if k.e.Name == name {
			return k
		}
	}
	return nil
}

// resort re-orders every directory that has been read, so "s" applies to the whole tree
// and not only to the rows that happen to be on screen.
func (b *Browser) resort(n *node) {
	if n == nil || !n.loaded {
		return
	}
	ents := make([]sftpx.Entry, len(n.kids))
	for i, k := range n.kids {
		ents[i] = k.e
	}
	b.setKids(n, ents)
	for _, k := range n.kids {
		b.resort(k)
	}
}
