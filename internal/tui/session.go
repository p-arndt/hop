package tui

import (
	"strconv"

	tea "github.com/charmbracelet/bubbletea"

	"hop/internal/filebrowser"
	"hop/internal/sshx"
	"hop/internal/store"
	"hop/internal/terminal"
)

// session bundles a live SSH client with the shells open on it and/or an
// optional SFTP file browser. A session may hold only a browser (browser-only,
// no shells) when the SFTP view was opened for a host without a live shell.
type session struct {
	client *sshx.Client

	// shells are the interactive shells open on this host, shown as tabs when
	// there is more than one. activeSh indexes into it. Each is its own channel on
	// the one connection, so a second shell costs no handshake.
	shells   []*shellTab
	activeSh int

	browser *filebrowser.Browser

	// editors are the files opened from the browser, each a remote editor running
	// on its own SSH session, shown as tabs. activeEd indexes into it.
	editors  []*editorTab
	activeEd int
}

// shellTab is one interactive shell: an SSH session on a pty, rendered through a
// terminal pane. The id is stable across tab removals, so an exit maps back to
// its tab.
type shellTab struct {
	id   int
	pane *terminal.Pane
	sess *sshx.Session
}

// editorTab is one open file: a remote editor process on its own SSH session,
// rendered through the same terminal pane the remote shell uses.
type editorTab struct {
	id   int // stable across tab removals, so an exit maps back to its tab
	name string
	path string
	pane *terminal.Pane
	sess *sshx.Session
}

// shell returns the shell currently shown, or nil when the host has none.
func (s *session) shell() *shellTab {
	if s.activeSh < 0 || s.activeSh >= len(s.shells) {
		return nil
	}
	return s.shells[s.activeSh]
}

// dropShell closes the shell with the given id and removes its tab, returning
// true if it was there.
func (s *session) dropShell(id int) bool {
	for i, sh := range s.shells {
		if sh.id != id {
			continue
		}
		sh.pane.Close()
		s.shells = append(s.shells[:i], s.shells[i+1:]...)
		s.activeSh = clamp(s.activeSh, 0, len(s.shells)-1)
		return true
	}
	return false
}

// closeShells tears down every shell open on the host.
func (s *session) closeShells() {
	for _, sh := range s.shells {
		sh.pane.Close()
	}
	s.shells = nil
	s.activeSh = 0
}

// editor returns the tab currently shown, or nil when none is open.
func (s *session) editor() *editorTab {
	if s.activeEd < 0 || s.activeEd >= len(s.editors) {
		return nil
	}
	return s.editors[s.activeEd]
}

// findEditor returns the index of the tab holding path, or -1.
func (s *session) findEditor(path string) int {
	for i, e := range s.editors {
		if e.path == path {
			return i
		}
	}
	return -1
}

// dropEditor closes the tab with the given id and removes it, returning true if
// it was there. The caller decides where focus goes next.
func (s *session) dropEditor(id int) bool {
	for i, e := range s.editors {
		if e.id != id {
			continue
		}
		e.pane.Close()
		s.editors = append(s.editors[:i], s.editors[i+1:]...)
		s.activeEd = clamp(s.activeEd, 0, len(s.editors)-1)
		return true
	}
	return false
}

// closeEditors tears down every open tab.
func (s *session) closeEditors() {
	for _, e := range s.editors {
		e.pane.Close()
	}
	s.editors = nil
	s.activeEd = 0
}

// close tears the whole session down: every shell and editor, the SFTP subsystem,
// and finally the connection they were all riding on.
func (s *session) close() {
	s.closeShells()
	s.closeEditors()
	if s.browser != nil {
		s.browser.Close()
		s.browser = nil
	}
	if s.client != nil {
		s.client.Close()
		s.client = nil
	}
}

// summary describes what a session is holding, for the details card: it is the
// answer to "what am I about to close?" before you press 'd'.
func (s *session) summary() []string {
	var parts []string
	if n := len(s.shells); n > 0 {
		parts = append(parts, strconv.Itoa(n)+" "+plural(n, "shell", "shells"))
	}
	if s.browser != nil {
		parts = append(parts, "sftp browser")
	}
	if n := len(s.editors); n > 0 {
		parts = append(parts, strconv.Itoa(n)+" "+plural(n, "editor", "editors"))
	}
	return parts
}

// ---- model-level session actions ----

// openShell focuses the host's current shell, or starts one. With extra set it
// always starts another shell, even when the host already has one: it is a second
// channel on the connection hop already holds, so there is no new handshake and
// no second authentication.
func (m *model) openShell(h store.Host, extra bool) tea.Cmd {
	// A connect for this host is already in flight. Waiting for it keeps a second
	// dial (and a second, orphaned client) from racing the first one in.
	if m.connecting[h.Alias] {
		m.setStatus(statusInfo, "connecting to %s…", h.Alias)
		return nil
	}

	s := m.sessions[h.Alias]
	if s != nil && !extra && s.shell() != nil {
		// Already has a shell: just focus it.
		m.focusShell(h.Alias)
		return nil
	}

	m.nextShID++
	m.setStatus(statusInfo, "connecting to %s…", h.Alias)
	m.connecting[h.Alias] = true

	if s != nil && s.client != nil {
		// The host is connected (a browser-only session, or one with shells
		// already): open the new shell on the connection it holds.
		cols, rows := m.shellSize(len(s.shells) + 1)
		return m.withSpinner(shellCmd(h.Alias, s.client, m.nextShID, cols, rows, m.notify))
	}
	cols, rows := m.shellSize(1)
	return m.withSpinner(connectCmd(h, "", m.prompter(h.Alias), extra, m.nextShID, cols, rows, m.notify))
}

// openShellTrusting retries a first-contact shell dial after the user approved
// the host key, trusting fingerprint. It is always a fresh dial — a prompt only
// arises when the host had no established connection to reuse — so it takes the
// first-shell path unconditionally.
func (m *model) openShellTrusting(h store.Host, extra bool, fingerprint string) tea.Cmd {
	if m.connecting[h.Alias] {
		return nil
	}
	m.nextShID++
	m.setStatus(statusInfo, "connecting to %s…", h.Alias)
	m.connecting[h.Alias] = true
	cols, rows := m.shellSize(1)
	return m.withSpinner(connectCmd(h, fingerprint, m.prompter(h.Alias), extra, m.nextShID, cols, rows, m.notify))
}

// focusShell hands the keyboard to the host's visible shell pane.
func (m *model) focusShell(alias string) {
	s := m.sessions[alias]
	if s == nil {
		return
	}
	m.active = alias
	m.focused = true
	m.browsing = false
	m.editing = false
	m.resizeShells(s)
}

// openBrowser opens the host's SFTP browser, on the connection hop already holds
// when there is one, and on a connection of its own when there is not.
func (m *model) openBrowser(h store.Host) tea.Cmd {
	var existing *sshx.Client
	if s := m.sessions[h.Alias]; s != nil {
		existing = s.client
	}
	m.setStatus(statusInfo, "opening sftp %s…", h.Alias)
	if existing == nil {
		// A dial is about to happen, so the host earns a spinner in the list.
		m.connecting[h.Alias] = true
		return m.withSpinner(openBrowserCmd(h, nil, "", m.prompter(h.Alias), m.browserOptions(), m.paneW, m.paneH))
	}
	return openBrowserCmd(h, existing, "", nil, m.browserOptions(), m.paneW, m.paneH)
}

// openBrowserTrusting retries a first-contact SFTP dial after the user approved
// the host key, trusting fingerprint. Like openShellTrusting it is always a fresh
// dial, since a reusable connection would never have prompted.
func (m *model) openBrowserTrusting(h store.Host, fingerprint string) tea.Cmd {
	if m.connecting[h.Alias] {
		return nil
	}
	m.setStatus(statusInfo, "opening sftp %s…", h.Alias)
	m.connecting[h.Alias] = true
	return m.withSpinner(openBrowserCmd(h, nil, fingerprint, m.prompter(h.Alias), m.browserOptions(), m.paneW, m.paneH))
}

// openFile opens the file the browser just activated in an editor tab. A file
// that is already open focuses its tab instead of starting a second editor on it.
func (m *model) openFile(msg filebrowser.OpenFileMsg) tea.Cmd {
	s := m.sessions[m.active]
	if s == nil || s.client == nil {
		return nil
	}
	if i := s.findEditor(msg.Path); i >= 0 {
		s.activeEd = i
		m.editing = true
		m.browsing = false
		return nil
	}

	m.nextEdID++
	ew, eh := m.editorSize()
	// The tab is labelled with the remote file's name, which ends up in the
	// breadcrumb, the mode chip and the tab strip — all rendered to the user's
	// terminal — so control characters are stripped from it here. The path stays
	// untouched: it is shell-quoted where it is used, never rendered.
	name := stripControl(msg.Name)
	m.setStatus(statusInfo, "opening %s…", name)
	return openEditorCmd(m.active, s.client, m.nextEdID, msg.Path, name, m.cfg.Editor, ew, eh, m.notify)
}

// disconnect closes everything open on a host and drops its connection.
func (m *model) disconnect(alias string) {
	s, live := m.sessions[alias]
	if !live {
		m.setStatus(statusWarn, "no live session for %s", alias)
		return
	}
	s.close()
	delete(m.sessions, alias)
	if m.active == alias {
		m.leaveAll()
	}
	m.setStatus(statusOK, "disconnected %s", alias)
}

// closeAll tears down every live session, on the way out.
func (m *model) closeAll() {
	for _, s := range m.sessions {
		s.close()
	}
	m.sessions = make(map[string]*session)
	m.leaveAll()
}
