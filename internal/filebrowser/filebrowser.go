// Package filebrowser implements a self-contained remote directory browser
// component for hop's TUI. It mirrors the shape of terminal.Pane: the TUI
// forwards key messages via Handle and renders the component with View. All
// SFTP operations run synchronously — a slow directory briefly stalls the UI,
// which is acceptable for the MVP.
package filebrowser

import (
	"fmt"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"runtime"
	"strings"

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

// SetAccent re-points the browser's highlight color, keeping it in step with the
// rest of hop when the accent is changed in the settings popover. The styles are
// values rather than lazy lookups, so they must be rebuilt.
func SetAccent(color string) {
	if color == "" {
		return
	}
	accent = lipgloss.Color(color)
	accentStyle = accentStyle.Foreground(accent)
	accentBold = accentBold.Foreground(accent)
	selBar = accentStyle.Render("▎")
}

// Client is the slice of *sftpx.Client the browser depends on. Narrowing it to
// an interface keeps the component testable without a live SFTP connection.
type Client interface {
	Home() (string, error)
	List(dir string) ([]sftpx.Entry, error)
	Download(remotePath, localPath string) (int64, error)
	Close() error
}

// OpenFileMsg asks the enclosing model to open a remote file in an editor pane.
// The browser deliberately does not open it itself: the editor runs on the remote
// host, over the SSH connection the TUI owns, and the browser knows nothing about
// either.
type OpenFileMsg struct {
	Path string // absolute remote path
	Name string
}

// Options are the user settings the browser honours. They can change while it is
// open — the settings popover applies them live — so they are held as a unit and
// replaced wholesale rather than baked in at construction.
type Options struct {
	// DownloadDir is where "d" puts a file.
	DownloadDir string
	// OpenWith is the local command "o" opens a file with, flags and all ("code").
	// Empty means the desktop's default application for the file type.
	OpenWith string

	// VimKeys binds the vim motions (hjkl, gg/G, H/M/L, ctrl+d/u/f/b). False leaves
	// them unbound: the arrows, backspace and enter are then the whole of movement.
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

	// tmpDir is the scratch directory files opened with "o" are fetched into,
	// created on first use. Empty until then.
	tmpDir string

	// keys resolves the listing's motion keys (and holds a half-typed "gg"). What
	// the motions then do to the cursor is Browser.move.
	keys keymap.Reader
}

// New builds a Browser starting in startDir (or the remote home when startDir
// is empty), ensuring the download directory exists on the local filesystem.
func New(c Client, startDir string, opts Options, w, h int) (*Browser, error) {
	if err := os.MkdirAll(opts.DownloadDir, 0o755); err != nil {
		return nil, err
	}

	dir := startDir
	if dir == "" {
		home, err := c.Home()
		if err != nil {
			return nil, err
		}
		dir = home
	}

	b := &Browser{
		client: c,
		cwd:    dir,
		opts:   opts,
		w:      w,
		h:      h,
	}
	b.load(dir)
	return b, nil
}

// SetOptions swaps in new user settings. A download directory that does not exist
// yet is created on the next download, not here: a settings edit should not fail
// because a directory is missing.
func (b *Browser) SetOptions(opts Options) { b.opts = opts }

// load lists dir and, on success, commits it as the current directory, resetting
// the cursor and scroll. On error it sets the status and leaves cwd/entries
// untouched.
func (b *Browser) load(dir string) {
	ents, err := b.client.List(dir)
	if err != nil {
		b.fail(err)
		return
	}
	b.cwd = dir
	b.entries = ents
	b.cursor = 0
	b.scroll = 0
	b.status = ""
	b.statusErr = false
}

// Handle applies a key message: the motions (resolved through the shared keymap,
// which is what decides whether the vim keys are live), then the browser's own
// keyboard — refresh, and the two file actions "o" (open a local copy in the
// desktop's default app) and "d" (download). All SFTP work runs synchronously.
//
// The returned tea.Cmd is non-nil only for entering a file, which yields an
// OpenFileMsg: opening an editor needs the SSH connection, which belongs to the
// model, not here.
//
// No key here leaves the browser: dismissal is the enclosing model's business
// (ctrl+o or a double esc). Out is a strict "up a directory", so bumping against
// the top is a no-op rather than a surprise exit.
func (b *Browser) Handle(msg tea.KeyMsg) tea.Cmd {
	key := msg.String()

	if mo := b.keys.Motion(keymap.Full, key, b.opts.VimKeys); mo != keymap.None {
		return b.move(mo)
	}

	switch key {
	case "backspace":
		// Not a motion — nothing else in hop scrolls with it — but here it is the
		// same "up a directory" the arrow is, because a file browser that ignored
		// backspace would be the odd one out.
		b.load(path.Dir(b.cwd))

	case "r":
		b.load(b.cwd)

	case "o":
		b.openInApp()

	case "d":
		b.download()
	}
	return nil
}

// ---- mouse ----
//
// The browser exposes the three things a pointer needs — which entry a view row
// holds, how to stand on one, and how to open the one you are standing on — rather
// than a Handle-shaped method taking mouse events. Where the wheel and a click
// should land, and what counts as a double-click, is the enclosing model's
// business (it owns the pane's borders and the clock); which row is which is the
// browser's, because it drew them.

// entryRows is the view row the first entry is drawn on: the path header and the
// rule above it. It is what RowAt subtracts, and it mirrors View.
const entryRows = 2

// RowAt maps a view row — 0 is the browser's own top line, the path header — to
// the entry drawn there. It reports false for a row holding no entry: the header,
// the rule, the status line, or the blank space under a short listing.
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

// Select stands the cursor on entry i, as a click on its row does. An index out of
// range is clamped rather than refused, which is what every other move here does.
func (b *Browser) Select(i int) {
	b.cursor = i
	b.clampScroll()
}

// Activate opens the entry under the cursor: descend into a directory, or ask the
// model to open a file in an editor. It is what enter does, exported for the
// double-click that means the same thing.
func (b *Browser) Activate() tea.Cmd { return b.activate() }

// Scroll moves the cursor n rows, negative for up — one notch of the wheel. The
// cursor moves rather than the window alone, because the cursor is what every
// other key here acts on: a wheel that slid the listing out from under it would
// leave "d" downloading a file that is no longer on screen.
func (b *Browser) Scroll(n int) {
	b.cursor += n
	b.clampScroll()
}

// move applies a motion to the listing. Unlike the host list, the browser scrolls,
// so the screen-relative motions (H/M/L) land inside the visible window while Top
// and Bottom address the directory.
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

// activate enters the directory under the cursor, or asks the model to open the
// file under it in an editor pane. Nothing is downloaded: the editor runs on the
// remote host, against the real file.
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

// openInApp fetches the file under the cursor and hands the local copy to the
// desktop's default application for its type. The launch is fire-and-forget: the
// app opens in its own window and hop keeps running. Directories have no default
// app, so "o" on one is a no-op.
func (b *Browser) openInApp() {
	e, ok := b.selected()
	if !ok || e.IsDir {
		return
	}
	if err := checkLocalName(e.Name); err != nil {
		b.fail(err)
		return
	}
	// Handing an executable-extension file to the OS default handler
	// (ShellExecute, `open`, `xdg-open`) would run it rather than view it, so a
	// server that names a payload like a document could get code executed
	// locally on a single "o". Refuse when the launch would reach the default
	// handler. An explicit OpenWith command receives the file as an argument to
	// a program the user chose, not through the default handler, so that path
	// is left alone.
	if b.opts.OpenWith == "" && executableName(e.Name) {
		b.fail(fmt.Errorf("refusing to open executable file %q — use d to download instead", e.Name))
		return
	}

	local, err := b.fetch(e)
	if err != nil {
		b.fail(err)
		return
	}
	cmd := openCmd(b.opts.OpenWith, local)
	if err := cmd.Start(); err != nil {
		b.fail(fmt.Errorf("open %s: %w", e.Name, err))
		return
	}
	// The launcher exits as soon as the real application is up; reap it so it
	// does not linger as a zombie.
	go cmd.Wait()
	b.ok("opened " + e.Name)
}

// download copies the file under the cursor into downloadDir, where — unlike the
// scratch copy "o" makes — it is meant to be kept.
func (b *Browser) download() {
	e, ok := b.selected()
	if !ok || e.IsDir {
		return
	}
	if err := checkLocalName(e.Name); err != nil {
		b.fail(err)
		return
	}

	local := filepath.Join(b.opts.DownloadDir, e.Name)
	if err := os.MkdirAll(b.opts.DownloadDir, 0o755); err != nil {
		b.fail(err)
		return
	}
	if _, err := b.client.Download(path.Join(b.cwd, e.Name), local); err != nil {
		b.fail(err)
		return
	}
	b.ok(fmt.Sprintf("downloaded %s → %s", e.Name, b.opts.DownloadDir))
}

// selected returns the entry under the cursor, or ok=false in an empty listing.
func (b *Browser) selected() (sftpx.Entry, bool) {
	if len(b.entries) == 0 {
		return sftpx.Entry{}, false
	}
	return b.entries[b.cursor], true
}

// fetch downloads e into the scratch directory and returns the local path.
func (b *Browser) fetch(e sftpx.Entry) (string, error) {
	dir, err := b.scratch()
	if err != nil {
		return "", err
	}
	local := filepath.Join(dir, e.Name)
	if _, err := b.client.Download(path.Join(b.cwd, e.Name), local); err != nil {
		return "", err
	}
	// The copy came from a remote host and is about to be handed to the OS
	// default handler, so mark it the way a browser download would be. On macOS
	// that sets com.apple.quarantine and keeps Gatekeeper in the loop for file
	// types the extension guard does not know about; elsewhere it is a no-op.
	if err := quarantine(local); err != nil {
		return "", fmt.Errorf("quarantine %s: %w", e.Name, err)
	}
	return local, nil
}

// scratch returns the browser's temp directory, creating it on first use. Files
// handed to the desktop's default app land here instead of downloadDir, so merely
// looking at a remote file leaves no clutter behind. It is deliberately never
// removed: the app may still hold the file open long after the browser is closed,
// so cleanup is left to the OS.
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

// ok and fail set the status line and the colour View renders it in.
func (b *Browser) ok(msg string) {
	b.status = msg
	b.statusErr = false
}

func (b *Browser) fail(err error) {
	b.status = err.Error()
	b.statusErr = true
}

// checkLocalName rejects a server-supplied entry name that cannot safely be
// used as a local file name. The listing comes from the remote host, so a
// hostile or compromised server can put anything in it: path separators or
// ".." would let a "download" write outside the download directory, a colon
// would address an NTFS alternate data stream or a drive, and the reserved
// device names open devices rather than files on Windows.
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
	// Windows strips trailing dots and spaces while normalizing a name, so
	// "CON .txt" and "con." both resolve to the reserved device CON. Trim them
	// before splitting off the stem, or the check is trivially side-stepped.
	trimmed := strings.TrimRight(name, ". ")
	stem := trimmed
	if i := strings.IndexByte(stem, '.'); i >= 0 {
		stem = stem[:i]
	}
	// The stem itself can carry trailing spaces that Windows drops, so "CON .txt"
	// normalizes to the device CON. Trim them before the comparison.
	stem = strings.TrimRight(stem, " ")
	switch s := strings.ToUpper(stem); s {
	case "CON", "PRN", "AUX", "NUL",
		"COM1", "COM2", "COM3", "COM4", "COM5", "COM6", "COM7", "COM8", "COM9",
		"LPT1", "LPT2", "LPT3", "LPT4", "LPT5", "LPT6", "LPT7", "LPT8", "LPT9":
		return fmt.Errorf("refusing reserved device name %q", name)
	}
	return nil
}

// executableExts are the final extensions the desktop's default handler will
// run rather than merely open. On Windows that handler is ShellExecute: native
// executables, installer and control-panel formats, Windows Script Host
// targets, shortcuts that can point anywhere, and shell-namespace files that
// can trigger code when double-clicked. On macOS, `open` executes rather than
// views Terminal profiles (.terminal runs its embedded CommandString with no
// execute bit needed), shell scripts, AppleScript, Automator workflows, and
// the location/shortcut plists; on Linux, xdg-open backends may honor a
// .desktop entry's Exec= line. A remote-named file carrying any of these would
// be executed locally when handed to the OS default handler. The set is
// checked on every platform rather than per-GOOS: refusing a .desktop file on
// macOS costs nothing, while missing one costs code execution. It is a
// package-level set the guard and its test both range over.
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

// executableName reports whether name's final extension is one the OS default
// handler would execute rather than view. The name comes from the remote host,
// so a hostile server can label a payload as a document ("invoice.pdf.hta");
// only the last extension decides what ShellExecute does with it. Trailing dots
// and spaces are trimmed first because Windows drops them while normalizing the
// name, so "evil.exe ." reaches ShellExecute as "evil.exe".
func executableName(name string) bool {
	trimmed := strings.TrimRight(name, ". ")
	return executableExts[strings.ToLower(filepath.Ext(trimmed))]
}

// openCmd builds the command that opens p. With an explicit "open with" setting
// that command is used verbatim (it may carry flags, as in "code -n"); otherwise
// p goes to the desktop's default handler for its file type. A variable so tests
// can swap in something harmless.
//
// On Windows the handler is explorer.exe, not "cmd /c start": cmd re-parses its
// command line, so metacharacters in a (remote-chosen) file name — "&", "%",
// "^" are all legal in Windows names — could be executed as commands.
// explorer.exe takes the path as a plain argument, with no shell in between.
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

// windowRows is the number of entry rows actually filled on screen, which is
// the viewport height except on a short final page.
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

// clampScroll clamps the cursor into range and slides the scroll window so the
// cursor stays visible within the content rows.
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

// contentRows is the number of entry rows shown, reserving a header, a rule and
// a status line.
func (b *Browser) contentRows() int {
	r := b.h - 3
	if r < 1 {
		r = 1
	}
	return r
}

// View renders the current listing to at most w columns and h rows. Every line
// is truncated to w so it can never wrap out of its box.
func (b *Browser) View() string {
	if b.w <= 0 || b.h <= 0 {
		return ""
	}

	rows := b.contentRows()
	lines := make([]string, 0, b.h)

	// Header: current path (dim), tail-truncated with a leading "…/".
	lines = append(lines, dimStyle.Render(truncPath(stripControl(b.cwd), b.w)))
	// Faint horizontal rule.
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

	// Status line: red-ish for errors, green for a completed action, empty
	// otherwise.
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

// renderRow renders a single entry: a leading accent bar + bold accent name for
// the selection, directories in accent with a trailing "/", and files in the
// default color with a right-aligned dim size.
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

	// Width available for the name after the 2-cell prefix (and size + gap).
	avail := b.w - 2
	if sizeText != "" {
		avail -= lipgloss.Width(sizeText) + 1
	}
	if avail < 1 {
		// No room for a size column; drop it and give the name the full width.
		sizeText = ""
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

	if sizeText == "" {
		return prefix + nameStyled
	}

	gap := b.w - 2 - nameW - lipgloss.Width(sizeText)
	if gap < 1 {
		gap = 1
	}
	return prefix + nameStyled + strings.Repeat(" ", gap) + dimStyle.Render(sizeText)
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

// humanizeBytes renders n as a compact size (B/K/M/G), using one decimal for
// values of a kibibyte or more.
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

// stripControl removes control characters (C0, DEL and C1) from s. Entry names
// and error texts originate on the remote host; rendered raw, an embedded
// escape sequence would be interpreted by the user's terminal — repainting the
// UI, retitling the window, or worse — instead of being displayed.
func stripControl(s string) string {
	return strings.Map(func(r rune) rune {
		if r < 0x20 || (r >= 0x7f && r < 0xa0) {
			return -1
		}
		return r
	}, s)
}

// truncateText shortens s (measured by display width) to at most w cells,
// appending an ellipsis when it must cut. It operates on unstyled text.
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

// truncPath truncates a remote path to w cells, keeping the tail and prefixing
// "…/" when it must cut.
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
