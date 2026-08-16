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

// remove deletes the entry under the cursor, behind a confirm that names it. "x" is one
// key away from every motion in the listing, and a delete over SFTP has no undo, so the
// name is spelled out in the question: a mis-pressed x on the wrong row is visible
// before it commits rather than after.
func (b *Browser) remove() tea.Cmd {
	e, ok := b.selected()
	if !ok {
		return nil
	}
	what := "file"
	if e.IsDir {
		what = "directory"
	}
	// The row is captured now rather than read again in the callback: nothing can move
	// the cursor while the overlay owns the keyboard, but the delete is meant for the
	// entry the question named, not for whatever is under the cursor when it is answered.
	at := b.cursor
	// The directory is captured with the row, for the same reason: the question names an
	// entry in *this* directory, and the answer must not be carried out in another one.
	dir := b.cwd
	b.askConfirm(fmt.Sprintf("delete %s %s? (y/n)", what, e.Name), func(b *Browser, _ string) tea.Cmd {
		if err := b.client.Remove(path.Join(dir, e.Name)); err != nil {
			// The server refuses a directory that still has contents, and this browser
			// does not offer a recursive delete: walking a remote tree and deleting it
			// leaf-first is a great deal of destruction behind one keystroke, and a
			// symlink met on the way would take it outside the directory entirely. So
			// the refusal is passed on with the reason spelled out instead of being
			// swallowed and retried.
			if e.IsDir {
				b.fail(fmt.Errorf("delete %s: %w (a directory must be empty first)", e.Name, err))
			} else {
				b.fail(fmt.Errorf("delete %s: %w", e.Name, err))
			}
			return nil
		}
		if !b.refresh() {
			// The delete worked, the re-list did not. The listing error is the news, and
			// reporting the delete over it would leave a stale entry on screen looking
			// like a successful one.
			return nil
		}
		// The row the deleted entry occupied now holds its successor, which is where the
		// eye already is. Standing back on it beats jumping to the top of a directory the
		// user was in the middle of.
		b.cursor = at
		b.clampScroll()
		b.ok("deleted " + e.Name)
		return nil
	})
	return nil
}

// rename renames the entry under the cursor within the current directory, prefilled with
// its current name so the common case is an edit rather than a retype.
func (b *Browser) rename() tea.Cmd {
	e, ok := b.selected()
	if !ok {
		return nil
	}
	dir := b.cwd
	b.ask("rename to:", e.Name, func(b *Browser, name string) tea.Cmd {
		if err := checkTypedName(name); err != nil {
			b.fail(err)
			return nil
		}
		// Answering with the name that is already there is what happens when the prompt
		// is opened and confirmed without an edit. Nothing to do, and nothing wrong.
		if name == e.Name {
			return nil
		}
		if err := b.client.Rename(path.Join(dir, e.Name), path.Join(dir, name)); err != nil {
			b.fail(fmt.Errorf("rename %s: %w", e.Name, err))
			return nil
		}
		if !b.refresh() {
			return nil
		}
		b.focus(name)
		b.ok(fmt.Sprintf("renamed %s → %s", e.Name, name))
		return nil
	})
	return nil
}

// mkdir creates a directory under the current one.
func (b *Browser) mkdir() tea.Cmd {
	dir := b.cwd
	b.ask("new directory:", "", func(b *Browser, name string) tea.Cmd {
		if err := checkTypedName(name); err != nil {
			b.fail(err)
			return nil
		}
		if err := b.client.Mkdir(path.Join(dir, name)); err != nil {
			b.fail(fmt.Errorf("mkdir %s: %w", name, err))
			return nil
		}
		if !b.refresh() {
			return nil
		}
		b.focus(name)
		b.ok("created " + name)
		return nil
	})
	return nil
}

// checkTypedName rejects a name the user typed for an entry in the current directory.
//
// A slash is refused because both keys act inside cwd: "R" is a rename and not a move,
// so a typed path would quietly relocate the file the user meant to retitle, and "m"
// creates parents, so a typo containing a slash would build a tree rather than a
// directory. "." and ".." are the current directory and its parent, which cannot be
// created and must not be renamed onto. Control characters are refused because they are
// stripped from the listing, so the entry would afterwards read as a name it does not
// have, both here and in any shell that later has to address it.
func checkTypedName(name string) error {
	if name == "" || name == "." || name == ".." {
		return fmt.Errorf("refusing name %q", name)
	}
	if strings.Contains(name, "/") {
		return fmt.Errorf("refusing name %q: this is a name, not a path", name)
	}
	for _, r := range name {
		if r < 0x20 || r == 0x7f {
			return fmt.Errorf("refusing name with control characters")
		}
	}
	return nil
}

// refresh re-lists the current directory after a mutation, reporting whether it worked.
// load is the navigation path and so starts a fresh listing at the top, which is wrong
// here: the user has not gone anywhere. Callers put the cursor back themselves, since
// where "back" is depends on what they did — and skip their own success message when
// this returns false, or it would paint over the listing error.
func (b *Browser) refresh() bool { return b.load(b.cwd) }

// focus stands the cursor on the entry called name, and leaves it alone when the listing
// has no such entry — a server that renamed to something other than what was asked, or a
// directory that vanished between the write and the re-list.
func (b *Browser) focus(name string) {
	for i, e := range b.entries {
		if e.Name == name {
			b.cursor = i
			break
		}
	}
	b.clampScroll()
}
