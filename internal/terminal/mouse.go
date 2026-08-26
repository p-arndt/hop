package terminal

import (
	"sync"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

// tracking: hop encodes mouse reports itself because vt's isModeSet is unexported.
type tracking int

const (
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

// trackingModes is ordered by level, so a program setting several gets the most specific.
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

// mouseState is written by the output pump and read by the UI goroutine, hence the mutex.
type mouseState struct {
	mu  sync.Mutex
	set map[ansi.DECMode]bool
	// sgr is DECSET 1006; without it reports fall back to X10's byte-per-coordinate form.
	sgr bool
}

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

// clear forgets every mode; called from the output pump on the RIS the mode callbacks do
// not report.
func (s *mouseState) clear() {
	s.mu.Lock()
	defer s.mu.Unlock()
	clear(s.set)
	s.sgr = false
}

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

func (p *Pane) MouseEnabled() bool {
	level, _ := p.mouse.state()
	return level != trackNone
}

// MouseEvent is a report on its way to the remote program: the fields the encoder needs,
// decoupled from Bubble Tea's four mouse message types.
type MouseEvent struct {
	Button  tea.MouseButton
	Mod     tea.KeyMod
	Motion  bool
	Release bool
}

func (p *Pane) SendMouse(ev MouseEvent, x, y int) bool {
	if p.isClosed() {
		return false
	}
	level, sgr := p.mouse.state()
	b := mouseBytes(ev, x, y, level, sgr)
	if len(b) == 0 {
		return false
	}
	p.send(b)
	return true
}

// mouseBytes encodes a mouse report, or nil when the tracking level does not cover it.
func mouseBytes(ev MouseEvent, x, y int, level tracking, sgr bool) []byte {
	if level == trackNone || x < 0 || y < 0 {
		return nil
	}

	button, ok := ansiButton(ev.Button)
	if !ok {
		return nil
	}

	motion, release := ev.Motion, ev.Release
	shift := ev.Mod.Contains(tea.ModShift)
	alt := ev.Mod.Contains(tea.ModAlt)
	ctrl := ev.Mod.Contains(tea.ModCtrl)

	switch {
	case isWheel(ev.Button):
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
			ansi.EncodeMouseButton(button, motion, shift, alt, ctrl),
			x, y, release,
		))
	}

	// X10 carries each coordinate in one byte offset by 33, so a cell past x10Max cannot be
	// addressed; xterm drops the report rather than naming the wrong cell.
	if x > x10Max || y > x10Max {
		return nil
	}
	if release {
		// X10 has no button field on a release: it is reported as button 3.
		button = ansi.MouseNone
	}
	b := ansi.EncodeMouseButton(button, motion, shift, alt, ctrl)
	// The button field carries the modifier bits, so a wheel event with all of them set
	// overflows the last byte a report may carry.
	if int(b)+x10Offset > x10Last {
		return nil
	}
	// Written here rather than by ansi.MouseX10, which builds bytes with string(byte(x)+33)
	// — a rune conversion, so coordinates from 95 up are UTF-8 encoded into two bytes.
	return []byte{0x1b, '[', 'M', b + x10Offset, byte(x) + x10Offset + 1, byte(y) + x10Offset + 1}
}

// x10Offset is the bias every field of an X10 report carries so no byte is a control
// character; x10Last is xterm's ceiling (0xff) and x10Max the last cell nameable.
const (
	x10Offset = 32
	x10Last   = 0xff
	x10Max    = x10Last - x10Offset - 1
)

// isWheel: the encoder needs the check before it knows the tracking level.
func isWheel(b tea.MouseButton) bool {
	return b == tea.MouseWheelUp || b == tea.MouseWheelDown ||
		b == tea.MouseWheelLeft || b == tea.MouseWheelRight
}

// ansiButton translates a Bubble Tea mouse button into the ansi package's. Written out
// rather than cast: a reordering in either would silently report the wrong button.
func ansiButton(b tea.MouseButton) (ansi.MouseButton, bool) {
	switch b {
	case tea.MouseNone:
		return ansi.MouseNone, true
	case tea.MouseLeft:
		return ansi.MouseLeft, true
	case tea.MouseMiddle:
		return ansi.MouseMiddle, true
	case tea.MouseRight:
		return ansi.MouseRight, true
	case tea.MouseWheelUp:
		return ansi.MouseWheelUp, true
	case tea.MouseWheelDown:
		return ansi.MouseWheelDown, true
	case tea.MouseWheelLeft:
		return ansi.MouseWheelLeft, true
	case tea.MouseWheelRight:
		return ansi.MouseWheelRight, true
	case tea.MouseBackward:
		return ansi.MouseBackward, true
	case tea.MouseForward:
		return ansi.MouseForward, true
	case tea.MouseButton10:
		return ansi.MouseButton10, true
	case tea.MouseButton11:
		return ansi.MouseButton11, true
	}
	return ansi.MouseNone, false
}
