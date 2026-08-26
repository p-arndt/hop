// Package terminal implements the embedded terminal pane: an SSH session wired to an
// in-process VT emulator.
package terminal

import (
	"fmt"
	"io"
	"strings"
	"sync"
	"unicode"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/vt"

	"hop/internal/sshx"
)

// paneWriter routes emulator auto-responses through the queue, so one cannot land
// halfway through a keystroke.
type paneWriter struct{ p *Pane }

func (w paneWriter) Write(b []byte) (int, error) {
	w.p.send(b)
	return len(b), nil
}

// inChunk is one input-queue item: bytes for the far end, a marker Flush waits on, or both.
type inChunk struct {
	b    []byte
	done chan struct{}
}

// inQueue caps chunks waiting for the wire; only reached by a far end that stopped reading.
const inQueue = 1024

// Pane is a single embedded terminal: a VT emulator bound to an SSH session.
type Pane struct {
	emu  *vt.SafeEmulator
	sess *sshx.Session

	// in is drained to sess.Stdin by one goroutine: an SSH write blocks when the remote
	// window fills, and SendKey runs on Bubble Tea's update goroutine.
	in chan inChunk

	// paneW/paneH are the size Resize last applied. UI goroutine only.
	paneW, paneH int

	// scrollOffset lifts the scrollback window off the live bottom; 0 is live. Unguarded:
	// Bubble Tea's Update and View share one goroutine.
	scrollOffset int

	mouse  mouseState
	paste  pasteState
	cursor cursorState

	// clipMu guards clipSink: installed from the UI goroutine, read by the output pump.
	clipSink  func(string)
	clipMu    sync.Mutex
	clipQueue chan string
	clipBusy  bool

	// cwdMu guards cwd: written by the output pump, read by the UI. osc is the pump's alone.
	cwd   string
	cwdMu sync.Mutex
	osc   oscScanner

	// firstOutput is closed once the first chunk of server output has been parsed.
	firstOutput chan struct{}

	onOutput func()

	// closed stops the shell-integration goroutine, which sleeps in seconds.
	closed    chan struct{}
	closeOnce sync.Once
}

// New creates an emulator sized w x h, binds it to sess, and starts the output and
// response pumps. The emulator's InputPipe()/Read() pair is the terminal->host response
// channel, not the parser feed: server output must go through Write().
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

	// Wired before the pumps start.
	emu.SetCallbacks(vt.Callbacks{
		EnableMode: func(mode ansi.Mode) {
			p.mouse.setMode(mode, true)
			p.paste.setMode(mode, true)
		},
		DisableMode: func(mode ansi.Mode) {
			p.mouse.setMode(mode, false)
			p.paste.setMode(mode, false)
		},
		// Cursor callbacks rather than modes: DECTCEM is only half of it and DECSCUSR is
		// not a mode at all.
		CursorVisibility: func(visible bool) {
			p.cursor.setVisible(visible)
		},
		CursorStyle: func(style vt.CursorStyle, steady bool) {
			p.cursor.setStyle(style, steady)
		},
		// Leaving the alt screen drops the modes of the program that owned it; a callback
		// rather than a post-chunk check, because the next program's modes are in that chunk.
		AltScreen: func(on bool) {
			if on {
				return
			}
			p.mouse.clear()
			p.paste.clear()
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
				_, _ = emu.Write(buf[:n])
				if dir, ok := p.osc.feed(buf[:n]); ok {
					p.setCwd(dir)
				}
				// A full reset drops every mode and the emulator does that without a
				// callback — the scan above is the only warning.
				if p.osc.tookReset() {
					p.mouse.clear()
					p.paste.clear()
					p.cursor.clear()
				}
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

	// Emulator auto-responses -> remote.
	go func() {
		_, _ = io.Copy(paneWriter{p}, emu)
	}()

	// The queue -> remote: the only goroutine allowed to block on the session. A failed
	// write does not end the drain, or a live pane's input would go nowhere.
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

// View returns the rendered screen as an ANSI string. Render() draws no cursor, so hop
// overlays the cursor cell itself.
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

// SendKey queues a Bubble Tea key event for the far end, reporting whether it was taken.
func (p *Pane) SendKey(msg tea.KeyPressMsg) bool {
	return p.send(keyToBytes(msg))
}

// SendKeys queues a run of key events as one queue item.
func (p *Pane) SendKeys(msgs []tea.KeyPressMsg) bool {
	var b []byte
	for _, msg := range msgs {
		b = append(b, keyToBytes(msg)...)
	}
	return p.send(b)
}

// send queues a copy of b for the far end without blocking, reporting whether it was
// taken; false means the queue is full and the input was dropped.
func (p *Pane) send(b []byte) bool {
	if len(b) == 0 || p.isClosed() {
		return true
	}
	select {
	case p.in <- inChunk{b: append([]byte(nil), b...)}:
		return true
	default:
		return false
	}
}

// Flush blocks until everything queued when it was called has reached the session, or
// the pane closes. Used by tests; the UI must never wait on the wire.
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

// AltScreen reports whether a full-screen program has taken the screen.
func (p *Pane) AltScreen() bool {
	return p.emu.IsAltScreen()
}

// Resize resizes both the emulator screen and the remote PTY, dropping no-op resizes
// because a full-screen program redraws itself on every window-change.
func (p *Pane) Resize(w, h int) {
	if w == p.paneW && h == p.paneH {
		return
	}
	p.paneW, p.paneH = w, h
	p.emu.Resize(w, h)
	_ = p.sess.Resize(w, h)
}

// Close tears down the emulator and the SSH session. It closes the emulator's input pipe
// rather than emu.Close(), which sets an unguarded flag the response pump races on.
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
func keyToBytes(msg tea.KeyPressMsg) []byte {
	// xterm puts the modifier inside the sequence (ESC[1;5D for ctrl+left), so a modified
	// cursor key is handled before the meta prefix below.
	if b, ok := modifiedKeyBytes(msg); ok {
		return b
	}

	b := keyBytes(msg)
	// alt+<key> is that key's bytes behind an ESC.
	if msg.Mod.Contains(tea.ModAlt) && len(b) > 0 && msg.Code != tea.KeyEscape {
		return append([]byte{0x1b}, b...)
	}
	return b
}

// cursorFinal and tildeParam are the keys xterm reports with the modifier inside the
// sequence: CSI 1 ; <mod> <final>, and CSI <n> ; <mod> ~ for the tilde keys.
var (
	cursorFinal = map[rune]byte{
		tea.KeyUp: 'A', tea.KeyDown: 'B', tea.KeyRight: 'C', tea.KeyLeft: 'D',
		tea.KeyHome: 'H', tea.KeyEnd: 'F',
	}
	tildeParam = map[rune]int{
		tea.KeyInsert: 2, tea.KeyDelete: 3, tea.KeyPgUp: 5, tea.KeyPgDown: 6,
	}
)

// modifiedKeyBytes builds that sequence. KeyMod's low bits are already xterm's modifier
// encoding (shift 1, alt 2, ctrl 4), so the parameter is a plain cast.
func modifiedKeyBytes(msg tea.KeyPressMsg) ([]byte, bool) {
	mods := int(msg.Mod & (tea.ModShift | tea.ModAlt | tea.ModCtrl | tea.ModMeta))
	// A bare alt keeps the ESC prefix instead: xterm would send CSI 1;3D, but every remote
	// program hop has ever fed reads the meta form, so the migration does not change it.
	if mods == 0 || msg.Mod&(tea.ModShift|tea.ModCtrl) == 0 {
		return nil, false
	}
	if final, ok := cursorFinal[msg.Code]; ok {
		return fmt.Appendf(nil, "\x1b[1;%d%c", mods+1, final), true
	}
	if tilde, ok := tildeParam[msg.Code]; ok {
		return fmt.Appendf(nil, "\x1b[%d;%d~", tilde, mods+1), true
	}
	return nil, false
}

// keyBytes is keyToBytes without the meta prefix: the bytes for the key itself.
func keyBytes(msg tea.KeyPressMsg) []byte {
	// Before the printable branches: ctrl+space is NUL, not a space. See ctrlByte.
	if msg.Mod.Contains(tea.ModCtrl) {
		if b, ok := ctrlByte(msg.Code); ok {
			return []byte{b}
		}
	}

	switch msg.Code {
	case tea.KeyEnter:
		return []byte("\r")
	case tea.KeyTab:
		// CSI Z (back-tab) is shift+tab's own sequence, not a modified tab.
		if msg.Mod.Contains(tea.ModShift) {
			return []byte("\x1b[Z")
		}
		return []byte("\t")
	case tea.KeyBackspace:
		return []byte("\x7f")
	case tea.KeyEscape:
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
	case tea.KeyInsert:
		return []byte("\x1b[2~")
	}

	// Text is what the key actually typed, which is what a composed character carries.
	if msg.Text != "" {
		return []byte(msg.Text)
	}
	if msg.Code > 0 && msg.Code <= unicode.MaxRune {
		return []byte(string(msg.Code))
	}
	return nil
}

// ctrlByte is the control byte a ctrl chord sends: the key with its top three bits
// cleared. ctrl+space and ctrl+@ are both NUL, which is the same arithmetic.
func ctrlByte(code rune) (byte, bool) {
	switch {
	case code >= 'a' && code <= 'z':
		return byte(code) - 'a' + 1, true
	case code >= 'A' && code <= 'Z':
		return byte(code) - 'A' + 1, true
	case code == ' ' || code == '@' || code == '[' || code == '\\' ||
		code == ']' || code == '^' || code == '_':
		return byte(code) & 0x1f, true
	}
	return 0, false
}

// clampOffset pulls scrollOffset back into [0, ScrollbackLen()] and returns it; the
// ceiling rises as the output pump pushes more lines into scrollback.
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

// ScrollUp lifts the window n lines toward older output.
func (p *Pane) ScrollUp(n int) {
	if n <= 0 {
		return
	}
	p.scrollOffset += n
	p.clampOffset()
}

// ScrollDown lowers the window n lines back toward the live bottom.
func (p *Pane) ScrollDown(n int) {
	if n <= 0 {
		return
	}
	p.scrollOffset -= n
	p.clampOffset()
}

// ScrollToTop lifts the window as far as history allows.
func (p *Pane) ScrollToTop() {
	p.scrollOffset = p.emu.ScrollbackLen()
	p.clampOffset()
}

// ScrollToBottom drops the window back to live.
func (p *Pane) ScrollToBottom() {
	p.scrollOffset = 0
}

// ScrollOffset reports how far the window is lifted off the live bottom; 0 is live.
func (p *Pane) ScrollOffset() int {
	return p.scrollOffset
}

// ScrollbackLen reports how many lines are buffered above the live screen; 0 on the alt screen.
func (p *Pane) ScrollbackLen() int {
	return p.emu.ScrollbackLen()
}

// AtBottom reports whether the window is live.
func (p *Pane) AtBottom() bool {
	return p.scrollOffset == 0
}

// ViewScrollback renders the windowed view at the current scroll offset, cursorless and
// exactly emu.Height() lines tall.
func (p *Pane) ViewScrollback() string {
	return p.ViewRows(0, p.emu.Height()-1)
}

// ViewRows renders rows [from, to] inclusive, numbered from the top of what
// ViewScrollback shows; negative rows reach into scrollback and out-of-buffer rows
// render blank so the numbering stays aligned.
func (p *Pane) ViewRows(from, to int) string {
	if to < from {
		return ""
	}
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
	lines := make([]string, 0, to-from+1)
	for y := from; y <= to; y++ {
		vi := top + y
		var line string
		switch {
		case vi < 0:
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
