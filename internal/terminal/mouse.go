package terminal

import (
	"sync"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

// A remote program that wants the mouse says so by setting one of the DEC private modes
// below; until it does, the wheel belongs to whatever is drawing the screen. hop honours
// the same contract: the TUI asks MouseEnabled before forwarding anything.
//
// The modes are watched through the emulator's mode callbacks rather than the stream,
// since that is where the parsing already happened — and vt's isModeSet is unexported,
// so its SendMouse cannot be asked whether a report would go anywhere. hop encodes the
// event itself (see mouseBytes) and writes it under the mutex SendKey uses.
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

// trackingModes maps each mouse-reporting mode to the level it asks for. They are
// checked in order of that level, so a program setting several gets the most specific —
// the resolution vt's own SendMouse makes.
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

// mouseState is the mouse reporting the far end has asked for. Written by the output
// pump (the mode callbacks run inside Write) and read by the UI goroutine, hence the
// mutex.
type mouseState struct {
	mu  sync.Mutex
	set map[ansi.DECMode]bool
	// sgr is DECSET 1006, the extended encoding modern programs ask for alongside their
	// tracking mode. Without it the report falls back to X10's byte-per-coordinate form.
	sgr bool
}

// setMode records a mode the remote program has enabled or disabled, ignoring the ones
// hop does not care about.
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

// clear forgets every mode, as a full terminal reset does. Called from the output pump
// on the RIS the mode callbacks do not report.
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

// MouseEnabled reports whether the far end has asked for mouse reporting — what the TUI
// checks before forwarding an event. With nothing asking, the wheel is hop's.
func (p *Pane) MouseEnabled() bool {
	level, _ := p.mouse.state()
	return level != trackNone
}

// SendMouse forwards a mouse event to the remote program, addressed to the pane's own
// cell (x, y): the caller has already subtracted the border and any tab strip.
//
// It reports whether anything was sent. An event the far end did not ask for is dropped
// rather than encoded — a program in DECSET 1000 would be confused by 1002's motion.
func (p *Pane) SendMouse(msg tea.MouseMsg, x, y int) bool {
	// A closed pane has no far end to report to. See writeString.
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

// mouseBytes encodes a mouse event as the report a terminal would send, or nil when the
// tracking level in force does not cover it. Pure, so it is testable without a session.
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
		// The wheel is reported by every tracking level.
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

	// X10 carries each coordinate in one byte offset by 33, so a cell past x10Max cannot
	// be addressed. Dropping the report is what xterm does; the alternative names a cell
	// nobody pointed at.
	if x > x10Max || y > x10Max {
		return nil
	}
	if release {
		// X10 has no button field on a release: it is reported as button 3.
		button = ansi.MouseNone
	}
	b := ansi.EncodeMouseButton(button, motion, msg.Shift, msg.Alt, msg.Ctrl)
	// The button field carries the modifier bits, so a wheel event with all of them set
	// overflows the last byte a report may carry. See x10Max.
	if int(b)+x10Offset > x10Last {
		return nil
	}
	// Written here rather than by ansi.MouseX10, which builds them with string(byte(x)+33)
	// — a rune conversion, so every coordinate from 95 up is UTF-8 encoded into two bytes
	// and the report arrives malformed.
	return []byte{0x1b, '[', 'M', b + x10Offset, byte(x) + x10Offset + 1, byte(y) + x10Offset + 1}
}

// x10Offset is the bias every field of an X10 mouse report carries, so no byte of it can
// be a control character; x10Last is the last byte one may hold, and x10Max the last cell
// one can name.
//
// The ceiling is xterm's 0xff, so the encoding runs to column 222. Stopping at 0x7e —
// refusing a byte with its top bit set, so that junk from a stale mouse mode stays
// decodable on a UTF-8 pty — cost every non-SGR program (older vim, mc, ncurses) its
// clicks past column 94. A program that is decoding wants exactly what xterm sends.
//
// The stale mode itself is handled where it happens: the modes go with the alt screen
// they were set on, and with a RIS. See terminal.go.
const (
	x10Offset = 32
	x10Last   = 0xff
	x10Max    = x10Last - x10Offset - 1
)

// isWheel reports whether b is one of the four wheel directions. Bubble Tea has this as
// a method on MouseEvent, which MouseMsg does not inherit.
func isWheel(b tea.MouseButton) bool {
	return b == tea.MouseButtonWheelUp || b == tea.MouseButtonWheelDown ||
		b == tea.MouseButtonWheelLeft || b == tea.MouseButtonWheelRight
}

// ansiButton translates a Bubble Tea mouse button into the ansi package's. The two
// enumerations agree, but the mapping is written out rather than cast: a reordering in
// either would otherwise silently report the wrong button.
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
