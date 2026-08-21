package filebrowser

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"hop/internal/pathx"
)

// transferInterval also paces the indeterminate bar: one cell per tick.
const transferInterval = 150 * time.Millisecond

// transfer is a copy in flight.
type transfer struct {
	name   string // the item in flight, as shown
	arrow  string // "↓ ", "↑ " or "⇢ ", the direction as the progress line shows it
	verb   string // "download", "upload", "copy" — how a failure names the operation
	remote string
	local  string

	// moved is written by the copy goroutine; total is zero when the size is not knowable.
	moved atomic.Int64
	total int64

	started time.Time

	items []batchItem
	at    int
	done  int
	bytes int64

	// landed runs on the UI goroutine once the last item is across.
	landed func(b *Browser, t *transfer, n int64)
}

// batchItem is one file of a job; run performs the blocking copy.
type batchItem struct {
	name   string
	remote string
	local  string
	total  int64
	run    func(progress func(int64)) (int64, error)
}

func (t *transfer) startItem() {
	if t.at < len(t.items) {
		it := t.items[t.at]
		t.name, t.remote, t.local, t.total = it.name, it.remote, it.local, it.total
	}
	t.moved.Store(0)
}

// transferTickMsg carries the *transfer itself, so a tick left over from a finished copy can be dropped.
type transferTickMsg struct{ t *transfer }

// transferDoneMsg is the blocking copy's result; n is the byte count sftpx reports.
type transferDoneMsg struct {
	t   *transfer
	n   int64
	err error
}

// begin puts t in flight and returns the commands driving its first item.
func (b *Browser) begin(t *transfer) tea.Cmd {
	if len(t.items) == 0 {
		return nil
	}
	t.started = time.Now()
	t.at = 0
	t.startItem()
	b.xfer = t
	// A stale note would otherwise reappear under the progress line the moment the transfer ends.
	b.clearNote()

	return tea.Batch(b.runItem(t), b.tickFor(t))
}

// runItem copies items[at] off the UI goroutine; the item is read here so the closure holds a stable value.
func (b *Browser) runItem(t *transfer) tea.Cmd {
	alias := b.alias
	it := t.items[t.at]
	return func() tea.Msg {
		n, err := it.run(t.moved.Store)
		return Msg{Alias: alias, Body: transferDoneMsg{t: t, n: n, err: err}}
	}
}

// tickFor schedules the next repaint of t.
func (b *Browser) tickFor(t *transfer) tea.Cmd {
	alias := b.alias
	return tea.Tick(transferInterval, func(time.Time) tea.Msg {
		return Msg{Alias: alias, Body: transferTickMsg{t: t}}
	})
}

// refusalFor is how long a "still transferring" refusal holds the last row.
const refusalFor = 2 * time.Second

// busy reports a refusal when a copy is already running.
func (b *Browser) busy() bool {
	if b.xfer == nil {
		return false
	}
	b.say(fmt.Sprintf("%s is still transferring — wait for it to finish", b.xfer.name), refusalFor)
	return true
}

// handleTransferMsg takes the messages a running transfer produces.
func (b *Browser) handleTransferMsg(msg tea.Msg) tea.Cmd {
	var about *transfer
	done, finished := msg.(transferDoneMsg)
	switch m := msg.(type) {
	case transferTickMsg:
		about = m.t
	case transferDoneMsg:
		about = m.t
	default:
		return nil
	}
	// A message about a transfer that is no longer in flight is spent: a late tick must not resurrect the progress line.
	if b.xfer != about {
		return nil
	}

	if !finished {
		return b.tickFor(b.xfer)
	}
	return b.finish(b.xfer, done.n, done.err)
}

// finish takes the result of one item: it either starts the next one, or ends the job.
func (b *Browser) finish(t *transfer, n int64, err error) tea.Cmd {
	if err != nil {
		b.xfer = nil // stops the ticks: the next one finds no match
		total := max(len(t.items), 1)
		b.fail(batchError(t.verb, t.name, err.Error(), t.done, total, total-t.at-1))
		return nil
	}

	// Marks are spent per item, so a batch that stops halfway leaves the remainder marked for a retry. See batchError.
	delete(b.marks, t.remote)

	t.done++
	t.bytes += n
	if t.at+1 < len(t.items) {
		t.at++
		t.startItem()
		// No new tick: b.xfer still points at t, so the running chain re-arms itself; a second would double the bar's pace.
		return b.runItem(t)
	}

	b.xfer = nil
	if t.landed != nil {
		t.landed(b, t, n)
	}
	return nil
}

// ---- the keys ----

// download copies what is marked — or the file under the cursor — into downloadDir, skipping directories.
func (b *Browser) download() tea.Cmd {
	var files []*node
	dirs := 0
	for _, n := range b.targets() {
		if n.e.IsDir {
			dirs++
			continue
		}
		files = append(files, n)
	}
	if len(files) == 0 {
		return nil
	}
	// Names come from the remote host and are checked before any path join, so none can steer a write out of downloadDir.
	for _, n := range files {
		if err := checkLocalName(n.e.Name); err != nil {
			b.fail(err)
			return nil
		}
	}
	if b.busy() {
		return nil
	}

	clashes := 0
	for _, n := range files {
		if _, err := os.Stat(filepath.Join(b.opts.DownloadDir, n.e.Name)); err == nil {
			clashes++
		}
	}
	if clashes > 0 {
		q := fmt.Sprintf("overwrite local %s? (y/n)", files[0].e.Name)
		if len(files) > 1 {
			q = fmt.Sprintf("overwrite %d local files? (y/n)", clashes)
		}
		b.askConfirm(q, func(b *Browser, _ string) tea.Cmd {
			return b.startDownload(files, dirs)
		})
		return nil
	}
	return b.startDownload(files, dirs)
}

// startFetch pulls every node in srcs into localDir and runs landed once the last one is across.
func (b *Browser) startFetch(srcs []*node, localDir string, after func(local string) error,
	landed func(*Browser, *transfer, int64)) tea.Cmd {

	client := b.client
	t := &transfer{arrow: "↓ ", verb: "download", landed: landed}
	for _, n := range srcs {
		it := batchItem{
			name:   n.e.Name,
			remote: n.path,
			local:  filepath.Join(localDir, n.e.Name),
			total:  n.e.Size,
		}
		it.run = func(progress func(int64)) (int64, error) {
			moved, err := client.DownloadProgress(it.remote, it.local, progress)
			if err != nil || after == nil {
				return moved, err
			}
			return moved, after(it.local)
		}
		t.items = append(t.items, it)
	}
	return b.begin(t)
}

// startDownload begins the copy into DownloadDir, created here because the setting can change under a live browser.
func (b *Browser) startDownload(files []*node, skipped int) tea.Cmd {
	if err := os.MkdirAll(b.opts.DownloadDir, 0o755); err != nil {
		b.fail(err)
		return nil
	}
	return b.startFetch(files, b.opts.DownloadDir, nil, func(b *Browser, t *transfer, _ int64) {
		into := filepath.Dir(t.local)
		msg := fmt.Sprintf("downloaded %s → %s", t.name, into)
		if t.done > 1 {
			msg = fmt.Sprintf("downloaded %d files → %s (%s)", t.done, into, humanizeBytes(t.bytes))
		}
		if skipped > 0 {
			msg += fmt.Sprintf(" — %d directories skipped", skipped)
		}
		b.ok(msg)
	})
}

// upload copies a local file into the current remote directory; the path is typed, not picked.
func (b *Browser) upload() tea.Cmd {
	if b.busy() {
		return nil
	}
	// The destination is fixed when the question is asked, not when it is answered.
	dir := b.cwd
	b.ask("upload local file:", "", func(b *Browser, local string) tea.Cmd {
		return b.askedUpload(dir, local)
	})
	return nil
}

// askedUpload takes the typed path and starts the copy, or explains why it cannot.
func (b *Browser) askedUpload(dir, local string) tea.Cmd {
	local = pathx.ExpandHome(local)

	fi, err := os.Stat(local)
	if err != nil {
		b.fail(fmt.Errorf("upload %s: %w", local, err))
		return nil
	}
	if fi.IsDir() {
		b.fail(fmt.Errorf("%s is a directory — upload takes a single file", local))
		return nil
	}

	// The clash is looked for in the destination's own node, not the visible rows: the tree may show several directories.
	name := filepath.Base(local)
	if n := b.nodeAt(dir); n != nil && childNamed(n, name) != nil {
		b.askConfirm(fmt.Sprintf("overwrite remote %s? (y/n)", name), func(b *Browser, _ string) tea.Cmd {
			return b.startUpload(dir, local, name, fi.Size())
		})
		return nil
	}
	return b.startUpload(dir, local, name, fi.Size())
}

func (b *Browser) startUpload(dir, local, name string, size int64) tea.Cmd {
	client := b.client
	it := batchItem{
		name:   name,
		remote: path.Join(dir, name),
		local:  local,
		total:  size, // from the local file, since the remote side reports nothing
	}
	it.run = func(progress func(int64)) (int64, error) {
		return client.UploadProgress(it.local, it.remote, progress)
	}
	return b.begin(&transfer{arrow: "↑ ", verb: "upload", items: []batchItem{it}, landed: uploaded})
}

// uploaded reads its destination off the transfer, since navigation is not blocked while a copy runs.
func uploaded(b *Browser, t *transfer, n int64) {
	dest := path.Dir(t.remote)
	if b.nodeAt(dest) != nil {
		// refresh clears the note, so the message is set after it.
		if !b.refresh() {
			return
		}
		b.reveal(path.Join(dest, t.name))
	}
	b.ok(fmt.Sprintf("uploaded %s → %s (%s)", t.name, dest, humanizeBytes(n)))
}

// openInApp fetches the file under the cursor into the scratch directory and hands it to the desktop.
func (b *Browser) openInApp() tea.Cmd {
	cur := b.cur()
	if cur == nil || cur.e.IsDir {
		return nil
	}
	e := cur.e
	if err := checkLocalName(e.Name); err != nil {
		b.fail(err)
		return nil
	}
	// The OS default handler would run an executable-extension file rather than view it; an explicit OpenWith is the user's own choice.
	if b.opts.OpenWith == "" && executableName(e.Name) {
		b.fail(fmt.Errorf("refusing to open executable file %q — use d to download instead", e.Name))
		return nil
	}
	if b.busy() {
		return nil
	}

	dir, err := b.scratch()
	if err != nil {
		b.fail(err)
		return nil
	}

	// Runs on the transfer goroutine; on macOS it sets com.apple.quarantine, elsewhere it is a no-op.
	mark := func(local string) error {
		if err := quarantine(local); err != nil {
			return fmt.Errorf("quarantine %s: %w", e.Name, err)
		}
		return nil
	}
	return b.startFetch([]*node{cur}, dir, mark, launch)
}

// launch hands a finished scratch copy to the desktop's application, fire-and-forget.
func launch(b *Browser, t *transfer, _ int64) {
	cmd := openCmd(b.opts.OpenWith, t.local)
	if err := cmd.Start(); err != nil {
		b.fail(fmt.Errorf("open %s: %w", t.name, err))
		return
	}
	// The launcher exits as soon as the real application is up; reap it.
	go cmd.Wait()
	b.ok("opened " + t.name)
}

// scratch returns the browser's temp directory, never removed: an app may still hold a file open.
func (b *Browser) scratch() (string, error) {
	if b.tmpDir != "" {
		return b.tmpDir, nil
	}
	dir, err := os.MkdirTemp("", "hop-sftp-*")
	if err != nil {
		return "", err
	}
	b.tmpDir = dir
	return dir, nil
}

// ---- the progress line ----

// progressLine renders the transfer in flight into at most w cells.
func (b *Browser) progressLine(w int) string {
	t := b.xfer
	if t == nil || w <= 0 {
		return ""
	}

	// One read of the count for the whole line, so the numbers and the bar cannot disagree.
	moved := t.moved.Load()

	var tail string
	if t.total > 0 {
		tail = fmt.Sprintf("  %s/%s %d%%", humanizeBytes(moved), humanizeBytes(t.total), percent(moved, t.total))
	} else {
		tail = fmt.Sprintf("  %s  %.0fs", humanizeBytes(moved), time.Since(t.started).Seconds())
	}

	label := stripControl(t.name)
	if len(t.items) > 1 {
		label = fmt.Sprintf("%d/%d · %s", t.at+1, len(t.items), label)
	}

	// The arrow and the tail are ASCII by construction, so len is their cell width.
	avail := w - len(t.arrow) - len(tail)
	if avail < 1 {
		// Too narrow for the numbers; the name is the more useful half.
		return truncateText(t.arrow+label, w)
	}
	name := truncateText(label, avail)

	// A bar only where one would be wide enough to read.
	if room := avail - lipgloss.Width(name) - 1; room >= 8 {
		return t.arrow + name + " " + t.bar(room, moved) + tail
	}
	return t.arrow + name + tail
}

// percent caps at 100: a server reporting a stale size can otherwise put the bar past its own end.
func percent(moved, total int64) int64 {
	return min(moved*100/total, 100)
}

// bar draws t's progress into exactly w cells; an unknown total gets a bouncing block instead. Callers guard w >= 8.
func (t *transfer) bar(w int, moved int64) string {
	inner := w - 2
	cells := []rune(strings.Repeat("░", inner))

	if t.total > 0 {
		filled := min(max(int(int64(inner)*moved/t.total), 0), inner)
		for i := 0; i < filled; i++ {
			cells[i] = '█'
		}
	} else {
		// Bounce a 3-cell block across the bar, one cell per tick.
		const blockW = 3
		span := inner - blockW
		if span < 1 {
			span = 1
		}
		step := int(time.Since(t.started) / transferInterval)
		pos := step % (2 * span)
		if pos > span {
			pos = 2*span - pos
		}
		for i := pos; i < pos+blockW && i < inner; i++ {
			cells[i] = '█'
		}
	}
	return "[" + string(cells) + "]"
}
