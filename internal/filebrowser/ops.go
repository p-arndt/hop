package filebrowser

import (
	"fmt"
	"path"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

// The server-mutating keys ("x", "R", "m") only open the prompt overlay and return; the
// work happens in the callback, once the question is answered.

// remove deletes what is marked, or the entry under the cursor, behind a confirm.
func (b *Browser) remove() tea.Cmd {
	// Captured now, not in the callback: the delete must hit what the question described.
	victims := b.targets()
	if len(victims) == 0 {
		return nil
	}
	at := b.cursor

	b.askConfirm(deleteQuestion(victims), func(b *Browser, _ string) tea.Cmd {
		done := 0
		for i, n := range victims {
			if err := b.client.Remove(n.path); err != nil {
				// No recursive delete: a non-empty directory is refused, not retried.
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
			// Delete worked, re-list did not: the listing error is the news.
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

// deleteQuestion is the confirm's wording: the name for one entry, counts for several.
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

// batchError reports a plural operation failing part-way: the policy is to stop at the
// first error, leaving marks up so the same keystroke retries what did not happen.
func batchError(verb, name, why string, done, total, skipped int) error {
	if total == 1 {
		return fmt.Errorf("%s %s: %s", verb, name, why)
	}
	return fmt.Errorf("%s %s: %s — %d of %d done, %d skipped", verb, name, why, done, total, skipped)
}

// rename renames the entry under the cursor, prefilled with its current name.
func (b *Browser) rename() tea.Cmd {
	n := b.cur()
	if n == nil {
		return nil
	}
	e, dir := n.e, path.Dir(n.path)
	b.ask("rename to:", e.Name, func(b *Browser, name string) tea.Cmd {
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

// commit checks a typed name, does the thing, re-lists, stands on the result and says so.
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
	// Full path, since a bare name would match the first such row anywhere in the tree.
	b.reveal(path.Join(dir, name))
	b.ok(okMsg)
	return nil
}

// checkTypedName rejects a typed name; a slash is refused because both keys act inside cwd.
func checkTypedName(name string) error {
	if err := checkNameBasics(name); err != nil {
		return err
	}
	if strings.Contains(name, "/") {
		return fmt.Errorf("refusing name %q: this is a name, not a path", name)
	}
	return nil
}

// refresh re-lists every directory the tree has read, keeping it rooted where it is.
// Callers restore the cursor themselves and must skip their success message when false.
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

// reload re-lists n and every read directory under it in place; a failure stops the walk
// and leaves the tree part old, part new.
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

// focusRow stands the cursor on row i, clamped, when there is no name left to look for.
func (b *Browser) focusRow(i int) {
	b.cursor = i
	b.clampScroll()
}
