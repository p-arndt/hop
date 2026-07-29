// Package terminal implements the embedded terminal pane: it wires a live SSH
// session to an in-process VT emulator so a remote shell can be rendered and
// driven entirely inside the TUI (tmux-in-app feel), with no external terminal.
package terminal

import (
	"io"
	"strings"
	"sync"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/vt"

	"hop/internal/sshx"
)

// lockedWriter serializes all writes to the underlying SSH stdin behind a shared
// mutex. Both the emulator->remote auto-response pump and interactive SendKey
// writes go through the same mutex so they can never interleave a single Write.
type lockedWriter struct {
	mu *sync.Mutex
	w  io.Writer
}

func (lw *lockedWriter) Write(p []byte) (int, error) {
	lw.mu.Lock()
	defer lw.mu.Unlock()
	return lw.w.Write(p)
}

// Pane is a single embedded terminal: a VT emulator bound to an SSH session.
// The emulator (SafeEmulator) is internally concurrency-safe, so the output
// pump and UI Render() may run concurrently. All writes to the session stdin
// are additionally guarded by mu.
type Pane struct {
	emu  *vt.SafeEmulator
	sess *sshx.Session
	mu   sync.Mutex // guards ALL writes to sess.Stdin

	// scrollOffset is how far the scrollback window is lifted off the live bottom,
	// measured in lines: 0 is live (the current screen), N is scrolled N lines up
	// into history. It is the one piece of pane state that carries NO mutex, and on
	// purpose.
	//
	// Bubble Tea's Update and View both run on the single UI goroutine, and those are
	// the only two places this field is ever touched — the scroll methods below are
	// called from Update, ViewScrollback reads it from View. The output pump never so
	// much as looks at it. So there is no second goroutine to race with, and the only
	// shared thing the scroll methods reach through — the emulator — is a SafeEmulator,
	// concurrency-safe in its own right. A mutex here would guard against a contender
	// that does not exist.
	scrollOffset int

	// mouse is the mouse reporting the far end has asked for, tracked through the
	// emulator's mode callbacks. It is what decides whether a wheel event over this
	// pane belongs to the remote program or to hop's own scrollback. See mouse.go.
	mouse mouseState

	// cwd is the remote shell's working directory as last reported over OSC 7, and
	// cwdMu guards it: it is written by the output pump and read by the UI. osc is
	// the scanner that produces it, touched by the pump alone. See cwd.go.
	cwd   string
	cwdMu sync.Mutex
	osc   oscScanner

	// firstOutput is closed once the pane has parsed its first chunk of server
	// output — the tell that the far end has started talking, which is what the
	// shell-integration injection waits for before typing into it.
	firstOutput chan struct{}

	// onOutput is the repaint callback New was given, kept so the changes hop makes
	// to the screen itself — erasing the line the shell integration typed — reach the
	// UI as promptly as the server's own output does.
	onOutput func()

	// closed is closed by Close, and is what the shell-integration goroutine watches
	// to give up. That goroutine outlives nothing else here: it sleeps in seconds
	// while it waits for a prompt, so a tab closed underneath it would otherwise
	// still write to the emulator afterwards — which Close's contract forbids.
	closed    chan struct{}
	closeOnce sync.Once
}

// New creates an emulator sized w x h, binds it to sess, and starts the two
// data pumps described in DATA FLOW:
//
//	remote output (sess.Stdout) -> emu.Write()      (feeds the parser, drives the screen)
//	emu auto-responses (emu.Read via io.Copy) -> sess.Stdin  (e.g. cursor reports)
//
// Note: the emulator's InputPipe()/Read() pair is the terminal->host response
// channel (a loopback pipe), NOT the parser feed. Server output must go through
// Write(), which advances the ANSI parser; SafeEmulator.Write is concurrency-safe.
//
// onOutput (may be nil) is invoked after each chunk of server output has been
// parsed into the emulator. The TUI uses it to trigger an immediate repaint, so
// keystroke echoes render as soon as bytes arrive rather than waiting on a timer
// (this is what removes the perceived typing lag).
//
// Both goroutines run until their streams close (session teardown / Close).
func New(sess *sshx.Session, w, h int, onOutput func()) *Pane {
	emu := vt.NewSafeEmulator(w, h)
	p := &Pane{
		emu: emu, sess: sess,
		firstOutput: make(chan struct{}),
		closed:      make(chan struct{}),
		onOutput:    onOutput,
	}

	// Watch the mode changes the remote program makes, for the one thing hop needs
	// to know about them: whether it has asked for the mouse (see mouse.go). Wired
	// before the pumps start, so nothing is parsed while the callbacks are being set.
	emu.SetCallbacks(vt.Callbacks{
		EnableMode:  func(mode ansi.Mode) { p.mouse.setMode(mode, true) },
		DisableMode: func(mode ansi.Mode) { p.mouse.setMode(mode, false) },
	})

	// Remote/server output -> emulator parser: update the rendered screen. We
	// read in a loop (instead of io.Copy) so we can notify the UI right after
	// each chunk is parsed — event-driven repaints instead of polling.
	go func() {
		first := true
		buf := make([]byte, 32*1024)
		for {
			n, err := sess.Stdout.Read(buf)
			if n > 0 {
				_, _ = emu.Write(buf[:n])
				// The same bytes, watched for the one sequence that reports the remote
				// shell's directory (see cwd.go). It is a scan, not a second parse: the
				// emulator above remains the only thing interpreting the stream.
				if dir, ok := p.osc.feed(buf[:n]); ok {
					p.setCwd(dir)
				}
				// A full reset takes every mode with it, mouse reporting included — and
				// the emulator makes that change without a callback, so the scan above is
				// the only warning hop gets. See oscScanner.ris.
				if p.osc.tookReset() {
					p.mouse.clear()
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

	// Emulator auto-generated responses -> remote, through the mutex-guarded
	// writer so it never clashes with SendKey.
	go func() {
		_, _ = io.Copy(&lockedWriter{mu: &p.mu, w: sess.Stdin}, emu)
	}()

	return p
}

// View returns the full rendered screen as an ANSI string, ready to be placed
// into the surrounding TUI layout. The emulator's Render() draws cell contents
// but NOT a cursor (a real terminal draws its own hardware cursor), so we overlay
// a reverse-video block at the emulator's cursor position.
func (p *Pane) View() string {
	rendered := p.emu.Render()
	pos := p.emu.CursorPosition()
	return overlayCursor(rendered, pos.X, pos.Y)
}

// overlayCursor draws a reverse-video block cursor at cell (cx, cy) on top of the
// already-rendered screen. It operates on the row's string, column-aware and
// skipping ANSI escape sequences, so it never touches emulator state (no races).
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

// reverseAtColumn wraps the character at visible column col in reverse video,
// advancing the visible column past ANSI escape sequences (which occupy no
// cells). If the cursor sits past the end of the line's content, it pads with
// spaces and appends a reversed block.
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

// runeWidth returns the terminal cell width of r (>=1 so column tracking always
// advances).
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

// AltScreen reports whether a full-screen program — vim, htop, less — has taken the
// screen. Such a program owns the whole keyboard and keeps no scrollback of hop's,
// so it is what the scrollback chords check before taking a key.
func (p *Pane) AltScreen() bool {
	return p.emu.IsAltScreen()
}

// Resize resizes both the emulator screen and the remote PTY.
func (p *Pane) Resize(w, h int) {
	p.emu.Resize(w, h)
	_ = p.sess.Resize(w, h)
}

// Close tears down the emulator and the underlying SSH session. The two pumps
// started in New unblock once these streams close, and neither touches the emulator
// again afterwards.
//
// It closes the emulator's *input pipe* rather than calling emu.Close(), and that is
// the whole point of the dance: emu.Close() sets an unguarded `closed` flag that the
// response pump — parked inside emu.Read for the life of the pane — is reading at
// that very moment, and SafeEmulator locks neither one (its Read passes straight
// through, and it has no Close of its own). Closing a pane while its pump was alive
// was therefore a data race, which -race reported on any test that closed one.
//
// Closing the pipe does the one thing emu.Close() was wanted for — Read returns EOF,
// the copy finishes, the goroutine goes — and it writes nothing, so there is nothing
// left to race on. The flag is all that is skipped, and it guards a Write that can no
// longer be reached: the session is closed just below, so no further output arrives
// to be parsed. Nothing leaks — the emulator is memory, and it goes with the pane.
//
// The fallback keeps the old behaviour if vt ever stops handing back a Closer: a
// benign race beats a stranded goroutine holding a dead session open.
func (p *Pane) Close() error {
	// First, so anything still working on this pane — the shell-integration goroutine,
	// which may be parked for seconds waiting on a prompt — gives up before the
	// emulator underneath it goes.
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

// keyToBytes maps a Bubble Tea (v1) key event to the raw byte sequence a
// terminal application expects on stdin.
//
//	printable runes -> UTF-8 bytes; space -> " "; enter -> "\r"; tab -> "\t";
//	backspace -> "\x7f"; esc -> "\x1b";
//	up/down/right/left -> ESC[A / ESC[B / ESC[C / ESC[D;
//	home -> ESC[H; end -> ESC[F; delete -> ESC[3~; pgup -> ESC[5~; pgdown -> ESC[6~;
//	ctrl+<letter> -> the corresponding control byte (ctrl+c -> 0x03, ctrl+d -> 0x04, ...).
func keyToBytes(msg tea.KeyMsg) []byte {
	b := keyBytes(msg)
	// A meta-modified key is that key's bytes behind an ESC, which is how a terminal
	// sends alt+<key> and how readline (alt+b, alt+f) and vim (<esc>o typed fast enough
	// to arrive as one event) read it back. Without the prefix, a forwarded alt chord
	// reaches the remote as the bare key — 'o' inserting a line vim is still in insert
	// mode for, rather than the <esc> that was pressed.
	if msg.Alt && len(b) > 0 && msg.Type != tea.KeyEsc {
		return append([]byte{0x1b}, b...)
	}
	return b
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
	}

	// Ctrl combinations (and anything else) are detected via the canonical
	// string form, e.g. "ctrl+c". The control byte for ctrl+<letter> is the
	// letter with its top three bits cleared (c & 0x1f): ctrl+a -> 1 ... ctrl+z -> 26.
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

// clampOffset pulls scrollOffset back into the range history can actually
// support, [0, ScrollbackLen()], and returns it. It is called at the top of every
// scroll operation because the ceiling is not fixed: the output pump is pushing
// lines into scrollback the whole time the user is reading it, so the top of the
// window rises under our feet. An offset that was pinned to the oldest line a
// moment ago is still the oldest line — clamping never has to lower it — but an
// offset can never point above the buffer or below the live screen, and this is
// where both ends are kept honest.
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

// ScrollUp lifts the window n lines toward older output, stopping at the oldest
// line scrollback still holds. A non-positive n is nothing to do, so it is left
// alone. The clamp is against ScrollbackLen() read fresh, because more history may
// have arrived since the last scroll and the ceiling only ever rises.
func (p *Pane) ScrollUp(n int) {
	if n <= 0 {
		return
	}
	p.scrollOffset += n
	p.clampOffset()
}

// ScrollDown lowers the window n lines back toward the live bottom, stopping at 0
// — the live screen. A non-positive n is a no-op. Going below 0 has no meaning
// (there is nothing newer than live), so the floor holds it there.
func (p *Pane) ScrollDown(n int) {
	if n <= 0 {
		return
	}
	p.scrollOffset -= n
	p.clampOffset()
}

// ScrollToTop lifts the window as far as history allows: the offset becomes
// ScrollbackLen(), which puts the oldest line the buffer still holds at the top of
// the window. Read fresh each time, since the buffer may have grown.
func (p *Pane) ScrollToTop() {
	p.scrollOffset = p.emu.ScrollbackLen()
	p.clampOffset()
}

// ScrollToBottom drops the window back to live — offset 0, the current screen with
// nothing lifted off it.
func (p *Pane) ScrollToBottom() {
	p.scrollOffset = 0
}

// ScrollOffset reports how far the window is lifted off the live bottom, in lines.
// 0 means live.
func (p *Pane) ScrollOffset() int {
	return p.scrollOffset
}

// ScrollbackLen reports how many lines have scrolled off above the live screen and
// are still held in the buffer — a pass-through to the emulator, which returns 0 on
// the alt screen. It is the ceiling every scroll offset is clamped against.
func (p *Pane) ScrollbackLen() int {
	return p.emu.ScrollbackLen()
}

// AtBottom reports whether the window is live — offset 0, showing the current
// screen. It is the tell the TUI uses to decide whether new output should keep the
// view pinned to the bottom or leave a reader's scrolled-up position undisturbed.
func (p *Pane) AtBottom() bool {
	return p.scrollOffset == 0
}

// ViewScrollback renders the windowed view at the current scroll offset, exactly
// emu.Height() lines tall so it slots into the layout in place of View() with no
// change to the surrounding geometry. Unlike View() it lays down NO cursor: this is
// history being read, not a live line being edited, and a cursor adrift in old
// output would only mislead.
//
// The trick is to treat scrollback and the live screen as one tall virtual buffer:
// the sbLen lines of history first, then the h lines of the current screen, indexed
// 0 .. sbLen+h-1. The window is a height-h slice of that buffer whose top sits at
// virtual index sbLen-offset — so at offset 0 the top is at sbLen, the window is
// exactly the live screen, and this returns what View() would minus the cursor
// (a property the tests pin down). Lift the offset and the same slice walks up into
// history.
//
// Widths are left alone, as View() leaves them: lipgloss pads the lines out when it
// draws the pane border, and pre-padding here would fight it.
func (p *Pane) ViewScrollback() string {
	h := p.emu.Height()
	offset := p.clampOffset()
	sbLen := p.emu.ScrollbackLen()

	sb := p.emu.Scrollback()
	if sb == nil {
		sbLen = 0
	}

	// The live screen, split into rows. Render may hand back fewer than h lines; pad
	// so the index arithmetic below can trust screen[0..h-1] to exist.
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
			// Above the oldest line the buffer holds — only reachable if the buffer
			// shrank under a stale offset; render it blank rather than out of range.
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
