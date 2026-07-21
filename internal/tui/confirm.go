package tui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"hop/internal/store"
)

// confirmUI is the delete-confirmation card's own state. There is exactly one
// thing to remember while it is up — which host the cursor was on when it was
// armed — because a destructive action has to name what it is about to destroy
// even after the list underneath it has scrolled or been filtered.
type confirmUI struct {
	open  bool
	alias string
}

// openConfirmDelete arms the card for h. It captures the alias now rather than
// re-reading the cursor at delete time, so the host that gets removed is always
// the one the question named — not whatever happens to be under the cursor by
// the time you answer.
func (m *model) openConfirmDelete(h store.Host) {
	m.confirm = confirmUI{open: true, alias: h.Alias}
	m.status = ""
}

// closeConfirm dismisses the card, deciding nothing.
func (m *model) closeConfirm() {
	m.confirm = confirmUI{}
}

// handleConfirmKey routes a key while the card is up. Like every modal here it
// swallows everything: a "yes/no" that let unhandled keys fall through to the
// list behind it could act on a host you never confirmed. Only an explicit yes
// deletes; anything that is not a yes and not a cancel simply keeps the question
// on screen.
func (m *model) handleConfirmKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "y", "enter":
		m.confirmDelete()
	case "n", "esc", "q":
		m.closeConfirm()
	}
	return m, nil
}

// confirmDelete carries out the deletion the card was armed for. A host with a
// live connection is torn down first, so the session and its panes go away
// cleanly instead of being orphaned against a row that no longer exists. The
// store is the source of truth, so a failed delete leaves the list untouched and
// says why; a successful one reloads the list, re-clamps the cursor onto whatever
// row took the deleted host's place, and reports what went.
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

	// reloadHosts re-clamps the cursor through applyFilter, which is what settles it
	// onto the row that took the deleted host's place.
	m.reloadHosts()
	m.closeConfirm()
	m.setStatus(statusOK, "deleted %s", alias)
}

// Card geometry. The delete card is deliberately narrower than the settings
// popover — it holds one short question, not a form — and shrinks to the window
// the same way, with a floor below which it truncates rather than trying to fit.
const (
	confirmMaxW   = 44 // content width, borders and padding excluded
	confirmFloorW = 20
)

// confirmInnerW is the width available to a rendered line: the box minus its
// border and padding, held to the window so the card can never be wider than the
// screen it is centered on.
func (m *model) confirmInnerW() int {
	room := max(m.width-2*cardPadX-2, confirmFloorW)
	return clamp(confirmMaxW, confirmFloorW, room)
}

// renderConfirm draws the card: a title, the question naming the host in the
// accent color, and a two-key hint. Every line is truncated to the inner width,
// because a modal that wraps spills outside its own border.
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
