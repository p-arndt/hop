package filebrowser

import (
	"fmt"
	"path"
	"strings"
)

// Multi-selection and the copy target. Marks are held for the whole tree, keyed by
// absolute remote path, not per directory: open directories interleave their rows.

// markGlyph is the one-cell tick drawn beside (not in place of) the cursor bar.
var markGlyph = accentStyle.Render("✓")

func (b *Browser) marked(n *node) bool { return b.marks[n.path] }

// toggleMark flips the mark on the entry under the cursor and steps down a row, never
// wrapping: a wrap would land back on a marked entry and the next press would unmark it.
func (b *Browser) toggleMark() {
	n := b.cur()
	if n == nil {
		return
	}
	if b.marks[n.path] {
		delete(b.marks, n.path)
	} else {
		if b.marks == nil {
			b.marks = map[string]bool{}
		}
		b.marks[n.path] = true
	}
	b.cursor++
	b.clampScroll()
	b.noteMarks()
}

// toggleMarkAll marks every entry of the current directory, or clears them when all are
// marked; it works on the directory, not the screen, so open subdirectories are untouched.
func (b *Browser) toggleMarkAll() {
	dir := b.cwdNode()
	if dir == nil || len(dir.kids) == 0 {
		return
	}
	all := true
	for _, k := range dir.kids {
		if !b.marks[k.path] {
			all = false
			break
		}
	}
	if b.marks == nil {
		b.marks = map[string]bool{}
	}
	for _, k := range dir.kids {
		if all {
			delete(b.marks, k.path)
		} else {
			b.marks[k.path] = true
		}
	}
	b.noteMarks()
}

// noteMarks reports the size of the selection after a key changed it.
func (b *Browser) noteMarks() {
	switch n := len(b.marks); n {
	case 0:
		b.ok("nothing marked")
	case 1:
		b.ok("1 marked")
	default:
		b.ok(fmt.Sprintf("%d marked", n))
	}
}

func (b *Browser) clearMarks() { b.marks = nil }

// pruneTarget drops the copy target once its directory is gone; only a listed parent
// counts, since a target under an unread directory is unknown rather than absent.
func (b *Browser) pruneTarget() {
	if b.target == "" || b.nodeAt(b.target) != nil {
		return
	}
	parent := b.nodeAt(path.Dir(b.target))
	if parent == nil || !parent.loaded {
		return
	}
	b.target = ""
}

// pruneMarks drops marks whose entry is no longer in the tree; called after each listing.
func (b *Browser) pruneMarks() {
	if len(b.marks) == 0 || b.root == nil {
		return
	}
	live := make(map[string]bool, len(b.marks))
	walk(b.root, func(n *node) {
		if b.marks[n.path] {
			live[n.path] = true
		}
	})
	// Safe to drop everything unwalked: a mark is only ever set on an existing node.
	b.marks = live
	if len(b.marks) == 0 {
		b.marks = nil
	}
}

// targets is what an operation acts on: marked entries in tree order, or the cursor's
// entry. The tree is walked, not the visible rows, so a collapsed mark still counts.
func (b *Browser) targets() []*node {
	if len(b.marks) == 0 {
		if n := b.cur(); n != nil {
			return []*node{n}
		}
		return nil
	}
	var out []*node
	if b.root != nil {
		walk(b.root, func(n *node) {
			if b.marks[n.path] {
				out = append(out, n)
			}
		})
	}
	return out
}

// ---- the copy target ----

// setTarget makes the directory under the cursor, or cwd when on a file, the destination
// for "c" and "v".
func (b *Browser) setTarget() {
	dst := b.cwd
	if n := b.cur(); n != nil && n.e.IsDir {
		dst = n.path
	}
	if dst == "" {
		return
	}
	if b.target == dst {
		// Re-pressing "t" on the target is the only way back to having none.
		b.target = ""
		b.ok("target cleared")
		return
	}
	b.target = dst
	b.ok("target: " + dst)
}

// destFor answers with the destination, the entries worth sending, and how many were
// already there. A directory into itself is refused up front — sftpx catches it too, but
// only after part of the tree is written.
func (b *Browser) destFor(verb string, srcs []*node) (string, []*node, int, bool) {
	if b.target == "" {
		b.fail(fmt.Errorf("%s: no target — press t on a directory first", verb))
		return "", nil, 0, false
	}
	keep := make([]*node, 0, len(srcs))
	for _, n := range srcs {
		if n.e.IsDir && descends(b.target, n.path) {
			b.fail(fmt.Errorf("%s %s: cannot %s a directory into itself", verb, n.e.Name, verb))
			return "", nil, 0, false
		}
		if path.Dir(n.path) == b.target {
			continue
		}
		keep = append(keep, n)
	}
	skipped := len(srcs) - len(keep)
	if len(keep) == 0 {
		b.say(fmt.Sprintf("%s: everything selected is already in %s", verb, b.target), refusalFor)
		return "", nil, 0, false
	}
	return b.target, keep, skipped, true
}

// descends reports whether p is dir or lies inside it.
func descends(p, dir string) bool {
	return p == dir || strings.HasPrefix(p, strings.TrimSuffix(dir, "/")+"/")
}
