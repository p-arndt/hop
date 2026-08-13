package terminal

// Selecting text with the pointer, inside hop.
//
// While hop reports the mouse, the drag never reaches the terminal, so its own selection
// never happens. A TUI that takes the mouse has to hand that back itself, which is the
// two operations here, over a rendered pane rather than over the emulator:
//
//	Highlight  paint the span in reverse video, for the screen
//	PlainText  read the span back as text, for the clipboard
//
// Working on the rendered string is what makes both work everywhere a pane is drawn: the
// live screen, the scrollback window, and an editor pane. Where the drag comes from is
// the TUI's problem — see internal/tui/selection.go.

import (
	"strings"
	"unicode"
)

// Cell is a position in a pane's rendered content: column X of row Y, both
// zero-based, with (0, 0) the top-left cell of what is on screen.
type Cell struct {
	X, Y int
}

// Before reports whether c comes before d in reading order.
func (c Cell) Before(d Cell) bool {
	if c.Y != d.Y {
		return c.Y < d.Y
	}
	return c.X < d.X
}

// Span is a selected run of cells, from the anchor to the head, in reading order rather
// than the order they were pointed at.
//
// It flows like a terminal's own selection rather than covering a rectangle: the first
// row runs from the anchor to the end of the line, the rows between are taken whole, and
// the last ends at the head — what selecting a wrapped command line has to do.
type Span struct {
	From, To Cell
}

// NewSpan orders two cells into a span. The head cell is included, so both ends on one
// cell select that character.
func NewSpan(anchor, head Cell) Span {
	if head.Before(anchor) {
		return Span{From: head, To: anchor}
	}
	return Span{From: anchor, To: head}
}

// Empty reports whether the span covers nothing, as an unstarted selection does.
func (s Span) Empty() bool { return s == Span{} }

// bounds is the half-open column range [lo, hi) the span covers on row y, and whether it
// covers any of it. The rows between the ends are covered to width, so the highlight runs
// to the edge as a terminal's does.
func (s Span) bounds(y, width int) (int, int, bool) {
	if y < s.From.Y || y > s.To.Y {
		return 0, 0, false
	}
	lo, hi := 0, width
	if y == s.From.Y {
		lo = s.From.X
	}
	if y == s.To.Y {
		hi = s.To.X + 1
	}
	if lo < 0 {
		lo = 0
	}
	if hi > width {
		hi = width
	}
	if lo >= hi {
		return 0, 0, false
	}
	return lo, hi, true
}

// Highlight paints the span onto a rendered view in reverse video, leaving every other
// cell — and every escape sequence — as it was.
//
// width decides how far a fully covered row is highlighted. A shorter row is highlighted
// to its own end rather than padded, which would draw reverse-video space over the
// pane's background.
func Highlight(view string, s Span, width int) string {
	if s.Empty() {
		return view
	}
	lines := strings.Split(view, "\n")
	for y := range lines {
		lo, hi, ok := s.bounds(y, width)
		if !ok {
			continue
		}
		lines[y] = reverseSpan(lines[y], lo, hi)
	}
	return strings.Join(lines, "\n")
}

// PlainText reads the span back as the text it covers, escape sequences dropped and each
// row's trailing blanks trimmed — a terminal row is padded to the screen's width, which
// is not what was selected.
//
// Rows are joined with newlines, and a row blank inside the span stays an empty line:
// the blank between two commands is part of a selection over both.
func PlainText(view string, s Span, width int) string {
	if s.Empty() {
		return ""
	}
	lines := strings.Split(view, "\n")
	var out []string
	for y := range lines {
		lo, hi, ok := s.bounds(y, width)
		if !ok {
			continue
		}
		out = append(out, strings.TrimRightFunc(sliceColumns(lines[y], lo, hi), unicode.IsSpace))
	}
	return strings.Join(out, "\n")
}

// reverseSpan wraps the cells in [lo, hi) of one rendered row in reverse video. The
// escape sequences are copied through and occupy no column, as reverseAtColumn walks
// them, with one addition: an SGR reset inside the span would cancel the reverse
// attribute, so it is re-asserted after every sequence copied while inside.
func reverseSpan(line string, lo, hi int) string {
	var b strings.Builder
	inside := false
	col := 0

	walkRow(line, func(esc string, r rune) {
		if esc != "" {
			b.WriteString(esc)
			if inside {
				b.WriteString("\x1b[7m")
			}
			return
		}
		switch {
		case !inside && col >= lo && col < hi:
			b.WriteString("\x1b[7m")
			inside = true
		case inside && col >= hi:
			b.WriteString("\x1b[27m")
			inside = false
		}
		b.WriteRune(r)
		col += runeWidth(r)
	})

	if inside {
		b.WriteString("\x1b[27m")
	}
	return b.String()
}

// sliceColumns returns the characters of one rendered row occupying columns [lo, hi),
// escape sequences dropped: the row as text.
func sliceColumns(line string, lo, hi int) string {
	var b strings.Builder
	col := 0
	walkRow(line, func(esc string, r rune) {
		if esc != "" {
			return
		}
		if col >= lo && col < hi {
			b.WriteRune(r)
		}
		col += runeWidth(r)
	})
	return b.String()
}

// walkRow walks a rendered row, calling fn once per escape sequence (esc set, r zero) and
// once per character (esc empty) — reverseAtColumn's traversal, factored out so the two
// operations above agree about which cell is which.
func walkRow(line string, fn func(esc string, r rune)) {
	runes := []rune(line)
	for i := 0; i < len(runes); {
		if runes[i] != 0x1b {
			fn("", runes[i])
			i++
			continue
		}
		j := escapeEnd(runes, i)
		fn(string(runes[i:j]), 0)
		i = j
	}
}

// escapeEnd returns the index just past the escape sequence starting at i, handling the
// two forms a rendered row carries — CSI and OSC — and treating anything else as ESC plus
// one byte.
func escapeEnd(runes []rune, i int) int {
	j := i + 1
	if j >= len(runes) {
		return j
	}
	switch runes[j] {
	case '[':
		for j++; j < len(runes) && (runes[j] < 0x40 || runes[j] > 0x7e); j++ {
		}
		if j < len(runes) {
			j++
		}
	case ']':
		for j++; j < len(runes); j++ {
			if runes[j] == 0x07 {
				j++
				break
			}
			if runes[j] == 0x1b && j+1 < len(runes) && runes[j+1] == '\\' {
				j += 2
				break
			}
		}
	default:
		j++
	}
	return j
}
