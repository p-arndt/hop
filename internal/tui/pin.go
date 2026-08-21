package tui

// Pinning lifts a host into a PINNED section ordered by hand; the store owns both facts
// (store.SetPinned / store.MovePin) and the reload here keeps the cursor on the same host.

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

// movePin moves the pinned host under the cursor delta places within the PINNED section.
// An unpinned host says so rather than doing nothing silently.
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
