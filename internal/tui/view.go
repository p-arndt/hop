package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// View composes the screen: a header rule, the host list beside the right pane,
// and a context-sensitive key legend along the bottom. The modal cards are
// composited over the finished screen rather than replacing any part of it, so
// the hosts and the pane stay visible behind them.
func (m *model) View() string {
	if !m.ready {
		return "loading hop…"
	}

	bodyH := m.bodyHeight()
	body := lipgloss.JoinHorizontal(lipgloss.Top,
		m.renderList(m.listWidth(), bodyH),
		m.renderRight(bodyH),
	)

	screen := lipgloss.JoinVertical(lipgloss.Left, m.renderHeader(), body, m.renderFooter())

	if card := m.modalCard(); card != "" {
		// The cards size themselves to the window, but they are composited onto the
		// screen by splicing each of their lines into a row of it — so a line that
		// overran the window would push that row's right-hand border off the screen
		// and nothing else would. Hold them to it here as well.
		card = clampLines(card, m.width)
		x, y := centered(m.width, m.height, lipgloss.Width(card), lipgloss.Height(card))
		screen = overlay(screen, card, x, y)
	}
	return screen
}

// modalCard is the popover currently up, or "" when there is none. Only one can
// be open: each of them takes every key while it is.
func (m *model) modalCard() string {
	switch {
	case m.help:
		return m.renderHelp()
	case m.settings.open:
		return m.renderSettings()
	}
	return ""
}

// ---- header ----

// renderHeader draws the title on the left and the state of the world on the
// right: where the keyboard is going, how many hosts are connected, and the last
// thing that happened.
func (m *model) renderHeader() string {
	left := headerBadge.Render("hop") + subtitle.Render(" "+m.breadcrumb())

	var chips []string
	if c := m.modeChip(); c != "" {
		chips = append(chips, c)
	}
	if n := len(m.sessions); n > 0 {
		chips = append(chips, chipStyle.Render(fmt.Sprintf("%d %s", n, plural(n, "session", "sessions"))))
	}
	if st := m.styledStatus(); st != "" {
		chips = append(chips, st)
	}
	right := strings.Join(chips, " ")

	gap := max(m.width-lipgloss.Width(left)-lipgloss.Width(right), 0)
	return truncate(left+strings.Repeat(" ", gap)+right, m.width)
}

// breadcrumb says where you are, which on a screen with four modes and a pane
// full of somebody else's program is worth a line of its own.
func (m *model) breadcrumb() string {
	s := m.sessions[m.active]
	switch {
	case m.editing && s != nil && s.editor() != nil:
		return "ssh manager › " + m.active + " › " + s.editor().name
	case m.browsing && m.active != "":
		return "ssh manager › " + m.active + " › sftp"
	case m.focused && m.active != "":
		return "ssh manager › " + m.active
	default:
		return "ssh manager"
	}
}

// modeChip names where the keystrokes are going. Nothing is more disorienting in
// a TUI than typing into the wrong thing, so the mode is always on screen.
func (m *model) modeChip() string {
	s := m.sessions[m.active]
	switch {
	case m.active == "":
		return ""
	case m.editing && s != nil && s.editor() != nil:
		return chipStyle.Render("✎ " + s.editor().name)
	case m.browsing:
		return chipStyle.Render("▤ sftp")
	case m.focused:
		chip := greenText.Bold(true).Render("● " + m.active)
		if s != nil && len(s.shells) > 1 {
			chip += " " + chipStyle.Render(fmt.Sprintf("shell %d/%d", s.activeSh+1, len(s.shells)))
		}
		return chip
	}
	return ""
}

// styledStatus colors the status line by what it means. The meaning is carried on
// the message (see statusKind), not guessed from its wording.
func (m *model) styledStatus() string {
	if m.status == "" {
		return ""
	}
	icon, style := "·", dimStyle
	switch m.statusKind {
	case statusOK:
		icon, style = "✓", greenText
	case statusWarn:
		icon, style = "!", yellowText
	case statusErr:
		icon, style = "✗", redText
	}
	// A status that would push the header out of shape is cut, not wrapped: it is
	// a note, and the chrome it sits in is not negotiable.
	return style.Render(truncate(icon+" "+m.status, max(m.width/2, 20)))
}

// ---- right pane ----

// renderRight draws whatever the active session is showing — an editor, the SFTP
// browser, a shell — and the details card for the host under the cursor when it
// is showing nothing.
func (m *model) renderRight(h int) string {
	innerH := max(h-2, 1)
	s := m.sessions[m.active]

	// The content is cut to the pane rather than allowed to grow it: a details
	// card taller than a short window would push the footer off the screen.
	pane := func(active bool, content string) string {
		style := paneBorder
		if active {
			style = paneBorderActive
		}
		return style.Width(m.paneW).Height(innerH).Render(fitLines(content, innerH))
	}

	switch {
	// Editing: a tab strip over the open editor's screen.
	case m.editing && s != nil && s.editor() != nil:
		return pane(true, m.renderEditorTabs(s)+"\n"+s.editor().pane.View())

	// Browsing: the session's file browser.
	case m.browsing && s != nil && s.browser != nil:
		return pane(true, s.browser.View())

	// A live shell, with its strip of tabs once there is a second one to switch to.
	case m.active != "" && s != nil && s.shell() != nil:
		content := s.shell().pane.View()
		if len(s.shells) > 1 {
			content = m.renderShellTabs(s) + "\n" + content
		}
		return pane(m.focused, content)
	}

	return pane(false, m.renderDetails(m.paneW))
}

// ---- footer ----

// renderFooter is the key legend for the mode you are in. It is the same list the
// help card holds, cut down to the keys that are live right now — the card is
// there for when that is not enough.
func (m *model) renderFooter() string {
	const sep = "  "

	var hints []string
	switch {
	case m.help:
		hints = []string{keyHint("esc", "close")}

	case m.settings.open && m.settings.editing:
		hints = []string{keyHint("enter", "save"), keyHint("esc", "cancel"), keyHint("ctrl+u", "clear")}

	case m.settings.open:
		hints = []string{keyHint("↑↓", "move"), keyHint("enter", "edit"), keyHint("r", "reset"), keyHint("esc", "close")}

	case m.editing && m.active != "":
		hints = []string{
			keyHint("alt+←→", "tab"), keyHint("alt+1-9", "jump"),
			keyHint(":q", "close"), keyHint("ctrl+o", "browser"),
			dimStyle.Render("keys →") + " " + greenText.Render("editor"),
		}

	case m.browsing && m.active != "":
		hints = []string{
			keyHint("↑↓", "move"), keyHint("enter", "edit"), keyHint("o", "open local"),
			keyHint("d", "download"), keyHint("←", "up"), keyHint("r", "refresh"),
			keyHint("ctrl+o", "back"),
		}

	case m.focused && m.active != "":
		// alt+0 is named even on a host with one shell — it is the key that *makes*
		// the second one, so a footer that waited for a second shell to mention it
		// would only ever tell you what you had already worked out.
		hints = []string{keyHint("ctrl+o", "back"), keyHint("esc esc", "back"), keyHint("alt+0", "new shell")}
		if s := m.sessions[m.active]; s != nil && len(s.shells) > 1 {
			hints = append(hints, keyHint("alt+←→", "shell"), keyHint("alt+1-9", "jump"))
		}
		hints = append(hints, dimStyle.Render("keys →")+" "+greenText.Render(m.active))

	case m.filtering:
		hints = []string{keyHint("type", "filter"), keyHint("↑↓", "move"), keyHint("enter", "apply"), keyHint("esc", "clear")}

	default:
		// The per-host keys (S, s, o) are spelled out in the details card beside
		// this line, so the footer keeps to the ones that are always true.
		hints = []string{
			keyHint("↑↓", "move"), keyHint("enter", "connect"), keyHint("f", "sftp"),
			keyHint("d", "disconnect"), keyHint("/", "filter"), keyHint(",", "settings"),
			keyHint("?", "keys"), keyHint("q", "quit"),
		}
	}
	return footerStyle.Render(truncate(strings.Join(hints, sep), max(m.width-2, 0)))
}
