package terminal

import (
	"io"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/vt"

	"hop/internal/sshx"
)

func TestReverseAtColumn(t *testing.T) {
	const on, off = "\x1b[7m", "\x1b[27m"
	cases := []struct {
		name string
		line string
		col  int
		want string
	}{
		{"middle", "hello", 1, "h" + on + "e" + off + "llo"},
		{"first", "hello", 0, on + "h" + off + "ello"},
		{"skip-ansi-prefix", "\x1b[31mhello", 0, "\x1b[31m" + on + "h" + off + "ello"},
		{"skip-ansi-midline", "a\x1b[1mbc", 2, "a\x1b[1mb" + on + "c" + off},
		{"past-end", "ab", 4, "ab  " + on + " " + off},
	}
	for _, c := range cases {
		if got := reverseAtColumn(c.line, c.col); got != c.want {
			t.Errorf("%s: reverseAtColumn(%q,%d)=%q, want %q", c.name, c.line, c.col, got, c.want)
		}
	}
}

func TestOverlayCursorPicksRow(t *testing.T) {
	const on, off = "\x1b[7m", "\x1b[27m"
	got := overlayCursor("row0\nrow1\nrow2", 1, 1, blockMark)
	want := "row0\nr" + on + "o" + off + "w1\nrow2"
	if got != want {
		t.Errorf("overlayCursor row select = %q, want %q", got, want)
	}
	// Out-of-range row leaves the input unchanged.
	if got := overlayCursor("only", 0, 5, blockMark); got != "only" {
		t.Errorf("overlayCursor out-of-range = %q, want unchanged", got)
	}
}

// An alt-modified key forwarded to the remote is that key behind an ESC — the way a
// terminal sends it, and the way readline (alt+b, alt+f) and vim (<esc>o) read it
// back. hop reserves a few alt chords of its own, but the ones it does not take have
// to arrive intact.
func TestKeyToBytesPrefixesAltWithEsc(t *testing.T) {
	cases := []struct {
		name string
		msg  tea.KeyMsg
		want string
	}{
		{"alt+o", tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'o'}, Alt: true}, "\x1bo"},
		{"alt+b", tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'b'}, Alt: true}, "\x1bb"},
		{"alt+left", tea.KeyMsg{Type: tea.KeyLeft, Alt: true}, "\x1b\x1b[D"},
		{"plain o is untouched", tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'o'}}, "o"},
		{"esc stays one esc", tea.KeyMsg{Type: tea.KeyEsc, Alt: true}, "\x1b"},
	}
	for _, c := range cases {
		if got := string(keyToBytes(c.msg)); got != c.want {
			t.Errorf("%s: keyToBytes = %q, want %q", c.name, got, c.want)
		}
	}
}

// A ctrl- or shift-modified cursor key carries its modifier inside the sequence
// (ESC[1;5D), not behind an ESC. These used to fall through keyToBytes as nil —
// ctrl+left/right, the word-wise motion every shell binds, reached the remote as
// nothing at all.
func TestKeyToBytesModifiedCursorKeys(t *testing.T) {
	cases := []struct {
		name string
		msg  tea.KeyMsg
		want string
	}{
		{"ctrl+left", tea.KeyMsg{Type: tea.KeyCtrlLeft}, "\x1b[1;5D"},
		{"ctrl+right", tea.KeyMsg{Type: tea.KeyCtrlRight}, "\x1b[1;5C"},
		{"ctrl+up", tea.KeyMsg{Type: tea.KeyCtrlUp}, "\x1b[1;5A"},
		{"ctrl+down", tea.KeyMsg{Type: tea.KeyCtrlDown}, "\x1b[1;5B"},
		{"ctrl+home", tea.KeyMsg{Type: tea.KeyCtrlHome}, "\x1b[1;5H"},
		{"ctrl+end", tea.KeyMsg{Type: tea.KeyCtrlEnd}, "\x1b[1;5F"},
		{"shift+left", tea.KeyMsg{Type: tea.KeyShiftLeft}, "\x1b[1;2D"},
		{"shift+right", tea.KeyMsg{Type: tea.KeyShiftRight}, "\x1b[1;2C"},
		{"shift+up", tea.KeyMsg{Type: tea.KeyShiftUp}, "\x1b[1;2A"},
		{"shift+end", tea.KeyMsg{Type: tea.KeyShiftEnd}, "\x1b[1;2F"},
		{"ctrl+shift+left", tea.KeyMsg{Type: tea.KeyCtrlShiftLeft}, "\x1b[1;6D"},
		{"ctrl+shift+home", tea.KeyMsg{Type: tea.KeyCtrlShiftHome}, "\x1b[1;6H"},
		{"shift+down", tea.KeyMsg{Type: tea.KeyShiftDown}, "\x1b[1;2B"},
		{"ctrl+shift+up", tea.KeyMsg{Type: tea.KeyCtrlShiftUp}, "\x1b[1;6A"},
		{"ctrl+shift+right", tea.KeyMsg{Type: tea.KeyCtrlShiftRight}, "\x1b[1;6C"},
		{"ctrl+shift+end", tea.KeyMsg{Type: tea.KeyCtrlShiftEnd}, "\x1b[1;6F"},
		{"ctrl+pgup", tea.KeyMsg{Type: tea.KeyCtrlPgUp}, "\x1b[5;5~"},
		{"ctrl+pgdown", tea.KeyMsg{Type: tea.KeyCtrlPgDown}, "\x1b[6;5~"},
		// alt on top of one of these is a bit in the same parameter, not an ESC prefix.
		{"ctrl+alt+left", tea.KeyMsg{Type: tea.KeyCtrlLeft, Alt: true}, "\x1b[1;7D"},
		// The unmodified keys are unchanged.
		{"left", tea.KeyMsg{Type: tea.KeyLeft}, "\x1b[D"},
		{"pgup", tea.KeyMsg{Type: tea.KeyPgUp}, "\x1b[5~"},
		// The other keys whose String() matches no branch and used to drop to nil.
		{"shift+tab", tea.KeyMsg{Type: tea.KeyShiftTab}, "\x1b[Z"},
		{"insert", tea.KeyMsg{Type: tea.KeyInsert}, "\x1b[2~"},
	}
	for _, c := range cases {
		if got := string(keyToBytes(c.msg)); got != c.want {
			t.Errorf("%s: keyToBytes = %q, want %q", c.name, got, c.want)
		}
	}
}

// The three shapes DECSCUSR asks for, and what hop draws for each: a block is the cell
// in reverse video, an underline is the character with a line under it, and a bar — which
// has no half-cell to stand in — is the thinnest glyph there is, in place of the
// character.
func TestMarkAtColumnPerStyle(t *testing.T) {
	cases := []struct {
		name string
		line string
		col  int
		mark cursorMark
		want string
	}{
		{"underline", "hello", 1, underlineMark, "h\x1b[4me\x1b[24mllo"},
		{"underline-past-end", "ab", 3, underlineMark, "ab \x1b[4m \x1b[24m"},
		{"bar", "hello", 1, barMark, "h▏llo"},
		{"bar-past-end", "ab", 3, barMark, "ab ▏"},
		{"bar-keeps-ansi", "\x1b[31mab", 0, barMark, "\x1b[31m▏b"},
		// A one-cell bar in place of a two-cell character would slide the rest of the row
		// left, so a wide cell wears the block instead.
		{"bar-on-wide-char", "日本", 0, barMark, "\x1b[7m日\x1b[27m本"},
	}
	for _, c := range cases {
		if got := markAtColumn(c.line, c.col, c.mark); got != c.want {
			t.Errorf("%s: markAtColumn(%q,%d)=%q, want %q", c.name, c.line, c.col, got, c.want)
		}
	}
}

// markFor maps the emulator's styles onto the marks, and anything it does not know onto
// the block: a cursor in the wrong shape beats no cursor at all.
func TestMarkForStyle(t *testing.T) {
	cases := []struct {
		style vt.CursorStyle
		want  cursorMark
	}{
		{vt.CursorBlock, blockMark},
		{vt.CursorUnderline, underlineMark},
		{vt.CursorBar, barMark},
		{vt.CursorStyle(99), blockMark},
	}
	for _, c := range cases {
		if got := markFor(c.style); got != c.want {
			t.Errorf("markFor(%v) = %+v, want %+v", c.style, got, c.want)
		}
	}
}

// A cursor the far end has hidden (DECTCEM) is not drawn, and showing it again brings it
// back: a full-screen program that hides the cursor while it paints must not have hop
// painting one over its output.
func TestPaneHonoursHiddenCursor(t *testing.T) {
	p, w := cursorPane(t)

	go io.WriteString(w, "\x1b[?25l")
	if !waitFor(func() bool { return p.cursor.look().hidden }) {
		t.Fatal("DECTCEM off left the cursor visible")
	}
	if strings.Contains(p.View(), "\x1b[7m") {
		t.Error("a hidden cursor was still drawn")
	}

	go io.WriteString(w, "\x1b[?25h")
	if !waitFor(func() bool { return !p.cursor.look().hidden }) {
		t.Fatal("DECTCEM on left the cursor hidden")
	}
	if !strings.Contains(p.View(), "\x1b[7m") {
		t.Error("a cursor shown again was not drawn")
	}
}

// DECSCUSR picks the shape and says whether it blinks; hop draws the shape and remembers
// the rest for its own blink clock.
func TestPaneHonoursCursorStyle(t *testing.T) {
	cases := []struct {
		name   string
		seq    string
		style  vt.CursorStyle
		steady bool
		draws  string
	}{
		{"blinking block", "\x1b[1 q", vt.CursorBlock, false, "\x1b[7m"},
		{"steady block", "\x1b[2 q", vt.CursorBlock, true, "\x1b[7m"},
		{"blinking underline", "\x1b[3 q", vt.CursorUnderline, false, "\x1b[4m"},
		{"steady underline", "\x1b[4 q", vt.CursorUnderline, true, "\x1b[4m"},
		{"blinking bar", "\x1b[5 q", vt.CursorBar, false, "▏"},
		{"steady bar", "\x1b[6 q", vt.CursorBar, true, "▏"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			p, w := cursorPane(t)

			go io.WriteString(w, c.seq)
			if !waitFor(func() bool {
				look := p.cursor.look()
				return look.style == c.style && look.steady == c.steady
			}) {
				look := p.cursor.look()
				t.Fatalf("%q gave style %v steady %v, want %v / %v", c.seq, look.style, look.steady, c.style, c.steady)
			}
			if !strings.Contains(p.View(), c.draws) {
				t.Errorf("%s was not drawn with %q", c.name, c.draws)
			}
		})
	}
}

// A full reset (RIS) takes the cursor with it, as it takes every mode: the emulator
// rewrites its own state without a callback, so the pane has to notice.
func TestCursorResetOnRIS(t *testing.T) {
	p, w := cursorPane(t)

	go io.WriteString(w, "\x1b[6 q\x1b[?25l")
	if !waitFor(func() bool { return p.cursor.look().style == vt.CursorBar }) {
		t.Fatal("the bar never arrived")
	}

	go io.WriteString(w, "\x1bc")
	if !waitFor(func() bool {
		look := p.cursor.look()
		return look.style == vt.CursorBlock && !look.hidden && !look.steady
	}) {
		t.Fatalf("RIS left the cursor at %+v, want a visible blinking block", p.cursor.look())
	}
}

// A program killed on the alt screen never puts the cursor back, so the shell underneath
// would inherit its bar. Leaving the alt screen resets the shape, as it resets the modes.
func TestCursorResetLeavingAltScreen(t *testing.T) {
	p, w := cursorPane(t)

	go io.WriteString(w, "\x1b[?1049h\x1b[6 q")
	if !waitFor(func() bool { return p.cursor.look().style == vt.CursorBar }) {
		t.Fatal("the alt-screen program's bar never arrived")
	}

	go io.WriteString(w, "\x1b[?1049l")
	if !waitFor(func() bool { return p.cursor.look().style == vt.CursorBlock }) {
		t.Fatalf("leaving the alt screen kept the program's style %+v", p.cursor.look())
	}
}

// The blink is hop's own clock: a frame with the cursor down draws nothing, and a cursor
// the far end asked to stand still ignores the frame entirely.
func TestCursorBlinkPhase(t *testing.T) {
	p, w := cursorPane(t)

	if !p.CursorBlinks() {
		t.Fatal("a fresh cursor does not blink; a terminal powers on blinking")
	}

	p.SetCursorPhase(false)
	if strings.Contains(p.View(), "\x1b[7m") {
		t.Error("a cursor down for a blink frame was still drawn")
	}
	p.SetCursorPhase(true)
	if !strings.Contains(p.View(), "\x1b[7m") {
		t.Error("a cursor back up was not drawn")
	}

	// Steady: the far end asked for no blink, so the frame is not its business.
	go io.WriteString(w, "\x1b[2 q")
	if !waitFor(func() bool { return p.cursor.look().steady }) {
		t.Fatal("the steady block never arrived")
	}
	p.SetCursorPhase(false)
	if !strings.Contains(p.View(), "\x1b[7m") {
		t.Error("a steady cursor was blinked away")
	}
	if p.CursorBlinks() {
		t.Error("a steady cursor reports that it blinks")
	}

	// Hidden beats both: there is nothing to blink.
	go io.WriteString(w, "\x1b[?25l")
	if !waitFor(func() bool { return p.cursor.look().hidden }) {
		t.Fatal("DECTCEM off never arrived")
	}
	if p.CursorBlinks() {
		t.Error("a hidden cursor reports that it blinks")
	}
}

// cursorPane is a pane wired to a pipe the test writes server output into.
func cursorPane(t *testing.T) (*Pane, io.WriteCloser) {
	t.Helper()
	out, w := io.Pipe()
	p := New(&sshx.Session{Stdin: nopWriteCloser{io.Discard}, Stdout: out}, 20, 5, nil)
	t.Cleanup(func() { p.Close() })
	return p, w
}
