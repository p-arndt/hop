package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// overlay composites fg on top of bg with its top-left corner at cell (x, y),
// leaving the rest of bg visible around it — a floating window, not a screen
// swap.
//
// Splicing a styled line at a column is the whole difficulty: cutting a string
// that carries ANSI escapes at an arbitrary cell can orphan a colour that was
// opened before the cut and closed after it. ansi.Truncate and ansi.TruncateLeft
// handle exactly that, re-emitting the styles in force at the seam, so the
// background keeps its colours on both sides of the box.
func overlay(bg, fg string, x, y int) string {
	if fg == "" {
		return bg
	}
	if x < 0 {
		x = 0
	}
	if y < 0 {
		y = 0
	}

	bgLines := strings.Split(bg, "\n")
	fgLines := strings.Split(fg, "\n")

	for i, fgLine := range fgLines {
		row := y + i
		if row >= len(bgLines) {
			break // the box runs off the bottom; nothing to composite onto
		}

		bgLine := bgLines[row]
		fgW := lipgloss.Width(fgLine)

		// Left slice of the background, padded out when the row is shorter than x.
		left := ansi.Truncate(bgLine, x, "")
		if gap := x - lipgloss.Width(left); gap > 0 {
			left += strings.Repeat(" ", gap)
		}
		// Right slice: everything the box does not cover.
		right := ansi.TruncateLeft(bgLine, x+fgW, "")

		bgLines[row] = left + fgLine + right
	}

	return strings.Join(bgLines, "\n")
}

// centered returns the top-left cell at which a w x h box is centred in a
// bw x bh area, clamped so it never starts off-screen.
func centered(bw, bh, w, h int) (int, int) {
	x, y := (bw-w)/2, (bh-h)/2
	if x < 0 {
		x = 0
	}
	if y < 0 {
		y = 0
	}
	return x, y
}
