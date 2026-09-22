package tui

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/lipgloss"
	"github.com/sahilm/fuzzy"

	"hop/internal/keys"
)

// picker is the typing and moving a filtered card does: the palette's and the switcher's.
type picker struct {
	query  string
	cursor int
}

// key applies one typing or moving key against n rows, reporting whether the query changed
// so the caller re-filters. esc and enter are the caller's: each card means its own by them.
func (p *picker) key(msg tea.KeyPressMsg, n int) (changed bool) {
	switch msg.String() {
	case "up", "ctrl+p":
		p.cursor--
	case "down", "ctrl+n":
		p.cursor++
	case "backspace":
		if r := []rune(p.query); len(r) > 0 {
			p.query = string(r[:len(r)-1])
			changed = true
		}
	case "ctrl+u":
		p.query, changed = "", p.query != ""
	default:
		if msg.Text != "" {
			p.query += msg.Text
			changed = true
		}
	}
	p.cursor = clamp(p.cursor, 0, max(n-1, 0))
	return changed
}

// paletteUI is the command palette's state. The matches are held rather than recomputed
// at render time, since the cursor indexes them.
type paletteUI struct {
	open bool
	picker
	items []action
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
func (m *model) handlePaletteKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
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

	default:
		if m.palette.key(msg, len(m.palette.items)) {
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
	return m.renderPicker("ACTIONS", m.palette.picker, len(m.palette.items), "run",
		func(i int, selected bool, w int) string { return actionRow(m.palette.items[i], selected, w) })
}

// renderPicker draws a filtered card: a title, the query, a window of n rows kept around
// the cursor, and the keys that work it.
func (m *model) renderPicker(title string, p picker, n int, verb string, row func(i int, selected bool, w int) string) string {
	w := m.paletteInnerW()
	var b strings.Builder

	b.WriteString(truncate(titleStyle.Render(title), w))
	b.WriteString("\n\n")

	query := accentText.Render("> ") + stripControl(p.query) + accentText.Render("▏")
	b.WriteString(truncate(query, w))
	b.WriteString("\n\n")

	if n == 0 {
		b.WriteString(truncate(faint.Render("nothing matches "+stripControl(p.query)), w))
		b.WriteString("\n")
	}

	start := 0
	if p.cursor >= paletteRows {
		start = p.cursor - paletteRows + 1
	}
	for i := start; i < min(start+paletteRows, n); i++ {
		b.WriteString(padTo(row(i, i == p.cursor, w), w))
		b.WriteString("\n")
	}

	b.WriteString("\n")
	b.WriteString(truncate(keyHint("enter", verb)+"  "+keyHint("↑↓", "move")+"  "+keyHint("esc", "close"), w))

	return cardBox.Width(w + 2*cardPadX).Render(b.String())
}

// actionRow is one row of the palette or the menu: lead bar and label left, key right.
func actionRow(a action, selected bool, w int) string {
	return pickRow(a.label, kc(a.keycap()), selected, w)
}

// pickRow lays a label and an already-styled right side against a known width, so a long
// label gives way rather than pushing the right side off.
func pickRow(label, right string, selected bool, w int) string {
	lead, styled := "  ", dimStyle.Render(label)
	if selected {
		lead, styled = selBar+" ", selectedAliasStyle.Render(label)
	}

	room := max(w-lipgloss.Width(lead)-lipgloss.Width(right)-1, 1)
	left := lead + truncate(styled, room)
	gap := max(w-lipgloss.Width(left)-lipgloss.Width(right), 1)
	return left + strings.Repeat(" ", gap) + right
}
