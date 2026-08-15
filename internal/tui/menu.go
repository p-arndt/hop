package tui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// menuKey opens the context menu on the host under the cursor. space is the key nothing
// else in the list wants, and it reads as "act on this one" rather than as a command of
// its own.
const menuKey = " "

// menuKeyName is how that key is written on a legend: the character it sends is a blank,
// which no keycap can show.
const menuKeyName = "space"

// menuUI is the context menu's state: the host it was opened on, what can be done to it,
// and which of those is selected.
//
// The alias is captured at open time, like the delete confirmation's, so the menu keeps
// naming the host it was opened about even if the list underneath reorders. The items are
// captured with it: an action list that changed under the cursor would move the selection
// without a key being pressed.
type menuUI struct {
	open   bool
	alias  string
	items  []action
	cursor int
	// row is the screen row the host was drawn on, which is where the menu is anchored.
	row int
}

// openHostMenu raises the menu on the host under the cursor. A host with nothing
// available — which the registries make impossible today, but the predicates could —
// opens nothing rather than an empty box.
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

// handleMenuKey routes a key while the menu is up, swallowing everything for the reason
// the confirmation card does: the keys underneath act on a host, and this menu is the
// question of which action to take on one.
func (m *model) handleMenuKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc", "q", menuKey:
		m.closeMenu()

	case "enter", "right", "l":
		if m.menu.cursor >= len(m.menu.items) {
			return m, nil
		}
		a := m.menu.items[m.menu.cursor]
		// Closed first: the action may raise a card of its own.
		m.closeMenu()
		return m.runAction(a)

	case "up", "k", "ctrl+p":
		m.menu.cursor = clamp(m.menu.cursor-1, 0, max(len(m.menu.items)-1, 0))

	case "down", "j", "ctrl+n":
		m.menu.cursor = clamp(m.menu.cursor+1, 0, max(len(m.menu.items)-1, 0))
	}
	return m, nil
}

// Menu geometry: narrow enough to sit over the list without covering the pane, and
// bordered like the cards so it reads as hop's own chrome rather than as content.
const (
	menuMaxW   = 40
	menuFloorW = 18
)

// menuInnerW is the width of a menu row: the widest label-plus-key it holds, inside the
// bounds, so the box is as small as the list it names.
func (m *model) menuInnerW() int {
	want := 0
	for _, a := range m.menu.items {
		want = max(want, lipgloss.Width(a.label)+lipgloss.Width(a.keycap())+6)
	}
	room := max(m.width-2*cardPadX-2, menuFloorW)
	return clamp(want, menuFloorW, min(menuMaxW, room))
}

// renderMenu draws the menu: the host it belongs to, then the slice of its actions the
// window has room for, with their keys.
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

	// Padded on the sides only: a menu is a list of rows, and blank rows above and below
	// them would make it read as a card.
	return menuBox.Width(w + 2).Render(b.String())
}

// menuChrome is what the box costs beside its items: two borders, the host, the two
// rules and the hint line.
const menuChrome = 6

// menuPlace decides where the menu goes and how much of it is shown: under the row it
// belongs to when there is room below, above that row when there is more room there, and
// scrolled to the selection when neither side can hold the whole list.
//
// Anchoring is the point of this menu — it is the answer to "what can I do to *that*" —
// so the row it names stays visible, and it is the list that gives way.
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

// menuBottom is the last row the menu may reach: the window less the status bar and the
// footer. Those two rows are the ones saying where you are and what to press, which is
// the last thing to cover with a box that is itself a list of what to press.
func (m *model) menuBottom() int { return max(m.height-2, 1) }

// menuAt is the menu and the top-left cell to composite it at. The indent leaves the
// accent bar of the row it belongs to showing to its left.
func (m *model) menuAt() (card string, x, y int) {
	start, rows, y := m.menuPlace()
	return clampLines(m.renderMenu(start, rows), m.width), 2, y
}

// cursorScreenRow is the screen row the selected host is drawn on: renderList's
// bookkeeping, the same way listRowAt runs it backwards for the mouse.
func (m *model) cursorScreenRow() int {
	rows := m.listRows()
	return m.listFirstRow() + m.cursorRow() - m.listStart(rows)
}
