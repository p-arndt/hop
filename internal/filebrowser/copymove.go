package filebrowser

import (
	"fmt"
	"path"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

// "c" and "v": copy and move what is marked into the directory "t" aimed at.
//
// Only a move is cheap. SFTP has no server-side copy, so sftpx.Copy reads every byte down
// to this process and writes it back up the same connection — a remote-to-remote copy
// costs twice what downloading the same file costs. A move is a rename the server does by
// itself and costs nothing, right up until source and target sit on different
// filesystems, where sftpx falls back to exactly that copy. Both therefore go through the
// transfer machinery: a move that looks instant nine times out of ten must not freeze the
// UI the tenth.

// copyToTarget copies the selection into the target directory.
func (b *Browser) copyToTarget() tea.Cmd {
	srcs := b.targets()
	if len(srcs) == 0 {
		return nil
	}
	dst, srcs, skipped, ok := b.destFor("copy", srcs)
	if !ok {
		return nil
	}
	if b.busy() {
		return nil
	}
	// sftpx.Copy writes through Create, which truncates. Every other direction asks before
	// it destroys something — "d" for a local file, "u" for a remote one — and a copy has
	// no reason to be the exception.
	if clash := b.collisions(dst, srcs); clash != "" {
		b.askConfirm("overwrite "+clash+"? (y/n)", func(b *Browser, _ string) tea.Cmd {
			return b.startCopy(dst, srcs, skipped)
		})
		return nil
	}
	return b.startCopy(dst, srcs, skipped)
}

// startCopy is copyToTarget once the destination is settled, so the confirmed and the
// unobstructed path build the same job.
func (b *Browser) startCopy(dst string, srcs []*node, skipped int) tea.Cmd {
	client := b.client
	t := &transfer{arrow: "⇢ ", verb: "copy", landed: copied(dst, skipped)}
	for _, n := range srcs {
		it := batchItem{
			name:   n.e.Name,
			remote: n.path,
			local:  dst, // where it is going, which is what copied reports
			total:  n.e.Size,
		}
		it.run = func(progress func(int64)) (int64, error) {
			return client.Copy(it.remote, dst, progress)
		}
		t.items = append(t.items, it)
	}
	return b.begin(t)
}

// copied is what a finished copy job does: show the destination's new contents and say
// how much landed there. dst travels in the closure because the transfer's own fields
// describe the last item, not the job.
func copied(dst string, skipped int) func(*Browser, *transfer, int64) {
	return func(b *Browser, t *transfer, _ int64) {
		if b.nodeAt(dst) != nil && !b.refresh() {
			return
		}
		if t.done == 1 {
			b.ok("copied " + t.name + " → " + dst + alreadyThere(skipped))
			return
		}
		b.ok(fmt.Sprintf("copied %d entries → %s (%s)%s", t.done, dst, humanizeBytes(t.bytes), alreadyThere(skipped)))
	}
}

// alreadyThere is the tail the outcomes carry when part of the selection was skipped for
// being in the target already, so a count that is short of what was marked says why.
func alreadyThere(skipped int) string {
	if skipped == 0 {
		return ""
	}
	return fmt.Sprintf(" — %d already there", skipped)
}

// moveToTarget moves the selection into the target directory.
//
// It stops at the first failure, as every plural operation here does: a move that fails
// has almost always failed on the destination — no permission, a name already taken,
// another filesystem — and the remaining entries are still where they were, still marked,
// ready for the same keystroke once the cause is dealt with. See batchError.
func (b *Browser) moveToTarget() tea.Cmd {
	srcs := b.targets()
	if len(srcs) == 0 {
		return nil
	}
	dst, srcs, skipped, ok := b.destFor("move", srcs)
	if !ok {
		return nil
	}
	if b.busy() {
		return nil
	}
	// A move cannot offer the same overwrite: sftpx.Move refuses a name that is taken, and
	// clearing the way would mean a recursive remote delete this package does not have. So
	// the collision is reported from the keystroke rather than as a raw server error from
	// the middle of a batch that has already moved half of it.
	if clash := b.collisions(dst, srcs); clash != "" {
		b.fail(fmt.Errorf("move: %s already in %s — copy it, or delete what is there first", clash, dst))
		return nil
	}

	client := b.client
	t := &transfer{arrow: "⇢ ", verb: "move", landed: moved(dst, skipped)}
	for _, n := range srcs {
		it := batchItem{
			name:   n.e.Name,
			remote: n.path,
			local:  dst,
			total:  n.e.Size,
		}
		it.run = func(progress func(int64)) (int64, error) {
			// A rename reports nothing, so the count stays zero and the bar stays a
			// spinner. Only the cross-filesystem fallback ever moves the number.
			return 0, client.Move(it.remote, dst, progress)
		}
		t.items = append(t.items, it)
	}
	return b.begin(t)
}

// moved is what a finished move job does, mirroring copied.
func moved(dst string, skipped int) func(*Browser, *transfer, int64) {
	return func(b *Browser, t *transfer, _ int64) {
		if !b.refresh() {
			return
		}
		if t.done == 1 {
			b.reveal(path.Join(dst, t.name))
			b.ok("moved " + t.name + " → " + dst + alreadyThere(skipped))
			return
		}
		b.ok(fmt.Sprintf("moved %d entries → %s%s", t.done, dst, alreadyThere(skipped)))
	}
}

// collisions names what of srcs is already in dst, as the question or the refusal shows it
// — one name, or a count once there is more than one. It answers "" when the way is clear,
// and also when dst has not been listed yet: a directory nobody has opened cannot be
// checked without a round trip the keystroke should not be paying for, and both operations
// still refuse or overwrite correctly at the server.
func (b *Browser) collisions(dst string, srcs []*node) string {
	d := b.nodeAt(dst)
	if d == nil || !d.loaded {
		return ""
	}
	var names []string
	for _, n := range srcs {
		if childNamed(d, n.e.Name) != nil {
			names = append(names, n.e.Name)
		}
	}
	// Named, not counted. One "y" overwrites all of them at once, so the question has to
	// say which files that is — "overwrite 3 entries?" asks the user to consent to
	// something they cannot see. Past three the list is trimmed rather than allowed to run
	// off the status line.
	switch {
	case len(names) == 0:
		return ""
	case len(names) <= 3:
		return strings.Join(names, ", ")
	default:
		return fmt.Sprintf("%s and %d more", strings.Join(names[:3], ", "), len(names)-3)
	}
}
