package tui

// The session bar is the header row: the host in front and a chip for everything open on
// it, then the other connected hosts. Every chip is a way back, so it is laid out once, here,
// and both the renderer and the pointer read the same cells.

import (
	"path"
	"slices"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/lipgloss"
)

// barChipMax keeps one long name from taking the whole bar.
const barChipMax = 20

// barCell is one piece of the session bar at its column. A cell that jumps lands on t; the
// "+N" standing for what did not fit opens the switcher, where all of it is.
type barCell struct {
	x, w int
	text string
	t    target
	jump bool
	more bool
}

// sessionBar lays the header out for this width. With a host in front: its alias and its
// chips on the left, the other hosts on the right. With none: every connected host. A
// transient status takes the right side while it lasts; the far end of each side gives way
// first, the keyboard's own chip last.
func (m *model) sessionBar() []barCell {
	var left, right []barCell
	pinned := -1
	s := m.sessions[m.active]
	if m.active != "" && s != nil {
		left = append(left, barCell{text: headerBadge.Render(stripControl(m.active)), t: target{alias: m.active}, jump: true})
		here, ok := m.here()
		for _, t := range m.openTargets(m.active) {
			style := tabInactive
			if ok && t == here {
				style, pinned = tabActive, len(left)
			}
			left = append(left, barCell{text: style.Render(m.chipLabel(t)), t: t, jump: true})
		}
		for _, alias := range m.sessionAliases() {
			if alias != m.active {
				right = append(right, m.hostCell(alias))
			}
		}
	} else {
		left = append(left, barCell{text: headerBadge.Render("hop")})
		for _, alias := range m.sessionAliases() {
			left = append(left, m.hostCell(alias))
		}
	}

	if st := m.styledStatus(); st != "" {
		right = []barCell{{text: st}}
	}
	for i := range left {
		left[i].w = lipgloss.Width(left[i].text)
	}
	for i := range right {
		right[i].w = lipgloss.Width(right[i].text)
	}

	// The host in front comes first: the other side is held to what is left, and to a third
	// of the row when both sides want more than there is.
	rightRoom := 0
	if len(right) > 0 {
		rightRoom = min(rowWidth(right), max(m.width/3, right[0].w))
		if m.status != "" {
			rightRoom = right[0].w
		}
	}
	left = fitCells(left, pinned, max(m.width-rightRoom-barGroupGap, 0))
	right = fitCells(right, -1, max(m.width-rowWidth(left)-barGroupGap, 0))

	x := 0
	for i := range left {
		left[i].x = x
		x += left[i].w + 1
	}
	x = m.width - rowWidth(right)
	for i := range right {
		right[i].x = x
		x += right[i].w + 1
	}
	return append(left, right...)
}

// barGroupGap is the least room between the two sides.
const barGroupGap = 2

// hostCell is a connected host on the bar: its dot and its alias.
func (m *model) hostCell(alias string) barCell {
	return barCell{text: m.dotFor(alias) + " " + dimStyle.Render(stripControl(alias)), t: target{alias: alias}, jump: true}
}

// chipLabel is t's chip: its glyph and the last part of what makes it this one.
func (m *model) chipLabel(t target) string {
	s := m.sessions[t.alias]
	var l string
	switch t.kind {
	case targetShell:
		for i, sh := range s.shells {
			if sh.id == t.id {
				l = "$" + strconv.Itoa(i+1)
				if cwd := sh.pane.Cwd(); cwd != "" {
					l += " " + path.Base(cwd)
				}
			}
		}
	case targetBrowser:
		l = "▤ " + path.Base(s.browser.Path())
	case targetEditor:
		if i := s.findEditorID(t.id); i >= 0 {
			l = "✎ " + s.editors[i].name
		}
	case targetDrawer:
		l = "▭ terminal"
	case targetTunnels:
		l = "⇄ " + strconv.Itoa(len(s.tunnels))
	}
	return truncate(stripControl(l), barChipMax)
}

// rowWidth is what cells take side by side, a column apart.
func rowWidth(cells []barCell) int {
	w := 0
	for _, c := range cells {
		w += c.w
	}
	return w + max(len(cells)-1, 0)
}

// fitCells drops cells from the far end until they fit room, standing a "+N" in for them.
// The first cell (what the side is about) and the pinned one go last; if even the first
// cannot fit, it is cut rather than lost.
func fitCells(cells []barCell, pinned, room int) []barCell {
	if len(cells) == 0 || rowWidth(cells) <= room {
		return cells
	}
	kept := append([]barCell(nil), cells...)
	dropped := 0
	more := func() barCell {
		t := faint.Render("+" + strconv.Itoa(dropped))
		return barCell{text: t, w: lipgloss.Width(t), more: true}
	}
	for rowWidth(append(kept, more())) > room {
		drop := -1
		for i := len(kept) - 1; i > 0; i-- {
			if i != pinned {
				drop = i
				break
			}
		}
		if drop < 0 {
			break
		}
		kept = slices.Delete(kept, drop, drop+1)
		if drop < pinned {
			pinned--
		}
		dropped++
	}
	if dropped > 0 && rowWidth(append(kept, more())) <= room {
		kept = append(kept, more())
	}
	if rowWidth(kept) > room {
		kept[0].text = truncate(kept[0].text, room)
		kept[0].w = lipgloss.Width(kept[0].text)
		kept = kept[:1]
	}
	return kept
}

// renderHeader draws the session bar at the columns sessionBar gave it.
func (m *model) renderHeader() string {
	var b strings.Builder
	col := 0
	for _, c := range m.sessionBar() {
		if c.x > col {
			b.WriteString(strings.Repeat(" ", c.x-col))
			col = c.x
		}
		b.WriteString(c.text)
		col += c.w
	}
	if col < m.width {
		b.WriteString(strings.Repeat(" ", m.width-col))
	}
	return truncate(b.String(), m.width)
}

// barCellAt is the cell of the session bar under column x.
func (m *model) barCellAt(x int) (barCell, bool) {
	for _, c := range m.sessionBar() {
		if x >= c.x && x < c.x+c.w {
			return c, true
		}
	}
	return barCell{}, false
}

// clickBar lands where the clicked chip or host says, exactly as the switcher would.
func (m *model) clickBar(msg mouseEvt) (tea.Model, tea.Cmd) {
	if msg.Button != tea.MouseLeft || msg.action != actPress {
		return m, nil
	}
	c, ok := m.barCellAt(msg.X)
	switch {
	case !ok:
	case c.more:
		m.openHostSwitch()
	case c.jump:
		m.clearSelection()
		return m, m.jumpTo(c.t)
	}
	return m, nil
}
