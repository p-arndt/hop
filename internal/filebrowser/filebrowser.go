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

	"hop/internal/keymap"
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
	Close() error
}

// OpenFileMsg asks the enclosing model to open a remote file in an editor pane. The
// editor runs on the remote host over the SSH connection the TUI owns, which the
// browser knows nothing about.
type OpenFileMsg struct {
	Path string // absolute remote path
	Name string
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
}

// Browser is a remote directory browser the TUI drives by forwarding key
// messages and rendering View.
type Browser struct {
	client    Client
	cwd       string
	entries   []sftpx.Entry
	cursor    int
	scroll    int
	status    string
	statusErr bool
	opts      Options
	w, h      int

	// tmpDir is the scratch directory "o" fetches into, created on first use.
	tmpDir string

	// keys resolves the listing's motion keys and holds a half-typed "gg". What they do
	// to the cursor is Browser.move.
	keys keymap.Reader

	// overlay is the open question — a name to type, a yes to give — which owns the
	// keyboard while it is up. See prompt.go.
	overlay overlay

	// sortBy is the order the listing is held in, which the "s" key cycles. See sort.go.
	sortBy sortMode

	// xfer is the transfer in flight, if any: SFTP copies run off the UI goroutine so a
	// large file cannot stall a keystroke. See transfer.go.
	xfer *transfer

	// refusal is a "still transferring" message holding the last row for a moment, and
	// refusedAt when it took it. It cannot be an ordinary status: the row it would go on
	// is the one the progress line is already drawing. See Browser.busy.
	refusal   string
	refusedAt time.Time
}

// New builds a Browser starting in startDir (or the remote home when empty), ensuring
// the download directory exists locally.
//
// A startDir that cannot be listed does not fail the open — usually a host's default
// directory renamed on the server. The browser lands in the home directory with the
// listing error as its status.
func New(c Client, startDir string, opts Options, w, h int) (*Browser, error) {
	opts.DownloadDir = expandHome(opts.DownloadDir)
	if err := os.MkdirAll(opts.DownloadDir, 0o755); err != nil {
		return nil, err
	}

	b := &Browser{
		client: c,
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
	failed := b.status
	b.load(home)
	if b.cwd == "" {
		return nil, fmt.Errorf("%s", b.status)
	}
	if failed != "" {
		b.status, b.statusErr = failed, true
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
	opts.DownloadDir = expandHome(opts.DownloadDir)
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
	b.cwd = dir
	b.entries = b.applySort(ents)
	b.cursor = 0
	b.scroll = 0
	b.status = ""
	b.statusErr = false
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
	key := msg.String()

	// An open question first: while one is up every key is its answer, so "d" typed into
	// a filename is a "d" rather than a download.
	if cmd, handled := b.overlayKey(key); handled {
		return cmd
	}

	if mo := b.keys.Motion(keymap.Full, key, b.opts.VimKeys); mo != keymap.None {
		return b.move(mo)
	}

	switch key {
	case "backspace":
		// Not a motion elsewhere in hop, but a file browser that ignored it would be the
		// odd one out.
		b.load(path.Dir(b.cwd))

	case "r":
		b.load(b.cwd)

	case "o":
		return b.openInApp()

	case "d":
		return b.download()

	case "u":
		return b.upload()

	case "x":
		return b.remove()

	case "R":
		return b.rename()

	case "m":
		return b.mkdir()

	case "s":
		b.cycleSort()
	}
	return nil
}

// Update takes the messages the browser's own commands produce — transfer progress and
// completion — which the enclosing model routes back here. Keys come through Handle;
// this is the other half, and the reason a transfer can run without the UI waiting on it.
func (b *Browser) Update(msg tea.Msg) tea.Cmd { return b.handleTransferMsg(msg) }

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
	if i < 0 || i >= len(b.entries) {
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
func (b *Browser) Activate() tea.Cmd { return b.activate() }

// Scroll moves the cursor n rows, negative for up. The cursor moves rather than the
// window alone: every other key here acts on the cursor, so a wheel that slid the
// listing out from under it would leave "d" on a file no longer on screen.
func (b *Browser) Scroll(n int) {
	b.cursor += n
	b.clampScroll()
}

// move applies a motion to the listing. The browser scrolls, so H/M/L land inside the
// visible window while Top and Bottom address the whole directory.
func (b *Browser) move(mo keymap.Motion) tea.Cmd {
	switch mo {
	case keymap.Up:
		b.cursor--
	case keymap.Down:
		b.cursor++
	case keymap.Top:
		b.cursor = 0
	case keymap.Bottom:
		b.cursor = len(b.entries) - 1
	case keymap.HalfDown:
		b.cursor += b.halfPage()
	case keymap.HalfUp:
		b.cursor -= b.halfPage()
	case keymap.PageDown:
		b.cursor += b.contentRows()
	case keymap.PageUp:
		b.cursor -= b.contentRows()
	case keymap.ScreenTop:
		b.cursor = b.scroll
	case keymap.ScreenMid:
		b.cursor = b.scroll + b.windowRows()/2
	case keymap.ScreenBot:
		b.cursor = b.scroll + b.windowRows() - 1

	case keymap.Out:
		// path.Dir of "/" stays "/", so this is a no-op at the filesystem root.
		b.load(path.Dir(b.cwd))
		return nil

	case keymap.In:
		return b.activate()
	}

	b.clampScroll()
	return nil
}

// activate enters the directory under the cursor, or asks the model to open the file in
// an editor pane. Nothing is downloaded: the editor runs against the real remote file.
func (b *Browser) activate() tea.Cmd {
	e, ok := b.selected()
	if !ok {
		return nil
	}
	if e.IsDir {
		b.load(path.Join(b.cwd, e.Name))
		return nil
	}

	msg := OpenFileMsg{Path: path.Join(b.cwd, e.Name), Name: e.Name}
	return func() tea.Msg { return msg }
}

// selected returns the entry under the cursor, or ok=false in an empty listing.
func (b *Browser) selected() (sftpx.Entry, bool) {
	if len(b.entries) == 0 {
		return sftpx.Entry{}, false
	}
	return b.entries[b.cursor], true
}

// ok and fail set the status line and the colour View renders it in.
func (b *Browser) ok(msg string) {
	b.status = msg
	b.statusErr = false
}

func (b *Browser) fail(err error) {
	b.status = err.Error()
	b.statusErr = true
}

// checkLocalName rejects a server-supplied entry name that cannot safely be used as a
// local file name. Path separators or ".." would let a download write outside the
// download directory, a colon addresses an NTFS stream or a drive, and the reserved
// names open Windows devices rather than files.
func checkLocalName(name string) error {
	if name == "" || name == "." || name == ".." {
		return fmt.Errorf("refusing unsafe remote file name %q", name)
	}
	if strings.ContainsAny(name, `/\:`) {
		return fmt.Errorf("refusing unsafe remote file name %q", name)
	}
	for _, r := range name {
		if r < 0x20 || r == 0x7f {
			return fmt.Errorf("refusing remote file name with control characters")
		}
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
	n := len(b.entries) - b.scroll
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

// clampScroll clamps the cursor into range and slides the window to keep it visible.
func (b *Browser) clampScroll() {
	if len(b.entries) == 0 {
		b.cursor = 0
		b.scroll = 0
		return
	}
	if b.cursor < 0 {
		b.cursor = 0
	}
	if b.cursor > len(b.entries)-1 {
		b.cursor = len(b.entries) - 1
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
	maxScroll := len(b.entries) - rows
	if maxScroll < 0 {
		maxScroll = 0
	}
	if b.scroll > maxScroll {
		b.scroll = maxScroll
	}
}

// contentRows is the number of entry rows shown, less a header, rule and status line.
func (b *Browser) contentRows() int {
	r := b.h - 3
	if r < 1 {
		r = 1
	}
	return r
}

// View renders the listing to at most w columns and h rows, truncating every line to w
// so it can never wrap out of its box.
func (b *Browser) View() string {
	if b.w <= 0 || b.h <= 0 {
		return ""
	}

	rows := b.contentRows()
	lines := make([]string, 0, b.h)

	lines = append(lines, dimStyle.Render(truncPath(stripControl(b.cwd), b.w)))
	lines = append(lines, faintStyle.Render(strings.Repeat("─", b.w)))

	if len(b.entries) == 0 {
		lines = append(lines, dimStyle.Render("(empty)"))
	} else {
		end := b.scroll + rows
		if end > len(b.entries) {
			end = len(b.entries)
		}
		for i := b.scroll; i < end; i++ {
			lines = append(lines, b.renderRow(b.entries[i], i == b.cursor))
		}
	}

	// Pad the content area so the status sits on the last line.
	for len(lines) < 2+rows {
		lines = append(lines, "")
	}

	// The last line is one of four, in this order: an open question, which is the only
	// thing the keyboard is answering; a refusal, which is news the user just caused; a
	// transfer's progress, which is still moving; then the status of the last thing that
	// finished.
	switch {
	case b.overlay.active():
		lines = append(lines, b.overlay.view(b.w))
		if len(lines) > b.h {
			lines = lines[:b.h]
		}
		return strings.Join(lines, "\n")
	case b.refused() != "":
		lines = append(lines, redStyle.Render(truncateText(b.refused(), b.w)))
		if len(lines) > b.h {
			lines = lines[:b.h]
		}
		return strings.Join(lines, "\n")
	case b.xfer != nil:
		lines = append(lines, accentStyle.Render(truncateText(b.progressLine(b.w), b.w)))
		if len(lines) > b.h {
			lines = lines[:b.h]
		}
		return strings.Join(lines, "\n")
	}

	// Status line: red for errors, green for a completed action.
	if b.status != "" {
		txt := truncateText(stripControl(b.status), b.w)
		if b.statusErr {
			lines = append(lines, redStyle.Render(txt))
		} else {
			lines = append(lines, greenStyle.Render(txt))
		}
	} else {
		lines = append(lines, "")
	}

	if len(lines) > b.h {
		lines = lines[:b.h]
	}
	return strings.Join(lines, "\n")
}

// renderRow renders one entry: an accent bar and bold name for the selection,
// directories in accent with a trailing "/", files with a right-aligned dim size.
func (b *Browser) renderRow(e sftpx.Entry, selected bool) string {
	prefix := "  "
	if selected {
		prefix = selBar + " "
	}

	nameText := stripControl(e.Name)
	if e.IsDir {
		nameText += "/"
	}
	sizeText := ""
	if !e.IsDir {
		sizeText = humanizeBytes(e.Size)
	}
	// The modified time is a column of its own, kept out of the size text so a directory
	// — which has no size — still carries one. Empty when the row is too narrow to spare
	// the cells, which is the first thing dropped.
	timeText := b.modTimeCol(e)

	// Width left for the name after the 2-cell prefix, the columns and their gaps.
	const nameFloor = 12
	avail := b.w - 2
	if sizeText != "" {
		avail -= lipgloss.Width(sizeText) + 1
	}
	if timeText != "" {
		if avail-lipgloss.Width(timeText)-1 < nameFloor {
			timeText = ""
		} else {
			avail -= lipgloss.Width(timeText) + 1
		}
	}
	if avail < 1 {
		// No room for a size column either; give the name the full width.
		sizeText, timeText = "", ""
		avail = b.w - 2
	}
	nameText = truncateText(nameText, avail)
	nameW := lipgloss.Width(nameText)

	var nameStyled string
	switch {
	case selected:
		nameStyled = accentBold.Render(nameText)
	case e.IsDir:
		nameStyled = accentStyle.Render(nameText)
	default:
		nameStyled = nameText
	}

	tail := ""
	if timeText != "" {
		tail = faintStyle.Render(timeText)
	}
	if sizeText != "" {
		if tail != "" {
			tail = dimStyle.Render(sizeText) + " " + tail
		} else {
			tail = dimStyle.Render(sizeText)
		}
	}
	if tail == "" {
		return prefix + nameStyled
	}

	gap := b.w - 2 - nameW - lipgloss.Width(tail)
	if gap < 1 {
		gap = 1
	}
	return prefix + nameStyled + strings.Repeat(" ", gap) + tail
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
func (b *Browser) Status() string { return b.status }

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

// truncPath truncates a remote path to w cells, keeping the tail behind a "…/".
func truncPath(p string, w int) string {
	if lipgloss.Width(p) <= w {
		return p
	}
	const ell = "…/"
	avail := w - lipgloss.Width(ell)
	if avail < 1 {
		return truncateText(p, w)
	}
	r := []rune(p)
	tail := string(r[len(r)-avail:])
	return ell + tail
}
