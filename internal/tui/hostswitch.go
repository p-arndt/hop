package tui

import (
	"slices"

	tea "charm.land/bubbletea/v2"
	"github.com/sahilm/fuzzy"

	"hop/internal/store"
)

// hostSwitchUI is the host switcher's state; the matches are held, as the palette's are,
// since the cursor indexes them.
type hostSwitchUI struct {
	open bool
	picker
	items []store.Host
}

// openHostSwitch raises the host switcher on every host, unfiltered.
func (m *model) openHostSwitch() {
	m.hostSwitch = hostSwitchUI{open: true}
	m.filterHostSwitch()
	m.clearStatus()
}

func (m *model) closeHostSwitch() { m.hostSwitch = hostSwitchUI{} }

// filterHostSwitch re-runs the query over alias, user and host name, as the list's filter
// does, and puts the hosts with a session first. The partition is stable, so the list's
// order — or the match ranking — survives inside it.
func (m *model) filterHostSwitch() {
	hosts := m.hosts
	if q := m.hostSwitch.query; q != "" {
		hay := make([]string, len(m.hosts))
		for i, h := range m.hosts {
			hay[i] = h.Alias + " " + h.User + " " + h.HostName
		}
		hosts = nil
		for _, mt := range fuzzy.Find(q, hay) {
			hosts = append(hosts, m.hosts[mt.Index])
		}
	}

	items := slices.Clone(hosts)
	slices.SortStableFunc(items, func(a, b store.Host) int {
		return m.sessionRank(a.Alias) - m.sessionRank(b.Alias)
	})
	m.hostSwitch.items = items
	m.hostSwitch.cursor = clamp(m.hostSwitch.cursor, 0, max(len(items)-1, 0))
}

// sessionRank sorts a host with a session ahead of one without.
func (m *model) sessionRank(alias string) int {
	if m.sessions[alias] != nil {
		return 0
	}
	return 1
}

// handleHostSwitchKey routes a key while the host switcher is up; like the palette it
// swallows everything, or a key would reach the pane underneath.
func (m *model) handleHostSwitchKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.closeHostSwitch()

	case "enter":
		if m.hostSwitch.cursor >= len(m.hostSwitch.items) {
			return m, nil
		}
		h := m.hostSwitch.items[m.hostSwitch.cursor]
		m.closeHostSwitch()
		return m, m.hopTo(h)

	default:
		if m.hostSwitch.key(msg, len(m.hostSwitch.items)) {
			m.filterHostSwitch()
		}
	}
	return m, nil
}

// hopTo lands in h's shell: its own if it has one, a new connection's otherwise.
func (m *model) hopTo(h store.Host) tea.Cmd {
	// While m.active is still the host being left, so its pane snaps back to live.
	m.exitScrollback()
	m.reader.Reset()
	return m.openShell(h, false)
}

func (m *model) renderHostSwitch() string {
	return m.renderPicker("HOSTS", m.hostSwitch.picker, len(m.hostSwitch.items), "hop",
		func(i int, selected bool, w int) string {
			h := m.hostSwitch.items[i]
			return pickRow(stripControl(h.Alias), m.dotFor(h.Alias), selected, w)
		})
}

// ---- last host ----

// noteHost keeps the last host current: when the active host changes, the one being left
// becomes the last host, carrying the mode it was showing. Run after every message, so no
// path that moves the keyboard can forget to.
func (m *model) noteHost() {
	if m.active == "" {
		return
	}
	if m.active != m.shown.alias {
		if m.shown.alias != "" {
			m.last = m.shown
		}
		m.shown = hostView{alias: m.active}
	}
	if m.inPane() {
		m.shown.mode = m.mode
		if m.mode == modeScrollback {
			m.shown.mode = modeShell
		}
	}
}

// backToLastHost lands on the last host, in the mode it was showing when it was left, or
// the first of shell, browser, editor it still has.
func (m *model) backToLastHost() {
	alias := m.last.alias
	if alias == "" {
		m.setStatus(statusWarn, "no last host to go back to")
		return
	}
	s := m.sessions[alias]
	if s == nil {
		m.setStatus(statusWarn, "%s has no session any more", alias)
		return
	}

	mode := modeList
	for _, want := range []paneMode{m.last.mode, modeShell, modeBrowser, modeEditor} {
		if s.shows(want) {
			mode = want
			break
		}
	}
	if mode == modeList {
		m.setStatus(statusWarn, "nothing open on %s to go back to", alias)
		return
	}

	m.exitScrollback()
	m.reader.Reset()
	m.active, m.mode = alias, mode
	m.clearStatus()
	m.relayout()
}

// shows reports whether the session has something to show in mode.
func (s *session) shows(mode paneMode) bool {
	switch mode {
	case modeShell:
		return len(s.shells) > 0
	case modeBrowser:
		return s.browser != nil
	case modeEditor:
		return len(s.editors) > 0
	}
	return false
}
