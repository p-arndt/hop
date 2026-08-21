package tui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"hop/internal/store"
)

// confirmUI is the delete-confirmation card's state: the alias captured when it was armed,
// so the question still names the right host after the list underneath has moved.
type confirmUI struct {
	open  bool
	alias string
}

func (m *model) openConfirmDelete(h store.Host) {
	m.confirm = confirmUI{open: true, alias: h.Alias}
	m.status = ""
}

func (m *model) closeConfirm() {
	m.confirm = confirmUI{}
}

// handleConfirmKey routes a key while the card is up, swallowing everything: a key falling
// through could act on a host you never confirmed.
func (m *model) handleConfirmKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "y", "enter":
		m.confirmDelete()
	case "n", "esc", "q":
		m.closeConfirm()
	}
	return m, nil
}

// confirmDelete carries out the deletion. A live connection is torn down first, so its
// session and panes are not orphaned against a row that no longer exists.
func (m *model) confirmDelete() {
	alias := m.confirm.alias

	if m.sessions[alias] != nil {
		m.disconnect(alias)
	}

	if err := m.st.Delete(alias); err != nil {
		m.setStatus(statusErr, "delete host: %v", err)
		m.closeConfirm()
		return
	}

	// reloadHosts re-clamps the cursor through applyFilter, onto the row that took its place.
	m.reloadHosts()
	m.closeConfirm()
	m.setStatus(statusOK, "deleted %s", alias)
}

// Card geometry.
const (
	confirmMaxW   = 44 // content width, borders and padding excluded
	confirmFloorW = 20
)

func (m *model) confirmInnerW() int {
	room := max(m.width-2*cardPadX-2, confirmFloorW)
	return clamp(confirmMaxW, confirmFloorW, room)
}

// renderConfirm draws the card; every line is truncated, since a wrapping modal spills past its border.
func (m *model) renderConfirm() string {
	w := m.confirmInnerW()
	var b strings.Builder

	b.WriteString(truncate(titleStyle.Render("DELETE HOST"), w))
	b.WriteString("\n\n")

	body := dimStyle.Render("Delete ") + accentText.Render(m.confirm.alias) +
		dimStyle.Render("? This can't be undone.")
	b.WriteString(truncate(body, w))
	b.WriteString("\n\n")

	b.WriteString(truncate(keyHint("y", "delete")+"  "+keyHint("n", "cancel"), w))

	return cardBox.Width(w + 2*cardPadX).Render(b.String())
}
