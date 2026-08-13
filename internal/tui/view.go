package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// View composes the screen: a header rule, the host list beside the right pane, and a
// context-sensitive key legend along the bottom. The modal cards are composited over the
// finished screen, so the hosts and the pane stay visible behind them.
func (m *model) View() string {
	if !m.ready {
		return "loading hop…"
	}

	bodyH := m.bodyHeight()
	// Collapsed, the sidebar is not drawn at all: a zero-width box would still cost the
	// two columns its border takes.
	body := m.renderRight(bodyH)
	if w := m.listWidth(); w > 0 {
		body = lipgloss.JoinHorizontal(lipgloss.Top, m.renderList(w, bodyH), body)
	}

	screen := lipgloss.JoinVertical(lipgloss.Left, m.renderHeader(), body, m.renderFooter())

	if card := m.modalCard(); card != "" {
		// The cards are composited by splicing each line into a row, so a line that
		// overran the window would push that row's border off screen. Hold them to it.
		card = clampLines(card, m.width)
		x, y := centered(m.width, m.height, lipgloss.Width(card), lipgloss.Height(card))
		screen = overlay(screen, card, x, y)
	}
	// Last, so the key trail floats over the cards too. A no-op outside the demo build.
	return m.keycastDraw(screen)
}

// modalCard is the popover currently up, or "". Only one can be open: each takes every
// key while it is.
func (m *model) modalCard() string {
	switch {
	// Before the help card, for the reason handleKey gives: a dial is parked on this one.
	case m.auth.open:
		return m.renderAuth()
	case m.help:
		return m.renderHelp()
	case m.hostKey.open:
		return m.renderHostKeyConfirm()
	case m.confirm.open:
		return m.renderConfirm()
	case m.hostForm.open:
		return m.renderHostForm()
	case m.importer.open:
		return m.renderImport()
	case m.tunnels.open:
		return m.renderTunnels()
	case m.settings.open:
		return m.renderSettings()
	}
	return ""
}

// ---- header ----

// renderHeader draws the title on the left and the state of the world on the right:
// where the keyboard is going, how many hosts are connected, and what last happened.
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

// breadcrumb says where you are.
func (m *model) breadcrumb() string {
	s := m.sessions[m.active]
	switch {
	case s != nil && s.dead && m.active != "":
		return "ssh manager › " + m.active + " › disconnected"
	case m.editing() && s != nil && s.editor() != nil:
		return "ssh manager › " + m.active + " › " + s.editor().name
	case m.browsing() && m.active != "":
		return "ssh manager › " + m.active + " › sftp"
	case m.focused() && m.active != "":
		return "ssh manager › " + m.active
	default:
		return "ssh manager"
	}
}

// modeChip names where the keystrokes are going, which is always on screen.
func (m *model) modeChip() string {
	s := m.sessions[m.active]
	switch {
	case m.active == "":
		return ""
	case s != nil && s.dead:
		// On a dropped session the keystrokes are going nowhere, which is the most
		// important thing on the screen.
		return redText.Bold(true).Render("✗ " + m.active + " lost")
	case m.editing() && s != nil && s.editor() != nil:
		return chipStyle.Render("✎ " + s.editor().name)
	case m.browsing():
		return chipStyle.Render("▤ sftp")
	case m.focused() && m.scrolling() && s != nil && s.shell() != nil:
		// A distinct chip while paused in history: the offset and how far back it runs.
		p := s.shell().pane
		return accentText.Bold(true).Render(fmt.Sprintf("⇅ scrollback %d/%d", p.ScrollOffset(), p.ScrollbackLen()))
	case m.focused():
		chip := greenText.Bold(true).Render("● " + m.active)
		if s != nil && len(s.shells) > 1 {
			chip += " " + chipStyle.Render(fmt.Sprintf("shell %d/%d", s.activeSh+1, len(s.shells)))
		}
		return chip
	}
	return ""
}

// styledStatus colors the status line by what it means, carried on the message (see
// statusKind) rather than guessed from its wording.
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
	// A status that would push the header out of shape is cut, not wrapped.
	return style.Render(truncate(icon+" "+m.status, max(m.width/2, 20)))
}

// ---- right pane ----

// renderRight draws whatever the active session is showing — an editor, the SFTP
// browser, a shell — and the details card when it is showing nothing.
func (m *model) renderRight(h int) string {
	innerH := max(h-2, 1)
	s := m.sessions[m.active]

	// The content is cut to the pane in both directions. Width is the one that bites:
	// lipgloss wraps a line wider than the box instead of clipping it, so one over-wide
	// row makes the screen a row taller than the window and the terminal scrolls hop's
	// frame off its own top.
	pane := func(active bool, content string) string {
		style := paneBorder
		if active {
			style = paneBorderActive
		}
		return style.Width(m.paneW).Height(innerH).Render(clampLines(fitLines(content, innerH), m.paneW))
	}

	// A dropped session keeps its pane: the last screen the host drew, under a banner.
	// The border is drawn inactive even while the pane holds the keyboard, since the
	// accent would promise a live shell.
	if s != nil && s.dead && m.active != "" {
		return pane(false, m.deadBanner(s)+"\n"+m.deadContent(s))
	}

	switch {
	// A tab strip over the open editor's screen.
	case m.editing() && s != nil && s.editor() != nil:
		return pane(true, m.renderEditorTabs(s)+"\n"+m.selectedView(s.editor().pane.View()))

	// The session's file browser.
	case m.browsing() && s != nil && s.browser != nil:
		return pane(true, s.browser.View())

	// A live shell, with its strip of tabs once there is a second one to switch to.
	case m.active != "" && s != nil && s.shell() != nil:
		// Scrollback shows a window onto history rather than the live screen, but the
		// same number of lines, so the strip and border are unaffected.
		content := s.shell().pane.View()
		if m.focused() && m.scrolling() {
			content = s.shell().pane.ViewScrollback()
		}
		// The highlight goes on before the tab strip, so the selection's rows are the
		// coordinates the drag was measured in.
		content = m.selectedView(content)
		if len(s.shells) > 1 {
			content = m.renderShellTabs(s) + "\n" + content
		}
		return pane(m.focused(), content)
	}

	return pane(false, m.renderDetails(m.paneW))
}

// deadBanner is the line across the top of a dropped session's pane: that the connection
// is gone, why the transport said, and the two keys that answer it. It is on the pane
// because the status line expires after a few seconds and a dropped connection does not.
func (m *model) deadBanner(s *session) string {
	head := redText.Bold(true).Render("⚠ connection lost")
	if s.lostWhy != "" {
		// Off the wire, so stripped like any remote string.
		head += faint.Render(" · " + stripControl(s.lostWhy))
	}
	keys := keyHint("r", "reconnect") + "  " + keyHint("d", "drop")
	gap := max(m.paneW-lipgloss.Width(head)-lipgloss.Width(keys), 1)
	return truncate(head+strings.Repeat(" ", gap)+keys, m.paneW)
}

// deadContent is the frozen screen under the banner: whichever view the session was
// showing when its connection went. It mirrors renderRight's live cases, plus one for a
// session left with nothing but a dead connection.
func (m *model) deadContent(s *session) string {
	switch {
	case m.editing() && s.editor() != nil:
		return m.renderEditorTabs(s) + "\n" + s.editor().pane.View()
	case m.browsing() && s.browser != nil:
		return s.browser.View()
	case s.shell() != nil:
		content := s.shell().pane.View()
		if len(s.shells) > 1 {
			content = m.renderShellTabs(s) + "\n" + content
		}
		return content
	case s.browser != nil:
		return s.browser.View()
	}
	return "\n" + dimStyle.Render("  Nothing is left open on this connection.")
}

// ---- footer ----

// updateHint is the footer's "a newer hop exists" line, or "". It names the command
// rather than a key: updating swaps the running binary mid-session.
func (m *model) updateHint() string {
	if m.updateLatest == "" {
		return ""
	}
	return yellowText.Render("⬆ hop "+m.updateLatest+" available") + " " + dimStyle.Render("· hop self-update")
}

// sidebarHint is the footer's ctrl+b entry. It names the outcome rather than the toggle,
// so the legend does not leave you guessing which way the key goes.
func (m *model) sidebarHint() string {
	if m.sidebarHidden {
		return keyHint("ctrl+b", "show hosts")
	}
	return keyHint("ctrl+b", "hide hosts")
}

// renderFooter is the key legend for the mode you are in: the help card's list, cut down
// to the keys that are live right now.
func (m *model) renderFooter() string {
	const sep = "  "

	var hints []string
	switch {
	case m.auth.open:
		hints = []string{keyHint("enter", "submit"), keyHint("esc", "cancel"), keyHint("ctrl+u", "clear")}

	case m.help:
		hints = []string{keyHint("esc", "close")}

	case m.hostKey.open:
		hints = []string{keyHint("y", "trust"), keyHint("n", "cancel")}

	case m.confirm.open:
		hints = []string{keyHint("y", "delete"), keyHint("n", "cancel")}

	case m.hostForm.open:
		hints = []string{keyHint("tab", "next"), keyHint("enter", "save"), keyHint("esc", "cancel"), keyHint("ctrl+u", "clear")}

	case m.importer.open:
		exit := keyHint("esc", "cancel")
		if m.importer.first {
			exit = keyHint("esc", "skip")
		}
		hints = []string{keyHint("enter", "import"), exit, keyHint("ctrl+u", "clear")}

	case m.tunnels.open && m.tunnels.editing:
		hints = []string{keyHint("tab", "next"), keyHint("enter", "save"), keyHint("esc", "back"), keyHint("ctrl+u", "clear")}

	case m.tunnels.open:
		hints = []string{keyHint("↑↓", "move"), keyHint("enter", "start / stop"), keyHint("a", "add"), keyHint("e", "edit"), keyHint("x", "delete"), keyHint("esc", "close")}

	case m.settings.open && m.settings.editing:
		hints = []string{keyHint("enter", "save"), keyHint("esc", "cancel"), keyHint("ctrl+u", "clear")}

	case m.settings.open:
		hints = []string{keyHint("↑↓", "move"), keyHint("enter", "edit"), keyHint("r", "reset"), keyHint("esc", "close")}

	// Above every pane mode: while the leader is open the footer is the menu. It has to
	// be, since the leader waits indefinitely.
	case m.leaderArmed():
		hints = []string{
			accentText.Render("leader"),
			keyHint("o", "out"), keyHint("1-9", "tab"),
			keyHint("0", "new shell"), keyHint("c", "vs code here"),
			dimStyle.Render("any other key cancels"),
		}

	// Above the three pane modes, as handleKey routes them: a dead pane has its own small
	// keyboard, and a legend offering shift+←→ would list keys that do nothing.
	case m.active != "" && m.inPane() && m.activeDead():
		hints = []string{
			keyHint("r", "reconnect"), keyHint("d", "drop session"),
			keyHint("ctrl+o", "back"), m.sidebarHint(),
		}

	case m.editing() && m.active != "":
		hints = []string{
			keyHint("shift+←→", "tab"), keyHint(":q", "close"),
			keyHint("ctrl+o o", "browser"), m.sidebarHint(),
			dimStyle.Render("keys →") + " " + greenText.Render("editor"),
		}

	case m.browsing() && m.active != "":
		hints = []string{
			keyHint("↑↓", "move"), keyHint("enter", "edit"), keyHint("o", "open local"),
			keyHint("d", "download"), keyHint("←", "up"), keyHint("r", "refresh"),
			keyHint("ctrl+o", "back"), m.sidebarHint(),
		}

	case m.scrolling() && m.focused() && m.active != "":
		hints = []string{
			keyHint("↑↓", "scroll"), keyHint("pgup/pgdn", "page"),
			keyHint("g/G", "top/live"), keyHint("esc", "back to live"),
		}

	case m.focused() && m.active != "":
		// ctrl+o 0 is named even on a host with one shell: it is the chord that makes the
		// second one.
		hints = []string{keyHint("ctrl+o o", "back"), keyHint("esc esc", "back"), keyHint("ctrl+o", "leader")}
		s := m.sessions[m.active]
		// Only named where it would do what it says. Without a directory to hand it still
		// opens VS Code, but on the host's default one.
		if m.shellCwd(m.active) != "" {
			hints = append(hints, keyHint("ctrl+o c", "vs code here"))
		}
		if s != nil && len(s.shells) > 1 {
			hints = append(hints, keyHint("shift+←→", "shell"), keyHint("ctrl+o 1-9", "jump"))
		}
		// The same conditions the entry chord checks, so the hint never offers what the
		// key would decline.
		if s != nil && s.shell() != nil && !s.shell().pane.AltScreen() && s.shell().pane.ScrollbackLen() > 0 {
			hints = append(hints, keyHint("shift+↑", "scrollback"))
		}
		hints = append(hints, m.sidebarHint())
		hints = append(hints, dimStyle.Render("keys →")+" "+greenText.Render(m.active))

	case m.filtering:
		hints = []string{keyHint("type", "filter"), keyHint("↑↓", "move"), keyHint("enter", "apply"), keyHint("esc", "clear")}

	default:
		// The per-host keys are spelled out in the details card beside this line, so the
		// footer keeps to the ones that are always true.
		hints = []string{
			keyHint("↑↓", "move"), keyHint("enter", "connect"), keyHint("f", "sftp"),
			keyHint("t", "tunnels"),
			keyHint("a", "add"), keyHint("i", "import"), keyHint("e", "edit"), keyHint("x", "delete"),
			keyHint("p", "pin"),
			keyHint("/", "filter"), keyHint(",", "settings"), keyHint("?", "keys"), keyHint("q", "quit"),
		}
		// 'r' is always bound, but only worth a slot when there is a dropped session under
		// the cursor. Same for the reorder keys, which need a pinned host.
		if h, ok := m.selectedHost(); ok {
			if h.Pinned {
				hints = append([]string{keyHint("shift+jk", "reorder")}, hints...)
			}
			if s := m.sessions[h.Alias]; s != nil && s.dead {
				hints = append([]string{keyHint("r", "reconnect")}, hints...)
			}
		}
		// Collapsed, the way back matters most, so it goes first where truncation cannot
		// drop it.
		if m.sidebarHidden {
			hints = append([]string{m.sidebarHint()}, hints...)
		} else {
			hints = append(hints, m.sidebarHint())
		}
		// News, not a key, and first so truncation cannot drop it.
		if h := m.updateHint(); h != "" {
			hints = append([]string{h}, hints...)
		}
	}
	return footerStyle.Render(truncate(strings.Join(hints, sep), max(m.width-2, 0)))
}
