package tui

// Landings: the tail of each asynchronous open, merged into a session that may have moved
// on while it was away.

import (
	"errors"

	tea "github.com/charmbracelet/bubbletea"

	"hop/internal/sshx"
)

func (m *model) shellLanded(msg connectedMsg) (tea.Model, tea.Cmd) {
	delete(m.connecting, msg.alias)
	if msg.err != nil {
		// First contact: ask before trusting, carrying the shell intent into the retry.
		var unknown *sshx.UnknownHostKeyError
		if errors.As(msg.err, &unknown) {
			m.openHostKeyConfirm(msg.alias, unknown, hostKeyShell, msg.extra)
			return m, nil
		}
		// Drop the plan: a failed reconnect must not restore tabs on a later ordinary
		// connect. The host-key path returns first, since that dial is replayed with it.
		m.dropPlan(msg.alias)
		// Dismissing the authentication card already said so.
		if errors.Is(msg.err, sshx.ErrAuthCanceled) {
			return m, nil
		}
		m.setStatus(statusErr, "connect %s failed: %v", msg.alias, msg.err)
		return m, nil
	}
	// Merge into any existing session so what is open there survives.
	s := m.sessions[msg.alias]
	if s == nil {
		s = &session{}
		m.sessions[msg.alias] = s
	}
	if msg.client != nil {
		s.client = msg.client
	}
	s.shells = append(s.shells, msg.tab)
	s.activeSh = len(s.shells) - 1
	m.armClipboard(msg.tab.pane)
	m.st.Touch(msg.alias)
	m.reloadHosts()

	cmds := []tea.Cmd{waitShellCmd(msg.alias, msg.tab.id, msg.tab.sess)}
	if msg.client != nil {
		cmds = append(cmds, watchClientCmd(msg.alias, msg.client))
	}

	// A restored shell lands quietly: the reconnect already decided where the keyboard goes.
	if !msg.restore {
		m.focusShell(msg.alias)
		// First contact: say which key was just trusted, so TOFU is at least visible.
		if msg.client != nil && msg.client.NewHostKey != "" {
			m.setStatus(statusWarn, "%s: new host key trusted (%s)", msg.alias, msg.client.NewHostKey)
		} else {
			m.setStatus(statusOK, "connected to %s", msg.alias)
		}
		if cmd := m.applyPlan(msg.alias); cmd != nil {
			cmds = append(cmds, cmd)
		}
	} else {
		m.resizeShells(s)
	}
	return m, tea.Batch(cmds...)
}

func (m *model) shellExited(msg shellExitedMsg) (tea.Model, tea.Cmd) {
	s := m.sessions[msg.alias]
	if s == nil {
		return m, nil
	}
	// A cut-off shell is not an exit: dropping the last tab would close the session and
	// take the reconnect offer with it.
	if s.deadConnection() {
		m.markDead(msg.alias, lostReason(s))
		return m, nil
	}
	if !s.dropShell(msg.id) {
		return m, nil
	}
	m.resizeShells(s)
	if len(s.shells) > 0 {
		return m, nil
	}
	// Keep the session alive only for what is still open on its connection.
	if s.browser == nil && len(s.editors) == 0 && len(s.tunnels) == 0 {
		s.close()
		delete(m.sessions, msg.alias)
		if m.active == msg.alias {
			m.leaveAll()
		}
		return m, nil
	}
	if m.active == msg.alias && m.focused() {
		m.mode = modeList
		if s.browser != nil {
			m.mode = modeBrowser
		}
	}
	return m, nil
}

func (m *model) browserLanded(msg browserOpenedMsg) (tea.Model, tea.Cmd) {
	delete(m.connecting, msg.alias)
	if msg.err != nil {
		var unknown *sshx.UnknownHostKeyError
		if errors.As(msg.err, &unknown) {
			m.openHostKeyConfirm(msg.alias, unknown, hostKeyBrowser, false)
			return m, nil
		}
		m.dropPlan(msg.alias)
		if errors.Is(msg.err, sshx.ErrAuthCanceled) {
			return m, nil
		}
		m.setStatus(statusErr, "sftp %s failed: %v", msg.alias, msg.err)
		return m, nil
	}
	s := m.sessions[msg.alias]
	if s == nil {
		s = &session{}
		m.sessions[msg.alias] = s
	}
	if s.browser != nil {
		s.browser.Close()
	}
	s.browser = msg.browser
	if msg.client != nil {
		s.client = msg.client
	}
	m.st.Touch(msg.alias)
	m.reloadHosts()

	var cmds []tea.Cmd
	if msg.client != nil {
		cmds = append(cmds, watchClientCmd(msg.alias, msg.client))
	}

	// A restored browser does not take the keyboard; the shell restored first has it.
	if !msg.restore {
		m.active = msg.alias
		m.mode = modeBrowser
		if msg.client != nil && msg.client.NewHostKey != "" {
			m.setStatus(statusWarn, "%s: new host key trusted (%s)", msg.alias, msg.client.NewHostKey)
		} else {
			m.setStatus(statusOK, "sftp %s", msg.alias)
		}
		if cmd := m.applyPlan(msg.alias); cmd != nil {
			cmds = append(cmds, cmd)
		}
	}
	// Relayout before any resize: the new tree column changes what resizeAll measures.
	m.relayout()
	return m, tea.Batch(cmds...)
}

func (m *model) editorLanded(msg editorOpenedMsg) (tea.Model, tea.Cmd) {
	if msg.err != nil {
		m.setStatus(statusErr, "edit %s failed: %v", msg.alias, msg.err)
		m.abandonSplit(msg.alias)
		return m, nil
	}
	s := m.sessions[msg.alias]
	if s == nil {
		// The session went away while the editor was starting.
		msg.tab.pane.Close()
		return m, nil
	}
	s.editors = append(s.editors, msg.tab)
	// openFile already moved the focused half when opening beside another file.
	s.setEditor(len(s.editors) - 1)
	m.armClipboard(msg.tab.pane)
	m.active = msg.alias
	m.mode = modeEditor
	m.clearStatus()
	m.relayout()
	ew, eh := m.editorSize(s)
	msg.tab.pane.Resize(ew, eh)
	return m, waitEditorCmd(msg.alias, msg.tab.id, msg.tab.sess)
}

// abandonSplit undoes a split opened at request time (see openFile), which would otherwise
// draw the same file in both halves.
func (m *model) abandonSplit(alias string) {
	s := m.sessions[alias]
	if s == nil || !s.split || s.splitEd != s.activeEd {
		return
	}
	s.collapseSplit()
	m.relayout()
}
