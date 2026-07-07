package terminal

import "testing"

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
