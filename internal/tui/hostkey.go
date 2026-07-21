package tui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"hop/internal/sshx"
)

// hostKeyAction is which dial to retry once the user trusts a first-contact host
// key: the shell the list's connect was for, or the SFTP browser 'f' opened.
type hostKeyAction int

const (
	hostKeyShell hostKeyAction = iota
	hostKeyBrowser
)

// hostKeyUI is the new-host-key confirmation card's state. It holds what the
// question needs to name — the alias and the key it is asking about — plus the
// action to replay on "yes", captured at prompt time so the retry dials the same
// host the same way regardless of where the cursor has since wandered.
type hostKeyUI struct {
	open        bool
	alias       string
	fingerprint string
	keyType     string
	action      hostKeyAction
	// extra applies to a shell retry: whether the original request was for another
	// shell alongside the host's existing ones (S / alt+0) rather than the first.
	extra bool
}

// openHostKeyConfirm arms the card for the key sshx reported unknown. The status
// line is cleared so the question stands on its own.
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

// closeHostKey dismisses the card, trusting nothing.
func (m *model) closeHostKey() {
	m.hostKey = hostKeyUI{}
}

// handleHostKeyKey routes a key while the card is up. Like the other modal cards
// it swallows everything: a stray key must not fall through and connect on a key
// the user never approved. Only an explicit yes retries the dial, this time
// trusting the fingerprint on screen; anything else that is a cancel trusts
// nothing, and anything else keeps the question up.
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

// acceptHostKey replays the dial the card was armed for, now trusting the
// approved fingerprint. A prompt only arises when the host had no established
// connection to reuse, so the retry is always a fresh dial.
func (m *model) acceptHostKey(hk hostKeyUI) tea.Cmd {
	h, ok := m.hostByAlias(hk.alias)
	if !ok {
		return nil
	}
	switch hk.action {
	case hostKeyBrowser:
		return m.openBrowserTrusting(h, hk.fingerprint)
	default:
		return m.openShellTrusting(h, hk.extra, hk.fingerprint)
	}
}

// renderHostKeyConfirm draws the card, modelled on the delete confirmation but
// wrapping its body: a fingerprint is ~50 cells and must be shown whole to be
// compared, so lines wrap across the card rather than truncating it away.
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

// wrapCard word-wraps s to w cells, hard-breaking any token (a fingerprint has no
// spaces) that is itself wider than the card so nothing spills past the border.
func wrapCard(s string, w int) string {
	return lipgloss.NewStyle().Width(w).Render(s)
}
