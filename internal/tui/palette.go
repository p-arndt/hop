package tui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/sahilm/fuzzy"

	"hop/internal/keys"
)

// paletteUI is the command palette's state. The matches are held rather than recomputed
// at render time, since the cursor indexes them.
type paletteUI struct {
	open   bool
	query  string
	cursor int
	items  []action
}

// openPalette raises the palette on everything the current mode can do, unfiltered.
func (m *model) openPalette() {
	m.palette = paletteUI{open: true, items: m.contextActions()}
	m.clearStatus()
}

func (m *model) closePalette() { m.palette = paletteUI{} }

// filterPalette re-runs the query and re-clamps the cursor. Both label and keycap match.
func (m *model) filterPalette() {
	all := m.contextActions()
	if m.palette.query == "" {
		m.palette.items = all
		m.palette.cursor = clamp(m.palette.cursor, 0, max(len(all)-1, 0))
		return
	}

	hay := make([]string, len(all))
	for i, a := range all {
		hay[i] = a.label + " " + a.cap
	}

	items := make([]action, 0, len(all))
	for _, mt := range fuzzy.Find(m.palette.query, hay) {
		items = append(items, all[mt.Index])
	}
	m.palette.items = items
	m.palette.cursor = clamp(m.palette.cursor, 0, max(len(items)-1, 0))
}

// paletteKey reports whether key is the one that opens the palette in any of its layers.
func (m *model) paletteKey(key string) bool {
	for _, l := range []keys.Layer{keys.List, keys.Browser, keys.Leader} {
		switch m.binds.Action(l, key, m.cfg.VimKeys) {
		case keys.Palette, keys.BrowserPalette, keys.LeaderPalette:
			return true
		}
	}
	return false
}

// handlePaletteKey routes a key while the palette is up. It swallows everything, or a key
// would act on the host underneath while you were typing its name.
func (m *model) handlePaletteKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch key := msg.String(); {
	case key == "esc" || m.paletteKey(key):
		m.closePalette()

	case key == "enter":
		if m.palette.cursor >= len(m.palette.items) {
			return m, nil
		}
		a := m.palette.items[m.palette.cursor]
		// Closed before the action runs, so an action opening its own card is not stacked under this one.
		m.closePalette()
		return m.runAction(a)

	case key == "up" || key == "ctrl+p":
		m.palette.cursor--
		m.palette.cursor = clamp(m.palette.cursor, 0, max(len(m.palette.items)-1, 0))

	case key == "down" || key == "ctrl+n":
		m.palette.cursor++
		m.palette.cursor = clamp(m.palette.cursor, 0, max(len(m.palette.items)-1, 0))

	case key == "backspace":
		if r := []rune(m.palette.query); len(r) > 0 {
			m.palette.query = string(r[:len(r)-1])
			m.filterPalette()
		}

	case key == "ctrl+u":
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

// Palette geometry. A fixed row count keeps the card from jumping as the query narrows.
const (
	paletteMaxW   = 52
	paletteFloorW = 24
	paletteRows   = 8
)

func (m *model) paletteInnerW() int {
	room := max(m.width-2*cardPadX-2, paletteFloorW)
	return clamp(paletteMaxW, paletteFloorW, room)
}

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

// actionRow is one row of the palette or the menu: lead bar and label left, key right.
// Laid out against a known width so a long label gives way rather than pushing the key off.
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
