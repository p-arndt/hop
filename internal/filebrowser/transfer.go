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
	"hop/internal/sftpx"
)

// Transfers are the one thing the browser does that is not instant. A directory listing
// is a few hundred bytes and can be waited on; a file is not, and a synchronous copy
// froze the whole TUI — no repaint, no ctrl+c — for as long as the file took. So the
// blocking sftpx call runs on the command's goroutine and reports back through Update,
// and the UI goroutine only ever moves the progress line along.
//
// One at a time. A second "d" or "u" while a copy is running is refused rather than
// queued or run alongside: there is a single progress line and a single b.xfer, and the
// alternative is a scheduler and a transfer list for a case — two big files at once —
// that a browser pane is not really for.

// transferInterval is how often the progress line is redrawn — and, since the bar's
// indeterminate block moves one cell per tick, also how fast that block paces.
const transferInterval = 150 * time.Millisecond

// transfer is a copy in flight: what is being moved, between which two paths, and how
// far along it is.
//
// Only one field crosses goroutines. Everything else is set before begin starts the
// copying goroutine and never written again, so the copy reads paths that can no longer
// change and answers with a message rather than by writing back. The exception is moved,
// which sftpx reports from inside the copy while the UI goroutine reads it to draw the
// bar — hence the atomic.
//
// What a finished transfer should then do is a closure rather than a kind: "download",
// "upload" and "fetch it and open it" differ only in what happens at the end, and saying
// so where the transfer is built beats carrying a discriminator so finish can work back
// out which of the three built it.
type transfer struct {
	name   string // the file's name, as shown
	arrow  string // "↓ " or "↑ ", the direction as the progress line shows it
	remote string
	local  string

	// moved is the running byte count sftpx reports from the copying goroutine, and
	// total the bytes expected — zero when the size is not knowable in advance.
	moved atomic.Int64
	total int64

	started time.Time

	// landed runs on the UI goroutine once the bytes are across, with the byte count
	// sftpx reported. It is what the transfer was for.
	landed func(b *Browser, t *transfer, n int64)
}

// transferTickMsg is the repaint pulse of the transfer it names. It carries the
// *transfer rather than an id so a tick left over from a copy that has already finished
// can be recognised — pointer identity is the generation counter.
type transferTickMsg struct{ t *transfer }

// transferDoneMsg is the blocking copy answering with its result. n is the byte count
// sftpx reports, which is authoritative where the observed count was a guess.
type transferDoneMsg struct {
	t   *transfer
	n   int64
	err error
}

// begin puts t in flight and returns the two commands that drive it: run, which does the
// blocking copy off the UI goroutine, and the first tick. Both answer through Update.
func (b *Browser) begin(t *transfer, run func() (int64, error)) tea.Cmd {
	t.started = time.Now()
	b.xfer = t
	// The old note would otherwise sit under the progress line and reappear, stale, the
	// moment the transfer ends and fails to overwrite it — including a refusal naming a
	// file that is no longer the one in flight.
	b.clearNote()

	alias := b.alias
	return tea.Batch(
		func() tea.Msg {
			n, err := run()
			return Msg{Alias: alias, Body: transferDoneMsg{t: t, n: n, err: err}}
		},
		b.tickFor(t),
	)
}

// tickFor schedules the next repaint of t.
func (b *Browser) tickFor(t *transfer) tea.Cmd {
	alias := b.alias
	return tea.Tick(transferInterval, func(time.Time) tea.Msg {
		return Msg{Alias: alias, Body: transferTickMsg{t: t}}
	})
}

// refusalFor is how long a "still transferring" refusal holds the last row before the
// progress line takes it back. Long enough to read, short enough that the bar the user
// is watching is not hidden for meaningfully longer than the keystroke that hid it.
const refusalFor = 2 * time.Second

// busy reports a refusal when a copy is already running, so a second key says so instead
// of being silently dropped.
//
// It says it rather than failing it: an ordinary note waits its turn behind the progress
// line, and the row the progress line is drawing is the only row there is. A transient
// note takes that row for its deadline, and the ticks a running transfer is already
// producing are what redraw it when the time is up.
func (b *Browser) busy() bool {
	if b.xfer == nil {
		return false
	}
	b.say(fmt.Sprintf("%s is still transferring — wait for it to finish", b.xfer.name), refusalFor)
	return true
}

// handleTransferMsg takes the messages a running transfer produces. A message naming a
// transfer that is no longer the one in flight is dropped: a tick scheduled just before
// the copy finished still arrives, and it must not resurrect the progress line.
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
	// The one rule, stated once: a message about anything other than the copy in flight is
	// spent — a tick scheduled just before the copy finished still arrives, and must not
	// resurrect the progress line. Whose message it is was settled by the alias before it
	// got here.
	if b.xfer != about {
		return nil
	}

	if !finished {
		return b.tickFor(b.xfer) // a tick is purely "repaint"; the count reads itself
	}
	t := b.xfer
	b.xfer = nil // stops the ticks: the next one finds no match
	return b.finish(t, done.n, done.err)
}

// finish hands a completed transfer to whatever it was for. A failure is the same news
// whichever copy it was, so it is reported here rather than in three closures.
func (b *Browser) finish(t *transfer, n int64, err error) tea.Cmd {
	if err != nil {
		b.fail(err)
		return nil
	}
	t.landed(b, t, n)
	return nil
}

// ---- the keys ----

// download copies the file under the cursor into downloadDir, where — unlike the scratch
// copy "o" makes — it is meant to be kept. An existing file of that name is confirmed
// first: the download directory is the user's, and a remote name that happens to collide
// should not silently eat what is already there.
func (b *Browser) download() tea.Cmd {
	e, ok := b.selected()
	if !ok || e.IsDir {
		return nil
	}
	// Ahead of any path join: the name comes from the remote host and must not be able
	// to steer the write out of the download directory.
	if err := checkLocalName(e.Name); err != nil {
		b.fail(err)
		return nil
	}
	if b.busy() {
		return nil
	}

	if _, err := os.Stat(filepath.Join(b.opts.DownloadDir, e.Name)); err == nil {
		b.askConfirm(fmt.Sprintf("overwrite local %s? (y/n)", e.Name), func(b *Browser, _ string) tea.Cmd {
			return b.startDownload(e)
		})
		return nil
	}
	return b.startDownload(e)
}

// startFetch begins a copy of e from the current directory into localDir, and runs landed
// when it is across. Both keys that pull a file — "d" into the download directory, "o"
// into the scratch one — are this call with a different destination and a different
// ending; after is the work the copy goroutine does before reporting success, which is
// nil for a plain download.
func (b *Browser) startFetch(e sftpx.Entry, localDir string, after func(local string) error,
	landed func(*Browser, *transfer, int64)) tea.Cmd {

	t := &transfer{
		name:   e.Name,
		arrow:  "↓ ",
		remote: path.Join(b.cwd, e.Name),
		local:  filepath.Join(localDir, e.Name),
		total:  e.Size, // the listing already knows how big it is
		landed: landed,
	}
	client := b.client
	return b.begin(t, func() (int64, error) {
		n, err := client.DownloadProgress(t.remote, t.local, t.moved.Store)
		if err != nil || after == nil {
			return n, err
		}
		return n, after(t.local)
	})
}

// startDownload begins the copy of e into downloadDir. The directory is created here
// rather than at construction because the setting can change under a live browser.
func (b *Browser) startDownload(e sftpx.Entry) tea.Cmd {
	if err := os.MkdirAll(b.opts.DownloadDir, 0o755); err != nil {
		b.fail(err)
		return nil
	}
	return b.startFetch(e, b.opts.DownloadDir, nil, func(b *Browser, t *transfer, _ int64) {
		b.ok(fmt.Sprintf("downloaded %s → %s", t.name, filepath.Dir(t.local)))
	})
}

// upload copies a local file into the current remote directory. The path is typed rather
// than picked: a second file browser, over the local disk, inside the pane that is
// already a file browser is a lot of surface for a key that is mostly used with a path
// the user just copied from a shell.
func (b *Browser) upload() tea.Cmd {
	if b.busy() {
		return nil
	}
	// The destination is fixed when the question is asked, not when it is answered — the
	// user is aiming at the directory they can see.
	dir := b.cwd
	b.ask("upload local file:", "", func(b *Browser, local string) tea.Cmd {
		return b.askedUpload(dir, local)
	})
	return nil
}

// askedUpload takes the typed path and starts the copy, or explains why it cannot. A
// directory is refused outright: a recursive upload is a different operation, with its
// own progress and its own failure modes, and silently uploading nothing would be worse.
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

	name := filepath.Base(local)
	for _, e := range b.entries {
		if e.Name == name {
			b.askConfirm(fmt.Sprintf("overwrite remote %s? (y/n)", name), func(b *Browser, _ string) tea.Cmd {
				return b.startUpload(dir, local, name, fi.Size())
			})
			return nil
		}
	}
	return b.startUpload(dir, local, name, fi.Size())
}

// startUpload begins the copy of local into dir under name.
func (b *Browser) startUpload(dir, local, name string, size int64) tea.Cmd {
	t := &transfer{
		name:   name,
		arrow:  "↑ ",
		remote: path.Join(dir, name),
		local:  local,
		total:  size, // from the local file, since the remote side reports nothing
		landed: uploaded,
	}
	client := b.client
	return b.begin(t, func() (int64, error) {
		return client.UploadProgress(t.local, t.remote, t.moved.Store)
	})
}

// uploaded is what an upload does once the bytes are across.
//
// The destination is read back off the transfer rather than from b.cwd: navigation is not
// blocked while a copy runs, so the user may have walked elsewhere. Re-listing is only
// right when they are still standing where the file landed, and the message names the
// real destination either way.
func uploaded(b *Browser, t *transfer, n int64) {
	dest := path.Dir(t.remote)
	if dest == b.cwd {
		// Nothing else would show the new file: the listing was read before it existed.
		// load clears the note, so the message is set after it.
		if !b.load(b.cwd) {
			return
		}
	}
	b.ok(fmt.Sprintf("uploaded %s → %s (%s)", t.name, dest, humanizeBytes(n)))
}

// openInApp fetches the file under the cursor into the scratch directory and hands the
// local copy to the desktop's application, fire-and-forget. "o" on a directory is a
// no-op. The fetch goes through the same async machinery as "d", so a large file no
// longer stalls the UI; the launch happens in finish, once the bytes have landed.
//
// No overwrite confirm: the scratch directory is the browser's own, and a re-open of the
// same file is meant to refresh the copy.
func (b *Browser) openInApp() tea.Cmd {
	e, ok := b.selected()
	if !ok || e.IsDir {
		return nil
	}
	if err := checkLocalName(e.Name); err != nil {
		b.fail(err)
		return nil
	}
	// The OS default handler would run an executable-extension file rather than view it,
	// so a server that names a payload like a document could get code executed on a
	// single "o". An explicit OpenWith passes the file to a program the user chose, so
	// that path is left alone.
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

	// Marking the copy the way a browser download would be runs on the transfer's
	// goroutine, so the xattr call cannot stall a keystroke either. On macOS it sets
	// com.apple.quarantine, keeping Gatekeeper in the loop for types the extension guard
	// does not know about; elsewhere it is a no-op.
	mark := func(local string) error {
		if err := quarantine(local); err != nil {
			return fmt.Errorf("quarantine %s: %w", e.Name, err)
		}
		return nil
	}
	return b.startFetch(e, dir, mark, launch)
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

// scratch returns the browser's temp directory, creating it on first use. Files handed
// to the desktop's default app land here rather than in downloadDir. It is never
// removed: the app may still hold a file open long after the browser closes.
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

// progressLine renders the transfer in flight into at most w cells: which way, what, how
// far, and a bar when there is room for one worth reading. It is called from View only
// when b.xfer is set, but returns empty rather than panicking when it is not — View is
// not the only caller a future key might have.
func (b *Browser) progressLine(w int) string {
	t := b.xfer
	if t == nil || w <= 0 {
		return ""
	}

	// One read of the count for the whole line, so the numbers and the bar cannot
	// disagree about how far along the same transfer is.
	moved := t.moved.Load()

	// The fraction is real in both directions. It is still only shown when the total is
	// known — an entry whose size the listing did not carry leaves the elapsed time as
	// the only honest thing to say.
	var tail string
	if t.total > 0 {
		tail = fmt.Sprintf("  %s/%s %d%%", humanizeBytes(moved), humanizeBytes(t.total), percent(moved, t.total))
	} else {
		tail = fmt.Sprintf("  %s  %.0fs", humanizeBytes(moved), time.Since(t.started).Seconds())
	}

	// Both are ASCII by construction — an arrow plus a space, and digits, slashes and
	// unit letters — so their cell width is their byte length.
	avail := w - len(t.arrow) - len(tail)
	if avail < 1 {
		// Too narrow for the numbers; the name is the more useful half.
		return truncateText(t.arrow+stripControl(t.name), w)
	}
	name := truncateText(stripControl(t.name), avail)

	// A bar only where one would be wide enough to read; below that the percentage
	// carries the whole message.
	if room := avail - lipgloss.Width(name) - 1; room >= 8 {
		return t.arrow + name + " " + t.bar(room, moved) + tail
	}
	return t.arrow + name + tail
}

// percent is moved as a percentage of total, capped at 100 — a server that reports a
// stale size can otherwise put the bar past its own end.
func percent(moved, total int64) int64 {
	return min(moved*100/total, 100)
}

// bar draws t's progress into exactly w cells, brackets included. It fills from the left
// in proportion to the bytes moved; a transfer whose total is unknown has no proportion
// to fill, so it gets a block that paces back and forth — the line then reads as
// "working" rather than "stuck".
//
// Its only caller guards w >= 8, so there is always room for the brackets and something
// between them.
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
