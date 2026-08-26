package tui

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/lipgloss"

	"hop/internal/keys"
)

// menuUI captures alias and items at open time, so a list reordering underneath cannot
// rename the menu or move its selection.
type menuUI struct {
	open   bool
	alias  string
	items  []action
	cursor int
	// row is the screen row the host was drawn on, which is where the menu is anchored.
	row int
}

// openHostMenu opens nothing rather than an empty box when no action is available.
func (m *model) openHostMenu() {
	h, ok := m.selectedHost()
	if !ok {
		return
	}
	items := m.availableHostActions()
	if len(items) == 0 {
		return
	}
	m.menu = menuUI{open: true, alias: h.Alias, items: items, row: m.cursorScreenRow()}
	m.clearStatus()
}

func (m *model) closeMenu() { m.menu = menuUI{} }

// handleMenuKey swallows every key: the keys underneath act on a host.
func (m *model) handleMenuKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	// Dialog-convention keys are not in the registry (see internal/keys).
	switch key := msg.String(); {
	case key == "esc" || key == "q" ||
		m.binds.Action(keys.List, key, m.cfg.VimKeys) == keys.Menu:
		m.closeMenu()

	case key == "enter" || key == "right" || key == "l":
		if m.menu.cursor >= len(m.menu.items) {
			return m, nil
		}
		a := m.menu.items[m.menu.cursor]
		// Closed first: the action may raise a card of its own.
		m.closeMenu()
		return m.runAction(a)

	case key == "up" || key == "k" || key == "ctrl+p":
		m.menu.cursor = clamp(m.menu.cursor-1, 0, max(len(m.menu.items)-1, 0))

	case key == "down" || key == "j" || key == "ctrl+n":
		m.menu.cursor = clamp(m.menu.cursor+1, 0, max(len(m.menu.items)-1, 0))
	}
	return m, nil
}

// Menu geometry: narrow enough to sit over the list without covering the pane.
const (
	menuMaxW   = 40
	menuFloorW = 18
)

func (m *model) menuInnerW() int {
	want := 0
	for _, a := range m.menu.items {
		want = max(want, lipgloss.Width(a.label)+lipgloss.Width(a.keycap())+6)
	}
	room := max(m.width-2*cardPadX-2, menuFloorW)
	return clamp(want, menuFloorW, min(menuMaxW, room))
}

func (m *model) renderMenu(start, rows int) string {
	w := m.menuInnerW()
	var b strings.Builder

	b.WriteString(truncate(titleStyle.Render(stripControl(m.menu.alias)), w))
	b.WriteString("\n")
	b.WriteString(rule(w))
	b.WriteString("\n")

	for i := start; i < min(start+rows, len(m.menu.items)); i++ {
		b.WriteString(padTo(actionRow(m.menu.items[i], i == m.menu.cursor, w), w))
		b.WriteString("\n")
	}

	b.WriteString(rule(w))
	b.WriteString("\n")
	b.WriteString(truncate(keyHint("esc", "close"), w))

	// Sides only: vertical padding would make it read as a card.
	return menuBox.Width(w + 2).Render(b.String())
}

// menuChrome is what the box costs beside its items: two borders, host, two rules, hint.
const menuChrome = 6

// menuPlace keeps the anchored row visible: it is the list that gives way, not the anchor.
func (m *model) menuPlace() (start, rows, y int) {
	const top = 1 // the first body row, under the screen header

	below := m.menuBottom() - (m.menu.row + 1)
	above := m.menu.row - top
	rows = clamp(len(m.menu.items), 1, max(max(below, above)-menuChrome, 1))

	h := rows + menuChrome
	if below >= h {
		y = m.menu.row + 1
	} else {
		y = max(m.menu.row-h, top)
	}

	if m.menu.cursor >= start+rows {
		start = m.menu.cursor - rows + 1
	}
	return start, rows, y
}

// menuBottom keeps the menu clear of the status bar and the footer.
func (m *model) menuBottom() int { return max(m.height-2, 1) }

// menuAt indents by two cells to leave the anchored row's accent bar showing.
func (m *model) menuAt() (card string, x, y int) {
	start, rows, y := m.menuPlace()
	return clampLines(m.renderMenu(start, rows), m.width), 2, y
}

// cursorScreenRow mirrors renderList's scroll arithmetic (see listRowAt).
func (m *model) cursorScreenRow() int {
	rows := m.listRows()
	return m.listFirstRow() + m.cursorRow() - m.listStart(rows)
}
