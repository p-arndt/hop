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

// reset forgets what has been written, so a test can look at one write at a time.
func (s *syncBuf) reset() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.b = s.b[:0]
}

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
	// Every modifier on a wheel event: the one shape whose button field alone runs
	// past what an ASCII byte holds.
	wheelAllMods := tea.MouseMsg{
		Button: tea.MouseButtonWheelDown, Action: tea.MouseActionMotion,
		Shift: true, Alt: true, Ctrl: true,
	}

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
		{"x10 at the last ASCII column", press(0, 0), 93, 0, trackPress, false, "\x1b[M\x20\x7e\x21"},
		{"x10 the same for a row", press(0, 0), 0, 93, trackPress, false, "\x1b[M\x20\x21\x7e"},
		// Past that the coordinate byte has its top bit set, which is what xterm sends
		// and what a program decoding the report reads — raw bytes, not text. A wide
		// pane (the sidebar hidden) reaches these columns, and a program that asked for
		// the mouse without SGR has no other way to be told about them.
		{"x10 past the top bit, column", press(0, 0), 94, 0, trackPress, false, "\x1b[M\x20\x7f\x21"},
		{"x10 past the top bit, row", press(0, 0), 0, 200, trackPress, false, "\x1b[M\x20\x21\xe9"},
		{"x10 at the last cell it can name", press(0, 0), 222, 0, trackPress, false, "\x1b[M\x20\xff\x21"},
		// And past *that* the byte would wrap onto a different cell, so nothing is sent:
		// naming the wrong cell is worse than saying nothing.
		{"x10 stops where the byte runs out", press(0, 0), 223, 0, trackPress, false, ""},
		// The modifier bits live in the button field, so a wheel with all of them set
		// is a high byte too — and the same answer: it is what a decoder expects.
		{"x10 carries the modifier bits in the button field", wheelAllMods, 0, 0, trackPress, false, "\x1b[M\x9d\x21\x21"},
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

// A full-screen program that leaves the alt screen without releasing the mouse
// takes its ask with it anyway.
//
// This is the shape of a real defect rather than a hypothetical: the modes belong
// to the program, and a program that was killed — or that restored the screen and
// nothing else — never sends the "l" that switches them off. The shell underneath
// is then left "asking" for a mouse it knows nothing about, and every drag over it
// is encoded and typed into it as input.
func TestLeavingTheAltScreenDropsTheMouse(t *testing.T) {
	out, w := io.Pipe()
	p := New(&sshx.Session{Stdin: &syncBuf{}, Stdout: out}, 80, 24, nil)
	defer p.Close()

	// A full-screen program: alt screen, then the mouse and bracketed paste.
	go io.WriteString(w, "\x1b[?1049h\x1b[?1002h\x1b[?1006h\x1b[?2004h")
	if !waitFor(func() bool { return p.MouseEnabled() && p.BracketedPaste() }) {
		t.Fatal("the program's asks were not noticed")
	}

	// ...and it goes away without a word about either of them.
	go io.WriteString(w, "\x1b[?1049l")
	if !waitFor(func() bool { return !p.MouseEnabled() }) {
		t.Fatal("the shell under a program that never released the mouse is still reported to")
	}
	if p.BracketedPaste() {
		t.Fatal("bracketed paste outlived the program that asked for it")
	}
	if p.SendMouse(press(3, 4), 3, 4) {
		t.Fatal("an event was forwarded into a shell that never asked for one")
	}
}

// An inline program keeps what it asked for: leaving the alt screen is the tell,
// and a program that never took it has not left anything.
func TestInlineMouseSurvives(t *testing.T) {
	out, w := io.Pipe()
	p := New(&sshx.Session{Stdin: &syncBuf{}, Stdout: out}, 80, 24, nil)
	defer p.Close()

	go io.WriteString(w, "\x1b[?1002h\x1b[?1006hfzf\r\n")
	if !waitFor(func() bool { return p.MouseEnabled() }) {
		t.Fatal("an inline program asking for the mouse was not noticed")
	}
	if !waitFor(func() bool { return !p.emu.IsAltScreen() }) {
		t.Fatal("an inline program was taken to be on the alt screen")
	}
	if !p.MouseEnabled() {
		t.Fatal("an inline program's mouse was dropped without it leaving anything")
	}
}

// The exit and what comes after it arrive together: over SSH, vim's teardown and
// the shell's next prompt are one read, and readline announces bracketed paste
// before every line it reads. The asks that follow the exit are the *shell's*, and
// they must survive it — dropping the modes after the whole chunk was parsed
// discarded them, and hop then pasted unbracketed into a shell that runs each line
// of what it is given.
func TestAltScreenExitKeepsWhatFollowsInTheSameChunk(t *testing.T) {
	out, w := io.Pipe()
	p := New(&sshx.Session{Stdin: &syncBuf{}, Stdout: out}, 80, 24, nil)
	defer p.Close()

	go io.WriteString(w, "\x1b[?1049h\x1b[?1002h\x1b[?1006h\x1b[?2004h")
	if !waitFor(func() bool { return p.MouseEnabled() && p.BracketedPaste() }) {
		t.Fatal("the program's asks were not noticed")
	}

	// One write: the program's exit, then the prompt readline draws under it.
	go io.WriteString(w, "\x1b[?1049l\x1b[?2004h$ ")
	if !waitFor(func() bool { return !p.MouseEnabled() }) {
		t.Fatal("the mouse outlived the program that asked for it")
	}
	if !p.BracketedPaste() {
		t.Fatal("the shell's bracketed paste was dropped with the program that exited above it")
	}
}
