package tui

import (
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"hop/internal/sshx"
	"hop/internal/store"
)

var tunnelFields = [...]hostFormField{
	{"Direction", "local"},
	{"Bind address", "127.0.0.1"},
	{"Bind port", "required"},
	{"Target host", "required"},
	{"Target port", "required"},
}

const (
	tfKind = iota
	tfBindHost
	tfBindPort
	tfTargetHost
	tfTargetPort
)

// tunnelUI is the forwarding manager's list state and, while editing, its form.
type tunnelUI struct {
	open      bool
	alias     string
	cursor    int
	editing   bool
	editID    int64
	field     int
	buf       [len(tunnelFields)]string
	addingNew bool
}

func (m *model) openTunnels(h store.Host) {
	m.tunnels = tunnelUI{open: true, alias: h.Alias}
	m.clearStatus()
}

func (m *model) closeTunnels() { m.tunnels = tunnelUI{} }

func (m *model) tunnelHost() (store.Host, bool) { return m.hostByAlias(m.tunnels.alias) }

func (m *model) selectedForward() (store.Forward, bool) {
	h, ok := m.tunnelHost()
	if !ok || len(h.Forwards) == 0 {
		return store.Forward{}, false
	}
	m.tunnels.cursor = clamp(m.tunnels.cursor, 0, len(h.Forwards)-1)
	return h.Forwards[m.tunnels.cursor], true
}

func (m *model) handleTunnelsKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.tunnels.editing {
		return m.handleTunnelEditKey(msg)
	}

	h, ok := m.tunnelHost()
	if !ok {
		m.closeTunnels()
		return m, nil
	}
	switch msg.String() {
	case "esc", "q", "T":
		m.closeTunnels()
	case "up":
		m.tunnels.cursor = clamp(m.tunnels.cursor-1, 0, max(len(h.Forwards)-1, 0))
	case "down":
		m.tunnels.cursor = clamp(m.tunnels.cursor+1, 0, max(len(h.Forwards)-1, 0))
	case "k":
		if m.cfg.VimKeys {
			m.tunnels.cursor = clamp(m.tunnels.cursor-1, 0, max(len(h.Forwards)-1, 0))
		}
	case "j":
		if m.cfg.VimKeys {
			m.tunnels.cursor = clamp(m.tunnels.cursor+1, 0, max(len(h.Forwards)-1, 0))
		}
	case "a":
		m.tunnels.editing = true
		m.tunnels.addingNew = true
		m.tunnels.field = 0
		m.tunnels.buf = [len(tunnelFields)]string{}
		m.tunnels.buf[tfKind] = string(store.ForwardLocal)
	case "e":
		if f, ok := m.selectedForward(); ok {
			m.editForward(f)
		}
	case "x":
		if f, ok := m.selectedForward(); ok {
			if err := m.st.DeleteForward(h.ID, f.ID); err != nil {
				m.setStatus(statusErr, "delete tunnel: %v", err)
				break
			}
			m.stopTunnel(h.Alias, f.ID, false)
			m.reloadHostsSelecting(h.Alias)
			m.tunnels.cursor = clamp(m.tunnels.cursor, 0, max(len(h.Forwards)-2, 0))
			m.setStatus(statusOK, "deleted tunnel")
		}
	case "enter", " ":
		if f, ok := m.selectedForward(); ok {
			return m, m.toggleTunnel(h, f)
		}
	case "t":
		m.closeTunnels()
		return m, m.toggleTunnels(h)
	}
	return m, nil
}

func (m *model) editForward(f store.Forward) {
	m.tunnels.editing = true
	m.tunnels.addingNew = false
	m.tunnels.editID = f.ID
	m.tunnels.field = 0
	m.tunnels.buf[tfKind] = string(f.Kind)
	m.tunnels.buf[tfBindHost] = f.BindHost
	m.tunnels.buf[tfBindPort] = strconv.Itoa(f.BindPort)
	m.tunnels.buf[tfTargetHost] = f.TargetHost
	m.tunnels.buf[tfTargetPort] = strconv.Itoa(f.TargetPort)
}

func (m *model) handleTunnelEditKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.tunnels.editing = false
		m.tunnels.buf = [len(tunnelFields)]string{}
	case "up", "shift+tab":
		m.tunnels.field = (m.tunnels.field + len(tunnelFields) - 1) % len(tunnelFields)
	case "down", "tab":
		m.tunnels.field = (m.tunnels.field + 1) % len(tunnelFields)
	case "left", "right":
		if m.tunnels.field == tfKind {
			m.flipTunnelKind()
		}
	case "enter":
		m.saveTunnelEdit()
	case "backspace":
		if m.tunnels.field != tfKind {
			b := m.tunnels.buf[m.tunnels.field]
			if b != "" {
				r := []rune(b)
				m.tunnels.buf[m.tunnels.field] = string(r[:len(r)-1])
			}
		}
	case "ctrl+u":
		if m.tunnels.field != tfKind {
			m.tunnels.buf[m.tunnels.field] = ""
		}
	default:
		if m.tunnels.field == tfKind {
			if msg.String() == " " {
				m.flipTunnelKind()
			}
		} else if len(msg.Runes) > 0 {
			m.tunnels.buf[m.tunnels.field] += string(msg.Runes)
		}
	}
	return m, nil
}

func (m *model) flipTunnelKind() {
	if m.tunnels.buf[tfKind] == string(store.ForwardRemote) {
		m.tunnels.buf[tfKind] = string(store.ForwardLocal)
	} else {
		m.tunnels.buf[tfKind] = string(store.ForwardRemote)
	}
}

func (m *model) saveTunnelEdit() {
	h, ok := m.tunnelHost()
	if !ok {
		return
	}
	bindPort, err := parseTunnelPort(m.tunnels.buf[tfBindPort], "bind")
	if err != nil {
		m.setStatus(statusErr, "%v", err)
		return
	}
	targetPort, err := parseTunnelPort(m.tunnels.buf[tfTargetPort], "target")
	if err != nil {
		m.setStatus(statusErr, "%v", err)
		return
	}
	f := store.Forward{
		ID:         m.tunnels.editID,
		HostID:     h.ID,
		Kind:       store.ForwardKind(m.tunnels.buf[tfKind]),
		BindHost:   strings.TrimSpace(m.tunnels.buf[tfBindHost]),
		BindPort:   bindPort,
		TargetHost: strings.TrimSpace(m.tunnels.buf[tfTargetHost]),
		TargetPort: targetPort,
	}
	if err := f.Validate(); err != nil {
		m.setStatus(statusErr, "%v", err)
		return
	}

	if m.tunnels.addingNew {
		id, err := m.st.AddForward(h.ID, f)
		if err != nil {
			m.setStatus(statusErr, "%v", err)
			return
		}
		m.reloadHostsSelecting(h.Alias)
		if updated, ok := m.hostByAlias(h.Alias); ok {
			for i := range updated.Forwards {
				if updated.Forwards[i].ID == id {
					m.tunnels.cursor = i
					break
				}
			}
		}
		m.setStatus(statusOK, "added tunnel")
	} else {
		if err := m.st.UpdateForward(f); err != nil {
			m.setStatus(statusErr, "%v", err)
			return
		}
		// A running listener still embodies the old definition; the next enter/t starts the replacement.
		m.stopTunnel(h.Alias, f.ID, false)
		m.reloadHostsSelecting(h.Alias)
		m.setStatus(statusOK, "updated tunnel — start it to apply")
	}
	m.tunnels.editing = false
	m.tunnels.buf = [len(tunnelFields)]string{}
}

func parseTunnelPort(s, name string) (int, error) {
	n, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil || n < 1 || n > 65535 {
		return 0, fmt.Errorf("%s port must be between 1 and 65535", name)
	}
	return n, nil
}

// toggleTunnels is the one-key host action: any running definition means stop all,
// otherwise start all. With nothing defined it opens the manager at its add action.
func (m *model) toggleTunnels(h store.Host) tea.Cmd {
	if len(h.Forwards) == 0 {
		m.openTunnels(h)
		return nil
	}
	if s := m.sessions[h.Alias]; s != nil && len(s.tunnels) > 0 {
		n := len(s.tunnels)
		m.stopAllTunnels(h.Alias)
		m.setStatus(statusOK, "stopped %d %s on %s", n, plural(n, "tunnel", "tunnels"), h.Alias)
		return nil
	}
	ids := make([]int64, len(h.Forwards))
	for i := range h.Forwards {
		ids[i] = h.Forwards[i].ID
	}
	return m.startTunnelIDs(h, ids, "", false)
}

func (m *model) toggleTunnel(h store.Host, f store.Forward) tea.Cmd {
	if s := m.sessions[h.Alias]; s != nil && s.tunnels[f.ID] != nil {
		m.stopTunnel(h.Alias, f.ID, true)
		return nil
	}
	return m.startTunnelIDs(h, []int64{f.ID}, "", false)
}

func (m *model) startTunnelIDs(h store.Host, ids []int64, trustedFP string, restore bool) tea.Cmd {
	if m.connecting[h.Alias] {
		m.setStatus(statusInfo, "connecting to %s…", h.Alias)
		return nil
	}
	s := m.sessions[h.Alias]
	if s != nil && s.dead {
		return m.reconnect(h)
	}
	defs := forwardDefinitions(h, ids)
	if len(defs) == 0 {
		m.setStatus(statusWarn, "no tunnel definitions to start")
		return nil
	}
	var existing *sshx.Client
	if s != nil {
		existing = s.client
	}
	if existing == nil {
		m.connecting[h.Alias] = true
	}
	m.setStatus(statusInfo, "starting %d %s on %s…", len(defs), plural(len(defs), "tunnel", "tunnels"), h.Alias)
	return m.withSpinner(startTunnelsCmd(h, existing, trustedFP, m.prompter(h.Alias), defs, restore))
}

func (m *model) startTunnelIDsTrusting(h store.Host, ids []int64, fingerprint string) tea.Cmd {
	_, restoring := m.pending[h.Alias]
	return m.startTunnelIDs(h, ids, fingerprint, restoring)
}

func forwardDefinitions(h store.Host, ids []int64) []store.Forward {
	want := make(map[int64]bool, len(ids))
	for _, id := range ids {
		want[id] = true
	}
	defs := make([]store.Forward, 0, len(ids))
	for _, f := range h.Forwards {
		if want[f.ID] {
			defs = append(defs, f)
		}
	}
	return defs
}

func (m *model) tunnelsLanded(msg tunnelsStartedMsg) (tea.Model, tea.Cmd) {
	delete(m.connecting, msg.alias)
	if msg.err != nil {
		var unknown *sshx.UnknownHostKeyError
		if errors.As(msg.err, &unknown) {
			m.openTunnelHostKeyConfirm(msg.alias, unknown, msg.ids)
			return m, nil
		}
		m.dropPlan(msg.alias)
		if errors.Is(msg.err, sshx.ErrAuthCanceled) {
			return m, nil
		}
		m.setStatus(statusErr, "start tunnels on %s: %v", msg.alias, msg.err)
		return m, nil
	}

	s := m.sessions[msg.alias]
	if s == nil {
		s = &session{}
		m.sessions[msg.alias] = s
	}
	if s.tunnels == nil {
		s.tunnels = make(map[int64]*sshx.Tunnel)
	}
	if msg.client != nil {
		s.client = msg.client
		m.st.Touch(msg.alias)
		m.reloadHostsSelecting(msg.alias)
	}
	cmds := make([]tea.Cmd, 0, len(msg.tunnels)+2)
	if msg.client != nil {
		cmds = append(cmds, watchClientCmd(msg.alias, msg.client))
	}
	for id, tunnel := range msg.tunnels {
		s.tunnels[id] = tunnel
		cmds = append(cmds, watchTunnelCmd(msg.alias, id, tunnel))
	}
	if !msg.restore {
		m.setStatus(statusOK, "started %d %s on %s", len(msg.tunnels), plural(len(msg.tunnels), "tunnel", "tunnels"), msg.alias)
	}
	if cmd := m.applyPlan(msg.alias); cmd != nil {
		cmds = append(cmds, cmd)
	}
	return m, tea.Batch(cmds...)
}

func (m *model) tunnelStopped(msg tunnelStoppedMsg) (tea.Model, tea.Cmd) {
	s := m.sessions[msg.alias]
	if s == nil || s.tunnels[msg.id] != msg.tunnel {
		return m, nil
	}
	if s.deadConnection() {
		m.markDead(msg.alias, lostReason(s))
		return m, nil
	}
	delete(s.tunnels, msg.id)
	if msg.err != nil {
		m.setStatus(statusErr, "%s tunnel stopped: %v", msg.alias, msg.err)
	} else {
		m.setStatus(statusWarn, "%s tunnel stopped", msg.alias)
	}
	if s.empty() {
		s.close()
		delete(m.sessions, msg.alias)
	}
	return m, nil
}

func (m *model) stopTunnel(alias string, id int64, report bool) {
	s := m.sessions[alias]
	if s == nil || s.tunnels[id] == nil {
		return
	}
	tunnel := s.tunnels[id]
	delete(s.tunnels, id) // make the watcher's eventual message stale
	_ = tunnel.Close()
	if report {
		m.setStatus(statusOK, "stopped tunnel on %s", alias)
	}
	if s.empty() {
		s.close()
		delete(m.sessions, alias)
		if m.active == alias {
			m.leaveAll()
		}
	}
}

func (m *model) stopAllTunnels(alias string) {
	s := m.sessions[alias]
	if s == nil {
		return
	}
	tunnels := s.tunnels
	s.tunnels = nil // make all watcher messages stale before Close wakes them
	for _, tunnel := range tunnels {
		_ = tunnel.Close()
	}
	if s.empty() {
		s.close()
		delete(m.sessions, alias)
		if m.active == alias {
			m.leaveAll()
		}
	}
}

func forwardText(f store.Forward) string {
	bindHost := f.BindHost
	if bindHost == "" {
		bindHost = "127.0.0.1"
	}
	dir := "L"
	if f.Kind == store.ForwardRemote {
		dir = "R"
	}
	return fmt.Sprintf("%s  %s → %s", dir,
		net.JoinHostPort(bindHost, strconv.Itoa(f.BindPort)),
		net.JoinHostPort(f.TargetHost, strconv.Itoa(f.TargetPort)))
}

func (m *model) renderTunnels() string {
	h, ok := m.tunnelHost()
	if !ok {
		return ""
	}
	if m.tunnels.editing {
		return m.renderTunnelEdit()
	}
	w := min(max(m.width-2*cardPadX-2, 24), 72)
	var b strings.Builder
	b.WriteString(titleStyle.Render("TUNNELS"))
	b.WriteString(faint.Render("  " + stripControl(h.Alias)))
	b.WriteString("\n\n")
	if len(h.Forwards) == 0 {
		b.WriteString(dimStyle.Render("No forwards defined."))
		b.WriteString("\n\n")
		b.WriteString(keyHint("a", "add the first one"))
	} else {
		available := max(m.height-10, 1)
		start := 0
		if m.tunnels.cursor >= available {
			start = m.tunnels.cursor - available + 1
		}
		end := min(start+available, len(h.Forwards))
		for i := start; i < end; i++ {
			f := h.Forwards[i]
			running := false
			if s := m.sessions[h.Alias]; s != nil && !s.dead {
				running = s.tunnels[f.ID] != nil
			}
			dot := idleDot
			if running {
				dot = connectedDot
			}
			lead := "  "
			style := dimStyle
			if i == m.tunnels.cursor {
				lead, style = selBar+" ", settingsValueSel
			}
			b.WriteString(truncate(lead+dot+" "+style.Render(forwardText(f)), w))
			b.WriteString("\n")
		}
		if end < len(h.Forwards) {
			b.WriteString(faint.Render(fmt.Sprintf("  … %d more", len(h.Forwards)-end)))
			b.WriteString("\n")
		}
		b.WriteString("\n")
		b.WriteString(rule(w))
		b.WriteString("\n")
		b.WriteString(keyHint("enter", "start / stop"))
		b.WriteString("  ")
		b.WriteString(keyHint("a", "add"))
		b.WriteString("  ")
		b.WriteString(keyHint("e", "edit"))
		b.WriteString("  ")
		b.WriteString(keyHint("x", "delete"))
		b.WriteString("  ")
		b.WriteString(keyHint("esc", "close"))
	}
	return cardBox.Width(w + 2*cardPadX).Render(b.String())
}

func (m *model) renderTunnelEdit() string {
	w := min(max(m.width-2*cardPadX-2, 24), hostFormMaxW)
	var b strings.Builder
	title := "NEW TUNNEL"
	if !m.tunnels.addingNew {
		title = "EDIT TUNNEL"
	}
	b.WriteString(titleStyle.Render(title))
	b.WriteString("\n\n")
	for i, field := range tunnelFields {
		selected := i == m.tunnels.field
		bar, label := "  ", settingsLabel.Render(field.label)
		if selected {
			bar, label = selBar+" ", settingsLabelSel.Render(field.label)
		}
		b.WriteString(truncate(bar+label, w))
		b.WriteString("\n")
		value := m.tunnels.buf[i]
		if i == tfKind {
			local, remote := "local", "remote"
			if value == string(store.ForwardLocal) {
				local = accentText.Render(local)
			} else {
				remote = accentText.Render(remote)
			}
			b.WriteString("    ")
			b.WriteString(local)
			b.WriteString(faint.Render("  ↔  "))
			b.WriteString(remote)
		} else if selected {
			b.WriteString("    ")
			b.WriteString(inputStyle.Width(w - 4).Render(truncate(value, w-7) + accentText.Render("▏")))
		} else if value == "" {
			b.WriteString("    ")
			b.WriteString(settingsPlaceholder.Render(field.placeholder))
		} else {
			b.WriteString("    ")
			b.WriteString(settingsValue.Render(truncate(value, w-6)))
		}
		b.WriteString("\n\n")
	}
	b.WriteString(rule(w))
	b.WriteString("\n")
	b.WriteString(keyHint("tab", "next"))
	b.WriteString("  ")
	b.WriteString(keyHint("←→", "direction"))
	b.WriteString("  ")
	b.WriteString(keyHint("enter", "save"))
	b.WriteString("  ")
	b.WriteString(keyHint("esc", "back"))
	return cardBox.Width(w + 2*cardPadX).Render(b.String())
}
