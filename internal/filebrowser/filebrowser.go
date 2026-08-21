// Package filebrowser implements a remote directory browser for hop's TUI.
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

// SetAccent re-points the browser's highlight color; styles are values, so they must be rebuilt.
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

// Client is the slice of *sftpx.Client the browser depends on.
type Client interface {
	Home() (string, error)
	List(dir string) ([]sftpx.Entry, error)
	DownloadProgress(remotePath, localPath string, progress func(int64)) (int64, error)
	UploadProgress(localPath, remotePath string, progress func(int64)) (int64, error)
	Mkdir(p string) error
	Remove(p string) error
	Rename(oldp, newp string) error
	// Copy overwrites a name that is taken; Move refuses it.
	Copy(srcPath, dstDir string, progress func(int64)) (int64, error)
	Move(srcPath, dstDir string, progress func(int64)) error
	Close() error
}

// Msg wraps everything a Browser sends its enclosing model, tagged with the session alias.
type Msg struct {
	Alias string
	Body  any
}

// OpenFileMsg asks the enclosing model to open a remote file in an editor pane.
type OpenFileMsg struct {
	Path string // absolute remote path
	Name string
	// Beside asks for the file beside the one already open rather than as another tab.
	Beside bool
}

// Options are the user settings the browser honours, replaced wholesale on a settings edit.
type Options struct {
	// DownloadDir is where "d" puts a file.
	DownloadDir string
	// OpenWith is the local command "o" opens a file with, flags and all ("code").
	// Empty means the desktop's default application for the file type.
	OpenWith string

	// VimKeys binds the vim motions (hjkl, gg/G, H/M/L, ctrl+d/u/f/b).
	VimKeys bool

	// Keys is the resolved keyboard; the zero value is hop's defaults.
	Keys keys.Map
}

// Browser is a remote directory browser the TUI drives by forwarding key
// messages and rendering View.
type Browser struct {
	client Client
	alias  string
	// cwd is the directory the cursor is inside, which Path() hands to the rest of hop.
	cwd string
	// Every index in this package — cursor, scroll, RowAt — indexes rows; see tree.go.
	root   *node
	rows   []*node
	cursor int
	scroll int

	// marks is keyed by absolute remote path across the whole tree. See marks.go.
	marks  map[string]bool
	target string

	note note

	opts Options
	w, h int

	// tmpDir is the scratch directory "o" fetches into, created on first use.
	tmpDir string

	// reader holds a half-typed "gg"; used only by Handle, since the model resolves its own keys.
	reader keys.Reader

	// overlay owns the keyboard while it is up. See prompt.go.
	overlay overlay

	sortBy sortMode

	// xfer is the transfer in flight, if any. See transfer.go.
	xfer *transfer
}

// New builds a Browser starting in startDir, or the remote home when empty or unlistable.
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

// SetOptions swaps in new user settings; the download dir is expanded here so an
// unexpanded "~" cannot create a literal "~" directory and skip the overwrite check.
func (b *Browser) SetOptions(opts Options) {
	opts.DownloadDir = pathx.ExpandHome(opts.DownloadDir)
	b.opts = opts
}

// load lists dir and commits it as the current directory, reporting whether it did; on
// error cwd is untouched, so callers must not report their own success over the failure.
func (b *Browser) load(dir string) bool {
	ents, err := b.client.List(dir)
	if err != nil {
		b.fail(err)
		return false
	}
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

// Handle applies a key message.
func (b *Browser) Handle(msg tea.KeyMsg) tea.Cmd {
	// An open question first: while one is up every key is its answer.
	if cmd, handled := b.overlayKey(msg); handled {
		return cmd
	}

	res := b.reader.Read(b.opts.Keys, keys.Browser, msg.String(), b.opts.VimKeys)
	if res.Pending {
		// First key of a chord: swallowed so nothing downstream sees it.
		return nil
	}
	return b.Do(res.Action)
}

// Do runs one resolved action; an action this layer does not own is a no-op.
func (b *Browser) Do(a keys.Action) tea.Cmd {
	switch a {
	case keys.Up, keys.Down, keys.Top, keys.Bottom, keys.HalfUp, keys.HalfDown,
		keys.PageUp, keys.PageDown, keys.ScreenTop, keys.ScreenMid, keys.ScreenBot,
		keys.In, keys.Out:
		return b.move(a)

	case keys.BrowserUp:
		// Re-roots the tree rather than following the cursor's directory.
		b.load(path.Dir(b.rootPath()))

	case keys.BrowserRefresh:
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

// Update takes the transfer messages the model routes back here by alias.
func (b *Browser) Update(msg Msg) tea.Cmd { return b.handleTransferMsg(msg.Body) }

// send wraps a browser message as the command that delivers it.
func (b *Browser) send(body any) tea.Cmd {
	msg := Msg{Alias: b.alias, Body: body}
	return func() tea.Msg { return msg }
}

// ---- mouse ----

// entryRows is the view row the first entry is drawn on; mirrors View.
const entryRows = 2

// RowAt maps a view row (0 is the path header) to the entry drawn there.
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

// Select stands the cursor on entry i, clamped.
func (b *Browser) Select(i int) {
	b.cursor = i
	b.clampScroll()
}

// Activate opens the entry under the cursor, as enter does.
func (b *Browser) Activate() tea.Cmd { return b.activate(false) }

// ActivateBeside is Activate for the split key (see keys.BrowserSplit).
func (b *Browser) ActivateBeside() tea.Cmd { return b.activate(true) }

// Scroll moves the cursor n rows, negative for up.
func (b *Browser) Scroll(n int) {
	b.cursor += n
	b.clampScroll()
}

// move applies a motion: H/M/L address the visible window, Top/Bottom the whole listing.
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

// outward is left/h: collapse, else step to the parent, else re-root one directory higher.
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

// rootPath is the directory the tree is rooted at; not b.cwd, which follows the cursor.
func (b *Browser) rootPath() string {
	if b.root == nil {
		return b.cwd
	}
	return b.root.path
}

// activate toggles the directory under the cursor, or asks the model to open the file.
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

// note is what the browser has to say on its last row.
type note struct {
	text string
	err  bool
	// until is zero for a note that holds until replaced; a live deadline outranks the
	// transfer progress bar for the same row.
	until time.Time
}

func (n note) live() bool { return !n.until.IsZero() && time.Now().Before(n.until) }

func (b *Browser) ok(msg string) { b.note = note{text: msg} }

func (b *Browser) fail(err error) { b.note = note{text: err.Error(), err: true} }

func (b *Browser) say(msg string, d time.Duration) {
	b.note = note{text: msg, err: true, until: time.Now().Add(d)}
}

func (b *Browser) clearNote() { b.note = note{} }

// checkLocalName rejects a server-supplied name unsafe as a local file name: separators
// escape the download dir, a colon addresses an NTFS stream, and device names open devices.
func checkLocalName(name string) error {
	if err := checkNameBasics(name); err != nil {
		return err
	}
	if strings.ContainsAny(name, `/\:`) {
		return fmt.Errorf("refusing unsafe remote file name %q", name)
	}
	// Windows strips trailing dots and spaces while normalizing, so "con." resolves to CON.
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

// checkNameBasics rejects the empty and dot names, and control characters (stripped for
// display, so such a name would read on screen as one it does not have).
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

// executableExts are extensions a desktop handler runs rather than opens; checked on
// every platform, since missing one costs code execution.
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

// executableName reports whether name's final extension is one the OS handler would run;
// trailing dots and spaces are trimmed because Windows drops them while normalizing.
func executableName(name string) bool {
	trimmed := strings.TrimRight(name, ". ")
	return executableExts[strings.ToLower(filepath.Ext(trimmed))]
}

// openCmd builds the command that opens p; a variable so tests can swap it. On Windows
// it is explorer.exe, not "cmd /c start": cmd re-parses metacharacters in the file name.
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

// windowRows is the number of entry rows actually filled.
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

// clampScroll clamps the cursor, slides the window to keep it visible, and re-points cwd —
// every motion in the package ends here.
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

// stripControl removes control characters from s: remote text could otherwise smuggle
// escape sequences into the user's terminal.
func stripControl(s string) string {
	return strings.Map(func(r rune) rune {
		if r < 0x20 || (r >= 0x7f && r < 0xa0) {
			return -1
		}
		return r
	}, s)
}

// truncateText shortens s to at most w display cells, with an ellipsis. Unstyled text only.
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
