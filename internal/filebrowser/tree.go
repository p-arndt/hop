package filebrowser

import (
	"path"
	"strings"

	"hop/internal/sftpx"
)

// node is one entry in the tree; loaded is what tells an empty directory from an unread one.
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

// setKids replaces n's children with ents, carrying over the expansion and cached kids of every directory still there.
func (b *Browser) setKids(n *node, ents []sftpx.Entry) {
	old := make(map[string]*node, len(n.kids))
	for _, k := range n.kids {
		old[k.e.Name] = k
	}

	sorted := b.applySort(ents)
	kids := make([]*node, len(sorted))
	for i, e := range sorted {
		k := newNode(n, e)
		// A name that was a directory and is now a file has nothing to carry over.
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

// walk calls fn for every node under n, open or not: marks span the whole tree, not just the visible rows.
func walk(n *node, fn func(*node)) {
	for _, k := range n.kids {
		fn(k)
		walk(k, fn)
	}
}

// loadKids lists n and hangs the result off it; a failure leaves the node closed rather than drawn empty.
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

// collapse shuts a directory node, pulling the cursor back onto it when it stood on something inside.
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

// cwdNode is the directory the cursor is in: itself when open, otherwise its parent. See syncCwd.
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

// syncCwd re-points b.cwd at the directory the cursor is in, which is what Path(), "m" and "u" read.
func (b *Browser) syncCwd() {
	if n := b.cwdNode(); n != nil {
		b.cwd = n.path
	}
}

// focusPath stands the cursor on the row for p, and leaves it where it is when p is not on screen.
func (b *Browser) focusPath(p string) {
	for i, n := range b.rows {
		if n.path == p {
			b.cursor = i
			break
		}
	}
	b.clampScroll()
}

// reveal opens every directory between the root and p and stands the cursor on it.
func (b *Browser) reveal(p string) {
	// The separator matters: a bare prefix test makes /ab/c look like it is under /a.
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

// nodeAt finds the node for the absolute path p anywhere in the part of the tree that has been read, or nil.
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

// resort re-orders every directory that has been read, not only the rows on screen.
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
