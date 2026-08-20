package tui

import (
	"strconv"

	tea "github.com/charmbracelet/bubbletea"

	"hop/internal/filebrowser"
	"hop/internal/sshx"
	"hop/internal/store"
	"hop/internal/terminal"
)

// session bundles a live SSH client with the shells open on it and an optional SFTP file
// browser. It may hold only a browser, when SFTP was opened for a host with no shell.
type session struct {
	client *sshx.Client

	// tunnels are the running forwarding definitions on this connection, keyed by
	// their persistent id. A connection may be tunnel-only, with no visible pane.
	tunnels map[int64]*sshx.Tunnel

	// shells are the interactive shells open on this host, shown as tabs when there is
	// more than one; activeSh indexes into it. Each is its own channel on the one
	// connection, so a second shell costs no handshake.
	shells   []*shellTab
	activeSh int

	browser *filebrowser.Browser

	// editors are the files opened from the browser, each a remote editor running on its
	// own SSH session, shown as tabs. Which of them is on screen is asked per half of the
	// content area: activeEd is the left half's tab — the only one while the content is
	// not split — and splitEd the right half's.
	editors  []*editorTab
	activeEd int
	splitEd  int

	// split is true while the content area is halved so two files can be read side by
	// side (see keys.BrowserSplit); splitRight says which half the keyboard is in. Both
	// halves index the one editors slice, so a split is a second cursor into one tab strip
	// rather than a second set of tabs — which is why closing a file only has to clamp two
	// indices rather than reconcile two lists.
	split      bool
	splitRight bool

	// dead is set once the connection under this session has dropped. The session is kept
	// anyway: the panes still hold the last screen the host drew, and it is what 'r'
	// reconnects. While it is set, no key reaches the remote and nothing is torn down.
	dead bool
	// lostWhy is what the transport reported when it went, for the banner. Often empty.
	lostWhy string
}

// shellTab is one interactive shell: an SSH session on a pty, rendered through a terminal
// pane. The id is stable across tab removals, so an exit maps back to its tab.
type shellTab struct {
	id   int
	pane *terminal.Pane
	sess *sshx.Session
}

// editorTab is one open file: a remote editor on its own SSH session, rendered through
// the same terminal pane a shell uses.
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

// dropShell closes the shell with the given id and removes its tab, reporting whether it
// was there.
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

// focusedHalf is which half of the content area the keyboard is in: false for the left,
// which is the only half there is while the content is not split.
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

// editor returns the tab the keyboard is in, or nil when none is open. Every caller that
// used to mean "the tab on screen" means this one: with two halves drawn there is no
// single tab on screen, and the one that answers to the keyboard is the one they were
// all really asking about.
func (s *session) editor() *editorTab { return s.editorAt(s.focusedHalf()) }

// setEditor points the focused half at tab i. The tab strip, the digits and the pointer
// all move the half you are in rather than the left one.
func (s *session) setEditor(i int) {
	if s.focusedHalf() {
		s.splitEd = i
		return
	}
	s.activeEd = i
}

// focusTab puts the keyboard on tab i: on the half already showing it when one is, and
// otherwise on the half already focused, pointed at it. One file cannot be open in two
// editors, so "go to this file" is a question about halves before it is one about tabs.
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

// openSplit halves the content area and hands the keyboard to the new right half. Until
// the file being opened lands, the right half mirrors the left: both halves index one tab
// list, so there is no empty half to draw and no second list to keep in step.
func (s *session) openSplit() {
	s.split, s.splitRight = true, true
	s.splitEd = s.activeEd
}

// collapseSplit puts the content area back to one pane, keeping whichever file the
// focused half was showing: closing something must not also move you somewhere else.
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

// dropEditor closes the tab with the given id and removes it, reporting whether it was
// there. The caller decides where focus goes next.
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
			// A half wants a file of its own. With one left there is nothing to put
			// beside it, so the split collapses back to the pane it came from — which is
			// what "closing the last tab in a half" means when both halves share a list.
			s.collapseSplit()
		case s.splitEd == s.activeEd:
			// The half that lost its file would otherwise show the other half's. It takes
			// the next tab along instead.
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

// close tears the whole session down: every shell and editor, the SFTP subsystem, and
// finally the connection they rode on.
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

// summary describes what a session is holding, for the details card — the answer to
// "what am I about to close?".
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

// openShell focuses the host's current shell, or starts one. With extra set it always
// starts another — a second channel on the connection hop holds, so no handshake and no
// second authentication.
func (m *model) openShell(h store.Host, extra bool) tea.Cmd {
	// A connect is already in flight; a second dial would race an orphaned client in.
	if m.connecting[h.Alias] {
		m.setStatus(statusInfo, "connecting to %s…", h.Alias)
		return nil
	}

	s := m.sessions[h.Alias]
	if s != nil && s.dead {
		// Nothing can be opened on a connection that is gone. Every way of asking for a
		// shell here means "get me back on this host".
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
		// The host is connected: open the new shell on the connection it holds.
		cols, rows := m.shellSize(len(s.shells) + 1)
		return m.withSpinner(shellCmd(h.Alias, h.DefaultDir, s.client, m.nextShID, cols, rows, m.notify, false))
	}
	cols, rows := m.shellSize(1)
	return m.withSpinner(connectCmd(h, "", m.prompter(h.Alias), extra, m.nextShID, cols, rows, m.notify))
}

// openShellTrusting retries a first-contact shell dial after the user approved the host
// key. It is always a fresh dial, since a prompt only arises with no connection to reuse.
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
	// A change of active session is a change of columns: this host may have a browser
	// where the last one had none, and the tree column comes and goes with it.
	m.relayout()
}

// openBrowser opens the host's SFTP browser, on the connection hop already holds or on
// one of its own.
func (m *model) openBrowser(h store.Host) tea.Cmd {
	var existing *sshx.Client
	if s := m.sessions[h.Alias]; s != nil {
		if s.dead {
			// As with a shell, 'f' on a dead session means reconnect it.
			return m.reconnect(h)
		}
		existing = s.client
	}
	m.setStatus(statusInfo, "opening sftp %s…", h.Alias)
	if existing == nil {
		// A dial is about to happen, so the host earns a spinner.
		m.connecting[h.Alias] = true
		return m.withSpinner(openBrowserCmd(h, nil, "", m.prompter(h.Alias), m.browserOptions(), h.DefaultDir, m.paneW, m.paneH, false))
	}
	return openBrowserCmd(h, existing, "", nil, m.browserOptions(), h.DefaultDir, m.paneW, m.paneH, false)
}

// openBrowserTrusting retries a first-contact SFTP dial after the user approved the host
// key. Like openShellTrusting it is always a fresh dial.
func (m *model) openBrowserTrusting(h store.Host, fingerprint string) tea.Cmd {
	if m.connecting[h.Alias] {
		return nil
	}
	m.setStatus(statusInfo, "opening sftp %s…", h.Alias)
	m.connecting[h.Alias] = true
	return m.withSpinner(openBrowserCmd(h, nil, fingerprint, m.prompter(h.Alias), m.browserOptions(), m.browserStartDir(h), m.paneW, m.paneH, false))
}

// openFile opens the file the browser just activated in an editor tab, focusing the
// existing tab when the file is already open. The alias comes with the message rather
// than from m.active: the browser that asked is the one that gets the tab, even if the
// user has moved on since.
func (m *model) openFile(alias string, msg filebrowser.OpenFileMsg) tea.Cmd {
	s := m.sessions[alias]
	if s == nil || s.client == nil {
		return nil
	}
	// Whether this file was asked for beside the current one rode in on the message, so it
	// still describes the key press that started this open rather than whatever the user
	// has done in the round trip since. See splitOpen.
	if i := s.findEditor(msg.Path); i >= 0 {
		// Already open, so there is nothing for "beside" to mean: a second editor on one
		// file is not a split, it is two views of a buffer neither end knows about. The
		// keyboard goes to the half the file is already in.
		s.focusTab(i)
		m.mode = modeEditor
		return nil
	}

	if msg.Beside && len(s.editors) > 0 && m.splitFits() {
		// The split opens now rather than when the editor lands, so the pane about to be
		// started is told the width it will actually have. Until it arrives the right half
		// mirrors the left, which is a frame of one file drawn twice — cheaper than an
		// empty box and shorter-lived than the SSH handshake behind it.
		s.openSplit()
		m.relayout()
	}

	m.nextEdID++
	ew, eh := m.editorSize(s)
	// The name ends up in the breadcrumb, the mode chip and the tab strip, so control
	// characters are stripped here. The path stays untouched: it is shell-quoted where it
	// is used, never rendered.
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
	// The browser that went took its column with it; the panes still open on other hosts
	// are owed the columns it was holding.
	m.relayout()
	m.setStatus(statusOK, "disconnected %s", alias)
}

// splitOpen answers keys.BrowserSplit: open whatever the browser's cursor is on beside
// the file already showing, rather than behind it as another tab.
//
// It goes through the browser's own ActivateBeside — the same path enter takes, with the
// intent carried along — so a directory still just opens in place and only a file comes
// back marked to split for. Nothing is remembered on the session in between: the message
// that returns says which key asked for it.
func (m *model) splitOpen() tea.Cmd {
	s := m.sessions[m.active]
	if s == nil || s.browser == nil {
		return nil
	}
	switch {
	case len(s.editors) == 0:
		// Nothing to put it beside. This is an ordinary open, and saying so would be
		// pedantry about a distinction the user cannot see yet.
	case !m.splitFits():
		m.setStatus(statusWarn, "too narrow to split: opening as a tab")
	default:
		// A directory is expanded in place and answers with nothing, so the intent goes
		// nowhere; only a file comes back wearing it.
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
