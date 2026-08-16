package filebrowser

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

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

// transferInterval is how often the progress line is redrawn. Fast enough that a bar
// looks alive, slow enough that a download's os.Stat poll is not a syscall storm.
const transferInterval = 150 * time.Millisecond

// direction is which way the bytes are moving, which decides the arrow, the wording of
// the finished status, and whether the byte count can be observed at all.
type direction int

const (
	down direction = iota
	up
)

// transfer is a copy in flight: what is being moved, between which two paths, and how
// far along it is. Nothing here needs a lock, and the reason is worth stating: every
// field is set before begin starts the copying goroutine and none is written afterwards
// except done, which only the UI goroutine touches. So the copying goroutine reads paths
// that can no longer change, and answers with a message rather than by writing back.
type transfer struct {
	dir    direction
	name   string // the file's name, as shown
	remote string
	local  string

	// done is the bytes moved so far and total the bytes expected, either of which may
	// be zero when it is not knowable. See observe.
	done  int64
	total int64

	started time.Time

	// openAfter marks the scratch fetch "o" makes: the completion hands the local copy
	// to the desktop's application rather than reporting a download.
	openAfter bool
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
	// The old status would otherwise sit under the progress line and reappear, stale,
	// the moment the transfer ends and fails to overwrite it.
	b.status, b.statusErr = "", false

	return tea.Batch(
		func() tea.Msg {
			n, err := run()
			return transferDoneMsg{t: t, n: n, err: err}
		},
		tickFor(t),
	)
}

// tickFor schedules the next repaint of t.
func tickFor(t *transfer) tea.Cmd {
	return tea.Tick(transferInterval, func(time.Time) tea.Msg {
		return transferTickMsg{t: t}
	})
}

// busy reports a refusal when a copy is already running, so a second key says so on the
// status line instead of clobbering the transfer the user is watching.
func (b *Browser) busy() bool {
	if b.xfer == nil {
		return false
	}
	b.fail(fmt.Errorf("%s is still transferring — wait for it to finish", b.xfer.name))
	return true
}

// handleTransferMsg takes the messages a running transfer produces. A message naming a
// transfer that is no longer the one in flight is dropped: a tick scheduled just before
// the copy finished still arrives, and it must not resurrect the progress line.
func (b *Browser) handleTransferMsg(msg tea.Msg) tea.Cmd {
	switch m := msg.(type) {
	case transferTickMsg:
		if b.xfer == nil || b.xfer != m.t {
			return nil
		}
		b.xfer.observe()
		return tickFor(b.xfer)

	case transferDoneMsg:
		if b.xfer == nil || b.xfer != m.t {
			return nil
		}
		t := b.xfer
		b.xfer = nil // stops the ticks: the next one finds no match
		return b.finish(t, m.n, m.err)
	}
	return nil
}

// finish reports a completed transfer on the status line and does whatever the copy was
// for — launching the application for "o", refreshing the listing after an upload.
func (b *Browser) finish(t *transfer, n int64, err error) tea.Cmd {
	if err != nil {
		b.fail(err)
		return nil
	}

	switch {
	case t.openAfter:
		cmd := openCmd(b.opts.OpenWith, t.local)
		if err := cmd.Start(); err != nil {
			b.fail(fmt.Errorf("open %s: %w", t.name, err))
			return nil
		}
		// The launcher exits as soon as the real application is up; reap it.
		go cmd.Wait()
		b.ok("opened " + t.name)

	case t.dir == up:
		// Nothing else would show the new file: the listing was read before it existed.
		// load resets the status, so the message is set after it.
		b.load(b.cwd)
		b.ok(fmt.Sprintf("uploaded %s → %s (%s)", t.name, b.cwd, humanizeBytes(n)))

	default:
		b.ok(fmt.Sprintf("downloaded %s → %s", t.name, b.opts.DownloadDir))
	}
	return nil
}

// observe updates the byte count from whatever the system will tell us.
//
// sftpx.Client.Download and Upload are whole-file io.Copy calls that block until they
// are finished and report no intermediate progress, so the count here is watched rather
// than reported. For a download the growing local file is the evidence: os.Stat it on
// each tick. An upload writes to the remote host, where the browser has no cheap way to
// look — a stat over SFTP every tick would compete with the transfer itself — so its
// line shows the total and the elapsed time and its bar is indeterminate.
func (t *transfer) observe() {
	if t.dir != down {
		return
	}
	if fi, err := os.Stat(t.local); err == nil {
		t.done = fi.Size()
	}
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

// startDownload begins the copy of e into downloadDir. The directory is created here
// rather than at construction because the setting can change under a live browser.
func (b *Browser) startDownload(e sftpx.Entry) tea.Cmd {
	if err := os.MkdirAll(b.opts.DownloadDir, 0o755); err != nil {
		b.fail(err)
		return nil
	}
	t := &transfer{
		dir:    down,
		name:   e.Name,
		remote: path.Join(b.cwd, e.Name),
		local:  filepath.Join(b.opts.DownloadDir, e.Name),
		total:  e.Size, // the listing already knows how big it is
	}
	client := b.client
	return b.begin(t, func() (int64, error) { return client.Download(t.remote, t.local) })
}

// upload copies a local file into the current remote directory. The path is typed rather
// than picked: a second file browser, over the local disk, inside the pane that is
// already a file browser is a lot of surface for a key that is mostly used with a path
// the user just copied from a shell.
func (b *Browser) upload() tea.Cmd {
	if b.busy() {
		return nil
	}
	b.ask("upload local file:", "", (*Browser).askedUpload)
	return nil
}

// askedUpload takes the typed path and starts the copy, or explains why it cannot. A
// directory is refused outright: a recursive upload is a different operation, with its
// own progress and its own failure modes, and silently uploading nothing would be worse.
func (b *Browser) askedUpload(local string) tea.Cmd {
	local = expandHome(local)

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
				return b.startUpload(local, name, fi.Size())
			})
			return nil
		}
	}
	return b.startUpload(local, name, fi.Size())
}

// startUpload begins the copy of local into the current directory under name.
func (b *Browser) startUpload(local, name string, size int64) tea.Cmd {
	t := &transfer{
		dir:    up,
		name:   name,
		remote: path.Join(b.cwd, name),
		local:  local,
		total:  size, // from the local file, since the remote side reports nothing
	}
	client := b.client
	return b.begin(t, func() (int64, error) { return client.Upload(t.local, t.remote) })
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

	t := &transfer{
		dir:       down,
		name:      e.Name,
		remote:    path.Join(b.cwd, e.Name),
		local:     filepath.Join(dir, e.Name),
		total:     e.Size,
		openAfter: true,
	}
	client := b.client
	return b.begin(t, func() (int64, error) {
		n, err := client.Download(t.remote, t.local)
		if err != nil {
			return n, err
		}
		// Mark the copy the way a browser download would be. On macOS that sets
		// com.apple.quarantine, keeping Gatekeeper in the loop for types the extension
		// guard does not know about; elsewhere it is a no-op. It runs here, on the
		// transfer's goroutine, so the xattr call cannot stall a keystroke either.
		if err := quarantine(t.local); err != nil {
			return n, fmt.Errorf("quarantine %s: %w", t.name, err)
		}
		return n, nil
	})
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

// expandHome resolves a leading "~" against the user's home directory. Only a leading
// one, and only as a whole path element: "~/x" and "~" expand, "~user/x" does not — that
// is a shell's business, and a local file literally named "~foo" should stay itself.
func expandHome(p string) string {
	if p != "~" && !strings.HasPrefix(p, "~/") && !strings.HasPrefix(p, `~\`) {
		return p
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return p
	}
	if p == "~" {
		return home
	}
	return filepath.Join(home, filepath.FromSlash(p[2:]))
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

	prefix := "↓ "
	if t.dir == up {
		prefix = "↑ "
	}

	// A download's fraction is real, observed from the local file. An upload has no
	// fraction to show, so it shows the size it is moving and how long it has been at it.
	var tail string
	switch {
	case t.dir == down && t.total > 0:
		pct := t.done * 100 / t.total
		if pct > 100 {
			pct = 100
		}
		tail = fmt.Sprintf("  %s/%s %d%%", humanizeBytes(t.done), humanizeBytes(t.total), pct)
	case t.total > 0:
		tail = fmt.Sprintf("  %s  %.0fs", humanizeBytes(t.total), time.Since(t.started).Seconds())
	default:
		tail = fmt.Sprintf("  %.0fs", time.Since(t.started).Seconds())
	}

	avail := w - lipgloss.Width(prefix) - lipgloss.Width(tail)
	if avail < 1 {
		// Too narrow for the numbers; the name is the more useful half.
		return truncateText(prefix+stripControl(t.name), w)
	}
	name := truncateText(stripControl(t.name), avail)

	line := prefix + name + tail
	// A bar only where one would be wide enough to read; below that the percentage
	// carries the whole message.
	if room := w - lipgloss.Width(line) - 1; room >= 8 {
		line = prefix + name + " " + t.bar(room) + tail
	}
	return truncateText(line, w)
}

// bar draws t's progress into exactly w cells, brackets included. A download fills from
// the left in proportion to the bytes on disk; an upload, whose progress is unknowable,
// gets a block that paces back and forth so the line reads as "working", not "stuck".
func (t *transfer) bar(w int) string {
	inner := w - 2
	if inner < 1 {
		return strings.Repeat("─", w)
	}

	cells := make([]rune, inner)
	for i := range cells {
		cells[i] = '░'
	}

	if t.dir == down && t.total > 0 {
		filled := int(int64(inner) * t.done / t.total)
		if filled < 0 {
			filled = 0
		}
		if filled > inner {
			filled = inner
		}
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
