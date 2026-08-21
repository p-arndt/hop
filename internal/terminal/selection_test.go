package terminal

import (
	"strings"
	"testing"
)

// A span is in reading order, flowing over row ends rather than covering a rectangle.
func TestNewSpanOrdersItsEnds(t *testing.T) {
	a, b := Cell{X: 5, Y: 1}, Cell{X: 2, Y: 3}
	if NewSpan(a, b) != NewSpan(b, a) {
		t.Fatal("a drag made upward selected something other than the same drag downward")
	}
	if s := NewSpan(a, b); s.From != a || s.To != b {
		t.Fatalf("span = %+v, want from %+v to %+v", s, a, b)
	}
	if !(Span{}).Empty() {
		t.Fatal("the zero span is not empty")
	}
}

// The column range each row of a span covers.
func TestSpanBounds(t *testing.T) {
	s := NewSpan(Cell{X: 3, Y: 1}, Cell{X: 4, Y: 3})

	cases := []struct {
		y      int
		lo, hi int
		ok     bool
	}{
		{0, 0, 0, false}, // above
		{1, 3, 10, true}, // the anchor row runs to the edge
		{2, 0, 10, true}, // whole
		{3, 0, 5, true},  // the head row, head included
		{4, 0, 0, false}, // below
	}
	for _, c := range cases {
		lo, hi, ok := s.bounds(c.y, 10)
		if lo != c.lo || hi != c.hi || ok != c.ok {
			t.Errorf("bounds(%d) = %d, %d, %v; want %d, %d, %v", c.y, lo, hi, ok, c.lo, c.hi, c.ok)
		}
	}

	lo, hi, ok := NewSpan(Cell{X: 2, Y: 0}, Cell{X: 2, Y: 0}).bounds(0, 10)
	if !ok || lo != 2 || hi != 3 {
		t.Fatalf("a one-cell span covered %d..%d (%v), want 2..3", lo, hi, ok)
	}
}

// Rows outside the span come back untouched, byte for byte.
func TestHighlightPaintsOnlyTheSpan(t *testing.T) {
	view := "hello world\nsecond line\nthird line"
	got := Highlight(view, NewSpan(Cell{X: 10, Y: 0}, Cell{X: 6, Y: 0}), 11)

	want := "hello \x1b[7mworld\x1b[27m\nsecond line\nthird line"
	if got != want {
		t.Fatalf("Highlight =\n%q\nwant\n%q", got, want)
	}

	if got := Highlight(view, Span{}, 11); got != view {
		t.Fatal("an empty span painted something")
	}
}

// Escapes occupy no column, and a reset inside the span must not cancel the highlight.
func TestHighlightSurvivesEscapesInTheRow(t *testing.T) {
	// "ab" in colour, then "cd" plain; columns 1..2 straddle the reset.
	row := "\x1b[31mab\x1b[0mcd"
	got := Highlight(row, NewSpan(Cell{X: 1, Y: 0}, Cell{X: 2, Y: 0}), 4)

	if plain := sliceColumns(got, 0, 4); plain != "abcd" {
		t.Fatalf("highlighting changed the row's text: %q", plain)
	}
	if strings.Count(got, "\x1b[7m") != 2 {
		t.Fatalf("the reverse attribute was not re-asserted after the reset: %q", got)
	}
	if !strings.HasSuffix(got, "\x1b[27md") {
		t.Fatalf("the highlight did not end at the span: %q", got)
	}
}

// PlainText drops the escapes and trims each row's padding.
func TestPlainText(t *testing.T) {
	view := strings.Join([]string{
		"\x1b[32mparndt@allthing\x1b[0m:~$ apt update   ",
		"Reading package lists... Done          ",
		"parndt@allthing:~$                     ",
	}, "\n")

	got := PlainText(view, NewSpan(Cell{X: 19, Y: 0}, Cell{X: 4, Y: 1}), 39)
	want := "apt update\nReadi"
	if got != want {
		t.Fatalf("PlainText = %q, want %q", got, want)
	}

	// A row blank inside the span still counts as a line.
	got = PlainText("one\n   \ntwo", NewSpan(Cell{X: 0, Y: 0}, Cell{X: 2, Y: 2}), 3)
	if got != "one\n\ntwo" {
		t.Fatalf("PlainText = %q, want %q", got, "one\n\ntwo")
	}

	if got := PlainText(view, Span{}, 39); got != "" {
		t.Fatalf("an empty span read back %q", got)
	}
}

func TestSelectionCountsWideRunes(t *testing.T) {
	row := "a世b"
	if got := PlainText(row, NewSpan(Cell{X: 1, Y: 0}, Cell{X: 2, Y: 0}), 4); got != "世" {
		t.Fatalf("PlainText over a wide rune = %q, want %q", got, "世")
	}
	if got := PlainText(row, NewSpan(Cell{X: 3, Y: 0}, Cell{X: 3, Y: 0}), 4); got != "b" {
		t.Fatalf("the column after a wide rune = %q, want \"b\"", got)
	}
}
