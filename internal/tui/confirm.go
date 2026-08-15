package tui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"hop/internal/store"
)

// confirmUI is the delete-confirmation card's own state: which host the cursor was on
// when it was armed, so the question still names the right one after the list underneath
// has scrolled or been filtered.
type confirmUI struct {
	open  bool
	alias string
}

// openConfirmDelete arms the card for h, capturing the alias now rather than re-reading
// the cursor at delete time: the host removed is the one the question named.
func (m *model) openConfirmDelete(h store.Host) {
	m.confirm = confirmUI{open: true, alias: h.Alias}
	m.status = ""
}

// closeConfirm dismisses the card, deciding nothing.
func (m *model) closeConfirm() {
	m.confirm = confirmUI{}
}

// handleConfirmKey routes a key while the card is up, swallowing everything: a yes/no
// that let keys fall through could act on a host you never confirmed. Only an explicit
// yes deletes; anything that is neither yes nor cancel keeps the question up.
func (m *model) handleConfirmKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "y", "enter":
		m.confirmDelete()
	case "n", "esc", "q":
		m.closeConfirm()
	}
	return m, nil
}

// confirmDelete carries out the deletion the card was armed for. A host with a live
// connection is torn down first, so its session and panes are not orphaned against a row
// that no longer exists. A failed delete leaves the list untouched and says why.
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

	// reloadHosts re-clamps the cursor through applyFilter, settling it onto the row that
	// took the deleted host's place.
	m.reloadHosts()
	m.closeConfirm()
	m.setStatus(statusOK, "deleted %s", alias)
}

// Card geometry. Narrower than the settings popover — one short question, not a form —
// and shrinking to the window the same way, with a floor below which it truncates.
const (
	confirmMaxW   = 44 // content width, borders and padding excluded
	confirmFloorW = 20
)

// confirmInnerW is the width available to a rendered line: the box less its border and
// padding, held to the window so the card is never wider than the screen.
func (m *model) confirmInnerW() int {
	room := max(m.width-2*cardPadX-2, confirmFloorW)
	return clamp(confirmMaxW, confirmFloorW, room)
}

// renderConfirm draws the card: a title, the question naming the host, and a two-key
// hint. Every line is truncated, since a modal that wraps spills outside its border.
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
