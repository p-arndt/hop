package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// overlay composites fg on top of bg with its top-left corner at cell (x, y). ansi.Truncate
// and ansi.TruncateLeft re-emit the styles in force at each seam, so cutting a line mid-ANSI
// does not orphan a colour the background had opened.
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
		right := ansi.TruncateLeft(bgLine, x+fgW, "")

		bgLines[row] = left + fgLine + right
	}

	return strings.Join(bgLines, "\n")
}

// centered returns the top-left cell at which a w x h box is centred in a bw x bh area.
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
