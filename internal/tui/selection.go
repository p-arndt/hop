package tui

// Selecting text in a pane with the pointer.
//
// hop reports the mouse, so the terminal's own click-and-drag selection never happens and
// the drag arrives here instead. The rules are a terminal's:
//
//   - a drag selects from where the button went down to where the pointer is, flowing
//     over row ends rather than covering a rectangle (see terminal.Span);
//   - releasing copies;
//   - the wheel scrolls the view without ending the drag, and a drag held against the top
//     or bottom row of the pane keeps going by itself — either way the selection is not
//     limited to what was on screen when the button went down (see wheelShell and
//     dragAutoScroll);
//   - a selection rides the text it was made on while the view scrolls under it, so the
//     highlight stays over the words it covers (see shiftSelection);
//   - anything else clears it — a keystroke, a click elsewhere — since a stale highlight
//     over a screen that has moved on is worse than no highlight.
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
	// edge is which way a drag held against a pane edge is scrolling the view: -1 for
	// up, +1 for down, 0 while the pointer is somewhere in the middle. It is what says
	// a repeat is already armed, so motion does not start a second clock.
	edge int

	// box is the content box the drag was started in, kept because a selection only means
	// anything against the rows it was measured in. Reading the width back off the layout
	// later cannot answer it: the content area may be one box or two, and which one this
	// selection belongs to is a fact about where the pointer went down, not about what the
	// screen looks like now. It also survives a resize mid-drag.
	box rect
}

// span is the selection as an ordered span, or the empty span when there is none.
func (s selection) span() terminal.Span {
	if !s.active {
		return terminal.Span{}
	}
	return terminal.NewSpan(s.anchor, s.head)
}

// startSelection begins a drag at the pane cell the button went down on.
func (m *model) startSelection(c terminal.Cell, box rect) {
	m.sel = selection{active: true, dragging: true, anchor: c, head: c, box: box}
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
	m.sel.edge = 0

	span := m.sel.span()
	// A selection grown by scrolling covers rows that are no longer on screen, and the
	// screen is all `view` holds. A shell pane can render the rest back.
	if rows, top, ok := m.shellSpanView(span); ok {
		view, span = rows, shiftSpan(span, top)
	}
	text := terminal.PlainText(view, span, m.sel.box.innerW())
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

// shiftSelection moves the selection dy rows down the screen, which is what a view that
// has scrolled dy lines has done to the text under it. Nothing else about the selection
// changes: the same words stay covered.
//
// The head of a live drag is left where it is — it belongs to the pointer, which did not
// move, so the caller places it. A drag scrolled far enough carries its anchor off the
// screen; that is a selection wider than a screenful, and the span simply covers every
// row between, painting the ones that are visible.
func (m *model) shiftSelection(dy int) {
	if !m.sel.active || dy == 0 {
		return
	}
	m.sel.anchor.Y += dy
	if !m.sel.dragging {
		m.sel.head.Y += dy
	}
}

// shellSpanView renders exactly the rows a span covers out of the focused shell's pane,
// reaching above the window into scrollback and below it toward the live screen, and
// reports the row the rendering starts at. It declines for anything that is not a shell:
// an editor pane keeps no history here, so its screen is all there is to copy.
func (m *model) shellSpanView(s terminal.Span) (string, int, bool) {
	if !m.focused() && !m.scrolling() {
		return "", 0, false
	}
	sess := m.sessions[m.active]
	if sess == nil || sess.shell() == nil {
		return "", 0, false
	}
	return sess.shell().pane.ViewRows(s.From.Y, s.To.Y), s.From.Y, true
}

// shiftSpan renumbers a span's rows against a view that starts at row top — what turns
// pane-view coordinates into coordinates over the rows shellSpanView handed back.
func shiftSpan(s terminal.Span, top int) terminal.Span {
	s.From.Y -= top
	s.To.Y -= top
	return s
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

// selectionW is how wide a row of the content the selection was made over is — the inner
// width of the box that content was rendered in, not of the content area as a whole.
// Both operations need it: it is where a fully covered row stops, and where the last
// row's end column is clamped to.
//
// It was m.paneW until the content area learned to hold two editors side by side, at
// which point one number stopped answering for every pane on screen. The cases here are
// renderContent's, in the same order, because the selection is painted onto exactly what
// that switch decided to draw:
//
//   - a shell pane is never one of two boxes — the split belongs to the files the tree
//     column opens — so it is always the whole content area, even while the session it
//     belongs to has a split of editors sitting behind it. That is why this cannot simply
//     ask contentW: contentW answers for the session, and a session with s.split set
//     still draws its shell full width.
//   - an editor is contentW's question exactly: one box, or half of one.
//
// Anything else — the details pane, a dead session's last screen, the browser in the
// narrow-window fallback — is drawn at m.paneW and never has a highlight painted on it
// anyway, so the fallback is the whole content area.
// selectedView is a pane's rendered content with the selection painted onto it, a no-op
// when nothing is selected.
func (m *model) selectedView(content string) string {
	if !m.sel.active {
		return content
	}
	return terminal.Highlight(content, m.sel.span(), m.sel.box.innerW())
}

// countLines is how many lines a copied string spans, for the status line.
func countLines(s string) int { return strings.Count(s, "\n") + 1 }
