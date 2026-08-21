package tui

import (
	"strings"

	"hop/internal/terminal"
)

// selection is the drag in progress, or the one just made; anchor and head are in the
// pane's content coordinates.
type selection struct {
	active   bool
	dragging bool
	anchor   terminal.Cell
	head     terminal.Cell
	// edge is which way a drag held against a pane edge is scrolling: it also says a
	// repeat is already armed, so motion does not start a second clock.
	edge int

	// box is which box of the content area the drag started in. A name, not a width, so a
	// resize mid-drag still measures against the box the pane is drawn in now. Known gap:
	// a split arriving mid-drag leaves this naming the whole area.
	box selBox
}

// selBox names one of the boxes the content area is drawn as.
type selBox uint8

const (
	// selContent is the zero value: with no layout behind it a selection measures against
	// the whole area.
	selContent selBox = iota
	selLeft
	selRight
)

// namedBox reads a content box of the current frame back as its name. The halves are only
// told apart while there are two: unsplit, frame.left is frame.content.
func (m *model) namedBox(box rect) selBox {
	if m.frame.right.empty() {
		return selContent
	}
	switch box {
	case m.frame.right:
		return selRight
	case m.frame.left:
		return selLeft
	}
	return selContent
}

// selWidth is the inner width of the box the selection is drawn in: where a fully covered
// row stops, and where the last row's end column is clamped to.
func (m *model) selWidth() int {
	switch m.sel.box {
	case selLeft:
		return m.frame.half(false).innerW()
	case selRight:
		return m.frame.half(true).innerW()
	}
	return m.frame.content.innerW()
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
	m.sel = selection{active: true, dragging: true, anchor: c, head: c, box: m.namedBox(box)}
}

// dragSelection moves the head of a drag in progress.
func (m *model) dragSelection(c terminal.Cell) {
	if !m.sel.dragging {
		return
	}
	m.sel.head = c
}

// endSelection finishes a drag and copies what it covers; an empty selection is dropped
// rather than copied, which would clear the clipboard on a click.
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
	text := terminal.PlainText(view, span, m.selWidth())
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

// shiftSelection moves the selection dy rows down, which is what a view scrolled dy lines
// has done to the text under it. The head of a live drag belongs to the pointer, which did
// not move, so the caller places it.
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
// reaching into scrollback, and reports the row it starts at. It declines for anything
// that is not a shell, since an editor pane keeps no history here.
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

// shiftSpan renumbers a span's rows against a view that starts at row top.
func shiftSpan(s terminal.Span, top int) terminal.Span {
	s.From.Y -= top
	s.To.Y -= top
	return s
}

// clearSelection takes the highlight down, reporting whether there was one.
func (m *model) clearSelection() bool {
	if !m.sel.active {
		return false
	}
	m.sel = selection{}
	return true
}

// selectedView is a pane's rendered content with the selection painted onto it.
func (m *model) selectedView(content string) string {
	if !m.sel.active {
		return content
	}
	return terminal.Highlight(content, m.sel.span(), m.selWidth())
}

func countLines(s string) int { return strings.Count(s, "\n") + 1 }
