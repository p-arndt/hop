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

// paneWriter is the emulator's auto-response pump on its way to the far end: it queues
// like everything else hop sends, so a response cannot land halfway through a keystroke.
type paneWriter struct{ p *Pane }

func (w paneWriter) Write(b []byte) (int, error) {
	w.p.send(b)
	return len(b), nil
}

// inChunk is one item of the input queue: bytes for the far end, a marker Flush waits
// on, or both. A marker is closed once everything queued ahead of it has been written,
// which is the only way to observe the queue from outside it.
type inChunk struct {
	b    []byte
	done chan struct{}
}

// inQueue is how many chunks may be waiting for the wire at once. Far past anything a
// hand or a paste produces, so it is only ever reached by a far end that has stopped
// reading altogether — at which point the input is dropped rather than kept forever.
const inQueue = 1024

// Pane is a single embedded terminal: a VT emulator bound to an SSH session. The
// SafeEmulator is concurrency-safe, so the output pump and Render() may run at once;
// everything hop sends to the far end goes through the input queue. See send.
type Pane struct {
	emu  *vt.SafeEmulator
	sess *sshx.Session

	// in is the input queue, drained to sess.Stdin by one goroutine. Nothing writes to
	// the session directly, for two reasons:
	//
	//   - An SSH channel Write blocks once the remote's window is full or the link
	//     stalls, and SendKey/SendMouse run on Bubble Tea's update goroutine. A blocked
	//     Write there holds the entire TUI: no repaint, no other key, until the far end
	//     reads again. A big paste could freeze it outright.
	//   - One queue keeps the order a mutex used to: a keystroke and the emulator's own
	//     auto-response cannot interleave halfway through a sequence.
	in chan inChunk

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

	// cursor is the cursor the far end has asked for — hidden or not, and its shape —
	// tracked through the emulator's cursor callbacks, plus the frame of hop's own blink
	// clock. It decides what View draws over the cell the emulator reports. See cursor.go.
	cursor cursorState

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
		in:    make(chan inChunk, inQueue),
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
		// The cursor's own callbacks rather than the mode above: DECTCEM is only half of
		// it, DECSCUSR is not a mode at all, and vt re-reports visibility when the
		// alternate screen switches, each screen carrying its own cursor.
		CursorVisibility: func(visible bool) {
			p.cursor.setVisible(visible)
		},
		CursorStyle: func(style vt.CursorStyle, steady bool) {
			p.cursor.setStyle(style, steady)
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
			// The style goes the same way as the modes: a vim killed mid-insert never
			// puts the block back, and the shell underneath would inherit its bar. The
			// visibility that follows this callback is vt's own, for the screen returned to.
			p.cursor.clear()
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
					p.cursor.clear()
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

	// Emulator auto-responses -> remote, through the queue so they never clash with a
	// keystroke.
	go func() {
		_, _ = io.Copy(paneWriter{p}, emu)
	}()

	// The queue -> remote. The only goroutine that writes to the session, and the only
	// one allowed to block on it.
	//
	// A failed write is not the end of it: the session may still be usable, and quitting
	// here would leave a live pane whose input goes nowhere — queued, never written, and
	// dropped once the queue fills. Only closing the pane ends the drain.
	go func() {
		for {
			select {
			case c := <-p.in:
				if len(c.b) > 0 {
					_, _ = sess.Stdin.Write(c.b)
				}
				if c.done != nil {
					close(c.done)
				}
			case <-p.closed:
				return
			}
		}
	}()

	return p
}

// View returns the rendered screen as an ANSI string. The emulator's Render() draws
// cells but no cursor, so hop marks the cursor's cell itself — in the shape the far end
// asked for, and not at all while it has the cursor hidden or hop's blink clock has it
// down. See cursor.go.
func (p *Pane) View() string {
	rendered := p.emu.Render()
	look := p.cursor.look()
	if !look.drawn() {
		return rendered
	}
	pos := p.emu.CursorPosition()
	return overlayCursor(rendered, pos.X, pos.Y, markFor(look.style))
}

// runeWidth returns the terminal cell width of r, at least 1 so tracking advances.
func runeWidth(r rune) int {
	if w := lipgloss.Width(string(r)); w > 1 {
		return w
	}
	return 1
}

// SendKey translates a Bubble Tea key event into a terminal input byte sequence
// and queues it for the far end.
func (p *Pane) SendKey(msg tea.KeyMsg) {
	p.send(keyToBytes(msg))
}

// SendKeys queues a run of key events as one item, which is what a burst replayed as
// keystrokes is: the bytes are exactly what SendKey per key would have put on the wire,
// but they occupy one slot of the queue rather than one each. See internal/tui/paste.go.
func (p *Pane) SendKeys(msgs []tea.KeyMsg) {
	var b []byte
	for _, msg := range msgs {
		b = append(b, keyToBytes(msg)...)
	}
	p.send(b)
}

// send queues b for the far end and returns at once — the caller is usually the UI
// goroutine, which must never wait on the wire. b is copied, so a caller may reuse it.
//
// A full queue means the far end has stopped reading entirely; the input is dropped
// rather than blocking the UI or growing without bound. Anything a hand or a paste
// produces is orders of magnitude short of that.
func (p *Pane) send(b []byte) {
	if len(b) == 0 || p.isClosed() {
		return
	}
	select {
	case p.in <- inChunk{b: append([]byte(nil), b...)}:
	default:
	}
}

// Flush blocks until everything queued when it was called has been handed to the
// session, or the pane closes. hop itself never waits on the wire, so nothing in the UI
// calls this; it is how a caller outside the pane — a test — can tell that queued input
// has gone.
//
// It queues a marker rather than counting what is in flight: a counter would be raised
// by SendKey and the emulator's response pump while this is waiting on it, which is the
// one thing a WaitGroup may not have done to it.
func (p *Pane) Flush() {
	done := make(chan struct{})
	select {
	case p.in <- inChunk{done: done}:
	case <-p.closed:
		return
	}
	select {
	case <-done:
	case <-p.closed:
	}
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
		return fmt.Appendf(nil, "\x1b[1;%d%c", mods+1, final), true
	}
	return fmt.Appendf(nil, "\x1b[%d;%d~", tilde, mods+1), true
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
