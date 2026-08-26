package tui

import (
	"strconv"

	tea "charm.land/bubbletea/v2"

	"hop/internal/filebrowser"
	"hop/internal/sshx"
	"hop/internal/store"
	"hop/internal/terminal"
)

// session bundles an SSH client with the shells, browser and editors on it; any of them may be absent.
type session struct {
	client *sshx.Client

	// tunnels are keyed by persistent id; a connection may be tunnel-only, with no visible pane.
	tunnels map[int64]*sshx.Tunnel

	// shells are each their own channel on the one connection, so a second shell costs no handshake.
	shells   []*shellTab
	activeSh int

	browser *filebrowser.Browser

	// activeEd is the left half's tab — the only one while unsplit — and splitEd the right half's.
	editors  []*editorTab
	activeEd int
	splitEd  int

	// Both halves index the one editors slice, so closing a file only has to clamp two indices.
	split      bool
	splitRight bool

	// dead keeps the session: the panes still hold the last screen, and it is what 'r' reconnects.
	dead bool
	// lostWhy is what the transport reported when it went, for the banner. Often empty.
	lostWhy string
}

// shellTab is one interactive shell; the id is stable across tab removals, so an exit maps back to its tab.
type shellTab struct {
	id   int
	pane *terminal.Pane
	sess *sshx.Session
}

// editorTab is one open file, rendered through the same terminal pane a shell uses.
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

// dropShell closes the shell with the given id, reporting whether it was there.
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

// focusedHalf is false for the left half, which is the only half there is while unsplit.
func (s *session) focusedHalf() bool { return s.split && s.splitRight }

// editorIndex is the tab the given half of the content area is showing.
func (s *session) editorIndex(right bool) int {
	if right {
		return s.splitEd
	}
	return s.activeEd
}

// editorAt returns the tab shown in the given half, or nil when it holds none.
func (s *session) editorAt(right bool) *editorTab {
	i := s.editorIndex(right)
	if i < 0 || i >= len(s.editors) {
		return nil
	}
	return s.editors[i]
}

// editor returns the tab the keyboard is in, or nil when none is open.
func (s *session) editor() *editorTab { return s.editorAt(s.focusedHalf()) }

// setEditor points the focused half at tab i.
func (s *session) setEditor(i int) {
	if s.focusedHalf() {
		s.splitEd = i
		return
	}
	s.activeEd = i
}

// focusTab puts the keyboard on tab i, preferring a half already showing it — one file cannot be open twice.
func (s *session) focusTab(i int) {
	switch {
	case s.split && s.splitEd == i:
		s.splitRight = true
	case s.split && s.activeEd == i:
		s.splitRight = false
	default:
		s.setEditor(i)
	}
}

// openSplit halves the content area; until the new file lands the right half mirrors the left.
func (s *session) openSplit() {
	s.split, s.splitRight = true, true
	s.splitEd = s.activeEd
}

// collapseSplit keeps whichever file the focused half was showing: closing must not also move you elsewhere.
func (s *session) collapseSplit() {
	if s.focusedHalf() {
		s.activeEd = s.splitEd
	}
	s.split, s.splitRight, s.splitEd = false, false, 0
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

// dropEditor closes the tab with the given id, reporting whether it was there. The caller decides focus.
func (s *session) dropEditor(id int) bool {
	for i, e := range s.editors {
		if e.id != id {
			continue
		}
		e.pane.Close()
		s.editors = append(s.editors[:i], s.editors[i+1:]...)
		s.activeEd = clamp(s.activeEd, 0, len(s.editors)-1)
		s.splitEd = clamp(s.splitEd, 0, len(s.editors)-1)
		switch {
		case len(s.editors) < 2:
			// Nothing left to put beside the survivor.
			s.collapseSplit()
		case s.splitEd == s.activeEd:
			// The half that lost its file would otherwise show the other half's.
			s.splitEd = cycle(s.activeEd, 1, len(s.editors))
		}
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
	s.collapseSplit()
}

// closeTunnels releases every local and remote listener on the connection.
func (s *session) closeTunnels() {
	for _, tunnel := range s.tunnels {
		_ = tunnel.Close()
	}
	s.tunnels = nil
}

func (s *session) empty() bool {
	return len(s.shells) == 0 && s.browser == nil && len(s.editors) == 0 && len(s.tunnels) == 0
}

// close tears the whole session down, the connection last.
func (s *session) close() {
	s.closeShells()
	s.closeEditors()
	s.closeTunnels()
	if s.browser != nil {
		s.browser.Close()
		s.browser = nil
	}
	if s.client != nil {
		s.client.Close()
		s.client = nil
	}
}

// summary describes what a session is holding, for the details card.
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
	if n := len(s.tunnels); n > 0 {
		parts = append(parts, strconv.Itoa(n)+" "+plural(n, "tunnel", "tunnels"))
	}
	return parts
}

// ---- model-level session actions ----

// openShell focuses the host's current shell, or starts one; extra always starts another.
func (m *model) openShell(h store.Host, extra bool) tea.Cmd {
	// A connect is already in flight; a second dial would race an orphaned client in.
	if m.connecting[h.Alias] {
		m.setStatus(statusInfo, "connecting to %s…", h.Alias)
		return nil
	}

	s := m.sessions[h.Alias]
	if s != nil && s.dead {
		// Nothing can be opened on a connection that is gone.
		return m.reconnect(h)
	}
	if s != nil && !extra && s.shell() != nil {
		m.focusShell(h.Alias)
		return nil
	}

	m.nextShID++
	m.setStatus(statusInfo, "connecting to %s…", h.Alias)
	m.connecting[h.Alias] = true

	if s != nil && s.client != nil {
		cols, rows := m.shellSize(len(s.shells) + 1)
		return m.withSpinner(shellCmd(h.Alias, h.DefaultDir, s.client, m.nextShID, cols, rows, m.notify, false))
	}
	cols, rows := m.shellSize(1)
	return m.withSpinner(connectCmd(h, "", m.prompter(h.Alias), extra, m.nextShID, cols, rows, m.notify))
}

// openShellTrusting retries after host-key approval; always a fresh dial, since a prompt means no connection to reuse.
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
	m.mode = modeShell
	// A change of active session is a change of columns: this host may have a browser where the last had none.
	m.relayout()
}

// openBrowser opens the host's SFTP browser, on the connection hop already holds or on one of its own.
func (m *model) openBrowser(h store.Host) tea.Cmd {
	var existing *sshx.Client
	if s := m.sessions[h.Alias]; s != nil {
		if s.dead {
			return m.reconnect(h)
		}
		existing = s.client
	}
	m.setStatus(statusInfo, "opening sftp %s…", h.Alias)
	// browserSize tests the window rather than asking treeWidth: there is no column on screen yet to measure.
	bw, bh := m.browserSize()
	if existing == nil {
		m.connecting[h.Alias] = true
		return m.withSpinner(openBrowserCmd(h, nil, "", m.prompter(h.Alias), m.browserOptions(), h.DefaultDir, bw, bh, false))
	}
	return openBrowserCmd(h, existing, "", nil, m.browserOptions(), h.DefaultDir, bw, bh, false)
}

// openBrowserTrusting retries after host-key approval; like openShellTrusting it is always a fresh dial.
func (m *model) openBrowserTrusting(h store.Host, fingerprint string) tea.Cmd {
	if m.connecting[h.Alias] {
		return nil
	}
	m.setStatus(statusInfo, "opening sftp %s…", h.Alias)
	m.connecting[h.Alias] = true
	bw, bh := m.browserSize()
	return m.withSpinner(openBrowserCmd(h, nil, fingerprint, m.prompter(h.Alias), m.browserOptions(), m.browserStartDir(h), bw, bh, false))
}

// openFile opens the activated file in an editor tab. The alias comes with the message, not from m.active.
func (m *model) openFile(alias string, msg filebrowser.OpenFileMsg) tea.Cmd {
	s := m.sessions[alias]
	if s == nil || s.client == nil {
		return nil
	}
	if i := s.findEditor(msg.Path); i >= 0 {
		// Already open, so "beside" has nothing to mean: the keyboard goes to the half holding it.
		s.focusTab(i)
		m.mode = modeEditor
		return nil
	}

	if msg.Beside && len(s.editors) > 0 && m.splitFits() {
		// Split now rather than when the editor lands, so the pane is started with the width it will have.
		s.openSplit()
		m.relayout()
	}

	m.nextEdID++
	ew, eh := m.editorSize(s)
	// The name is rendered in the breadcrumb, chip and tab strip; the path is shell-quoted, never drawn.
	name := stripControl(msg.Name)
	m.setStatus(statusInfo, "opening %s…", name)
	return openEditorCmd(alias, s.client, m.nextEdID, msg.Path, name, m.cfg.Editor, ew, eh, m.notify)
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
	// The browser that went took its column with it; the remaining panes are owed the space.
	m.relayout()
	m.setStatus(statusOK, "disconnected %s", alias)
}

// splitOpen answers keys.BrowserSplit via the browser's ActivateBeside, so a directory still opens in place.
func (m *model) splitOpen() tea.Cmd {
	s := m.sessions[m.active]
	if s == nil || s.browser == nil {
		return nil
	}
	switch {
	case len(s.editors) == 0:
		// Nothing to put it beside; the distinction is not visible to the user yet.
	case !m.splitFits():
		m.setStatus(statusWarn, "too narrow to split: opening as a tab")
	default:
		return s.browser.ActivateBeside()
	}
	return s.browser.Activate()
}

// closeAll tears down every live session, on the way out.
func (m *model) closeAll() {
	for _, s := range m.sessions {
		s.close()
	}
	m.sessions = make(map[string]*session)
	m.leaveAll()
}
