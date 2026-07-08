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
	p := &Pane{emu: emu, sess: sess}

	// Remote/server output -> emulator parser: update the rendered screen. We
	// read in a loop (instead of io.Copy) so we can notify the UI right after
	// each chunk is parsed — event-driven repaints instead of polling.
	go func() {
		buf := make([]byte, 32*1024)
		for {
			n, err := sess.Stdout.Read(buf)
			if n > 0 {
				_, _ = emu.Write(buf[:n])
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

// SendString writes s to the remote shell verbatim, as if it had been typed.
// The caller supplies any trailing newline.
func (p *Pane) SendString(s string) {
	if s == "" {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	_, _ = p.sess.Stdin.Write([]byte(s))
}

// Resize resizes both the emulator screen and the remote PTY.
func (p *Pane) Resize(w, h int) {
	p.emu.Resize(w, h)
	_ = p.sess.Resize(w, h)
}

// Close tears down the emulator and the underlying SSH session. The io.Copy
// pumps started in New unblock once these streams close.
func (p *Pane) Close() error {
	emuErr := p.emu.Close()
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
