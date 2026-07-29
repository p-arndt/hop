package terminal

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
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
	got := overlayCursor("row0\nrow1\nrow2", 1, 1)
	want := "row0\nr" + on + "o" + off + "w1\nrow2"
	if got != want {
		t.Errorf("overlayCursor row select = %q, want %q", got, want)
	}
	// Out-of-range row leaves the input unchanged.
	if got := overlayCursor("only", 0, 5); got != "only" {
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
