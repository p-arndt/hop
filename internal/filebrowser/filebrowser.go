// Package filebrowser implements a self-contained remote directory browser
// component for hop's TUI. It mirrors the shape of terminal.Pane: the TUI
// forwards key messages via Handle and renders the component with View. All
// SFTP operations run synchronously — a slow directory briefly stalls the UI,
// which is acceptable for the MVP.
package filebrowser

import (
	"fmt"
	"os"
	"path"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

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

// Client is the slice of *sftpx.Client the browser depends on. Narrowing it to
// an interface keeps the component testable without a live SFTP connection.
type Client interface {
	Home() (string, error)
	List(dir string) ([]sftpx.Entry, error)
	Download(remotePath, localPath string) (int64, error)
	Close() error
}

// Browser is a remote directory browser the TUI drives by forwarding key
// messages and rendering View.
type Browser struct {
	client      Client
	cwd         string
	entries     []sftpx.Entry
	cursor      int
	scroll      int
	status      string
	downloadDir string
	w, h        int

	// pendingG is set after a lone "g", so the next "g" completes the vim "gg"
	// motion. Any other key clears it.
	pendingG bool
}

// New builds a Browser starting in startDir (or the remote home when startDir
// is empty), ensuring downloadDir exists on the local filesystem.
func New(c Client, startDir, downloadDir string, w, h int) (*Browser, error) {
	if err := os.MkdirAll(downloadDir, 0o755); err != nil {
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
		client:      c,
		cwd:         dir,
		downloadDir: downloadDir,
		w:           w,
		h:           h,
	}
	b.load(dir)
	return b, nil
}

// load lists dir and, on success, commits it as the current directory, resetting
// the cursor and scroll. On error it sets the status and leaves cwd/entries
// untouched.
func (b *Browser) load(dir string) {
	ents, err := b.client.List(dir)
	if err != nil {
		b.status = err.Error()
		return
	}
	b.cwd = dir
	b.entries = ents
	b.cursor = 0
	b.scroll = 0
	b.status = ""
}

// Handle applies a key message: motions, directory entry, parent, refresh, and
// file download. All SFTP work runs synchronously.
//
// No key here leaves the browser: dismissal is the enclosing model's business
// (ctrl+o or a double esc). "left", "backspace" and "h" are all strict "up a
// directory", so bumping against the top is a no-op rather than a surprise exit.
func (b *Browser) Handle(msg tea.KeyMsg) {
	key := msg.String()

	// Complete or abandon a pending "gg".
	if b.pendingG {
		b.pendingG = false
		if key == "g" {
			b.cursor = 0
			b.scroll = 0
			return
		}
	}

	switch key {
	case "up", "k":
		b.cursor--
		b.clampScroll()

	case "down", "j":
		b.cursor++
		b.clampScroll()

	case "g":
		b.pendingG = true

	case "G":
		b.cursor = len(b.entries) - 1
		b.clampScroll()

	case "ctrl+d":
		b.cursor += b.halfPage()
		b.clampScroll()

	case "ctrl+u":
		b.cursor -= b.halfPage()
		b.clampScroll()

	case "ctrl+f", "pgdown":
		b.cursor += b.contentRows()
		b.clampScroll()

	case "ctrl+b", "pgup":
		b.cursor -= b.contentRows()
		b.clampScroll()

	case "H":
		b.cursor = b.scroll
		b.clampScroll()

	case "M":
		b.cursor = b.scroll + b.windowRows()/2
		b.clampScroll()

	case "L":
		b.cursor = b.scroll + b.windowRows() - 1
		b.clampScroll()

	case "r":
		b.load(b.cwd)

	case "left", "backspace", "h":
		// path.Dir of "/" stays "/", so this is a no-op at the filesystem root.
		b.load(path.Dir(b.cwd))

	case "enter", "right", "l":
		if len(b.entries) == 0 {
			return
		}
		e := b.entries[b.cursor]
		if e.IsDir {
			b.load(path.Join(b.cwd, e.Name))
			return
		}
		// Regular file: download into downloadDir.
		remote := path.Join(b.cwd, e.Name)
		local := path.Join(b.downloadDir, e.Name)
		if _, err := b.client.Download(remote, local); err != nil {
			b.status = err.Error()
			return
		}
		b.status = fmt.Sprintf("downloaded %s → %s", e.Name, b.downloadDir)
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
	lines = append(lines, dimStyle.Render(truncPath(b.cwd, b.w)))
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

	// Status line: green for downloads, red-ish for errors, empty otherwise.
	if b.status != "" {
		txt := truncateText(b.status, b.w)
		if strings.HasPrefix(b.status, "downloaded") {
			lines = append(lines, greenStyle.Render(txt))
		} else {
			lines = append(lines, redStyle.Render(txt))
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

	nameText := e.Name
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
