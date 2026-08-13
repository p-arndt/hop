package tui

// What happens when an asynchronous open lands. Every one of these is the tail of a
// tea.Cmd that went off to dial, start a channel or open a file, and each has the
// same two problems: the world may have moved on while it was away (the session was
// closed, the host deleted, the connection dropped), and whatever it opened has to be
// merged into a session that may meanwhile hold other tabs.

import (
	"errors"

	tea "github.com/charmbracelet/bubbletea"

	"hop/internal/sshx"
)

// shellLanded merges a newly-started shell into its host's session and focuses
// it.
func (m *model) shellLanded(msg connectedMsg) (tea.Model, tea.Cmd) {
	delete(m.connecting, msg.alias)
	if msg.err != nil {
		// A first-contact host key is no longer trusted silently: pause and ask,
		// carrying the shell intent (another shell vs a host's first) into the retry.
		var unknown *sshx.UnknownHostKeyError
		if errors.As(msg.err, &unknown) {
			m.openHostKeyConfirm(msg.alias, unknown, hostKeyShell, msg.extra)
			return m, nil
		}
		// The dial is over, so a reconnect's plan has nothing left to land on. Dropping
		// it here keeps a failed reconnect from restoring tabs on some later, ordinary
		// connect to the same host. (The host-key path above returns before this: that
		// dial is about to be replayed, and the plan is what it replays with.)
		m.dropPlan(msg.alias)
		// Dismissing the authentication card already said so; repeating it as a
		// connect failure would make the user's own choice look like a fault.
		if errors.Is(msg.err, sshx.ErrAuthCanceled) {
			return m, nil
		}
		m.setStatus(statusErr, "connect %s failed: %v", msg.alias, msg.err)
		return m, nil
	}
	// Merge the new shell into any existing session (a browser-only one, or one
	// that already has shells) so what is open there survives.
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
		// A new connection: watch it, so its loss is noticed rather than leaving a
		// pane that has quietly stopped updating.
		cmds = append(cmds, watchClientCmd(msg.alias, msg.client))
	}

	// A shell a reconnect is putting back lands quietly: the reconnect has already
	// decided where the keyboard goes, and its own status line says what came back.
	if !msg.restore {
		m.focusShell(msg.alias)
		// First contact with this host: say which key was just trusted, so TOFU is
		// at least visible — a fingerprint you can compare beats a silent accept.
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

// shellExited drops the tab of a shell that has ended, and decides what is left
// of the session behind it.
func (m *model) shellExited(msg shellExitedMsg) (tea.Model, tea.Cmd) {
	s := m.sessions[msg.alias]
	if s == nil {
		return m, nil
	}
	// A shell whose connection has already gone did not exit — it was cut off, along
	// with every other channel on that connection. Treating it as an exit would drop
	// the tab, and the last one would close the session and take the reconnect offer
	// with it. So the loss is recorded instead, and the tabs stay as they were: the
	// last screen the host drew, which is what you want to read when a link drops.
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
	// The last shell exited. Keep the session alive only for what is still open
	// on its connection; with nothing left, the connection is done — closing it
	// is what "exit" meant, and the host goes back to idle in the list.
	if s.browser == nil && len(s.editors) == 0 && len(s.tunnels) == 0 {
		s.close()
		delete(m.sessions, msg.alias)
		if m.active == msg.alias {
			m.leaveAll()
		}
		return m, nil
	}
	if m.active == msg.alias && m.focused() {
		// The shell that held the keyboard is gone; the browser still on the same
		// connection is the only thing left to hand it to.
		m.mode = modeList
		if s.browser != nil {
			m.mode = modeBrowser
		}
	}
	return m, nil
}

// browserLanded attaches a newly-opened SFTP browser to its host's session and
// shows it.
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

	// A browser a reconnect is reattaching goes back on the session without taking
	// the keyboard — the shell the reconnect landed first still has it.
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
	msg.browser.Resize(m.paneW, m.paneH)
	return m, tea.Batch(cmds...)
}

// editorLanded shows a newly-started remote editor as a tab.
func (m *model) editorLanded(msg editorOpenedMsg) (tea.Model, tea.Cmd) {
	if msg.err != nil {
		m.setStatus(statusErr, "edit %s failed: %v", msg.alias, msg.err)
		return m, nil
	}
	s := m.sessions[msg.alias]
	if s == nil {
		// The session went away while the editor was starting.
		msg.tab.pane.Close()
		return m, nil
	}
	s.editors = append(s.editors, msg.tab)
	s.activeEd = len(s.editors) - 1
	m.armClipboard(msg.tab.pane)
	m.active = msg.alias
	m.mode = modeEditor
	m.clearStatus()
	ew, eh := m.editorSize()
	msg.tab.pane.Resize(ew, eh)
	return m, waitEditorCmd(msg.alias, msg.tab.id, msg.tab.sess)
}
