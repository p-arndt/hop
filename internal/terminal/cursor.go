package terminal

import (
	"strings"
	"sync"

	"github.com/charmbracelet/x/vt"
)

// The cursor on screen is hop's own drawing: vt renders cells and no cursor, so both
// whether there is one and what shape it takes have to be tracked from what the far end
// asked for and painted onto the rendered row.
//
//	DECTCEM  (CSI ?25 h/l)     on screen or hidden
//	DECSCUSR (CSI Ps SP q)     block / underline / bar, blinking or steady
//
// Both arrive through the emulator's callbacks, which run on the output pump, and are
// read by the UI goroutine when it draws — the same split mouseState lives under, and
// the reason for the mutex.
//
// Blinking is hop's own clock rather than the terminal's, since the cell is drawn text
// and not a real cursor. It costs a repaint twice a second, so it is off unless asked
// for: the UI drives it through SetCursorPhase and this file only remembers the frame.
type cursorState struct {
	mu     sync.Mutex
	hidden bool
	style  vt.CursorStyle
	steady bool // the far end asked for a cursor that does not blink
	// down is a blink frame with the cursor off. Only hop's clock moves it, so a pane
	// nobody is blinking stays up.
	down bool
}

// cursorLook is one consistent reading of the state above, taken under the lock so the
// row being drawn cannot be marked with half of two cursors.
type cursorLook struct {
	hidden bool
	style  vt.CursorStyle
	steady bool
	down   bool
}

// drawn reports whether this look puts anything on the screen. A steady cursor ignores
// the blink frame — the far end asked for it not to blink.
func (l cursorLook) drawn() bool {
	return !l.hidden && (!l.down || l.steady)
}

// setVisible records DECTCEM. The emulator reports it on the mode itself and again when
// the alternate screen switches, since each screen carries its own.
func (c *cursorState) setVisible(visible bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.hidden = !visible
}

// setStyle records DECSCUSR. steady is vt's second argument, which is the negation of
// the sequence's blink bit: true means the far end asked for a cursor that stands still.
func (c *cursorState) setStyle(style vt.CursorStyle, steady bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.style = style
	c.steady = steady
}

// setPhase raises or drops the cursor for one blink frame, leaving what the far end
// asked for untouched.
func (c *cursorState) setPhase(up bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.down = !up
}

// clear forgets what the far end asked for: a visible, blinking block, which is what a
// terminal powers on with. Called on a full reset and when the alternate screen closes,
// neither of which withdraws the style the program that owned it set.
func (c *cursorState) clear() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.hidden = false
	c.style = vt.CursorBlock
	c.steady = false
}

// look takes a snapshot for the drawing goroutine.
func (c *cursorState) look() cursorLook {
	c.mu.Lock()
	defer c.mu.Unlock()
	return cursorLook{hidden: c.hidden, style: c.style, steady: c.steady, down: c.down}
}

// blinks reports whether this cursor would blink if hop ran the clock: on screen, and
// not one the far end asked to stand still.
func (c *cursorState) blinks() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return !c.hidden && !c.steady
}

// SetCursorPhase raises (up) or drops the cursor for one frame of hop's blink clock.
// Panes are phased together, so nothing is left mid-blink when focus moves; a pane whose
// cursor is steady or hidden ignores it.
func (p *Pane) SetCursorPhase(up bool) { p.cursor.setPhase(up) }

// CursorBlinks reports whether this pane has a cursor worth blinking — on screen, and
// not one DECSCUSR asked to stand still.
func (p *Pane) CursorBlinks() bool { return p.cursor.blinks() }

// cursorMark is how a style marks the cell it stands on: an SGR pair wrapped around the
// character, or a glyph drawn in place of it.
type cursorMark struct {
	on, off string
	// glyph replaces the cell's character. Zero keeps it.
	glyph rune
}

var (
	// blockMark is the reverse-video cell — the shape hop has always drawn, and the one
	// a terminal starts in.
	blockMark = cursorMark{on: "\x1b[7m", off: "\x1b[27m"}
	// underlineMark is the character with a line under it, which is what the style is.
	underlineMark = cursorMark{on: "\x1b[4m", off: "\x1b[24m"}
	// barMark is the insertion point. A bar stands on the left edge of a cell and a cell
	// grid has no edge to stand on, so hop draws the thinnest glyph there is in the
	// cell's own colours, in place of the character. That hides the character under the
	// cursor — the one about to be typed over, and usually a blank at the end of a line.
	barMark = cursorMark{glyph: '▏'}
)

// markFor is the mark a style is drawn with. An unknown style is a block: a cursor in
// the wrong shape beats no cursor at all.
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

// overlayCursor draws the cursor at cell (cx, cy) on the rendered screen. It works on
// the row's string, so it never touches emulator state.
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

// markAtColumn marks the character at visible column col, skipping ANSI escape
// sequences, which occupy no cells. A cursor past the end of the line pads with spaces
// and appends the mark on a blank.
//
// A glyph mark on a wide character falls back to the block: swapping a two-cell
// character for a one-cell bar would slide the rest of the row left.
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

// markedCell is one cell wearing the mark.
func markedCell(r rune, mark cursorMark) string {
	if mark.glyph != 0 {
		if runeWidth(r) > 1 {
			return blockMark.on + string(r) + blockMark.off
		}
		return string(mark.glyph)
	}
	return mark.on + string(r) + mark.off
}

// reverseAtColumn is markAtColumn with the block mark — the shape everything but
// DECSCUSR asks for.
func reverseAtColumn(line string, col int) string {
	return markAtColumn(line, col, blockMark)
}
