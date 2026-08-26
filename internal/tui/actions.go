package tui

import (
	tea "charm.land/bubbletea/v2"

	"hop/internal/keys"
)

// action is one offer: an id to run, the words to say, and the key that already does it.
type action struct {
	id keys.Action
	// layer is where that id lives, which decides which do* function runs it.
	layer keys.Layer
	// label comes from the registry unless the spec overrides it.
	label string
	// cap is already resolved: a leader chord is composed here, so rebinding the leader
	// moves every row that uses it.
	cap string
	// host marks an action about the host under the cursor; the menu shows only these.
	host bool
}

func (a action) keycap() string { return a.cap }

type spec struct {
	id keys.Action
	// label overrides the registry's wording; empty takes the registry's.
	label string
	host  bool
	// ok narrows it to the states it makes sense in. nil means always.
	ok func(m *model) bool
	// leader marks a chord: the key is drawn as the leader key and this one together.
	leader bool
}

// Registry order is the order the menu and an unfiltered palette show.

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

// browserSpecs is the SFTP browser's keyboard, motions left out.
var browserSpecs = []spec{
	{id: keys.In},
	{id: keys.Out},
	{id: keys.BrowserDownload},
	{id: keys.BrowserUpload},
	{id: keys.BrowserOpen},
	{id: keys.BrowserMark},
	{id: keys.BrowserMarkAll},
	{id: keys.BrowserTarget},
	{id: keys.BrowserCopy},
	{id: keys.BrowserMoveTo},
	{id: keys.BrowserRename},
	{id: keys.BrowserDelete},
	{id: keys.BrowserMkdir},
	{id: keys.BrowserSort},
	{id: keys.BrowserRefresh},
	{id: keys.BrowserFocusPane},
	{id: keys.BrowserSplit},
	{id: keys.BrowserTree},
	{id: keys.BrowserLeave},
	{id: keys.BrowserSettings},
	{id: keys.BrowserHelp},
}

// paneSpecs is a live shell's, mostly chords: an unreserved key belongs to the remote.
func (m *model) paneSpecs() []spec {
	ss := []spec{
		{id: keys.LeaderOut, leader: true},
		{id: keys.LeaderShell, leader: true},
		{id: keys.PaneNextTab},
		{id: keys.PanePrevTab},
	}
	// The same conditions the chords themselves check, so the palette never offers a key
	// that would decline.
	if m.shellCwd(m.active) != "" {
		ss = append(ss, spec{id: keys.LeaderVSCode, leader: true})
	}
	if s := m.sessions[m.active]; s != nil && s.shell() != nil &&
		!s.shell().pane.AltScreen() && s.shell().pane.ScrollbackLen() > 0 {
		ss = append(ss, spec{id: keys.PaneScroll})
	}
	return append(ss, spec{id: keys.Sidebar}, spec{id: keys.LeaderHelp, leader: true})
}

// editorSpecs is an open editor tab's; ":q" is the remote editor's, so it is not here.
var editorSpecs = []spec{
	{id: keys.LeaderOut, label: "back to the file browser", leader: true},
	{id: keys.EditorNextTab},
	{id: keys.EditorPrevTab},
	{id: keys.EditorFocusTree},
	{id: keys.EditorUnsplit, ok: func(m *model) bool {
		s := m.sessions[m.active]
		return s != nil && s.split
	}},
	{id: keys.Sidebar},
	{id: keys.LeaderHelp, leader: true},
}

// contextActions is everything the palette offers where the keyboard is now; the host
// list is the one mode with two registries.
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

// availableHostActions is what can be done to the host under the cursor right now.
func (m *model) availableHostActions() []action {
	if _, ok := m.selectedHost(); !ok {
		return nil
	}
	return m.resolve(keys.List, hostSpecs)
}

// resolve turns specs into offers, dropping anything the user has unbound.
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

// selectedSession is the session of the host under the cursor, or nil.
func (m *model) selectedSession() *session {
	h, ok := m.selectedHost()
	if !ok {
		return nil
	}
	return m.sessions[h.Alias]
}

// runAction carries out a by calling the same do* function its key would.
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
