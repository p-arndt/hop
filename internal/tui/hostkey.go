package tui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"hop/internal/sshx"
)

// hostKeyAction is which dial to retry once the user trusts a first-contact host key.
type hostKeyAction int

const (
	hostKeyShell hostKeyAction = iota
	hostKeyBrowser
	hostKeyTunnels
)

// hostKeyUI is the new-host-key confirmation card's state. The action is captured at
// prompt time so the retry dials the same host wherever the cursor has since wandered.
type hostKeyUI struct {
	open        bool
	alias       string
	fingerprint string
	keyType     string
	action      hostKeyAction
	// extra applies to a shell retry: another shell alongside the existing ones.
	extra bool
	// tunnelIDs applies to a tunnel retry: the definitions requested before the dial stopped.
	tunnelIDs []int64
}

func (m *model) openTunnelHostKeyConfirm(alias string, e *sshx.UnknownHostKeyError, ids []int64) {
	m.openHostKeyConfirm(alias, e, hostKeyTunnels, false)
	m.hostKey.tunnelIDs = append([]int64(nil), ids...)
}

func (m *model) openHostKeyConfirm(alias string, e *sshx.UnknownHostKeyError, action hostKeyAction, extra bool) {
	m.hostKey = hostKeyUI{
		open:        true,
		alias:       alias,
		fingerprint: e.Fingerprint,
		keyType:     e.KeyType,
		action:      action,
		extra:       extra,
	}
	m.status = ""
}

func (m *model) closeHostKey() {
	m.hostKey = hostKeyUI{}
}

// handleHostKeyKey routes a key while the card is up, swallowing everything: a stray key
// must not connect on a fingerprint the user never approved.
func (m *model) handleHostKeyKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "y", "enter":
		hk := m.hostKey
		m.closeHostKey()
		return m, m.acceptHostKey(hk)
	case "n", "esc", "q":
		alias := m.hostKey.alias
		m.closeHostKey()
		m.setStatus(statusWarn, "not trusting %s", alias)
	}
	return m, nil
}

func (m *model) acceptHostKey(hk hostKeyUI) tea.Cmd {
	h, ok := m.hostByAlias(hk.alias)
	if !ok {
		return nil
	}
	switch hk.action {
	case hostKeyBrowser:
		return m.openBrowserTrusting(h, hk.fingerprint)
	case hostKeyTunnels:
		return m.startTunnelIDsTrusting(h, hk.tunnelIDs, hk.fingerprint)
	default:
		return m.openShellTrusting(h, hk.extra, hk.fingerprint)
	}
}

// renderHostKeyConfirm draws the card; the body wraps so the fingerprint is shown whole.
func (m *model) renderHostKeyConfirm() string {
	w := m.confirmInnerW()
	var b strings.Builder

	b.WriteString(truncate(titleStyle.Render("NEW HOST KEY"), w))
	b.WriteString("\n\n")

	intro := dimStyle.Render("First contact with ") + accentText.Render(m.hostKey.alias) + dimStyle.Render(".")
	b.WriteString(wrapCard(intro, w))
	b.WriteString("\n\n")

	key := dimStyle.Render(m.hostKey.keyType+" ") + accentText.Render(m.hostKey.fingerprint)
	b.WriteString(wrapCard(key, w))
	b.WriteString("\n\n")

	warn := yellowText.Render("This key can't be verified. Only trust it if you expected a new host.")
	b.WriteString(wrapCard(warn, w))
	b.WriteString("\n\n")

	b.WriteString(truncate(keyHint("y", "trust")+"  "+keyHint("n", "cancel"), w))

	return cardBox.Width(w + 2*cardPadX).Render(b.String())
}

// wrapCard word-wraps s to w cells, hard-breaking tokens wider than the card — a
// fingerprint has no spaces.
func wrapCard(s string, w int) string {
	return lipgloss.NewStyle().Width(w).Render(s)
}
