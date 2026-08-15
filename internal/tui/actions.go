package tui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

// action is one thing hop can do, written down so it can be *found* rather than
// remembered: a label, and the key that already does it.
//
// An action deliberately owns no behaviour of its own. Running one replays its key
// through handleKey, which is the same path a keystroke takes — so the menu, the palette
// and the details card can never drift from the keyboard, and a key that grows a new
// condition grows it in one place. The cost is that only keys hop itself binds can be
// actions, which is exactly the set worth offering here.
type action struct {
	// key is the binding this action stands for, as handleKey names it. A chord is
	// written as its keys with a space between them ("ctrl+o o") and replayed in order.
	key string
	// show overrides how the key is drawn, for the ones whose name is longer than their
	// symbol ("shift+→"). Empty means key itself.
	show string
	// label says what it does, in the words the help card uses.
	label string
	// host marks an action about the host under the cursor, as opposed to one about hop.
	// The context menu shows only these; the palette shows both.
	host bool
	// ok narrows an action to the states it makes sense in — reconnect wants a dropped
	// session, unpin wants a pinned host. nil means always.
	ok func(m *model) bool
}

// keycap is how the action's key is drawn.
func (a action) keycap() string {
	if a.show != "" {
		return a.show
	}
	return a.key
}

// The registries, one per mode hop owns the keyboard in. Their order is the order the
// menu and an unfiltered palette show, so the common things stand at the top: what you
// came to the host to do, then what you do to the host, then what you do to hop.
//
// The labels are the help card's, verbatim where the card has one. Two places naming the
// same key differently is how a user learns to distrust both.

var hostActions = []action{
	{key: "enter", label: "connect", host: true, ok: func(m *model) bool {
		return m.selectedSession() == nil
	}},
	{key: "enter", label: "focus its shell", host: true, ok: func(m *model) bool {
		s := m.selectedSession()
		return s != nil && !s.dead
	}},
	{key: "r", label: "reconnect and reopen", host: true, ok: func(m *model) bool {
		s := m.selectedSession()
		return s != nil && s.dead
	}},
	{key: "S", label: "another shell, same connection", host: true},
	{key: "f", label: "sftp file browser", host: true},
	{key: "t", label: "start / stop all tunnels", host: true},
	{key: "T", label: "manage tunnel definitions", host: true},
	{key: "o", label: "open in VS Code Remote", host: true},
	{key: "e", label: "edit this host", host: true},
	{key: "p", label: "pin it to the top", host: true, ok: func(m *model) bool {
		h, ok := m.selectedHost()
		return ok && !h.Pinned
	}},
	{key: "p", label: "unpin it", host: true, ok: func(m *model) bool {
		h, ok := m.selectedHost()
		return ok && h.Pinned
	}},
	{key: "d", label: "disconnect everything on it", host: true, ok: func(m *model) bool {
		return m.selectedSession() != nil
	}},
	{key: "x", label: "delete this host", host: true},
}

var globalActions = []action{
	{key: "a", label: "add a new host"},
	{key: "i", label: "import an ssh config"},
	{key: "/", label: "filter the hosts"},
	{key: toggleSidebarKey, label: "hide / show the sidebar"},
	{key: ",", label: "settings"},
	{key: "?", label: "all the keys"},
	{key: "q", label: "quit hop"},
}

// browserActions is the SFTP browser's keyboard. The motions are left out for the reason
// the footer leaves them out: nobody opens a menu to move the cursor down.
var browserActions = []action{
	{key: "enter", label: "open the directory / edit the file"},
	{key: "left", show: "←", label: "up a directory"},
	{key: "d", label: "download the file"},
	{key: "o", label: "open the file locally"},
	{key: "r", label: "refresh the listing"},
	{key: "ctrl+o", label: "back to the host list"},
	{key: toggleSidebarKey, label: "hide / show the sidebar"},
	{key: ",", label: "settings"},
	{key: "?", label: "all the keys"},
}

// paneActions is a live shell's, and it is the one registry that is mostly chords: in a
// pane every unreserved key belongs to the remote program, so hop's own keyboard is
// behind the leader. Which is exactly the keyboard hardest to remember, and so the one
// the palette is worth most for.
func (m *model) paneActions() []action {
	as := []action{
		{key: "ctrl+o o", label: "back to the host list"},
		{key: "ctrl+o 0", label: "another shell on this host"},
		{key: "shift+right", show: "shift+→", label: "next shell"},
		{key: "shift+left", show: "shift+←", label: "previous shell"},
	}
	// The same conditions the chords themselves check, so the palette never offers a key
	// that would decline: VS Code wants a directory, scrollback wants history behind a
	// shell that is not on its alternate screen.
	if m.shellCwd(m.active) != "" {
		as = append(as, action{key: "ctrl+o c", label: "open this directory in VS Code"})
	}
	if s := m.sessions[m.active]; s != nil && s.shell() != nil &&
		!s.shell().pane.AltScreen() && s.shell().pane.ScrollbackLen() > 0 {
		as = append(as, action{key: "shift+up", show: "shift+↑", label: "scroll back through history"})
	}
	return append(as,
		action{key: toggleSidebarKey, label: "hide / show the sidebar"},
		action{key: "ctrl+o ?", label: "all the keys"},
	)
}

// editorActions is an open editor tab's. ":q" is the remote editor's own key rather than
// one of hop's, so it is not here — an action has to be a key hop can replay.
var editorActions = []action{
	{key: "ctrl+o o", label: "back to the file browser"},
	{key: "shift+right", show: "shift+→", label: "next file tab"},
	{key: "shift+left", show: "shift+←", label: "previous file tab"},
	{key: toggleSidebarKey, label: "hide / show the sidebar"},
	{key: "ctrl+o ?", label: "all the keys"},
}

// contextActions is everything the palette offers where the keyboard is now. The host
// list is the one mode with two registries — the host's, then hop's — because it is the
// one mode with something under the cursor.
func (m *model) contextActions() []action {
	switch m.mode {
	case modeBrowser:
		return available(m, browserActions)
	case modeEditor:
		return available(m, editorActions)
	case modeShell, modeScrollback:
		return available(m, m.paneActions())
	}
	return append(m.availableHostActions(), available(m, globalActions)...)
}

// availableHostActions is what can be done to the host under the cursor right now, in
// registry order. An empty list is the honest answer for a cursor standing on nothing.
func (m *model) availableHostActions() []action {
	if _, ok := m.selectedHost(); !ok {
		return nil
	}
	return available(m, hostActions)
}

// available filters a registry by its predicates.
func available(m *model, as []action) []action {
	var out []action
	for _, a := range as {
		if a.ok == nil || a.ok(m) {
			out = append(out, a)
		}
	}
	return out
}

// selectedSession is the session of the host under the cursor, or nil — the question
// most of the host predicates are asking.
func (m *model) selectedSession() *session {
	h, ok := m.selectedHost()
	if !ok {
		return nil
	}
	return m.sessions[h.Alias]
}

// runAction carries out a, by replaying the keys it stands for in order. A chord runs as
// the two keystrokes it is, so the leader opens and closes exactly as it would under a
// hand. See action.
func (m *model) runAction(a action) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd
	for _, k := range actionKeys(a.key) {
		_, cmd := m.handleKey(actionKeyMsg(k))
		cmds = append(cmds, cmd)
	}
	return m, tea.Batch(cmds...)
}

// actionKeys splits a chord into its keystrokes. The space key is a key, not a
// separator, so it is the one string that never splits.
func actionKeys(key string) []string {
	if key == menuKey {
		return []string{key}
	}
	return strings.Fields(key)
}

// actionKeyTypes names the non-rune keys the registries use — the inverse of what
// handleKey switches on. A rune key needs no entry.
var actionKeyTypes = map[string]tea.KeyType{
	"enter":       tea.KeyEnter,
	"esc":         tea.KeyEsc,
	"left":        tea.KeyLeft,
	"right":       tea.KeyRight,
	"shift+left":  tea.KeyShiftLeft,
	"shift+right": tea.KeyShiftRight,
	"shift+up":    tea.KeyShiftUp,
	"ctrl+o":      tea.KeyCtrlO,
	"ctrl+b":      tea.KeyCtrlB,
	"ctrl+k":      tea.KeyCtrlK,
}

// actionKeyMsg builds the tea.KeyMsg whose String() is key.
func actionKeyMsg(key string) tea.KeyMsg {
	if t, ok := actionKeyTypes[key]; ok {
		return tea.KeyMsg{Type: t}
	}
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(key)}
}
