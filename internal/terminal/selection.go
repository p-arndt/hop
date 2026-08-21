package terminal

// Selecting text with the pointer, inside hop. While hop reports the mouse the drag never
// reaches the terminal, so hop highlights and reads back spans over the rendered string —
// which works for the live screen, the scrollback window and an editor pane alike.

import (
	"strings"
	"unicode"
)

// Cell is column X of row Y in a pane's rendered content, zero-based from the top left.
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

// Span is a selected run of cells in reading order. It flows like a terminal's selection
// rather than covering a rectangle, so a wrapped command line selects whole.
type Span struct {
	From, To Cell
}

// NewSpan orders two cells into a span; the head cell is included.
func NewSpan(anchor, head Cell) Span {
	if head.Before(anchor) {
		return Span{From: head, To: anchor}
	}
	return Span{From: anchor, To: head}
}

func (s Span) Empty() bool { return s == Span{} }

// bounds is the half-open column range [lo, hi) the span covers on row y. Rows between the
// ends are covered to width, so the highlight runs to the edge as a terminal's does.
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

// Highlight paints the span onto a rendered view in reverse video. A row shorter than
// width is highlighted to its own end rather than padded.
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

// PlainText reads the span back as text, escapes dropped and trailing blanks trimmed — a
// terminal row is padded to the screen's width, which is not what was selected.
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

// reverseSpan wraps the cells in [lo, hi) of one rendered row in reverse video. An SGR
// reset inside the span would cancel it, so it is re-asserted after every sequence copied.
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
// escape sequences dropped.
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

// walkRow calls fn once per escape sequence (esc set, r zero) and once per character.
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

// escapeEnd returns the index just past the escape sequence at i, handling CSI and OSC and
// treating anything else as ESC plus one byte.
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
