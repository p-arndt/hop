package filebrowser

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"hop/internal/sftpx"
)

// withMoved stamps a hand-built transfer with a byte count, standing in for what the copy
// goroutine would have reported by now.
func withMoved(t *transfer, n int64) *transfer {
	t.moved.Store(n)
	return t
}

// drive runs cmd the way Bubble Tea's event loop does: execute it, unpack a batch into
// its parts, feed every message back through Update, and keep going with whatever that
// returns. Transfers are commands now, so this is the only way a test sees one finish.
//
// It terminates because the ticks stop on their own: once the completion message has
// been handled b.xfer is nil, so the next tick matches nothing and returns no successor.
// The step budget is a backstop against a bug that would otherwise hang the suite.
func drive(t *testing.T, b *Browser, cmd tea.Cmd) {
	t.Helper()

	queue := []tea.Cmd{cmd}
	for steps := 0; len(queue) > 0; steps++ {
		if steps > 50 {
			t.Fatal("drive: the transfer never finished — a command kept producing successors")
		}
		c := queue[0]
		queue = queue[1:]
		if c == nil {
			continue
		}
		msg := c()
		if batch, ok := msg.(tea.BatchMsg); ok {
			queue = append(queue, batch...)
			continue
		}
		// Everything a browser sends arrives wrapped and addressed; anything else on this
		// queue is not the browser's and would be the model's business.
		wrapped, ok := msg.(Msg)
		if !ok {
			t.Fatalf("drive: a browser command produced %T, want a filebrowser.Msg", msg)
		}
		if next := b.Update(wrapped); next != nil {
			queue = append(queue, next)
		}
	}
}

// xferBrowser builds a browser over one directory and two files, with a fresh download
// directory and scratch directory that the test owns.
func xferBrowser(t *testing.T) (*Browser, *fakeClient, string) {
	t.Helper()

	dl := t.TempDir()
	ents := []sftpx.Entry{
		{Name: "sub", IsDir: true},
		{Name: "a.txt", Size: 1024},
		{Name: "b.txt", Size: 2048},
	}
	fc := &fakeClient{entries: ents}
	b := &Browser{
		client:  fc,
		alias:   "web1",
		cwd:     "/home/u",
		entries: ents,
		opts:    Options{DownloadDir: dl},
		tmpDir:  t.TempDir(),
		w:       40,
		h:       13,
	}
	return b, fc, dl
}

// typePath answers an open text prompt with s, one key at a time, and returns whatever
// the enter produced. Only spaceless paths: the key helper has no name for a space.
func typePath(t *testing.T, b *Browser, s string) tea.Cmd {
	t.Helper()
	if !b.overlay.active() {
		t.Fatal("no prompt is open to type into")
	}
	typeText(t, b, s)
	return b.Handle(key(t, "enter"))
}

// A download reaches the client with the remote path and the local path it should, and
// lands a status only once the copy has actually finished.
func TestDownloadRunsAsync(t *testing.T) {
	b, fc, dl := xferBrowser(t)

	b.cursor = 1 // a.txt
	cmd := b.Handle(key(t, "d"))
	if cmd == nil {
		t.Fatal("d returned no tea.Cmd, want the transfer's first step")
	}
	// Nothing has run yet: the copy happens on the command's goroutine.
	if len(fc.downloads) != 0 {
		t.Fatalf("d downloaded %v before its command ran, want a synchronous no-op", fc.downloads)
	}
	if b.xfer == nil {
		t.Fatal("d left no transfer in flight, so View would show no progress")
	}

	drive(t, b, cmd)

	want := filepath.Join(dl, "a.txt")
	if len(fc.downloads) != 1 {
		t.Fatalf("downloads = %v, want exactly one", fc.downloads)
	}
	if got := fc.downloads[0]; got[0] != "/home/u/a.txt" || got[1] != want {
		t.Fatalf("download = %v, want {/home/u/a.txt %s}", got, want)
	}
	if b.xfer != nil {
		t.Fatal("the transfer is still in flight after it completed")
	}
	if b.note.err || !strings.HasPrefix(b.note.text, "downloaded a.txt") {
		t.Fatalf("status = %q (err=%v), want a downloaded... message", b.note.text, b.note.err)
	}
}

// A failing copy reports on the status line rather than leaving the progress line up.
func TestDownloadFailureClearsTransfer(t *testing.T) {
	b, fc, _ := xferBrowser(t)
	fc.errs = map[string]error{"download": os.ErrPermission}

	b.cursor = 1
	drive(t, b, b.Handle(key(t, "d")))

	if b.xfer != nil {
		t.Fatal("a failed transfer stayed in flight")
	}
	if !b.note.err {
		t.Fatalf("status = %q, want an error", b.note.text)
	}
}

// An existing local file of the same name is confirmed before it is overwritten, and a
// declined confirm leaves the file exactly as it was.
func TestDownloadConfirmsLocalOverwrite(t *testing.T) {
	for _, tc := range []struct {
		answer string
		want   int
	}{{"y", 1}, {"n", 0}} {
		t.Run(tc.answer, func(t *testing.T) {
			b, fc, dl := xferBrowser(t)
			local := filepath.Join(dl, "a.txt")
			if err := os.WriteFile(local, []byte("mine"), 0o644); err != nil {
				t.Fatal(err)
			}

			b.cursor = 1
			if cmd := b.Handle(key(t, "d")); cmd != nil {
				t.Fatal("d over an existing file started a transfer before the confirm")
			}
			if !b.overlay.active() {
				t.Fatal("d over an existing file asked nothing")
			}
			if len(fc.downloads) != 0 {
				t.Fatalf("d downloaded %v before the confirm was answered", fc.downloads)
			}

			drive(t, b, b.Handle(key(t, tc.answer)))

			if len(fc.downloads) != tc.want {
				t.Fatalf("answering %q gave %d downloads, want %d", tc.answer, len(fc.downloads), tc.want)
			}
			// The fake writes an empty file, so the original content surviving is proof
			// that nothing ran.
			body, err := os.ReadFile(local)
			if err != nil {
				t.Fatal(err)
			}
			if tc.answer == "n" && string(body) != "mine" {
				t.Fatalf("declining the confirm still replaced the file: %q", body)
			}
			if tc.answer == "y" && string(body) != "" {
				t.Fatalf("accepting the confirm left the old file: %q", body)
			}
		})
	}
}

// "u" asks for a local path, expands a leading "~", and uploads to the current remote
// directory under the file's base name.
func TestUploadExpandsHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home) // os.UserHomeDir on Windows
	src := filepath.Join(home, "notes.md")
	if err := os.WriteFile(src, []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}

	b, fc, _ := xferBrowser(t)
	if cmd := b.Handle(key(t, "u")); cmd != nil {
		t.Fatal("u started something before the path was typed")
	}

	drive(t, b, typePath(t, b, "~/notes.md"))

	if len(fc.uploads) != 1 {
		t.Fatalf("uploads = %v, want exactly one", fc.uploads)
	}
	if got := fc.uploads[0]; got[0] != src || got[1] != "/home/u/notes.md" {
		t.Fatalf("upload = %v, want {%s /home/u/notes.md}", got, src)
	}
	if b.note.err || !strings.HasPrefix(b.note.text, "uploaded notes.md") {
		t.Fatalf("status = %q (err=%v), want an uploaded... message", b.note.text, b.note.err)
	}
}

// An upload refreshes the listing: the file the user just sent has to appear.
func TestUploadRefreshesListing(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "new.txt")
	if err := os.WriteFile(src, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	b, fc, _ := xferBrowser(t)
	b.Handle(key(t, "u"))
	// The server "grows" the file the moment it is sent; the browser must re-list to see it.
	fc.entries = append(fc.entries, sftpx.Entry{Name: "new.txt", Size: 1})

	drive(t, b, typePath(t, b, src))

	var found bool
	for _, e := range b.entries {
		if e.Name == "new.txt" {
			found = true
		}
	}
	if !found {
		t.Fatalf("entries = %v, want the uploaded file among them", b.entries)
	}
}

// A path that is not there, and a path that is a directory, are refused on the status
// line without anything reaching the client.
func TestUploadRejectsMissingAndDirectory(t *testing.T) {
	dir := t.TempDir()
	for _, tc := range []struct{ name, p, want string }{
		{"missing", filepath.Join(dir, "nope.txt"), "upload"},
		{"directory", dir, "directory"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b, fc, _ := xferBrowser(t)
			b.Handle(key(t, "u"))

			if cmd := typePath(t, b, tc.p); cmd != nil {
				t.Fatal("a bad upload path still started a transfer")
			}
			if len(fc.uploads) != 0 {
				t.Fatalf("uploads = %v, want nothing sent", fc.uploads)
			}
			if b.xfer != nil {
				t.Fatal("a bad upload path left a transfer in flight")
			}
			if !b.note.err || !strings.Contains(b.note.text, tc.want) {
				t.Fatalf("status = %q (err=%v), want an error mentioning %q", b.note.text, b.note.err, tc.want)
			}
		})
	}
}

// Uploading over a name already in the listing is confirmed first.
func TestUploadConfirmsRemoteOverwrite(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "a.txt") // a.txt is already in the remote listing
	if err := os.WriteFile(src, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	b, fc, _ := xferBrowser(t)
	b.Handle(key(t, "u"))

	if cmd := typePath(t, b, src); cmd != nil {
		t.Fatal("an upload over an existing remote name started before the confirm")
	}
	if !b.overlay.active() {
		t.Fatal("an upload over an existing remote name asked nothing")
	}
	if len(fc.uploads) != 0 {
		t.Fatalf("uploads = %v, want nothing before the confirm", fc.uploads)
	}

	drive(t, b, b.Handle(key(t, "y")))

	if len(fc.uploads) != 1 || fc.uploads[0][1] != "/home/u/a.txt" {
		t.Fatalf("uploads = %v, want one to /home/u/a.txt", fc.uploads)
	}
}

// Only one copy runs at a time: a second "d" or "u" says so rather than replacing the
// transfer the user is watching.
func TestSecondTransferRefused(t *testing.T) {
	for _, k := range []string{"d", "u", "o"} {
		t.Run(k, func(t *testing.T) {
			b, fc, _ := xferBrowser(t)
			b.xfer = &transfer{arrow: "↓ ", name: "big.iso", total: 1 << 30}

			b.cursor = 1
			if cmd := b.Handle(key(t, k)); cmd != nil {
				t.Fatalf("%q started a second transfer while one was in flight", k)
			}
			if b.overlay.active() {
				t.Fatalf("%q opened a prompt while a transfer was in flight", k)
			}
			if len(fc.downloads) != 0 || len(fc.uploads) != 0 {
				t.Fatalf("%q reached the client: %v %v", k, fc.downloads, fc.uploads)
			}
			if b.xfer == nil || b.xfer.name != "big.iso" {
				t.Fatalf("%q clobbered the transfer in flight", k)
			}
			// Asserted through View, not through the status field: the refusal shares its
			// row with the progress line, so "it was recorded" and "it was shown" are two
			// different claims and only the second one matters.
			if view := b.View(); !strings.Contains(view, "big.iso") ||
				!strings.Contains(view, "still transferring") {
				t.Fatalf("%q: the refusal is not on screen. View:\n%s", k, view)
			}
		})
	}
}

// The refusal holds the last row only briefly, and then the bar the user is watching
// comes back — it must not sit on top of the transfer for the rest of its life.
func TestRefusalYieldsBackToTheProgressLine(t *testing.T) {
	b, _, _ := xferBrowser(t)
	b.xfer = withMoved(&transfer{arrow: "↓ ", name: "big.iso", total: 1 << 30}, 1<<29)

	b.cursor = 1
	b.Handle(key(t, "d"))
	if !b.note.live() {
		t.Fatal("the refusal did not take the row at all")
	}

	// Age it past its welcome rather than sleeping through it.
	b.note.until = time.Now().Add(-time.Second)
	if b.note.live() {
		t.Fatal("the refusal is still live after its deadline")
	}
	if view := b.View(); !strings.Contains(view, "50%") {
		t.Fatalf("the progress line did not come back. View:\n%s", view)
	}
}

// progressLine is only reached from View when a transfer is in flight, but it must not
// panic when there is none, nor overrun the width it is given.
func TestProgressLine(t *testing.T) {
	b, _, _ := xferBrowser(t)

	if got := b.progressLine(40); got != "" {
		t.Fatalf("progressLine with no transfer = %q, want empty", got)
	}

	b.xfer = withMoved(&transfer{arrow: "↓ ", name: "a.txt", total: 1024}, 512)
	for _, w := range []int{-1, 0, 1, 4, 12, 40, 200} {
		line := b.progressLine(w)
		if w <= 0 {
			if line != "" {
				t.Fatalf("progressLine(%d) = %q, want empty", w, line)
			}
			continue
		}
		if got := len([]rune(line)); got > w {
			t.Fatalf("progressLine(%d) is %d cells wide: %q", w, got, line)
		}
	}
	if line := b.progressLine(40); !strings.Contains(line, "a.txt") || !strings.Contains(line, "50%") {
		t.Fatalf("progressLine = %q, want the name and the half-done percentage", line)
	}

	// An upload's count is reported the same way a download's is, so it gets a real
	// percentage too.
	up1 := withMoved(&transfer{arrow: "↑ ", name: "a.txt", total: 1024}, 256)
	b.xfer = up1
	if line := b.progressLine(40); !strings.Contains(line, "25%") {
		t.Fatalf("upload progressLine = %q, want the reported percentage", line)
	}

	// Without a known total there is no fraction to show, in either direction: the line
	// falls back to what has moved so far and how long it has been going.
	b.xfer = withMoved(&transfer{arrow: "↑ ", name: "a.txt"}, 4096)
	if line := b.progressLine(40); strings.Contains(line, "%") {
		t.Fatalf("progressLine without a total = %q, want no percentage", line)
	}
}

// A tick is purely a repaint: it schedules the next one and nothing else. What the line
// shows is read straight from the count the copy publishes, so a tick cannot invent
// progress and cannot lag behind it either.
func TestTickOnlyRepaints(t *testing.T) {
	b, _, dl := xferBrowser(t)
	local := filepath.Join(dl, "a.txt")
	tr := &transfer{arrow: "↓ ", name: "a.txt", local: local, total: 1024}
	b.xfer = tr

	// Nothing reported yet.
	if cmd := b.Update(Msg{Body: transferTickMsg{t: tr}}); cmd == nil {
		t.Fatal("a tick for the running transfer scheduled no successor")
	}
	if line := b.progressLine(40); !strings.Contains(line, "0%") {
		t.Fatalf("progressLine = %q before anything was reported, want 0%%", line)
	}

	tr.moved.Store(300)
	if line := b.progressLine(40); !strings.Contains(line, "29%") {
		t.Fatalf("progressLine = %q after 300 of 1024 bytes, want 29%%", line)
	}

	// A tick belonging to a transfer that has already ended must be ignored rather than
	// bring the progress line back.
	b.xfer = nil
	if cmd := b.Update(Msg{Body: transferTickMsg{t: tr}}); cmd != nil {
		t.Fatal("a stale tick scheduled another one")
	}
	if b.xfer != nil {
		t.Fatal("a stale tick resurrected the transfer")
	}
}

// "o" still refuses a file the OS default handler would execute, and refuses it before
// any of the async machinery starts.
func TestOpenInAppRefusesExecutableAsync(t *testing.T) {
	for _, name := range []string{"invoice.pdf.hta", "invoices-2026.terminal", "report.desktop"} {
		t.Run(name, func(t *testing.T) {
			b, fc, _ := xferBrowser(t)
			opened, _ := stubOpen(t)

			b.entries[1].Name = name
			b.cursor = 1
			if cmd := b.Handle(key(t, "o")); cmd != nil {
				t.Fatalf("o on %q started a transfer, want a refusal first", name)
			}

			if len(fc.downloads) != 0 {
				t.Fatalf("o on %q fetched %v", name, fc.downloads)
			}
			if *opened != "" {
				t.Fatalf("o on %q launched the default app on %q", name, *opened)
			}
			if b.xfer != nil {
				t.Fatalf("o on %q left a transfer in flight", name)
			}
			if !b.note.err {
				t.Fatalf("o on %q: status = %q, want an error", name, b.note.text)
			}
		})
	}
}

// "o" fetches into the scratch directory — never the download directory — and hands that
// copy to the application only after the bytes have landed.
func TestOpenInAppFetchesAsync(t *testing.T) {
	b, fc, dl := xferBrowser(t)
	opened, _ := stubOpen(t)

	b.cursor = 1 // a.txt
	cmd := b.Handle(key(t, "o"))
	if cmd == nil {
		t.Fatal("o returned no tea.Cmd, want the fetch's first step")
	}
	if *opened != "" {
		t.Fatalf("o launched %q before the fetch ran", *opened)
	}

	drive(t, b, cmd)

	want := filepath.Join(b.tmpDir, "a.txt")
	if len(fc.downloads) != 1 || fc.downloads[0][1] != want {
		t.Fatalf("downloads = %v, want a single one to %s", fc.downloads, want)
	}
	if *opened != want {
		t.Fatalf("opened %q, want %q", *opened, want)
	}
	if b.note.err || b.note.text != "opened a.txt" {
		t.Fatalf("status = %q (err=%v), want %q", b.note.text, b.note.err, "opened a.txt")
	}
	if entries, _ := os.ReadDir(dl); len(entries) != 0 {
		t.Fatalf("o wrote into the download dir: %v", entries)
	}
}

// The count sftpx reports reaches the transfer, and a tick turns it into what the bar
// draws. This is the whole path — client callback, atomic, tick snapshot, rendered
// percentage — which no single one of the tests above covers end to end.
func TestReportedBytesReachTheProgressLine(t *testing.T) {
	b, fc, _ := xferBrowser(t)
	fc.steps = byteSteps{256, 512, 1024}

	b.cursor = 1 // a.txt, 1024 bytes in the listing
	cmd := b.Handle(key(t, "d"))
	tr := b.xfer
	if tr == nil {
		t.Fatal("d left no transfer in flight")
	}
	// Before anything has been reported the line is honestly at zero.
	if line := b.progressLine(40); !strings.Contains(line, "0%") {
		t.Fatalf("progressLine before any report = %q, want 0%%", line)
	}

	drive(t, b, cmd)

	if got := tr.moved.Load(); got != 1024 {
		t.Fatalf("the transfer saw %d bytes reported, want the client's final 1024", got)
	}

	// The transfer is off b.xfer by now, so put it back to render its final state.
	b.xfer = tr
	if line := b.progressLine(40); !strings.Contains(line, "100%") {
		t.Fatalf("progressLine = %q, want the reported bytes as 100%%", line)
	}
}

// An upload draws from the same count, so its bar is a real fraction rather than the
// indeterminate pacing block it used to be.
func TestUploadProgressIsReported(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "new.txt")
	if err := os.WriteFile(src, make([]byte, 800), 0o644); err != nil {
		t.Fatal(err)
	}

	b, fc, _ := xferBrowser(t)
	fc.steps = byteSteps{400, 800}
	b.Handle(key(t, "u"))
	cmd := typePath(t, b, src)
	tr := b.xfer
	if tr == nil {
		t.Fatal("the upload left no transfer in flight")
	}
	if tr.total != 800 {
		t.Fatalf("total = %d, want the local file's 800 bytes", tr.total)
	}

	drive(t, b, cmd)

	if got := tr.moved.Load(); got != 800 {
		t.Fatalf("the upload saw %d bytes reported, want 800", got)
	}
}

// An upload names the directory it actually went to, and only re-lists when the user is
// still standing there. Navigation is not blocked during a transfer, so both halves are
// reachable.
func TestUploadReportsItsRealDestination(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "new.txt")
	if err := os.WriteFile(src, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	b, fc, _ := xferBrowser(t)
	b.Handle(key(t, "u"))
	cmd := typePath(t, b, src)

	// The user wanders off while the bytes are moving.
	b.cwd = "/home/u/sub"
	before := len(fc.entries)
	fc.entries = append(fc.entries, sftpx.Entry{Name: "elsewhere.txt"})

	drive(t, b, cmd)

	if fc.uploads[0][1] != "/home/u/new.txt" {
		t.Fatalf("uploaded to %q, want the directory the upload was aimed at", fc.uploads[0][1])
	}
	if !strings.Contains(b.note.text, "/home/u") || strings.Contains(b.note.text, "/home/u/sub") {
		t.Fatalf("status = %q, want the real destination /home/u", b.note.text)
	}
	// Re-listing here would show /home/u/sub's contents as if the file had landed there.
	if len(b.entries) != before {
		t.Fatalf("entries = %d, want the %d it had — a directory the file did not land in must not be re-listed", len(b.entries), before)
	}
}

// A "~" in the download directory is a path the shell would have expanded, not a
// directory of that name. Left literal it creates "~" in the process's working directory
// and makes the overwrite check look somewhere the file will never be.
func TestDownloadDirExpandsHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)

	b, fc, _ := xferBrowser(t)
	b.SetOptions(Options{DownloadDir: "~/dl"})

	b.cursor = 1 // a.txt
	drive(t, b, b.Handle(key(t, "d")))

	want := filepath.Join(home, "dl", "a.txt")
	if len(fc.downloads) != 1 || fc.downloads[0][1] != want {
		t.Fatalf("downloads = %v, want one to %s", fc.downloads, want)
	}
	if _, err := os.Stat(filepath.Join(home, "dl")); err != nil {
		t.Fatalf("the expanded download directory was not created: %v", err)
	}

	// And the overwrite confirm has to see the same path, or it never fires.
	if cmd := b.Handle(key(t, "d")); cmd != nil {
		t.Fatal("a second d started a transfer without confirming the overwrite")
	}
	if !b.overlay.active() {
		t.Fatal("downloading over the existing file asked nothing — the check used the unexpanded path")
	}
}
