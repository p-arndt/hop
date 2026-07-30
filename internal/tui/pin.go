package tui

// Pinning a host lifts it out of the frecency order into a PINNED section at the
// top of the sidebar, in an order the user sets by hand. The store owns both facts
// (see store.SetPinned / store.MovePin); this file is the list's half of it — a key
// per verb, then a reload that re-reads the new order and keeps the cursor on the
// host you were standing on, so a pin or a move is something you watch happen
// rather than something you have to go and find.

// togglePin pins or unpins the host under the cursor.
func (m *model) togglePin() {
	h, ok := m.selectedHost()
	if !ok {
		return
	}
	if m.st == nil {
		return
	}
	if err := m.st.SetPinned(h.Alias, !h.Pinned); err != nil {
		m.setStatus(statusErr, "pin %s: %v", h.Alias, err)
		return
	}
	m.reloadHostsSelecting(h.Alias)

	if h.Pinned {
		m.setStatus(statusOK, "unpinned %s", h.Alias)
		return
	}
	m.setStatus(statusOK, "pinned %s", h.Alias)
}

// movePin moves the pinned host under the cursor delta places within the PINNED
// section — -1 up, +1 down. On an unpinned host it says so rather than doing
// nothing silently: shift+j on a host in the HOSTS section is a reasonable thing to
// try, and the answer is "pin it first". Hitting either end of the section is a
// no-op, the way a cursor at the top of the list is.
func (m *model) movePin(delta int) {
	h, ok := m.selectedHost()
	if !ok {
		return
	}
	if m.st == nil {
		return
	}
	if !h.Pinned {
		m.setStatus(statusWarn, "%s is not pinned — p pins it", h.Alias)
		return
	}
	moved, err := m.st.MovePin(h.Alias, delta)
	if err != nil {
		m.setStatus(statusErr, "move %s: %v", h.Alias, err)
		return
	}
	if !moved {
		return
	}
	m.reloadHostsSelecting(h.Alias)
	m.clearStatus()
}
