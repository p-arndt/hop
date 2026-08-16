package tui

import (
	tea "github.com/charmbracelet/bubbletea"

	"hop/internal/keys"
)

// This file is what hop can do *here*, which is a different question from what its keys
// are: the registry in internal/keys says a key runs an action, and the lists below say
// which of those actions are worth offering in a given mode and state.
//
// An action carries no behaviour of its own. Running one calls the same do* function the
// keystroke calls, so the menu, the palette and the details card cannot drift from the
// keyboard — and unlike the replay this replaced, a rebound key changes nothing here.

// action is one offer: an id to run, the words to say, and the key that already does it.
type action struct {
	// id is what to run, and the registry row the label and keycap come from.
	id keys.Action
	// layer is where that id lives, which decides which do* function runs it.
	layer keys.Layer
	// label says what it does. It comes from the registry unless the spec overrides it —
	// a state-dependent offer ("connect" vs "focus its shell") is one action with two
	// wordings, and the wording is the whole value of the row.
	label string
	// cap is how the key is drawn, already resolved: a leader chord is composed here
	// rather than written out, so rebinding the leader moves every row that uses it.
	cap string
	// host marks an action about the host under the cursor, as opposed to one about hop.
	// The context menu shows only these; the palette shows both.
	host bool
}

// keycap is how the action's key is drawn.
func (a action) keycap() string { return a.cap }

// spec is one row of a registry below: which action, in which layer, and the state it is
// worth offering in.
type spec struct {
	id keys.Action
	// label overrides the registry's wording; empty takes the registry's.
	label string
	// host marks it as an action about the host under the cursor.
	host bool
	// ok narrows it to the states it makes sense in — reconnect wants a dropped session,
	// unpin wants a pinned host. nil means always.
	ok func(m *model) bool
	// leader marks a chord: the key is drawn as the leader key and this one together.
	leader bool
}

// The registries, one per mode hop owns the keyboard in. Their order is the order the
// menu and an unfiltered palette show, so the common things stand at the top: what you
// came to the host to do, then what you do to the host, then what you do to hop.

var hostSpecs = []spec{
	{id: keys.In, label: "connect", host: true, ok: func(m *model) bool {
		return m.selectedSession() == nil
	}},
	{id: keys.In, label: "focus its shell", host: true, ok: func(m *model) bool {
		s := m.selectedSession()
		return s != nil && !s.dead
	}},
	{id: keys.HostReconnec, label: "reconnect and reopen", host: true, ok: func(m *model) bool {
		s := m.selectedSession()
		return s != nil && s.dead
	}},
	{id: keys.HostNewShell, host: true},
	{id: keys.HostBrowser, host: true},
	{id: keys.HostTunnels, host: true},
	{id: keys.HostTunnelUI, host: true},
	{id: keys.HostVSCode, host: true},
	{id: keys.HostEdit, host: true},
	{id: keys.HostPin, label: "pin it to the top", host: true, ok: func(m *model) bool {
		h, ok := m.selectedHost()
		return ok && !h.Pinned
	}},
	{id: keys.HostPin, label: "unpin it", host: true, ok: func(m *model) bool {
		h, ok := m.selectedHost()
		return ok && h.Pinned
	}},
	{id: keys.HostDrop, host: true, ok: func(m *model) bool {
		return m.selectedSession() != nil
	}},
	{id: keys.HostDelete, host: true},
}

var globalSpecs = []spec{
	{id: keys.HostAdd},
	{id: keys.HostImport},
	{id: keys.Filter},
	{id: keys.Sidebar},
	{id: keys.Settings},
	{id: keys.Help},
	{id: keys.Quit},
}

// browserSpecs is the SFTP browser's keyboard. The motions are left out for the reason
// the footer leaves them out: nobody opens a menu to move the cursor down.
var browserSpecs = []spec{
	{id: keys.In},
	{id: keys.Out},
	{id: keys.BrowserDownload},
	{id: keys.BrowserUpload},
	{id: keys.BrowserOpen},
	{id: keys.BrowserRename},
	{id: keys.BrowserDelete},
	{id: keys.BrowserMkdir},
	{id: keys.BrowserSort},
	{id: keys.BrowserRefresh},
	{id: keys.BrowserLeave},
	{id: keys.BrowserSettings},
	{id: keys.BrowserHelp},
}

// paneSpecs is a live shell's, and it is the one registry that is mostly chords: in a
// pane every unreserved key belongs to the remote program, so hop's own keyboard is
// behind the leader. Which is exactly the keyboard hardest to remember, and so the one
// the palette is worth most for.
func (m *model) paneSpecs() []spec {
	ss := []spec{
		{id: keys.LeaderOut, leader: true},
		{id: keys.LeaderShell, leader: true},
		{id: keys.PaneNextTab},
		{id: keys.PanePrevTab},
	}
	// The same conditions the chords themselves check, so the palette never offers a key
	// that would decline: VS Code wants a directory, scrollback wants history behind a
	// shell that is not on its alternate screen.
	if m.shellCwd(m.active) != "" {
		ss = append(ss, spec{id: keys.LeaderVSCode, leader: true})
	}
	if s := m.sessions[m.active]; s != nil && s.shell() != nil &&
		!s.shell().pane.AltScreen() && s.shell().pane.ScrollbackLen() > 0 {
		ss = append(ss, spec{id: keys.PaneScroll})
	}
	return append(ss, spec{id: keys.Sidebar}, spec{id: keys.LeaderHelp, leader: true})
}

// editorSpecs is an open editor tab's. ":q" is the remote editor's own key rather than
// one of hop's, so it is not here — an action has to be something hop can run.
var editorSpecs = []spec{
	{id: keys.LeaderOut, label: "back to the file browser", leader: true},
	{id: keys.EditorNextTab},
	{id: keys.EditorPrevTab},
	{id: keys.Sidebar},
	{id: keys.LeaderHelp, leader: true},
}

// contextActions is everything the palette offers where the keyboard is now. The host
// list is the one mode with two registries — the host's, then hop's — because it is the
// one mode with something under the cursor.
func (m *model) contextActions() []action {
	switch m.mode {
	case modeBrowser:
		return m.resolve(keys.Browser, browserSpecs)
	case modeEditor:
		return m.resolve(keys.Editor, editorSpecs)
	case modeShell, modeScrollback:
		return m.resolve(keys.Pane, m.paneSpecs())
	}
	return append(m.availableHostActions(), m.resolve(keys.List, globalSpecs)...)
}

// availableHostActions is what can be done to the host under the cursor right now, in
// registry order. An empty list is the honest answer for a cursor standing on nothing.
func (m *model) availableHostActions() []action {
	if _, ok := m.selectedHost(); !ok {
		return nil
	}
	return m.resolve(keys.List, hostSpecs)
}

// resolve turns specs into offers: predicates applied, labels and keys read out of the
// keyboard, and anything the user has unbound dropped. A row with no key left is not an
// offer — running it would do nothing the user could repeat by hand.
//
// layer is the keyboard the registry belongs to. Two rows do not come from it: a leader
// chord, which lives in the leader's own layer and is drawn as two keys, and the global
// pair, which every mode offers and no mode owns.
func (m *model) resolve(layer keys.Layer, ss []spec) []action {
	var out []action
	for _, sp := range ss {
		if sp.ok != nil && !sp.ok(m) {
			continue
		}

		l := layer
		if sp.leader {
			l = keys.Leader
		}
		b, ok := m.binds.BindingIn(l, sp.id)
		if !ok {
			// Not this layer's: the global pair, offered from inside every mode.
			if b, ok = m.binds.BindingIn(keys.Global, sp.id); !ok {
				continue
			}
			l = keys.Global
		}

		cap := b.Keycap()
		if sp.leader {
			lead := m.binds.Keycap(keys.LeaderKey)
			if lead == "" {
				continue // no leader, no chord to offer
			}
			cap = lead + " " + cap
		}
		if cap == "" {
			continue // the user unbound it
		}

		label := sp.label
		if label == "" {
			label = b.Label
		}
		out = append(out, action{id: sp.id, layer: l, label: label, cap: cap, host: sp.host})
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

// runAction carries out a, by calling the same do* function its key would. The leader
// rows run against the pane they were offered over, which is the active one: a palette
// opened from a pane is still standing in that pane when a row is picked.
func (m *model) runAction(a action) (tea.Model, tea.Cmd) {
	switch a.layer {
	case keys.List:
		return m.doList(a.id)
	case keys.Global:
		return m.doGlobal(a.id)
	case keys.Leader:
		return m.doLeader(a.id, m.active, m.editing())
	case keys.Pane:
		_, model, cmd := m.doPane(a.id)
		return model, cmd
	case keys.Editor:
		_, model, cmd := m.doEditor(a.id)
		return model, cmd
	case keys.Browser:
		return m.doBrowser(a.id)
	}
	return m, nil
}
