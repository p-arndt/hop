package terminal

import (
	"strings"
	"sync"

	"github.com/charmbracelet/x/vt"
)

// vt renders cells and no cursor, so hop tracks DECTCEM (CSI ?25 h/l) and DECSCUSR
// (CSI Ps SP q) itself and paints the cursor onto the rendered row.
//
// Written by the emulator's callbacks on the output pump, read by the UI goroutine when it
// draws, hence the mutex.
type cursorState struct {
	mu     sync.Mutex
	hidden bool
	style  vt.CursorStyle
	steady bool // the far end asked for a cursor that does not blink
	// down is a blink frame with the cursor off; only hop's clock moves it.
	down bool
}

type cursorLook struct {
	hidden bool
	style  vt.CursorStyle
	steady bool
	down   bool
}

// drawn: a steady cursor ignores the blink frame.
func (l cursorLook) drawn() bool {
	return !l.hidden && (!l.down || l.steady)
}

// setVisible records DECTCEM, reported both on the mode itself and when the alternate
// screen switches.
func (c *cursorState) setVisible(visible bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.hidden = !visible
}

// setStyle records DECSCUSR; steady is vt's negation of the sequence's blink bit.
func (c *cursorState) setStyle(style vt.CursorStyle, steady bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.style = style
	c.steady = steady
}

func (c *cursorState) setPhase(up bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.down = !up
}

// clear restores what a terminal powers on with: a visible, blinking block. Called on a
// full reset and when the alternate screen closes, neither of which withdraws the style.
func (c *cursorState) clear() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.hidden = false
	c.style = vt.CursorBlock
	c.steady = false
}

func (c *cursorState) look() cursorLook {
	c.mu.Lock()
	defer c.mu.Unlock()
	return cursorLook{hidden: c.hidden, style: c.style, steady: c.steady, down: c.down}
}

func (c *cursorState) blinks() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return !c.hidden && !c.steady
}

// SetCursorPhase raises (up) or drops the cursor for one frame of hop's blink clock.
func (p *Pane) SetCursorPhase(up bool) { p.cursor.setPhase(up) }

func (p *Pane) CursorBlinks() bool { return p.cursor.blinks() }

type cursorMark struct {
	on, off string
	// glyph replaces the cell's character. Zero keeps it.
	glyph rune
}

var (
	blockMark     = cursorMark{on: "\x1b[7m", off: "\x1b[27m"}
	underlineMark = cursorMark{on: "\x1b[4m", off: "\x1b[24m"}
	// barMark: a cell grid has no left edge to stand on, so hop draws the thinnest glyph
	// there is in place of the character.
	barMark = cursorMark{glyph: '▏'}
)

// markFor is the mark a style is drawn with; an unknown style is a block.
func markFor(style vt.CursorStyle) cursorMark {
	switch style {
	case vt.CursorUnderline:
		return underlineMark
	case vt.CursorBar:
		return barMark
	default:
		return blockMark
	}
}

func overlayCursor(rendered string, cx, cy int, mark cursorMark) string {
	if cx < 0 || cy < 0 {
		return rendered
	}
	lines := strings.Split(rendered, "\n")
	if cy >= len(lines) {
		return rendered
	}
	lines[cy] = markAtColumn(lines[cy], cx, mark)
	return strings.Join(lines, "\n")
}

// markAtColumn marks the character at visible column col, skipping ANSI escape sequences,
// which occupy no cells. Past the end of the line it pads with spaces.
func markAtColumn(line string, col int, mark cursorMark) string {
	var b strings.Builder
	visCol := 0
	marked := false

	walkRow(line, func(esc string, r rune) {
		if esc != "" {
			b.WriteString(esc)
			return
		}
		if !marked && visCol == col {
			b.WriteString(markedCell(r, mark))
			marked = true
		} else {
			b.WriteRune(r)
		}
		visCol += runeWidth(r)
	})

	if !marked {
		for visCol < col {
			b.WriteRune(' ')
			visCol++
		}
		b.WriteString(markedCell(' ', mark))
	}
	return b.String()
}

// markedCell is one cell wearing the mark. A glyph on a wide character falls back to the
// block: a two-cell character swapped for a one-cell bar would slide the row left.
func markedCell(r rune, mark cursorMark) string {
	if mark.glyph != 0 {
		if runeWidth(r) > 1 {
			return blockMark.on + string(r) + blockMark.off
		}
		return string(mark.glyph)
	}
	return mark.on + string(r) + mark.off
}

// reverseAtColumn is markAtColumn with the block mark.
func reverseAtColumn(line string, col int) string {
	return markAtColumn(line, col, blockMark)
}
