package filebrowser

import (
	"fmt"
	"path"
	"strings"

	tea "charm.land/bubbletea/v2"
)

// SFTP has no server-side copy, and a move across filesystems falls back to one, so both go through the transfer machinery.

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
	// sftpx.Copy writes through Create, which truncates, so ask first as every other direction does.
	if clash := b.collisions(dst, srcs); clash != "" {
		b.askConfirm("overwrite "+clash+"? (y/n)", func(b *Browser, _ string) tea.Cmd {
			return b.startCopy(dst, srcs, skipped)
		})
		return nil
	}
	return b.startCopy(dst, srcs, skipped)
}

// startCopy is copyToTarget once the destination is settled.
func (b *Browser) startCopy(dst string, srcs []*node, skipped int) tea.Cmd {
	client := b.client
	t := &transfer{arrow: "⇢ ", verb: "copy", landed: copied(dst, skipped)}
	for _, n := range srcs {
		it := batchItem{
			name:   n.e.Name,
			remote: n.path,
			local:  dst,
			total:  n.e.Size,
		}
		it.run = func(progress func(int64)) (int64, error) {
			return client.Copy(it.remote, dst, progress)
		}
		t.items = append(t.items, it)
	}
	return b.begin(t)
}

// copied ends a copy job; dst travels in the closure because the transfer's own fields describe the last item.
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

// alreadyThere is the tail naming how much of the selection was skipped for being in the target already.
func alreadyThere(skipped int) string {
	if skipped == 0 {
		return ""
	}
	return fmt.Sprintf(" — %d already there", skipped)
}

// moveToTarget moves the selection into the target directory, stopping at the first failure. See batchError.
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
	// sftpx.Move refuses a name that is taken and clearing the way would need a recursive remote delete, so report the clash here.
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
			// A rename reports no bytes; only the cross-filesystem fallback moves the count.
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

// collisions names what of srcs is already in dst, or "" when the way is clear — or when dst has not been listed yet.
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
	// Named, not counted: one "y" overwrites all of them, so the question has to say which.
	switch {
	case len(names) == 0:
		return ""
	case len(names) <= 3:
		return strings.Join(names, ", ")
	default:
		return fmt.Sprintf("%s and %d more", strings.Join(names[:3], ", "), len(names)-3)
	}
}
