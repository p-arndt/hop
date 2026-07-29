package terminal

import (
	"io"
	"sync"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"hop/internal/sshx"
)

// syncBuf is a writer the test can read while the pane writes to it: SendMouse runs
// on the UI goroutine and the emulator's response pump has the same stdin.
type syncBuf struct {
	mu sync.Mutex
	b  []byte
}

func (s *syncBuf) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.b = append(s.b, p...)
	return len(p), nil
}

func (s *syncBuf) Close() error { return nil }

func (s *syncBuf) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return string(s.b)
}

// press builds the mouse event a left click at (x, y) arrives as.
func press(x, y int) tea.MouseMsg {
	return tea.MouseMsg{X: x, Y: y, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress}
}

// A shell that has not asked for the mouse gets nothing: the wheel over it is
// hop's, to spend on its own scrollback. A program that asks — the DECSET pair vim
// and htop send — is reported to, and stops being reported to when it leaves.
func TestMouseEnabledFollowsTheRemote(t *testing.T) {
	out, w := io.Pipe()
	stdin := &syncBuf{}
	p := New(&sshx.Session{Stdin: stdin, Stdout: out}, 80, 24, nil)
	defer p.Close()

	if p.MouseEnabled() {
		t.Fatal("a fresh shell reports the mouse as wanted")
	}
	if p.SendMouse(press(3, 4), 3, 4) {
		t.Fatal("an event was forwarded to a program that never asked for one")
	}

	// What vim's `set mouse=a` sends: button tracking plus the SGR encoding.
	go io.WriteString(w, "\x1b[?1002h\x1b[?1006h")
	if !waitFor(func() bool { return p.MouseEnabled() }) {
		t.Fatal("a program asking for the mouse was not noticed")
	}

	if !p.SendMouse(press(3, 4), 3, 4) {
		t.Fatal("a click was not forwarded to a program that asked for the mouse")
	}
	// SGR: button 0 (left, no modifiers) at 1-based column 4, row 5.
	if got, want := stdin.String(), "\x1b[<0;4;5M"; got != want {
		t.Fatalf("forwarded %q, want %q", got, want)
	}

	go io.WriteString(w, "\x1b[?1002l")
	if !waitFor(func() bool { return !p.MouseEnabled() }) {
		t.Fatal("a program releasing the mouse left hop still forwarding to it")
	}
}

// The encoding, and the levels that decide whether an event is reported at all. A
// program in DECSET 1000 asked for presses and releases; the motion reports of 1002
// would be noise to it, so they are dropped rather than sent.
func TestMouseBytes(t *testing.T) {
	wheel := tea.MouseMsg{Button: tea.MouseButtonWheelUp, Action: tea.MouseActionPress}
	release := tea.MouseMsg{Button: tea.MouseButtonLeft, Action: tea.MouseActionRelease}
	drag := tea.MouseMsg{Button: tea.MouseButtonLeft, Action: tea.MouseActionMotion}
	hover := tea.MouseMsg{Button: tea.MouseButtonNone, Action: tea.MouseActionMotion}
	ctrlClick := tea.MouseMsg{Button: tea.MouseButtonLeft, Action: tea.MouseActionPress, Ctrl: true}

	cases := []struct {
		name  string
		msg   tea.MouseMsg
		x, y  int
		level tracking
		sgr   bool
		want  string
	}{
		{"nothing asked", press(0, 0), 0, 0, trackNone, true, ""},
		{"sgr press", press(0, 0), 0, 0, trackRelease, true, "\x1b[<0;1;1M"},
		{"sgr release", release, 9, 2, trackRelease, true, "\x1b[<0;10;3m"},
		{"sgr ctrl press", ctrlClick, 0, 0, trackRelease, true, "\x1b[<16;1;1M"},
		{"sgr wheel", wheel, 5, 5, trackPress, true, "\x1b[<64;6;6M"},
		{"sgr drag", drag, 1, 1, trackDrag, true, "\x1b[<32;2;2M"},
		{"drag needs 1002", drag, 1, 1, trackRelease, true, ""},
		{"release needs 1000", release, 1, 1, trackPress, true, ""},
		{"hover needs 1003", hover, 1, 1, trackDrag, true, ""},
		{"hover under 1003", hover, 1, 1, trackAll, true, "\x1b[<35;2;2M"},
		{"x10 press", press(0, 0), 0, 0, trackPress, false, "\x1b[M\x20\x21\x21"},
		{"x10 release is button 3", release, 0, 0, trackRelease, false, "\x1b[M\x23\x21\x21"},
		{"x10 at the last byte-addressable column", press(0, 0), 94, 0, trackPress, false, "\x1b[M\x20\x7f\x21"},
		{"x10 past it, where a rune conversion would corrupt the report", press(0, 0), 95, 0, trackPress, false, "\x1b[M\x20\x80\x21"},
		{"x10 at its ceiling", press(0, 0), 222, 0, trackPress, false, "\x1b[M\x20\xff\x21"},
		{"x10 cannot address past 222", press(0, 0), 223, 0, trackPress, false, ""},
		{"sgr can", press(0, 0), 300, 0, trackPress, true, "\x1b[<0;301;1M"},
		{"negative cells are not events", press(0, 0), -1, 0, trackRelease, true, ""},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := string(mouseBytes(c.msg, c.x, c.y, c.level, c.sgr))
			if got != c.want {
				t.Fatalf("mouseBytes = %q, want %q", got, c.want)
			}
		})
	}
}

// The reporting level is the most specific mode the program has set, and it holds
// while any of them do: a program that sets 1000 and 1002 wants motion, and turning
// 1002 back off leaves it with the 1000 it still asked for.
func TestTrackingLevel(t *testing.T) {
	var s mouseState

	if level, sgr := s.state(); level != trackNone || sgr {
		t.Fatalf("a fresh state = %v, %v; want trackNone, false", level, sgr)
	}

	s.setMode(ansi.ModeMouseNormal, true)
	s.setMode(ansi.ModeMouseButtonEvent, true)
	if level, _ := s.state(); level != trackDrag {
		t.Fatalf("with 1000 and 1002 set, level = %v; want trackDrag", level)
	}

	s.setMode(ansi.ModeMouseButtonEvent, false)
	if level, _ := s.state(); level != trackRelease {
		t.Fatalf("after 1002 was reset, level = %v; want the trackRelease of 1000", level)
	}

	s.setMode(ansi.ModeMouseNormal, false)
	if level, _ := s.state(); level != trackNone {
		t.Fatalf("with every mode reset, level = %v; want trackNone", level)
	}
}

// A full terminal reset (RIS — what `reset` and `tput reset` send, and what some
// programs emit on the way out) clears every mode, mouse reporting included. The
// emulator does that without firing its mode callbacks, so the reset is noticed in
// the byte stream instead. Missing it would leave hop forwarding the wheel into a
// shell that stopped asking, and its own scrollback wheel dead for the tab's life.
func TestFullResetForgetsTheMouse(t *testing.T) {
	out, w := io.Pipe()
	p := New(&sshx.Session{Stdin: &syncBuf{}, Stdout: out}, 80, 24, nil)
	defer p.Close()

	go io.WriteString(w, "\x1b[?1002h\x1b[?1006h")
	if !waitFor(func() bool { return p.MouseEnabled() }) {
		t.Fatal("a program asking for the mouse was not noticed")
	}

	go io.WriteString(w, "\x1bc")
	if !waitFor(func() bool { return !p.MouseEnabled() }) {
		t.Fatal("a full reset left hop still forwarding the mouse to the shell")
	}

	// And the modes are forgotten rather than merely masked: a program asking again
	// gets the mouse, with the encoding it asks for this time.
	go io.WriteString(w, "\x1b[?1000h")
	if !waitFor(func() bool { return p.MouseEnabled() }) {
		t.Fatal("a program asking again after a reset was not noticed")
	}
	if level, sgr := p.mouse.state(); level != trackRelease || sgr {
		t.Fatalf("state = %v, %v; want the trackRelease of ?1000 with SGR forgotten", level, sgr)
	}
}

// The reset is only a reset where it is one: an ESC c that goes past inside an OSC
// payload — a window title, say — is that payload's data, not a reset of the
// terminal, and must not drop what a program has asked for.
func TestResetInsideAnOSCIsNotAReset(t *testing.T) {
	var s oscScanner

	s.feed([]byte("\x1b]0;a title \x1bc still the title\x07"))
	if s.tookReset() {
		t.Fatal("an ESC c inside an OSC payload was taken for a full reset")
	}

	s.feed([]byte("\x1bc"))
	if !s.tookReset() {
		t.Fatal("a real RIS was missed")
	}
	if s.tookReset() {
		t.Fatal("the reset was reported twice; tookReset must clear it")
	}
}

// A closed pane has no far end to report to, and every other write path here checks
// before touching the session.
func TestSendMouseOnAClosedPane(t *testing.T) {
	out, w := io.Pipe()
	p := New(&sshx.Session{Stdin: &syncBuf{}, Stdout: out}, 80, 24, nil)

	go io.WriteString(w, "\x1b[?1002h\x1b[?1006h")
	if !waitFor(func() bool { return p.MouseEnabled() }) {
		t.Fatal("a program asking for the mouse was not noticed")
	}

	p.Close()
	if p.SendMouse(press(1, 1), 1, 1) {
		t.Fatal("an event was forwarded to a closed pane")
	}
}
