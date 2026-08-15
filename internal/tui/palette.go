package tui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/sahilm/fuzzy"
)

// paletteKey opens the command palette. ctrl+k rather than a letter, because it has to
// be free in every mode hop owns the keyboard in, and ctrl chords are the ones a stock
// macOS terminal delivers (see KEYBINDINGS.md).
const paletteKey = "ctrl+k"

// paletteUI is the command palette's state: a query, the actions matching it, and which
// one is selected. The matches are held rather than recomputed at render time, since the
// cursor indexes them and a render must not be able to move it.
type paletteUI struct {
	open   bool
	query  string
	cursor int
	items  []action
}

// openPalette raises the palette on everything the mode you are in can do. It opens
// unfiltered on purpose: the first thing it teaches is what exists, and typing is the
// second.
func (m *model) openPalette() {
	m.palette = paletteUI{open: true, items: m.contextActions()}
	m.clearStatus()
}

func (m *model) closePalette() { m.palette = paletteUI{} }

// filterPalette re-runs the query and re-clamps the cursor. Both the label and the key
// are matched, so "sft" finds the browser and "ctrl+b" finds the sidebar.
func (m *model) filterPalette() {
	all := m.contextActions()
	if m.palette.query == "" {
		m.palette.items = all
		m.palette.cursor = clamp(m.palette.cursor, 0, max(len(all)-1, 0))
		return
	}

	hay := make([]string, len(all))
	for i, a := range all {
		hay[i] = a.label + " " + a.key
	}

	items := make([]action, 0, len(all))
	for _, mt := range fuzzy.Find(m.palette.query, hay) {
		items = append(items, all[mt.Index])
	}
	m.palette.items = items
	m.palette.cursor = clamp(m.palette.cursor, 0, max(len(items)-1, 0))
}

// handlePaletteKey routes a key while the palette is up. It swallows everything: the
// palette is a text field over a list that binds single letters, so a key falling
// through would act on the host underneath while you were typing its name.
func (m *model) handlePaletteKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc", paletteKey:
		m.closePalette()

	case "enter":
		if m.palette.cursor >= len(m.palette.items) {
			return m, nil
		}
		a := m.palette.items[m.palette.cursor]
		// Closed before the action runs, so an action that opens a card of its own is
		// not opening it underneath this one.
		m.closePalette()
		return m.runAction(a)

	case "up", "ctrl+p":
		m.palette.cursor--
		m.palette.cursor = clamp(m.palette.cursor, 0, max(len(m.palette.items)-1, 0))

	case "down", "ctrl+n":
		m.palette.cursor++
		m.palette.cursor = clamp(m.palette.cursor, 0, max(len(m.palette.items)-1, 0))

	case "backspace":
		if r := []rune(m.palette.query); len(r) > 0 {
			m.palette.query = string(r[:len(r)-1])
			m.filterPalette()
		}

	case "ctrl+u":
		m.palette.query = ""
		m.filterPalette()

	default:
		if len(msg.Runes) > 0 {
			m.palette.query += string(msg.Runes)
			m.filterPalette()
		}
	}
	return m, nil
}

// Palette geometry. Wider than the confirmation card — these are sentences with a key
// pinned to their right — and it shows a fixed number of rows so the card does not
// jump about the screen as the query narrows.
const (
	paletteMaxW   = 52
	paletteFloorW = 24
	paletteRows   = 8
)

func (m *model) paletteInnerW() int {
	room := max(m.width-2*cardPadX-2, paletteFloorW)
	return clamp(paletteMaxW, paletteFloorW, room)
}

// renderPalette draws the palette: the query with a caret, then the matches with their
// keys along the right edge. The key is on every row on purpose — the palette's job is
// to make itself unnecessary.
func (m *model) renderPalette() string {
	w := m.paletteInnerW()
	var b strings.Builder

	b.WriteString(truncate(titleStyle.Render("ACTIONS"), w))
	b.WriteString("\n\n")

	query := accentText.Render("> ") + stripControl(m.palette.query) + accentText.Render("▏")
	b.WriteString(truncate(query, w))
	b.WriteString("\n\n")

	if len(m.palette.items) == 0 {
		b.WriteString(truncate(faint.Render("nothing matches "+stripControl(m.palette.query)), w))
		b.WriteString("\n")
	}

	start := 0
	if m.palette.cursor >= paletteRows {
		start = m.palette.cursor - paletteRows + 1
	}
	for i := start; i < min(start+paletteRows, len(m.palette.items)); i++ {
		b.WriteString(padTo(actionRow(m.palette.items[i], i == m.palette.cursor, w), w))
		b.WriteString("\n")
	}

	b.WriteString("\n")
	b.WriteString(truncate(keyHint("enter", "run")+"  "+keyHint("↑↓", "move")+"  "+keyHint("esc", "close"), w))

	return cardBox.Width(w + 2*cardPadX).Render(b.String())
}

// actionRow is one row of the palette or the menu: a lead bar and label on the left, the
// key on the right. The two are laid out against a known width rather than padded to a
// column, so a long label gives way to the key rather than pushing it off the card.
func actionRow(a action, selected bool, w int) string {
	lead, label := "  ", dimStyle.Render(a.label)
	if selected {
		lead, label = selBar+" ", selectedAliasStyle.Render(a.label)
	}
	key := kc(a.keycap())

	room := max(w-lipgloss.Width(lead)-lipgloss.Width(key)-1, 1)
	left := lead + truncate(label, room)
	gap := max(w-lipgloss.Width(left)-lipgloss.Width(key), 1)
	return left + strings.Repeat(" ", gap) + key
}
