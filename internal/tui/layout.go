package tui

// sidebarWidth is the host list's preferred width. It still yields to half the
// window on a narrow one.
const sidebarWidth = 32

// chromeRows is what the header and footer cost the body.
const chromeRows = 2

// recomputeLayout derives the left/right pane inner sizes from the window size.
func (m *model) recomputeLayout() {
	if m.width <= 0 || m.height <= 0 {
		return
	}
	// listWidth is the sidebar's *outer* width, borders included; the right pane
	// gets the rest of the row, less the two columns its own border takes.
	m.paneW = max(m.width-m.listWidth()-2, 10)
	m.paneH = max(m.bodyHeight()-2, 3)
}

// bodyHeight is the rows left for the two panes once the header and footer have
// taken theirs.
func (m *model) bodyHeight() int {
	return max(m.height-chromeRows, 3)
}

// listWidth is the outer width of the host list, borders included.
func (m *model) listWidth() int {
	return clamp(sidebarWidth, 16, max(m.width/2, 16))
}

// editorSize is the terminal size an editor pane gets: the right pane, less the
// row the tab bar sits on.
func (m *model) editorSize() (int, int) {
	return m.paneW, max(m.paneH-1, 1)
}

// shellSize is the terminal size a shell pane gets on a host with n shells open.
// A lone shell gets the whole right pane — the tab strip only appears once there
// is a second shell to switch to, and only then does it cost a row.
func (m *model) shellSize(n int) (int, int) {
	h := m.paneH
	if n > 1 {
		h--
	}
	return m.paneW, max(h, 1)
}

// resizeShells re-lays out every shell of s for the current pane size and tab
// strip. It runs whenever the shell count changes, because the strip appearing
// (or going away) resizes the panes underneath it.
func (m *model) resizeShells(s *session) {
	w, h := m.shellSize(len(s.shells))
	for _, sh := range s.shells {
		sh.pane.Resize(w, h)
	}
}

// resizeAll re-lays out every live pane, browser and editor for the current
// window. Hidden tabs are resized too, so one switched to after a window change
// is already laid out for it.
func (m *model) resizeAll() {
	ew, eh := m.editorSize()
	for _, s := range m.sessions {
		m.resizeShells(s)
		if s.browser != nil {
			s.browser.Resize(m.paneW, m.paneH)
		}
		for _, e := range s.editors {
			e.pane.Resize(ew, eh)
		}
	}
}

// listRows approximates the host rows visible in the list pane, which is the
// step ctrl+f/ctrl+b move by. It mirrors renderList's bookkeeping: the body loses
// the header and footer, the border takes two more, then the HOSTS title and
// (when present) the filter prompt.
func (m *model) listRows() int {
	r := m.height - 5
	if m.filtering || m.filter != "" {
		r--
	}
	return max(r, 1)
}

// halfPage is the ctrl+d/ctrl+u step: half a viewport, but never zero.
func (m *model) halfPage() int {
	return max(m.listRows()/2, 1)
}
