package filebrowser

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// View renders the listing to at most w columns and h rows, truncated so it can never wrap.
func (b *Browser) View() string {
	if b.w <= 0 || b.h <= 0 {
		return ""
	}

	rows := b.contentRows()
	lines := make([]string, 0, b.h)

	lines = append(lines, dimStyle.Render(truncPath(stripControl(b.cwd), b.w)))
	lines = append(lines, faintStyle.Render(strings.Repeat("─", b.w)))

	if len(b.rows) == 0 {
		lines = append(lines, dimStyle.Render("(empty)"))
	} else {
		end := b.scroll + rows
		if end > len(b.rows) {
			end = len(b.rows)
		}
		for i := b.scroll; i < end; i++ {
			lines = append(lines, b.renderRow(b.rows[i], i == b.cursor))
		}
	}

	for len(lines) < 2+rows {
		lines = append(lines, "")
	}

	lines = append(lines, b.footerLine(b.w))

	if len(lines) > b.h {
		lines = lines[:b.h]
	}
	return strings.Join(lines, "\n")
}

// footerLine is the browser's last row; the case order is the priority: open question, fresh note, transfer, standing note.
func (b *Browser) footerLine(w int) string {
	switch {
	case b.overlay.active():
		return b.overlay.view(w)
	case b.note.live():
		return redStyle.Render(truncateText(stripControl(b.note.text), w))
	case b.xfer != nil:
		return accentStyle.Render(b.progressLine(w))
	case b.note.text != "":
		txt := truncateText(stripControl(b.note.text), w)
		if b.note.err {
			return redStyle.Render(txt)
		}
		return greenStyle.Render(txt)
	}
	return b.selectionLine(w)
}

// selectionLine is what the footer says when nothing has happened lately.
func (b *Browser) selectionLine(w int) string {
	var parts []string
	if n := len(b.marks); n == 1 {
		parts = append(parts, "1 marked")
	} else if n > 1 {
		parts = append(parts, fmt.Sprintf("%d marked", n))
	}
	if b.target != "" {
		parts = append(parts, "→ "+b.target)
	}
	if len(parts) == 0 {
		return ""
	}
	return dimStyle.Render(truncateText(stripControl(strings.Join(parts, "  ")), w))
}

// tailCol is a row's right-hand column as both measured plain text and styled text.
type tailCol struct{ plain, styled string }

// renderRow renders one row of the tree; the gutter is always two cells, so a tick never displaces the cursor bar.
func (b *Browser) renderRow(n *node, selected bool) string {
	e := n.e

	bar := " "
	if selected {
		bar = selBar
	}
	mark := " "
	if b.marked(n) {
		mark = markGlyph
	}

	// Two cells per level, capped at half the pane so a deep tree cannot indent names off the edge.
	indent := strings.Repeat("  ", n.depth)
	if limit := b.w / 2; len(indent) > limit {
		indent = indent[:max(limit, 0)]
	}
	// The twisty column is two spaces on a file row too, so depth still reads.
	twisty := "  "
	if e.IsDir {
		twisty = "▸ "
		if n.expanded {
			twisty = "▾ "
		}
	}
	prefix := bar + mark + faintStyle.Render(indent+twisty)

	nameText := stripControl(e.Name)
	if e.IsDir {
		nameText += "/"
	}
	sizeText := ""
	if !e.IsDir {
		sizeText = humanizeBytes(e.Size)
	}
	// The modified time is its own column, so a directory — which has no size — still carries one.
	timeText := b.modTimeCol(e)

	// Tail candidates are tried widest first; both texts are ASCII, so len is their cell width.
	const nameFloor = 12
	// The twisty is two cells wide however many bytes its rune takes.
	room := b.w - 2 - len(indent) - 2
	if room < 1 {
		// Indented past the pane; the gutter still has to carry the cursor bar.
		return prefix
	}

	var tails []tailCol
	if sizeText != "" && timeText != "" {
		tails = append(tails, tailCol{sizeText + " " + timeText,
			dimStyle.Render(sizeText) + " " + faintStyle.Render(timeText)})
	}
	if timeText != "" && sizeText == "" {
		tails = append(tails, tailCol{timeText, faintStyle.Render(timeText)})
	}
	if sizeText != "" {
		tails = append(tails, tailCol{sizeText, dimStyle.Render(sizeText)})
	}
	tails = append(tails, tailCol{}) // the name on its own always fits

	var tail tailCol
	for _, c := range tails {
		if c.plain == "" || room-len(c.plain)-1 >= nameFloor {
			tail = c
			break
		}
	}

	avail := room
	if tail.plain != "" {
		avail -= len(tail.plain) + 1
	}
	nameText = truncateText(nameText, avail)

	// The target is a colour rather than a column: at 30 columns a marker costs a name cell, and the footer spells it out.
	var nameStyled string
	switch {
	case b.target != "" && n.path == b.target:
		nameStyled = greenStyle.Render(nameText)
	case selected:
		nameStyled = accentBold.Render(nameText)
	case e.IsDir:
		nameStyled = accentStyle.Render(nameText)
	default:
		nameStyled = nameText
	}

	if tail.plain == "" {
		return prefix + nameStyled
	}
	gap := max(room-lipgloss.Width(nameText)-len(tail.plain), 1)
	return prefix + nameStyled + strings.Repeat(" ", gap) + tail.styled
}

// truncPath truncates a remote path to w cells, keeping the tail behind a "…/".
func truncPath(p string, w int) string {
	if lipgloss.Width(p) <= w {
		return p
	}
	const ell = "…/"
	avail := w - lipgloss.Width(ell)
	if avail < 1 {
		return truncateText(p, w)
	}
	r := []rune(p)
	tail := string(r[len(r)-avail:])
	return ell + tail
}
