package filebrowser

import (
	"fmt"
	"path"
	"strings"
)

// Multi-selection and the copy target. Every operation that used to act on the entry
// under the cursor now asks targets() what it is acting on, and targets() answers with
// the marked set — or, when nothing is marked, with the cursor's entry alone. That is the
// whole of the change at the call sites: a browser nobody has pressed space in behaves
// exactly as it did before.
//
// Marks are held for the whole tree, keyed by absolute remote path, not per directory.
// The tree is the reason: several directories are open at once and their rows are
// interleaved on screen, so "the marks of the current directory" is not a set the user
// can see or reason about — they see a column of ticks running across directory
// boundaries, and an operation that silently ignored half of them because the cursor had
// moved into a different directory would be indefensible. The cost is that a mark inside
// a directory that is later collapsed still counts; the footer shows the total at all
// times, which is what makes that honest rather than a trap.

// markGlyph is the tick drawn in the second gutter cell of a marked row. It is one cell
// wide, and it sits beside the cursor bar rather than in place of it: the bar says where
// the keyboard is, the tick says what an operation would touch, and a row is very often
// both.
var markGlyph = accentStyle.Render("✓")

func (b *Browser) marked(n *node) bool { return b.marks[n.path] }

// toggleMark flips the mark on the entry under the cursor and steps down a row.
//
// Advancing is what makes marking a run of files a matter of holding one key rather than
// alternating two. It stops at the last row instead of wrapping: a wrap would put the
// cursor back on an entry that is already marked, and the next press would unmark it.
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

// toggleMarkAll marks every entry of the current directory, or clears them when they are
// already all marked. It works on the directory rather than on the screen: the rows of an
// open subdirectory belong to that subdirectory, and "a" in a parent should not sweep up
// the contents of whatever happens to be open below it.
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

// noteMarks reports the size of the selection after a key changed it. The standing footer
// says the same thing, but only once nothing else wants the row, and a keystroke whose
// entire effect is a one-cell tick needs an answer of its own.
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

// clearMarks empties the selection, so a finished operation cannot be aimed a second time
// at paths that no longer exist.
func (b *Browser) clearMarks() { b.marks = nil }

// pruneMarks drops marks whose entry is no longer anywhere in the tree, and keeps every
// one whose entry is still there. It is called after each listing, which is the only
// moment the answer can change — a mark is a path, and a path stops being valid when the
// directory holding it says so.
// pruneTarget drops the copy target once the directory it names is gone. It only acts on a
// parent that has actually been listed: a target inside a directory nobody has opened is
// not absent, it is unread, and forgetting it there would lose an aim the user set on
// purpose. Without this the footer keeps pointing at a deleted directory and "c" fails with
// a raw server error instead of saying the target is gone.
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
	// A mark inside a directory that has never been read is neither confirmed nor denied
	// by this listing, so it cannot be pruned — but it also cannot exist: marks are only
	// ever set on a node, and a node only exists once its directory has been read.
	b.marks = live
	if len(b.marks) == 0 {
		b.marks = nil
	}
}

// targets is what an operation acts on: the marked entries in tree order, or the entry
// under the cursor when nothing is marked.
//
// The tree is walked rather than the visible rows, so a mark inside a collapsed directory
// still counts. Hiding a file is not unmarking it, and an operation that quietly dropped
// entries the footer is still counting would be the worse surprise of the two.
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

// setTarget makes the directory under the cursor — or the current directory, when the
// cursor is on a file — the destination for "c" and "v".
//
// One target at a time, deliberately: the target is an aim, and a list of them would turn
// every copy into a question about which one was meant. Pressing "t" again re-aims it.
func (b *Browser) setTarget() {
	dst := b.cwd
	if n := b.cur(); n != nil && n.e.IsDir {
		dst = n.path
	}
	if dst == "" {
		return
	}
	if b.target == dst {
		// Pressing "t" on the directory that is already the target takes the aim off,
		// which is the only way to get back to having none.
		b.target = ""
		b.ok("target cleared")
		return
	}
	b.target = dst
	b.ok("target: " + dst)
}

// destFor checks that a copy or a move can be attempted at all and answers with the
// destination, the entries worth sending, and how many of the selection were already there.
//
// The two cases are not the same and are not treated the same. A directory copied into
// itself would recurse into what it is writing, so it refuses the keystroke outright —
// sftpx refuses it too, but by then the transfer has started and some of the tree is
// written. An entry that already sits in the target is merely nothing to do, so it is
// skipped and counted, the way download skips the directories in a selection: "a" then "c"
// on a screenful is the workflow "a" exists for, and refusing all of it because one file
// was already there would make marking useless in exactly that case.
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
