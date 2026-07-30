package terminal

import (
	"sync"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

// A remote program that wants the mouse says so, by setting one of the DEC
// private modes below; until it does, a terminal reports nothing and the wheel
// belongs to whatever is drawing the screen. hop honours the same contract: the
// TUI asks MouseEnabled before forwarding anything, so the wheel over an ordinary
// shell drives hop's scrollback (nothing asked for it) while the wheel over vim
// with `set mouse=a` reaches vim.
//
// The modes are watched through the emulator's mode callbacks rather than the
// stream, because that is where the emulator has already done the parsing — and
// vt's own isModeSet is unexported, so its SendMouse cannot be asked "would this
// have gone anywhere?". hop encodes the event itself (see mouseBytes) and writes
// it to the session under the same mutex SendKey uses, which also keeps it off
// the emulator's unsynchronised mode map.
type tracking int

const (
	// trackNone is a program that has not asked for the mouse.
	trackNone tracking = iota
	// trackPress is DECSET 9 (X10): button presses only, no releases, no motion.
	trackPress
	// trackRelease is DECSET 1000/1001: presses and releases.
	trackRelease
	// trackDrag is DECSET 1002: presses, releases and motion while a button is held.
	trackDrag
	// trackAll is DECSET 1003: the above plus motion with no button down.
	trackAll
)

// trackingModes maps each mouse-reporting mode to the level of reporting it asks
// for. The order they are checked in is the order of that level, which is how a
// program that sets several of them (or leaves an old one set) gets the most
// specific one it asked for — the same resolution vt's own SendMouse makes.
var trackingModes = []struct {
	mode  ansi.DECMode
	level tracking
}{
	{ansi.ModeMouseX10, trackPress},         // ?9
	{ansi.ModeMouseNormal, trackRelease},    // ?1000
	{ansi.ModeMouseHighlight, trackRelease}, // ?1001
	{ansi.ModeMouseButtonEvent, trackDrag},  // ?1002
	{ansi.ModeMouseAnyEvent, trackAll},      // ?1003
}

// mouseState is the mouse reporting the far end has asked for. It is written by
// the output pump (the emulator's mode callbacks run inside Write) and read by the
// UI goroutine, so it carries a mutex of its own — unlike scrollOffset, which only
// the UI touches.
type mouseState struct {
	mu  sync.Mutex
	set map[ansi.DECMode]bool
	// sgr is DECSET 1006, the extended encoding every modern program asks for
	// alongside its tracking mode. Without it the report falls back to X10's
	// byte-per-coordinate form, which cannot address a column past 223.
	sgr bool
}

// setMode records a mode the remote program has enabled or disabled. Modes hop
// does not care about are ignored, which is most of them.
func (s *mouseState) setMode(mode ansi.Mode, on bool) {
	dec, ok := mode.(ansi.DECMode)
	if !ok {
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if dec == ansi.ModeMouseExtSgr {
		s.sgr = on
		return
	}
	for _, tm := range trackingModes {
		if tm.mode != dec {
			continue
		}
		if s.set == nil {
			s.set = make(map[ansi.DECMode]bool, len(trackingModes))
		}
		s.set[dec] = on
		return
	}
}

// clear forgets every mode, which is what a full terminal reset does to them. It is
// called from the output pump, on the RIS the mode callbacks do not report.
func (s *mouseState) clear() {
	s.mu.Lock()
	defer s.mu.Unlock()
	clear(s.set)
	s.sgr = false
}

// state reports the reporting level in force and whether SGR encoding is on.
func (s *mouseState) state() (tracking, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	level := trackNone
	for _, tm := range trackingModes {
		if s.set[tm.mode] {
			level = tm.level
		}
	}
	return level, s.sgr
}

// MouseEnabled reports whether the program on the far end has asked for mouse
// reporting. It is what the TUI checks before forwarding an event: with nothing
// asking, the wheel is hop's to spend on its own scrollback.
func (p *Pane) MouseEnabled() bool {
	level, _ := p.mouse.state()
	return level != trackNone
}

// SendMouse forwards a mouse event to the remote program, addressed to the pane's
// own cell (x, y) — the caller has already subtracted the pane's border and any
// tab strip, so (0, 0) is the top-left cell of the emulated screen.
//
// It reports whether anything was sent. An event the far end did not ask for is
// dropped rather than encoded: a program in DECSET 1000 wants presses and
// releases and would be confused by the motion reports of 1002.
func (p *Pane) SendMouse(msg tea.MouseMsg, x, y int) bool {
	// A closed pane has no far end to report to, as every other write path here
	// checks before touching the session (see writeString).
	if p.isClosed() {
		return false
	}
	level, sgr := p.mouse.state()
	b := mouseBytes(msg, x, y, level, sgr)
	if len(b) == 0 {
		return false
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	_, _ = p.sess.Stdin.Write(b)
	return true
}

// mouseBytes encodes a mouse event as the report a terminal would send, or nil
// when the tracking level in force does not cover it. It is a pure function of
// the event so the encoding can be tested without a session behind it.
func mouseBytes(msg tea.MouseMsg, x, y int, level tracking, sgr bool) []byte {
	if level == trackNone || x < 0 || y < 0 {
		return nil
	}

	button, ok := ansiButton(msg.Button)
	if !ok {
		return nil
	}

	motion := msg.Action == tea.MouseActionMotion
	release := msg.Action == tea.MouseActionRelease

	switch {
	case isWheel(msg.Button):
		// The wheel is reported by every tracking level: it has no release, and a
		// program that asked for the mouse at all asked for the wheel.
	case motion && button == ansi.MouseNone:
		// Motion with no button down is 1003's alone.
		if level < trackAll {
			return nil
		}
	case motion:
		if level < trackDrag {
			return nil
		}
	case release:
		if level < trackRelease {
			return nil
		}
	}

	if sgr {
		return []byte(ansi.MouseSgr(
			ansi.EncodeMouseButton(button, motion, msg.Shift, msg.Alt, msg.Ctrl),
			x, y, release,
		))
	}

	// X10 encoding carries each coordinate in one byte offset by 33, so a cell past
	// column or row x10Max cannot be addressed at all. Dropping the report is what
	// xterm does; the alternative is to name a different cell than the one clicked,
	// which is worse than saying nothing — the program would act on a cell nobody
	// pointed at.
	if x > x10Max || y > x10Max {
		return nil
	}
	if release {
		// X10 has no button field on a release: it is reported as button 3, which is
		// what EncodeMouseButton returns for "no button".
		button = ansi.MouseNone
	}
	b := ansi.EncodeMouseButton(button, motion, msg.Shift, msg.Alt, msg.Ctrl)
	// The button field carries the modifier bits, so a wheel event with all of them
	// set overflows past the last byte a report may carry, just as a far-right column
	// does — and is dropped for the same reason. See x10Max.
	if int(b)+x10Offset > x10Last {
		return nil
	}
	// The three bytes are written here rather than by ansi.MouseX10, which builds them
	// with string(byte(x)+33) — a rune conversion, so every coordinate from 95 up is
	// UTF-8 encoded into *two* bytes and the report arrives malformed. A real terminal
	// sends raw bytes, which is the whole reason this encoding stops at 222.
	return []byte{0x1b, '[', 'M', b + x10Offset, byte(x) + x10Offset + 1, byte(y) + x10Offset + 1}
}

// x10Offset is the bias every field of an X10 mouse report carries, so that no byte
// of it can be a control character; x10Last is the last byte one may therefore
// hold, and x10Max the last cell one can name.
//
// The ceiling is xterm's: 0xff, so the encoding runs to column 222. It used to stop
// at 0x7e, refusing to write a byte with its top bit set — the reasoning being that
// the far end of an SSH session is a *UTF-8* pty, so a program that is not decoding
// the report (a shell left in mouse mode by something that exited without switching
// it off) takes the trailing bytes as input, and a raw 0x9f is not a character
// there. That trade was the wrong way round. The cost fell on every program that
// asks for the mouse *without* SGR — older vim, mc, plenty of ncurses — which lost
// every click past column 94, reachable on any wide pane with the sidebar hidden;
// the benefit was that junk typed onto a command line by a stale mouse mode is
// decodable junk rather than undecodable junk. A program that *is* decoding reads
// raw bytes and wants exactly what xterm sends, and hop's own terminal would have
// sent the same byte in the same situation.
//
// The stale mode itself is what is worth preventing, and that is handled where it
// happens: the modes go with the alt screen they were set on, and with a RIS. See
// terminal.go.
const (
	x10Offset = 32
	x10Last   = 0xff
	x10Max    = x10Last - x10Offset - 1
)

// isWheel reports whether b is one of the four wheel directions. Bubble Tea has
// this as a method on MouseEvent, which MouseMsg — a distinct type over it — does
// not inherit.
func isWheel(b tea.MouseButton) bool {
	return b == tea.MouseButtonWheelUp || b == tea.MouseButtonWheelDown ||
		b == tea.MouseButtonWheelLeft || b == tea.MouseButtonWheelRight
}

// ansiButton translates a Bubble Tea mouse button into the ansi package's. The
// two enumerations happen to agree, but they are separate types from separate
// packages, so the mapping is written out rather than cast — a reordering in
// either would otherwise silently start reporting the wrong button.
func ansiButton(b tea.MouseButton) (ansi.MouseButton, bool) {
	switch b {
	case tea.MouseButtonNone:
		return ansi.MouseNone, true
	case tea.MouseButtonLeft:
		return ansi.MouseLeft, true
	case tea.MouseButtonMiddle:
		return ansi.MouseMiddle, true
	case tea.MouseButtonRight:
		return ansi.MouseRight, true
	case tea.MouseButtonWheelUp:
		return ansi.MouseWheelUp, true
	case tea.MouseButtonWheelDown:
		return ansi.MouseWheelDown, true
	case tea.MouseButtonWheelLeft:
		return ansi.MouseWheelLeft, true
	case tea.MouseButtonWheelRight:
		return ansi.MouseWheelRight, true
	case tea.MouseButtonBackward:
		return ansi.MouseBackward, true
	case tea.MouseButtonForward:
		return ansi.MouseForward, true
	case tea.MouseButton10:
		return ansi.MouseButton10, true
	case tea.MouseButton11:
		return ansi.MouseButton11, true
	}
	return ansi.MouseNone, false
}
