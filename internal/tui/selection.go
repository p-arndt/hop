package tui

// Selecting text in a pane with the pointer.
//
// hop reports the mouse, so the terminal's own click-and-drag selection never happens and
// the drag arrives here instead. The rules are a terminal's:
//
//   - a drag selects from where the button went down to where the pointer is, flowing
//     over row ends rather than covering a rectangle (see terminal.Span);
//   - releasing copies;
//   - anything else clears it — a keystroke, a scroll, a click elsewhere — since a stale
//     highlight over a screen that has moved is worse than no highlight.
//
// A remote program that asked for the mouse keeps it and does its own selecting, so hop
// does not select over the top of it. See mouseShell.

import (
	"strings"

	"hop/internal/terminal"
)

// selection is the drag in progress, or the one just made. anchor is where the button
// went down and head where the pointer last was, both in the pane's content coordinates.
//
// dragging separates a live drag from the selection it leaves behind: the highlight
// outlives the button, but only a live drag moves the head.
type selection struct {
	active   bool
	dragging bool
	anchor   terminal.Cell
	head     terminal.Cell
}

// span is the selection as an ordered span, or the empty span when there is none.
func (s selection) span() terminal.Span {
	if !s.active {
		return terminal.Span{}
	}
	return terminal.NewSpan(s.anchor, s.head)
}

// startSelection begins a drag at the pane cell the button went down on.
func (m *model) startSelection(c terminal.Cell) {
	m.sel = selection{active: true, dragging: true, anchor: c, head: c}
}

// dragSelection moves the head of a drag in progress. A motion event with no drag behind
// it is the pointer merely crossing the pane.
func (m *model) dragSelection(c terminal.Cell) {
	if !m.sel.dragging {
		return
	}
	m.sel.head = c
}

// endSelection finishes a drag and puts what it covers on the clipboard. The highlight
// stays up until the next key or scroll.
//
// A selection of nothing is dropped rather than copied: it would clear the clipboard on a
// click, losing what somebody was about to paste.
func (m *model) endSelection(view string) {
	if !m.sel.dragging {
		return
	}
	m.sel.dragging = false

	text := terminal.PlainText(view, m.sel.span(), m.paneW)
	if strings.TrimSpace(text) == "" {
		m.sel = selection{}
		return
	}
	if err := m.writeClipboard()(text); err != nil {
		m.setStatus(statusErr, "copy: %v", err)
		return
	}
	m.setStatus(statusOK, "copied %d %s", countLines(text), plural(countLines(text), "line", "lines"))
}

// clearSelection takes the highlight down, reporting whether there was one. Every caller
// is somewhere the screen underneath is about to change.
func (m *model) clearSelection() bool {
	if !m.sel.active {
		return false
	}
	m.sel = selection{}
	return true
}

// selectedView is a pane's rendered content with the selection painted onto it, a no-op
// when nothing is selected.
func (m *model) selectedView(content string) string {
	return terminal.Highlight(content, m.sel.span(), m.paneW)
}

// countLines is how many lines a copied string spans, for the status line.
func countLines(s string) int { return strings.Count(s, "\n") + 1 }
