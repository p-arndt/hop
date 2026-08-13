// Package terminal implements the embedded terminal pane: a live SSH session wired to
// an in-process VT emulator, so a remote shell renders and runs inside the TUI.
package terminal

import (
	"fmt"
	"io"
	"strings"
	"sync"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/vt"

	"hop/internal/sshx"
)

// lockedWriter serializes writes to the SSH stdin: the emulator's auto-response pump
// and SendKey share the mutex, so they never interleave a Write.
type lockedWriter struct {
	mu *sync.Mutex
	w  io.Writer
}

func (lw *lockedWriter) Write(p []byte) (int, error) {
	lw.mu.Lock()
	defer lw.mu.Unlock()
	return lw.w.Write(p)
}

// Pane is a single embedded terminal: a VT emulator bound to an SSH session. The
// SafeEmulator is concurrency-safe, so the output pump and Render() may run at once;
// writes to the session stdin are guarded by mu.
type Pane struct {
	emu  *vt.SafeEmulator
	sess *sshx.Session
	mu   sync.Mutex // guards ALL writes to sess.Stdin

	// paneW/paneH are the size Resize last applied, so a resize to the size the pane
	// already has can be dropped. Touched only from the UI goroutine.
	paneW, paneH int

	// scrollOffset is how far the scrollback window is lifted off the live bottom, in
	// lines: 0 is live. Deliberately unguarded — Bubble Tea's Update and View share one
	// goroutine and are the only places it is touched.
	scrollOffset int

	// mouse is the mouse reporting the far end has asked for, tracked through the
	// emulator's mode callbacks. It decides whether a wheel event belongs to the remote
	// program or to hop's scrollback. See mouse.go.
	mouse mouseState

	// paste is the bracketed-paste mode the far end has asked for, tracked the same way:
	// it decides whether a paste is marked as one on its way out. See paste.go.
	paste pasteState

	// clipSink takes a clipboard write from the remote (OSC 52); clipMu guards it, since
	// it is installed from the UI goroutine and read by the output pump. nil drops the
	// sequence. clipQueue is the one-deep mailbox for the single sink worker, whose
	// running state is clipBusy. See clipboard.go.
	clipSink  func(string)
	clipMu    sync.Mutex
	clipQueue chan string
	clipBusy  bool

	// cwd is the remote shell's working directory as last reported over OSC 7, and
	// cwdMu guards it: it is written by the output pump and read by the UI. osc is
	// the scanner that produces it, touched by the pump alone. See cwd.go.
	cwd   string
	cwdMu sync.Mutex
	osc   oscScanner

	// firstOutput is closed once the pane has parsed its first chunk of server output —
	// what the shell-integration injection waits for before typing.
	firstOutput chan struct{}

	// onOutput is the repaint callback New was given, kept so hop's own changes to the
	// screen reach the UI as promptly as server output does.
	onOutput func()

	// closed is what the shell-integration goroutine watches to give up. It sleeps in
	// seconds waiting for a prompt, so without this a closed tab would still be written
	// to afterwards.
	closed    chan struct{}
	closeOnce sync.Once
}

// New creates an emulator sized w x h, binds it to sess, and starts the two pumps:
//
//	remote output (sess.Stdout) -> emu.Write()               (drives the screen)
//	emu auto-responses (emu.Read via io.Copy) -> sess.Stdin  (e.g. cursor reports)
//
// The emulator's InputPipe()/Read() pair is the terminal->host response channel, not
// the parser feed: server output must go through Write().
//
// onOutput (may be nil) fires after each chunk is parsed, so the TUI repaints as bytes
// arrive rather than on a timer. Both goroutines run until their streams close.
func New(sess *sshx.Session, w, h int, onOutput func()) *Pane {
	emu := vt.NewSafeEmulator(w, h)
	p := &Pane{
		emu: emu, sess: sess,
		paneW: w, paneH: h, // the pty was opened at this size
		firstOutput: make(chan struct{}),
		closed:      make(chan struct{}),
		onOutput:    onOutput,
		clipQueue:   make(chan string, 1),
	}

	// Watch the remote program's mode changes for the two hop cares about: the mouse
	// (mouse.go) and bracketed paste (paste.go). Wired before the pumps start.
	emu.SetCallbacks(vt.Callbacks{
		EnableMode: func(mode ansi.Mode) {
			p.mouse.setMode(mode, true)
			p.paste.setMode(mode, true)
		},
		DisableMode: func(mode ansi.Mode) {
			p.mouse.setMode(mode, false)
			p.paste.setMode(mode, false)
		},
		// Leaving the alt screen ends the program that owned it, so its mode requests go
		// with it — a killed program never withdraws them, leaving the shell underneath
		// "asking" for a mouse it knows nothing about.
		//
		// A callback rather than a check after the chunk, because the next program's modes
		// are in that same chunk: quitting vim arrives as vim's teardown plus the shell's
		// prompt, and readline re-announces bracketed paste before every line.
		AltScreen: func(on bool) {
			if on {
				return
			}
			p.mouse.clear()
			p.paste.clear()
		},
	})

	// Server output -> emulator parser. A read loop rather than io.Copy, so the UI can
	// be notified right after each chunk is parsed.
	go func() {
		first := true
		buf := make([]byte, 32*1024)
		for {
			n, err := sess.Stdout.Read(buf)
			if n > 0 {
				// Parsing fires the callbacks wired above, in the order the bytes arrived.
				_, _ = emu.Write(buf[:n])
				// The same bytes, scanned (not re-parsed) for the sequence that reports the
				// remote shell's directory. See cwd.go.
				if dir, ok := p.osc.feed(buf[:n]); ok {
					p.setCwd(dir)
				}
				// A full reset takes every mode with it, and the emulator does that without
				// a callback — the scan above is the only warning. See oscScanner.ris.
				if p.osc.tookReset() {
					p.mouse.clear()
					p.paste.clear()
				}
				// A remote yank to the system clipboard (OSC 52). See clipboard.go.
				if text, ok := p.osc.tookClipboard(); ok {
					p.copyOut(text)
				}
				if first {
					first = false
					close(p.firstOutput)
				}
				if onOutput != nil {
					onOutput()
				}
			}
			if err != nil {
				return
			}
		}
	}()

	// Emulator auto-responses -> remote, guarded so they never clash with SendKey.
	go func() {
		_, _ = io.Copy(&lockedWriter{mu: &p.mu, w: sess.Stdin}, emu)
	}()

	return p
}

// View returns the rendered screen as an ANSI string. The emulator's Render() draws
// cells but no cursor, so a reverse-video block is overlaid at the cursor position.
func (p *Pane) View() string {
	rendered := p.emu.Render()
	pos := p.emu.CursorPosition()
	return overlayCursor(rendered, pos.X, pos.Y)
}

// overlayCursor draws a reverse-video block cursor at cell (cx, cy) on the rendered
// screen. It works on the row's string, so it never touches emulator state.
func overlayCursor(rendered string, cx, cy int) string {
	if cx < 0 || cy < 0 {
		return rendered
	}
	lines := strings.Split(rendered, "\n")
	if cy >= len(lines) {
		return rendered
	}
	lines[cy] = reverseAtColumn(lines[cy], cx)
	return strings.Join(lines, "\n")
}

// reverseAtColumn wraps the character at visible column col in reverse video, skipping
// ANSI escape sequences, which occupy no cells. A cursor past the end of the line pads
// with spaces and appends a reversed block.
func reverseAtColumn(line string, col int) string {
	runes := []rune(line)
	var b strings.Builder
	visCol := 0
	wrapped := false

	for i := 0; i < len(runes); {
		r := runes[i]
		if r == 0x1b { // ESC: copy the whole escape sequence verbatim, no column advance.
			j := i + 1
			if j < len(runes) {
				switch runes[j] {
				case '[': // CSI ... final byte in 0x40-0x7E
					j++
					for j < len(runes) && !(runes[j] >= 0x40 && runes[j] <= 0x7e) {
						j++
					}
					if j < len(runes) {
						j++
					}
				case ']': // OSC ... terminated by BEL or ST (ESC \)
					j++
					for j < len(runes) {
						if runes[j] == 0x07 {
							j++
							break
						}
						if runes[j] == 0x1b && j+1 < len(runes) && runes[j+1] == '\\' {
							j += 2
							break
						}
						j++
					}
				default: // ESC + single byte
					j++
				}
			}
			b.WriteString(string(runes[i:j]))
			i = j
			continue
		}

		if !wrapped && visCol == col {
			b.WriteString("\x1b[7m")
			b.WriteRune(r)
			b.WriteString("\x1b[27m")
			wrapped = true
		} else {
			b.WriteRune(r)
		}
		visCol += runeWidth(r)
		i++
	}

	if !wrapped {
		for visCol < col {
			b.WriteRune(' ')
			visCol++
		}
		b.WriteString("\x1b[7m \x1b[27m")
	}
	return b.String()
}

// runeWidth returns the terminal cell width of r, at least 1 so tracking advances.
func runeWidth(r rune) int {
	if w := lipgloss.Width(string(r)); w > 1 {
		return w
	}
	return 1
}

// SendKey translates a Bubble Tea key event into a terminal input byte sequence
// and writes it to the session stdin under the shared mutex.
func (p *Pane) SendKey(msg tea.KeyMsg) {
	b := keyToBytes(msg)
	if len(b) == 0 {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	_, _ = p.sess.Stdin.Write(b)
}

// AltScreen reports whether a full-screen program has taken the screen. Such a program
// keeps no scrollback of hop's, so the scrollback chords check it before taking a key.
func (p *Pane) AltScreen() bool {
	return p.emu.IsAltScreen()
}

// Resize resizes both the emulator screen and the remote PTY. A resize to the size the
// pane already has is dropped: callers resize whole sessions at a time, so most calls
// are no-ops, and a full-screen program redraws itself on every window-change.
func (p *Pane) Resize(w, h int) {
	if w == p.paneW && h == p.paneH {
		return
	}
	p.paneW, p.paneH = w, h
	p.emu.Resize(w, h)
	_ = p.sess.Resize(w, h)
}

// Close tears down the emulator and the SSH session; the two pumps from New unblock
// once these streams close.
//
// It closes the emulator's input pipe rather than calling emu.Close(): that sets an
// unguarded flag the response pump is reading from emu.Read, which -race reports on
// any test that closes a pane. Closing the pipe ends the goroutine the same way. The
// fallback keeps the old behaviour if vt stops handing back a Closer.
func (p *Pane) Close() error {
	// First, so the shell-integration goroutine gives up before the emulator goes.
	p.closeOnce.Do(func() { close(p.closed) })

	var emuErr error
	if pipe, ok := p.emu.InputPipe().(io.Closer); ok {
		emuErr = pipe.Close()
	} else {
		emuErr = p.emu.Close()
	}

	sessErr := p.sess.Close()
	if sessErr != nil {
		return sessErr
	}
	return emuErr
}

// keyToBytes maps a Bubble Tea key event to the bytes a terminal application expects
// on stdin.
//
//	printable runes -> UTF-8 bytes; space -> " "; enter -> "\r"; tab -> "\t";
//	backspace -> "\x7f"; esc -> "\x1b";
//	up/down/right/left -> ESC[A / ESC[B / ESC[C / ESC[D;
//	home -> ESC[H; end -> ESC[F; delete -> ESC[3~; pgup -> ESC[5~; pgdown -> ESC[6~;
//	ctrl+<letter> -> the corresponding control byte (ctrl+c -> 0x03, ctrl+d -> 0x04, ...).
func keyToBytes(msg tea.KeyMsg) []byte {
	// A modified cursor key is not "the key behind an ESC": xterm puts the modifier
	// inside the sequence (ESC[1;5D for ctrl+left), so it is handled before the meta
	// prefix below.
	if b, ok := modifiedKeyBytes(msg); ok {
		return b
	}

	b := keyBytes(msg)
	// A meta-modified key is that key's bytes behind an ESC — how a terminal sends
	// alt+<key>, and how readline and vim read it back. Without the prefix, an alt chord
	// reaches the remote as the bare key.
	if msg.Alt && len(b) > 0 && msg.Type != tea.KeyEsc {
		return append([]byte{0x1b}, b...)
	}
	return b
}

// modifiedKeyBytes maps a cursor/navigation key carrying ctrl and/or shift to its xterm
// sequence, and reports whether the event was one.
//
// The encoding is CSI 1 ; <mod> <final> for arrows and home/end, CSI <n> ; <mod> ~ for
// the tilde keys, where <mod> is 1 + a bitmask (shift 1, alt 2, ctrl 4): ctrl+left is
// ESC[1;5D, shift+right ESC[1;2C. Without this, ctrl+left returned nil from keyBytes.
func modifiedKeyBytes(msg tea.KeyMsg) ([]byte, bool) {
	var final byte // 'A'/'B'/'C'/'D'/'H'/'F', or 0 for a tilde key
	var tilde int  // the CSI parameter of a tilde key (5 pgup, 6 pgdown)
	var mods int   // shift 1, alt 2, ctrl 4

	switch msg.Type {
	case tea.KeyCtrlUp:
		final, mods = 'A', 4
	case tea.KeyCtrlDown:
		final, mods = 'B', 4
	case tea.KeyCtrlRight:
		final, mods = 'C', 4
	case tea.KeyCtrlLeft:
		final, mods = 'D', 4
	case tea.KeyCtrlHome:
		final, mods = 'H', 4
	case tea.KeyCtrlEnd:
		final, mods = 'F', 4
	case tea.KeyShiftUp:
		final, mods = 'A', 1
	case tea.KeyShiftDown:
		final, mods = 'B', 1
	case tea.KeyShiftRight:
		final, mods = 'C', 1
	case tea.KeyShiftLeft:
		final, mods = 'D', 1
	case tea.KeyShiftHome:
		final, mods = 'H', 1
	case tea.KeyShiftEnd:
		final, mods = 'F', 1
	case tea.KeyCtrlShiftUp:
		final, mods = 'A', 5
	case tea.KeyCtrlShiftDown:
		final, mods = 'B', 5
	case tea.KeyCtrlShiftRight:
		final, mods = 'C', 5
	case tea.KeyCtrlShiftLeft:
		final, mods = 'D', 5
	case tea.KeyCtrlShiftHome:
		final, mods = 'H', 5
	case tea.KeyCtrlShiftEnd:
		final, mods = 'F', 5
	case tea.KeyCtrlPgUp:
		tilde, mods = 5, 4
	case tea.KeyCtrlPgDown:
		tilde, mods = 6, 4
	default:
		return nil, false
	}

	// alt here is another bit in the parameter, not the ESC prefix a plain alt+key gets.
	if msg.Alt {
		mods |= 2
	}

	if final != 0 {
		return []byte(fmt.Sprintf("\x1b[1;%d%c", mods+1, final)), true
	}
	return []byte(fmt.Sprintf("\x1b[%d;%d~", tilde, mods+1)), true
}

// keyBytes is keyToBytes without the meta prefix: the bytes for the key itself.
func keyBytes(msg tea.KeyMsg) []byte {
	switch msg.Type {
	case tea.KeyRunes:
		return []byte(string(msg.Runes))
	case tea.KeySpace:
		return []byte(" ")
	case tea.KeyEnter:
		return []byte("\r")
	case tea.KeyTab:
		return []byte("\t")
	case tea.KeyBackspace:
		return []byte("\x7f")
	case tea.KeyEsc:
		return []byte("\x1b")
	case tea.KeyUp:
		return []byte("\x1b[A")
	case tea.KeyDown:
		return []byte("\x1b[B")
	case tea.KeyRight:
		return []byte("\x1b[C")
	case tea.KeyLeft:
		return []byte("\x1b[D")
	case tea.KeyHome:
		return []byte("\x1b[H")
	case tea.KeyEnd:
		return []byte("\x1b[F")
	case tea.KeyDelete:
		return []byte("\x1b[3~")
	case tea.KeyPgUp:
		return []byte("\x1b[5~")
	case tea.KeyPgDown:
		return []byte("\x1b[6~")
	case tea.KeyShiftTab:
		// CSI Z (back-tab). Its String() is "shift+tab", which matches no branch below,
		// so without this case it was dropped.
		return []byte("\x1b[Z")
	case tea.KeyInsert:
		return []byte("\x1b[2~")
	}

	// Ctrl combinations are detected via the canonical string form. The control byte for
	// ctrl+<letter> is the letter with its top three bits cleared: ctrl+a -> 1.
	s := msg.String()
	if rest, ok := strings.CutPrefix(s, "ctrl+"); ok && len(rest) == 1 {
		c := rest[0]
		switch {
		case c >= 'a' && c <= 'z':
			return []byte{c - 'a' + 1}
		case c >= 'A' && c <= 'Z':
			return []byte{c - 'A' + 1}
		default:
			// ctrl+@, ctrl+[, ctrl+\, ctrl+], ctrl+^, ctrl+_ etc.
			return []byte{c & 0x1f}
		}
	}

	// Fallback: emit any runes carried on the event (e.g. alt-modified input).
	if len(msg.Runes) > 0 {
		return []byte(string(msg.Runes))
	}
	return nil
}

// clampOffset pulls scrollOffset back into [0, ScrollbackLen()] and returns it. Called
// at the top of every scroll operation, because the ceiling rises as the output pump
// pushes more lines into scrollback while the user reads.
func (p *Pane) clampOffset() int {
	sbLen := p.emu.ScrollbackLen()
	if p.scrollOffset > sbLen {
		p.scrollOffset = sbLen
	}
	if p.scrollOffset < 0 {
		p.scrollOffset = 0
	}
	return p.scrollOffset
}

// ScrollUp lifts the window n lines toward older output, stopping at the oldest line
// scrollback still holds.
func (p *Pane) ScrollUp(n int) {
	if n <= 0 {
		return
	}
	p.scrollOffset += n
	p.clampOffset()
}

// ScrollDown lowers the window n lines back toward the live bottom, stopping at 0.
func (p *Pane) ScrollDown(n int) {
	if n <= 0 {
		return
	}
	p.scrollOffset -= n
	p.clampOffset()
}

// ScrollToTop lifts the window as far as history allows, putting the oldest line the
// buffer holds at the top.
func (p *Pane) ScrollToTop() {
	p.scrollOffset = p.emu.ScrollbackLen()
	p.clampOffset()
}

// ScrollToBottom drops the window back to live.
func (p *Pane) ScrollToBottom() {
	p.scrollOffset = 0
}

// ScrollOffset reports how far the window is lifted off the live bottom, in lines.
// 0 means live.
func (p *Pane) ScrollOffset() int {
	return p.scrollOffset
}

// ScrollbackLen reports how many lines have scrolled off above the live screen and are
// still buffered — the ceiling every scroll offset is clamped against. It is 0 on the
// alt screen.
func (p *Pane) ScrollbackLen() int {
	return p.emu.ScrollbackLen()
}

// AtBottom reports whether the window is live, which is how the TUI decides whether
// new output pins the view to the bottom or leaves a reader's position alone.
func (p *Pane) AtBottom() bool {
	return p.scrollOffset == 0
}

// ViewScrollback renders the windowed view at the current scroll offset, exactly
// emu.Height() lines tall so it drops into the layout in place of View(). It lays down
// no cursor: this is history being read.
//
// Scrollback and the live screen are one tall virtual buffer — sbLen lines of history,
// then h lines of screen — and the window is a height-h slice whose top sits at virtual
// index sbLen-offset, so offset 0 is exactly the live screen.
func (p *Pane) ViewScrollback() string {
	h := p.emu.Height()
	offset := p.clampOffset()
	sbLen := p.emu.ScrollbackLen()

	sb := p.emu.Scrollback()
	if sb == nil {
		sbLen = 0
	}

	// Render may hand back fewer than h lines; pad so screen[0..h-1] exists.
	screen := strings.Split(p.emu.Render(), "\n")
	for len(screen) < h {
		screen = append(screen, "")
	}

	top := sbLen - offset
	lines := make([]string, 0, h)
	for i := 0; i < h; i++ {
		vi := top + i
		var line string
		switch {
		case vi < 0:
			// Above the oldest line held — only reachable if the buffer shrank under a
			// stale offset.
			line = ""
		case vi < sbLen:
			if l := sb.Line(vi); l != nil {
				line = l.Render()
			}
		default:
			if si := vi - sbLen; si >= 0 && si < len(screen) {
				line = screen[si]
			}
		}
		lines = append(lines, line)
	}
	return strings.Join(lines, "\n")
}
