package filebrowser

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// Drawing, and only drawing. Everything here turns state that is already settled into
// the lines of the pane — View and the row, footer and selection pieces it is built
// from — and nothing here decides anything: no key is read, no scroll is clamped, no
// listing is fetched. That is the line the split was made on, and it is worth keeping,
// because the width arithmetic in renderRow is fiddly enough to want reading on its own
// without the lifecycle of a Browser wrapped around it.
//
// What stayed behind is as much of the story as what came across. clampScroll and
// windowRows read like layout but are motion — every keypress that moves the cursor ends
// in them, and the tree, the marks and the file operations all call clampScroll — so they
// belong with navigation. humanizeBytes, truncateText and stripControl are drawn on here
// but are not owned here: the transfer machinery and the prompt need them too. And
// progressLine and bar, which are as much rendering as anything below, stay in
// transfer.go on purpose: they draw the transfer's own state, and pulling them out would
// have split that concern in order to tidy this one.

// View renders the listing to at most w columns and h rows, truncating every line to w
// so it can never wrap out of its box.
func (b *Browser) View() string {
	if b.w <= 0 || b.h <= 0 {
		return ""
	}

	rows := b.contentRows()
	lines := make([]string, 0, b.h)

	// The header is the current directory rather than the tree's root: it is the one that
	// answers "where would m and u put something", and it moves with the cursor.
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

	// Pad the content area so the footer sits on the last line.
	for len(lines) < 2+rows {
		lines = append(lines, "")
	}

	lines = append(lines, b.footerLine(b.w))

	if len(lines) > b.h {
		lines = lines[:b.h]
	}
	return strings.Join(lines, "\n")
}

// footerLine is the browser's last row, already styled and already fitted to w.
//
// Four things want that row and only one can have it, so the order is the whole content
// of this function: an open question first, because it is the only thing the keyboard is
// answering; then a note still inside its deadline, which is news the user just caused
// and must not be hidden by a bar; then a running transfer; then the standing note.
// Returning "" for "nothing to say" keeps the row present, which is what holds the pane
// at its full height.
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

// selectionLine is what the footer says when nothing has happened lately: how much is
// marked and where the target is aimed.
//
// It is last in the order on purpose — an outcome the user just caused outranks a
// standing count — but it has to exist, because both of those facts are otherwise carried
// by a one-cell tick and a colour, and an operation about to act on eleven files across
// four directories should say eleven somewhere in words.
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

// tailCol is one candidate for a row's right-hand columns, carried as both the plain
// text — which is what the width arithmetic may measure — and the styled text that is
// actually drawn.
type tailCol struct{ plain, styled string }

// renderRow renders one row of the tree: a two-cell gutter, the indent for its depth, a
// twisty for a directory, and the name with as much of the size and time columns as fits.
//
// The gutter is two cells and always both of them, so nothing below it ever shifts: the
// first carries the cursor bar, the second the tick of a marked row. They are separate
// cells because a row is very often both — the cursor sits on an entry while a run of
// them is being marked — and a tick that replaced the bar would lose the cursor exactly
// when the user is moving it fastest.
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

	// Two cells per level, capped at half the pane: a tree opened six deep in a 30-column
	// sidebar would otherwise indent every name off the right-hand edge, and an indent
	// that stops growing still shows the shape of the first few levels.
	indent := strings.Repeat("  ", n.depth)
	if limit := b.w / 2; len(indent) > limit {
		indent = indent[:max(limit, 0)]
	}
	// The twisty column is present on a file row too, as two spaces: without it a file and
	// the directory beside it start in different columns and the depth stops reading.
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
	// The modified time is a column of its own, kept out of the size text so a directory
	// — which has no size — still carries one.
	timeText := b.modTimeCol(e)

	// The row is the name plus as much of a right-hand tail as fits: the candidates are
	// tried widest first and the first one leaving the name a readable stub wins, so a
	// narrowing pane drops the time column, then the size, then nothing is left to drop.
	// Both texts are ASCII by construction — humanizeBytes and a fixed time layout — so
	// len is their cell width.
	const nameFloor = 12
	// The twisty is two cells wide however many bytes its rune takes.
	room := b.w - 2 - len(indent) - 2
	if room < 1 {
		// Indented past the pane. The gutter still has to be drawn — the cursor bar is on
		// it — but there is nothing left to put beside it.
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

	// The target directory is drawn green rather than given a column of its own: at 30
	// columns every cell spent on a marker is a cell off the names, and there is only ever
	// one target, so a colour is enough to find it. The footer spells it out in words.
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
