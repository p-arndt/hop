// Package filebrowser implements a remote directory browser for hop's TUI. It mirrors
// terminal.Pane: the TUI forwards keys via Handle, routes the browser's own messages
// back through Update, and renders with View. Listing runs synchronously — a directory
// is small — but transfers do not, so a large file cannot stall a keystroke.
package filebrowser

import (
	"fmt"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"hop/internal/keys"
	"hop/internal/pathx"
	"hop/internal/sftpx"
)

// ---- palette (matches hop's look) ----

var (
	accent = lipgloss.Color("212")
	dimC   = lipgloss.Color("245")
	faintC = lipgloss.Color("240")
	greenC = lipgloss.Color("42")
	redC   = lipgloss.Color("203")

	accentStyle = lipgloss.NewStyle().Foreground(accent)
	accentBold  = lipgloss.NewStyle().Bold(true).Foreground(accent)
	dimStyle    = lipgloss.NewStyle().Foreground(dimC)
	faintStyle  = lipgloss.NewStyle().Foreground(faintC)
	greenStyle  = lipgloss.NewStyle().Foreground(greenC)
	redStyle    = lipgloss.NewStyle().Foreground(redC)

	selBar = accentStyle.Render("▎")
)

// SetAccent re-points the browser's highlight color when the accent changes. The
// styles are values rather than lazy lookups, so they must be rebuilt.
func SetAccent(color string) {
	if color == "" {
		return
	}
	accent = lipgloss.Color(color)
	accentStyle = accentStyle.Foreground(accent)
	accentBold = accentBold.Foreground(accent)
	selBar = accentStyle.Render("▎")
	markGlyph = accentStyle.Render("✓")
}

// Client is the slice of *sftpx.Client the browser depends on, narrowed to an interface
// so it is testable without a live SFTP connection.
type Client interface {
	Home() (string, error)
	List(dir string) ([]sftpx.Entry, error)
	DownloadProgress(remotePath, localPath string, progress func(int64)) (int64, error)
	UploadProgress(localPath, remotePath string, progress func(int64)) (int64, error)
	Mkdir(p string) error
	Remove(p string) error
	Rename(oldp, newp string) error
	// Copy duplicates srcPath into the directory dstDir, reporting the cumulative byte
	// count as it goes, and Move relocates it there. Only Move is cheap: SFTP has no
	// server-side copy, so Copy streams every byte down and back up, costing twice a
	// download of the same size. Copy overwrites a name that is taken; Move refuses it.
	Copy(srcPath, dstDir string, progress func(int64)) (int64, error)
	Move(srcPath, dstDir string, progress func(int64)) error
	Close() error
}

// Msg wraps everything a Browser sends its enclosing model. The Alias says which session
// it came from, so the model can route it the way it routes every other per-session
// message rather than offering it to each browser in turn and letting them sort it out.
type Msg struct {
	Alias string
	// Body is the browser's own message. It is deliberately opaque: what a transfer tick
	// is remains the browser's business, and Update is where it is understood.
	Body any
}

// OpenFileMsg asks the enclosing model to open a remote file in an editor pane. The
// editor runs on the remote host over the SSH connection the TUI owns, which the
// browser knows nothing about. It arrives inside a Msg, like everything else.
type OpenFileMsg struct {
	Path string // absolute remote path
	Name string
	// Beside says the file was asked for beside the one already open rather than behind
	// it as another tab. It rides on the message because the key press and the editor that
	// answers it are separated by an SSH round trip: anything remembered on the side would
	// have to be spent by hand on every path that is not this one, and the model has two
	// input devices to forget on. Only a file can carry it — see ActivateBeside.
	Beside bool
}

// Options are the user settings the browser honours. The settings popover applies them
// live, so they are replaced wholesale rather than baked in at construction.
type Options struct {
	// DownloadDir is where "d" puts a file.
	DownloadDir string
	// OpenWith is the local command "o" opens a file with, flags and all ("code").
	// Empty means the desktop's default application for the file type.
	OpenWith string

	// VimKeys binds the vim motions (hjkl, gg/G, H/M/L, ctrl+d/u/f/b). False leaves the
	// arrows, backspace and enter as the whole of movement.
	VimKeys bool

	// Keys is the resolved keyboard, hop's defaults with the user's config applied. The
	// browser reads its own layer out of it rather than holding key literals, so a
	// rebound key needs no change here. The zero value is hop's defaults.
	Keys keys.Map
}

// Browser is a remote directory browser the TUI drives by forwarding key
// messages and rendering View.
type Browser struct {
	client Client
	// alias is the session this browser belongs to, stamped onto everything it sends so
	// the model can route it by name.
	alias string
	// cwd is the current directory, which the tree keeps pointed at whichever directory
	// the cursor is inside. Path() hands it to the rest of hop, and "m" and "u" act in it.
	cwd string
	// root is the directory the tree is rooted at, and rows the flattened list of what is
	// visible in it. Every index in this package — cursor, scroll, RowAt — is an index
	// into rows; see tree.go.
	root   *node
	rows   []*node
	cursor int
	scroll int

	// marks is the multi-selection, keyed by absolute remote path across the whole tree,
	// and target the directory "c" and "v" aim at. See marks.go.
	marks  map[string]bool
	target string

	// note is the last thing the browser has to say, and what footerLine draws when
	// nothing more urgent wants the row.
	note note

	opts Options
	w, h int

	// tmpDir is the scratch directory "o" fetches into, created on first use.
	tmpDir string

	// reader resolves keystrokes against Options.Keys and holds a half-typed "gg". It is
	// used only by Handle — the model resolves its own keys and calls Do — so the chord
	// has one home per keyboard rather than one per package.
	reader keys.Reader

	// overlay is the open question — a name to type, a yes to give — which owns the
	// keyboard while it is up. See prompt.go.
	overlay overlay

	// sortBy is the order the listing is held in, which the "s" key cycles. See sort.go.
	sortBy sortMode

	// xfer is the transfer in flight, if any: SFTP copies run off the UI goroutine so a
	// large file cannot stall a keystroke. See transfer.go.
	xfer *transfer
}

// New builds a Browser starting in startDir (or the remote home when empty), ensuring
// the download directory exists locally.
//
// A startDir that cannot be listed does not fail the open — usually a host's default
// directory renamed on the server. The browser lands in the home directory with the
// listing error as its status.
func New(c Client, alias, startDir string, opts Options, w, h int) (*Browser, error) {
	opts.DownloadDir = pathx.ExpandHome(opts.DownloadDir)
	if err := os.MkdirAll(opts.DownloadDir, 0o755); err != nil {
		return nil, err
	}

	b := &Browser{
		client: c,
		alias:  alias,
		opts:   opts,
		w:      w,
		h:      h,
	}

	if startDir != "" {
		if b.load(startDir); b.cwd != "" {
			return b, nil
		}
	}

	// No start directory, or one that could not be listed. Failing to find the home
	// directory too is a real failure: there is nothing left to show.
	home, err := c.Home()
	if err != nil {
		return nil, err
	}
	failed := b.note.text
	b.load(home)
	if b.cwd == "" {
		return nil, fmt.Errorf("%s", b.note.text)
	}
	if failed != "" {
		b.note = note{text: failed, err: true}
	}
	return b, nil
}

// SetOptions swaps in new user settings. A missing download directory is created on the
// next download, so a settings edit never fails on it.
//
// The download directory is expanded here rather than trusted as typed: the settings
// field offers "~/Downloads" as its placeholder, and an unexpanded "~" would have the
// browser create a directory literally called "~" and check the wrong path for an
// existing file — quietly skipping the overwrite confirm.
func (b *Browser) SetOptions(opts Options) {
	opts.DownloadDir = pathx.ExpandHome(opts.DownloadDir)
	b.opts = opts
}

// load lists dir and, on success, commits it as the current directory, reporting whether
// it did. On error it sets the status and leaves cwd/entries untouched — a caller that
// goes on to report its own success would paint over that error, so the ones that can
// fail check the result.
func (b *Browser) load(dir string) bool {
	ents, err := b.client.List(dir)
	if err != nil {
		b.fail(err)
		return false
	}
	// A new root, so nothing of the old tree survives: re-rooting is navigation, and the
	// directories the user had open belong to where they were, not to where they are.
	root := &node{e: sftpx.Entry{Name: path.Base(dir), IsDir: true}, path: dir, depth: -1, expanded: true}
	b.root = root
	b.setKids(root, ents)
	b.cwd = dir
	b.cursor = 0
	b.scroll = 0
	b.rebuild()
	b.pruneMarks()
	b.pruneTarget()
	b.clearNote()
	return true
}

// Handle applies a key message: an open question first (it owns the keyboard), then the
// motions through the shared keymap, then the browser's own keys — refresh, transfers,
// the file operations and the sort toggle.
//
// The returned tea.Cmd carries whatever the key started: an OpenFileMsg for entering a
// file, or a transfer's first step. No key here leaves the browser: dismissal is the
// model's business, and Out is a strict "up a directory".
func (b *Browser) Handle(msg tea.KeyMsg) tea.Cmd {
	// An open question first: while one is up every key is its answer, so "d" typed into
	// a filename is a "d" rather than a download.
	if cmd, handled := b.overlayKey(msg); handled {
		return cmd
	}

	res := b.reader.Read(b.opts.Keys, keys.Browser, msg.String(), b.opts.VimKeys)
	if res.Pending {
		// The first key of a chord. Nothing downstream of the browser wants it, so it is
		// swallowed while hop waits for the second.
		return nil
	}
	return b.Do(res.Action)
}

// Do runs one resolved action. It is the browser's real entry point: the model resolves
// keys against the same keyboard the browser would (one Reader, so one half-typed chord)
// and calls this, which is why a rebound key needs no change in this package.
//
// An action this layer does not own is a no-op rather than an error: the Browser layer is
// shared with the model, which owns the exits and the cards.
func (b *Browser) Do(a keys.Action) tea.Cmd {
	switch a {
	case keys.Up, keys.Down, keys.Top, keys.Bottom, keys.HalfUp, keys.HalfDown,
		keys.PageUp, keys.PageDown, keys.ScreenTop, keys.ScreenMid, keys.ScreenBot,
		keys.In, keys.Out:
		return b.move(a)

	case keys.BrowserUp:
		// Not a motion elsewhere in hop, but a file browser that ignored backspace would
		// be the odd one out. It re-roots the tree rather than following the cursor's
		// directory: backspace is how the visible tree grows upwards, and a cursor three
		// levels down would otherwise re-root somewhere the user never asked to go.
		b.load(path.Dir(b.rootPath()))

	case keys.BrowserRefresh:
		// The whole tree, not the current directory: the open directories are what the
		// user is looking at, and re-listing one of them is not what "r" promises.
		b.refresh()

	case keys.BrowserOpen:
		return b.openInApp()

	case keys.BrowserDownload:
		return b.download()

	case keys.BrowserUpload:
		return b.upload()

	case keys.BrowserDelete:
		return b.remove()

	case keys.BrowserRename:
		return b.rename()

	case keys.BrowserMkdir:
		return b.mkdir()

	case keys.BrowserSort:
		b.cycleSort()

	case keys.BrowserMark:
		b.toggleMark()

	case keys.BrowserMarkAll:
		b.toggleMarkAll()

	case keys.BrowserTarget:
		b.setTarget()

	case keys.BrowserCopy:
		return b.copyToTarget()

	case keys.BrowserMoveTo:
		return b.moveToTarget()
	}
	return nil
}

// Update takes the messages the browser's own commands produce — transfer progress and
// completion — which the enclosing model routes back here by alias. Keys come through
// Handle; this is the other half, and the reason a transfer can run without the UI
// waiting on it.
func (b *Browser) Update(msg Msg) tea.Cmd { return b.handleTransferMsg(msg.Body) }

// send wraps one of the browser's own messages as the command that delivers it, stamped
// with the session it belongs to.
func (b *Browser) send(body any) tea.Cmd {
	msg := Msg{Alias: b.alias, Body: body}
	return func() tea.Msg { return msg }
}

// ---- mouse ----
//
// The browser exposes what a pointer needs — which entry a row holds, how to stand on
// one, how to open it — rather than taking mouse events. Where a click lands and what
// counts as a double is the model's business; which row is which is the browser's.

// entryRows is the view row the first entry is drawn on, below the path header and the
// rule. It mirrors View, and is what RowAt subtracts.
const entryRows = 2

// RowAt maps a view row (0 is the path header) to the entry drawn there, reporting
// false for a row holding no entry.
func (b *Browser) RowAt(y int) (int, bool) {
	row := y - entryRows
	if row < 0 || row >= b.contentRows() {
		return 0, false
	}
	i := b.scroll + row
	if i < 0 || i >= len(b.rows) {
		return 0, false
	}
	return i, true
}

// Select stands the cursor on entry i, as a click on its row does. An out-of-range
// index is clamped, as everywhere else here.
func (b *Browser) Select(i int) {
	b.cursor = i
	b.clampScroll()
}

// Activate opens the entry under the cursor — what enter does, exported for the
// double-click that means the same thing.
func (b *Browser) Activate() tea.Cmd { return b.activate(false) }

// ActivateBeside is Activate for the split key: a file it opens comes back asking to be
// put beside the file already open (see keys.BrowserSplit). It is a second entry point
// rather than a mode set on the browser because the intent belongs to the one key press
// and to nothing else. A directory under the cursor is still only expanded in place and
// answers with no message at all, so the intent cannot outlive the press and land on
// whatever is opened next.
func (b *Browser) ActivateBeside() tea.Cmd { return b.activate(true) }

// Scroll moves the cursor n rows, negative for up. The cursor moves rather than the
// window alone: every other key here acts on the cursor, so a wheel that slid the
// listing out from under it would leave "d" on a file no longer on screen.
func (b *Browser) Scroll(n int) {
	b.cursor += n
	b.clampScroll()
}

// move applies a motion to the listing. The browser scrolls, so H/M/L land inside the
// visible window while Top and Bottom address the whole directory.
func (b *Browser) move(mo keys.Action) tea.Cmd {
	switch mo {
	case keys.Up:
		b.cursor--
	case keys.Down:
		b.cursor++
	case keys.Top:
		b.cursor = 0
	case keys.Bottom:
		b.cursor = len(b.rows) - 1
	case keys.HalfDown:
		b.cursor += b.halfPage()
	case keys.HalfUp:
		b.cursor -= b.halfPage()
	case keys.PageDown:
		b.cursor += b.contentRows()
	case keys.PageUp:
		b.cursor -= b.contentRows()
	case keys.ScreenTop:
		b.cursor = b.scroll
	case keys.ScreenMid:
		b.cursor = b.scroll + b.windowRows()/2
	case keys.ScreenBot:
		b.cursor = b.scroll + b.windowRows() - 1

	case keys.Out:
		b.outward()
		return nil

	case keys.In:
		return b.activate(false)
	}

	b.clampScroll()
	return nil
}

// outward is left/h in a tree, which has three meanings in one key and takes them in the
// order that keeps it a single "back out of here": shut an open directory, step up to the
// directory the cursor is inside, and — only when the cursor is already at the top level —
// re-root the tree one directory higher, which is what left used to do everywhere.
func (b *Browser) outward() {
	n := b.cur()
	switch {
	case n != nil && n.e.IsDir && n.expanded:
		b.collapse(n)
	case n != nil && n.depth > 0:
		b.focusPath(n.parent.path)
	default:
		// path.Dir of "/" stays "/", so this is a no-op at the filesystem root.
		b.load(path.Dir(b.rootPath()))
	}
}

// rootPath is the directory the tree is rooted at, which is where "up a directory" starts
// from. It is not b.cwd: the cursor may be several directories deep inside the tree.
func (b *Browser) rootPath() string {
	if b.root == nil {
		return b.cwd
	}
	return b.root.path
}

// activate opens or shuts the directory under the cursor, or asks the model to open the
// file in an editor pane. Nothing is downloaded: the editor runs against the real remote
// file.
//
// beside is only ever stamped on a file. The directory branch returns before the message
// is built, which is what makes the intent impossible to leave dangling.
func (b *Browser) activate(beside bool) tea.Cmd {
	n := b.cur()
	if n == nil {
		return nil
	}
	if n.e.IsDir {
		if n.expanded {
			b.collapse(n)
		} else {
			b.expand(n)
		}
		return nil
	}

	return b.send(OpenFileMsg{Path: n.path, Name: n.e.Name, Beside: beside})
}

// selected returns the entry under the cursor, or ok=false in an empty tree.
func (b *Browser) selected() (sftpx.Entry, bool) {
	n := b.cur()
	if n == nil {
		return sftpx.Entry{}, false
	}
	return n.e, true
}

// note is something the browser has to say on its last row: the outcome of the last
// action, or a refusal of one it would not start.
//
// A transient note carries a deadline, and that deadline is also its precedence: while it
// is live it outranks a running transfer's progress line, and once it expires the bar
// takes the row back. That is the whole reason the type has an expiry — a refusal has to
// be seen, and the row it would otherwise go on is the one the bar is drawing.
type note struct {
	text string
	err  bool
	// until is zero for a note that holds until something replaces it, and a deadline
	// for one that is only borrowing the row.
	until time.Time
}

// live reports whether a transient note is still within its time. A permanent note is
// never "live" in this sense: it does not outrank anything, it just waits its turn.
func (n note) live() bool { return !n.until.IsZero() && time.Now().Before(n.until) }

// ok, fail and say set the note. ok and fail are the outcome of an action and hold the
// row until the next one; say borrows it for d and then gives it back.
func (b *Browser) ok(msg string) { b.note = note{text: msg} }

func (b *Browser) fail(err error) { b.note = note{text: err.Error(), err: true} }

func (b *Browser) say(msg string, d time.Duration) {
	b.note = note{text: msg, err: true, until: time.Now().Add(d)}
}

// clearNote takes the last message down. Starting something that will report for itself
// does this first, so a stale outcome cannot reappear underneath it.
func (b *Browser) clearNote() { b.note = note{} }

// checkLocalName rejects a server-supplied entry name that cannot safely be used as a
// local file name. Path separators or ".." would let a download write outside the
// download directory, a colon addresses an NTFS stream or a drive, and the reserved
// names open Windows devices rather than files.
func checkLocalName(name string) error {
	if err := checkNameBasics(name); err != nil {
		return err
	}
	if strings.ContainsAny(name, `/\:`) {
		return fmt.Errorf("refusing unsafe remote file name %q", name)
	}
	// Windows strips trailing dots and spaces while normalizing, so "con." resolves to
	// the device CON. Trim before splitting off the stem.
	trimmed := strings.TrimRight(name, ". ")
	stem := trimmed
	if i := strings.IndexByte(stem, '.'); i >= 0 {
		stem = stem[:i]
	}
	// The stem can carry trailing spaces Windows drops, so "CON .txt" is CON too.
	stem = strings.TrimRight(stem, " ")
	switch s := strings.ToUpper(stem); s {
	case "CON", "PRN", "AUX", "NUL",
		"COM1", "COM2", "COM3", "COM4", "COM5", "COM6", "COM7", "COM8", "COM9",
		"LPT1", "LPT2", "LPT3", "LPT4", "LPT5", "LPT6", "LPT7", "LPT8", "LPT9":
		return fmt.Errorf("refusing reserved device name %q", name)
	}
	return nil
}

// checkNameBasics rejects what no file name may be, wherever it came from: the empty
// name, the two directory names, and anything carrying control characters.
//
// The control-character rule is the one worth spelling out. Entry names are stripped of
// them for display, so a name containing one would afterwards read on screen as a name it
// does not have — here, and in any shell that later has to address it.
func checkNameBasics(name string) error {
	if name == "" || name == "." || name == ".." {
		return fmt.Errorf("refusing name %q", name)
	}
	for _, r := range name {
		if r < 0x20 || r == 0x7f {
			return fmt.Errorf("refusing name with control characters")
		}
	}
	return nil
}

// executableExts are the final extensions the desktop's default handler runs rather
// than opens: ShellExecute targets on Windows, and on macOS the things `open` executes
// (.terminal runs its embedded CommandString with no execute bit needed), plus the
// .desktop Exec= line on Linux. Checked on every platform rather than per-GOOS —
// refusing a .desktop file on macOS costs nothing, missing one costs code execution.
var executableExts = map[string]bool{
	// Windows (ShellExecute)
	".exe": true, ".com": true, ".bat": true, ".cmd": true, ".scr": true,
	".pif": true, ".msi": true, ".msp": true, ".msc": true, ".cpl": true,
	".hta": true, ".js": true, ".jse": true, ".vbs": true, ".vbe": true,
	".ws": true, ".wsf": true, ".wsh": true, ".ps1": true, ".psm1": true,
	".lnk": true, ".url": true, ".reg": true, ".inf": true, ".application": true,
	".appx": true, ".msix": true, ".jar": true, ".sct": true,
	".settingcontent-ms": true, ".iso": true, ".vhd": true, ".vhdx": true,
	".library-ms": true, ".search-ms": true,
	// macOS (LaunchServices)
	".terminal": true, ".command": true, ".tool": true, ".app": true,
	".scpt": true, ".scptd": true, ".workflow": true, ".action": true,
	".fileloc": true, ".inetloc": true, ".webloc": true, ".dmg": true,
	// Linux (xdg-open / desktop environments)
	".desktop": true,
}

// executableName reports whether name's final extension is one the OS default handler
// would execute. Only the last extension decides, so "invoice.pdf.hta" is caught;
// trailing dots and spaces are trimmed because Windows drops them while normalizing.
func executableName(name string) bool {
	trimmed := strings.TrimRight(name, ". ")
	return executableExts[strings.ToLower(filepath.Ext(trimmed))]
}

// openCmd builds the command that opens p, using an explicit "open with" setting
// verbatim (flags and all) or the desktop's default handler. A variable so tests can
// swap in something harmless.
//
// On Windows the handler is explorer.exe, not "cmd /c start": cmd re-parses its command
// line, so metacharacters legal in a remote-chosen file name could run as commands.
var openCmd = func(with, p string) *exec.Cmd {
	if fields := strings.Fields(with); len(fields) > 0 {
		return exec.Command(fields[0], append(fields[1:], p)...)
	}
	switch runtime.GOOS {
	case "windows":
		return exec.Command("explorer", p)
	case "darwin":
		return exec.Command("open", p)
	default:
		return exec.Command("xdg-open", p)
	}
}

// windowRows is the number of entry rows actually filled, which is the viewport height
// except on a short final page.
func (b *Browser) windowRows() int {
	n := len(b.rows) - b.scroll
	if rows := b.contentRows(); n > rows {
		n = rows
	}
	if n < 1 {
		n = 1
	}
	return n
}

// halfPage is the ctrl+d/ctrl+u step: half a viewport, but never zero.
func (b *Browser) halfPage() int {
	if n := b.contentRows() / 2; n > 1 {
		return n
	}
	return 1
}

// clampScroll clamps the cursor into range and slides the window to keep it visible. It
// re-points the current directory on the way out, since every motion in the package ends
// here and the cursor is what decides which directory that is.
func (b *Browser) clampScroll() {
	if len(b.rows) == 0 {
		b.cursor = 0
		b.scroll = 0
		b.syncCwd()
		return
	}
	if b.cursor < 0 {
		b.cursor = 0
	}
	if b.cursor > len(b.rows)-1 {
		b.cursor = len(b.rows) - 1
	}

	rows := b.contentRows()
	if b.cursor < b.scroll {
		b.scroll = b.cursor
	}
	if b.cursor >= b.scroll+rows {
		b.scroll = b.cursor - rows + 1
	}
	if b.scroll < 0 {
		b.scroll = 0
	}
	maxScroll := len(b.rows) - rows
	if maxScroll < 0 {
		maxScroll = 0
	}
	if b.scroll > maxScroll {
		b.scroll = maxScroll
	}
	b.syncCwd()
}

// contentRows is the number of entry rows shown, less a header, rule and status line.
func (b *Browser) contentRows() int {
	r := b.h - 3
	if r < 1 {
		r = 1
	}
	return r
}

// Resize stores the new dimensions and re-clamps the scroll window.
func (b *Browser) Resize(w, h int) {
	b.w = w
	b.h = h
	b.clampScroll()
}

// Path returns the current remote directory.
func (b *Browser) Path() string { return b.cwd }

// Status returns the last-action message.
func (b *Browser) Status() string { return b.note.text }

// Close closes the underlying SFTP client.
func (b *Browser) Close() error { return b.client.Close() }

// ---- helpers ----

// humanizeBytes renders n as a compact size (B/K/M/G), one decimal above a kibibyte.
func humanizeBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%dB", n)
	}
	f := float64(n)
	units := []string{"K", "M", "G", "T"}
	i := 0
	for f >= unit && i < len(units)-1 {
		f /= unit
		i++
	}
	return fmt.Sprintf("%.1f%s", f, units[i-1])
}

// stripControl removes control characters (C0, DEL and C1) from s. Entry names and
// error texts come from the remote host, and an embedded escape sequence would be
// interpreted by the user's terminal rather than displayed.
func stripControl(s string) string {
	return strings.Map(func(r rune) rune {
		if r < 0x20 || (r >= 0x7f && r < 0xa0) {
			return -1
		}
		return r
	}, s)
}

// truncateText shortens s to at most w display cells, appending an ellipsis when it
// must cut. Unstyled text only.
func truncateText(s string, w int) string {
	if w <= 0 {
		return ""
	}
	if lipgloss.Width(s) <= w {
		return s
	}
	target := w - 1
	var b strings.Builder
	width := 0
	for _, r := range s {
		cw := lipgloss.Width(string(r))
		if width+cw > target {
			break
		}
		b.WriteRune(r)
		width += cw
	}
	return b.String() + "…"
}
