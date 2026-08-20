package filebrowser

import (
	"fmt"
	"path"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

// The three keys that change something on the server: "x" deletes, "R" renames, "m"
// makes a directory. Each of them asks first — a confirm for the delete, a line of text
// for the other two — so no single keystroke mutates the remote host. The question is
// the prompt overlay, which owns the keyboard until it is answered or dismissed; these
// functions therefore only open it and return, and the work happens in the callback.

// remove deletes what is marked — or the entry under the cursor when nothing is — behind
// a confirm. "x" is one key away from every motion in the listing, and a delete over SFTP
// has no undo, so the question spells out what is about to go: the name when it is one
// entry, and the count when it is several. A count rather than one of the names is the
// point of the plural form — "delete a.txt?" while eleven files are ticked would describe
// a tenth of what the key is about to do.
func (b *Browser) remove() tea.Cmd {
	// The selection is captured now rather than read again in the callback: nothing can
	// move the cursor while the overlay owns the keyboard, but the delete is meant for
	// what the question described, not for whatever is marked when it is answered.
	victims := b.targets()
	if len(victims) == 0 {
		return nil
	}
	// The row is captured too: after the delete it holds the successor of what was there,
	// which is where the eye already is.
	at := b.cursor

	b.askConfirm(deleteQuestion(victims), func(b *Browser, _ string) tea.Cmd {
		done := 0
		for i, n := range victims {
			if err := b.client.Remove(n.path); err != nil {
				// The server refuses a directory that still has contents, and this browser
				// does not offer a recursive delete: walking a remote tree and deleting it
				// leaf-first is a great deal of destruction behind one keystroke, and a
				// symlink met on the way would take it outside the directory entirely. So
				// the refusal is passed on with the reason spelled out instead of being
				// swallowed and retried.
				why := err.Error()
				if n.e.IsDir {
					why += " (a directory must be empty first)"
				}
				b.refresh()
				b.fail(batchError("delete", n.e.Name, why, done, len(victims), len(victims)-i-1))
				return nil
			}
			done++
		}
		b.clearMarks()
		if !b.refresh() {
			// The delete worked, the re-list did not. The listing error is the news, and
			// reporting the delete over it would leave a stale entry on screen looking
			// like a successful one.
			return nil
		}
		b.focusRow(at)
		if done == 1 {
			b.ok("deleted " + victims[0].e.Name)
		} else {
			b.ok(fmt.Sprintf("deleted %d entries", done))
		}
		return nil
	})
	return nil
}

// deleteQuestion is the confirm's wording: the entry by name when there is one, and the
// count — broken into files and directories, since a directory is the expensive mistake —
// when there are several.
func deleteQuestion(victims []*node) string {
	if len(victims) == 1 {
		what := "file"
		if victims[0].e.IsDir {
			what = "directory"
		}
		return fmt.Sprintf("delete %s %s? (y/n)", what, victims[0].e.Name)
	}
	dirs := 0
	for _, n := range victims {
		if n.e.IsDir {
			dirs++
		}
	}
	if dirs == 0 {
		return fmt.Sprintf("delete %d files? (y/n)", len(victims))
	}
	return fmt.Sprintf("delete %d entries (%d files, %d directories)? (y/n)",
		len(victims), len(victims)-dirs, dirs)
}

// batchError is how every plural operation reports a failure part-way through, and the
// place the partial-failure policy is written down.
//
// The policy is: stop at the first error and skip the rest. The errors that stop a batch
// are almost always about the destination rather than about one file — no permission, no
// space, the connection gone — so carrying on would produce the same message once per
// remaining file and bury the first one, and it would do so while writing into a place
// that has already said no. Stopping leaves the marks up and the remaining entries
// untouched, so the same keystroke retries exactly what did not happen once the cause is
// fixed. The message therefore has to carry all three numbers: what got through, what
// failed and why, and how much was left alone.
func batchError(verb, name, why string, done, total, skipped int) error {
	if total == 1 {
		return fmt.Errorf("%s %s: %s", verb, name, why)
	}
	return fmt.Errorf("%s %s: %s — %d of %d done, %d skipped", verb, name, why, done, total, skipped)
}

// rename renames the entry under the cursor, prefilled with its current name so the
// common case is an edit rather than a retype. It is the one operation that stays
// singular: a new name is a thing you type once, and there is no sense in which eleven
// files can be given it.
func (b *Browser) rename() tea.Cmd {
	n := b.cur()
	if n == nil {
		return nil
	}
	e, dir := n.e, path.Dir(n.path)
	b.ask("rename to:", e.Name, func(b *Browser, name string) tea.Cmd {
		// Answering with the name that is already there is what happens when the prompt
		// is opened and confirmed without an edit. Nothing to do, and nothing wrong.
		if name == e.Name {
			return nil
		}
		return b.commit(dir, name, "rename",
			func() error { return b.client.Rename(path.Join(dir, e.Name), path.Join(dir, name)) },
			fmt.Sprintf("renamed %s → %s", e.Name, name))
	})
	return nil
}

// mkdir creates a directory under the current one.
func (b *Browser) mkdir() tea.Cmd {
	dir := b.cwd
	b.ask("new directory:", "", func(b *Browser, name string) tea.Cmd {
		return b.commit(dir, name, "mkdir",
			func() error { return b.client.Mkdir(path.Join(dir, name)) },
			"created "+name)
	})
	return nil
}

// commit is what both keys that take a typed name do with it: check the name, do the
// thing, re-list, stand on the result and say so. verb names the operation in a failure.
//
// The refresh check is the part worth having in one place — a mutation that succeeded
// against a listing that then failed must report the listing error, and it would be easy
// for one of two near-identical callbacks to drift out of doing that.
func (b *Browser) commit(dir, name, verb string, do func() error, okMsg string) tea.Cmd {
	if err := checkTypedName(name); err != nil {
		b.fail(err)
		return nil
	}
	if err := do(); err != nil {
		b.fail(fmt.Errorf("%s %s: %w", verb, name, err))
		return nil
	}
	if !b.refresh() {
		return nil
	}
	// dir travels with the name because the tree may have the result several levels down:
	// a name alone would find the first row that happens to match it anywhere on screen.
	b.reveal(path.Join(dir, name))
	b.ok(okMsg)
	return nil
}

// checkTypedName rejects a name the user typed for an entry in the current directory.
//
// A slash is refused because both keys act inside cwd: "R" is a rename and not a move,
// so a typed path would quietly relocate the file the user meant to retitle, and "m"
// creates parents, so a typo containing a slash would build a tree rather than a
// directory. The rest — the empty and dot names, and control characters — is what any
// name has to clear, remote or local, and lives with the local check.
func checkTypedName(name string) error {
	if err := checkNameBasics(name); err != nil {
		return err
	}
	if strings.Contains(name, "/") {
		return fmt.Errorf("refusing name %q: this is a name, not a path", name)
	}
	return nil
}

// refresh re-lists every directory the tree has already read, reporting whether it
// worked. load is the navigation path and so starts a fresh tree at the top, which is
// wrong here: the user has not gone anywhere, and the directories they have open are the
// view they are working in. Callers put the cursor back themselves, since where "back" is
// depends on what they did — and skip their own success message when this returns false,
// or it would paint over the listing error.
//
// Marks survive it. Every listing prunes the ones whose entry is gone, so what is left is
// exactly the set that still exists.
func (b *Browser) refresh() bool {
	if b.root == nil {
		return b.load(b.cwd)
	}
	at := ""
	if n := b.cur(); n != nil {
		at = n.path
	}
	if !b.reload(b.root) {
		return false
	}
	b.rebuild()
	b.pruneMarks()
	b.pruneTarget()
	b.focusPath(at)
	b.clearNote()
	return true
}

// reload re-lists n and every directory under it that has been read, in place. A listing
// that fails stops the walk: the tree is then part old and part new, but the caller is
// about to report the error rather than the operation, and half a refresh shown under an
// error is better than the whole of it thrown away.
func (b *Browser) reload(n *node) bool {
	ents, err := b.client.List(n.path)
	if err != nil {
		b.fail(err)
		return false
	}
	b.setKids(n, ents)
	for _, k := range n.kids {
		if k.e.IsDir && k.loaded && !b.reload(k) {
			return false
		}
	}
	return true
}

// focusRow stands the cursor on row i, clamped. It is focus's other half: after a delete
// there is no name to look for, only the place the name used to be.
func (b *Browser) focusRow(i int) {
	b.cursor = i
	b.clampScroll()
}
