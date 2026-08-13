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
	// Collapsed, the sidebar is not drawn narrow — it is not drawn: the pane already
	// has the columns (see listWidth), and a zero-width box would still cost the two
	// its border takes.
	body := m.renderRight(bodyH)
	if w := m.listWidth(); w > 0 {
		body = lipgloss.JoinHorizontal(lipgloss.Top, m.renderList(w, bodyH), body)
	}

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
	// Last, so the key trail floats over the cards too. A no-op in every build that
	// is not the demo recording.
	return m.keycastDraw(screen)
}

// modalCard is the popover currently up, or "" when there is none. Only one can
// be open: each of them takes every key while it is.
func (m *model) modalCard() string {
	switch {
	// Before the help card, for the reason handleKey gives: a dial is parked on
	// this one, and it must not be hidden behind a card the user opened while
	// waiting for the connect.
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

// modeChip names where the keystrokes are going. Nothing is more disorienting in
// a TUI than typing into the wrong thing, so the mode is always on screen.
func (m *model) modeChip() string {
	s := m.sessions[m.active]
	switch {
	case m.active == "":
		return ""
	case s != nil && s.dead:
		// The chip's job is to say where the keystrokes are going. On a dropped session
		// they are going nowhere, and that is the most important thing on the screen.
		return redText.Bold(true).Render("✗ " + m.active + " lost")
	case m.editing() && s != nil && s.editor() != nil:
		return chipStyle.Render("✎ " + s.editor().name)
	case m.browsing():
		return chipStyle.Render("▤ sftp")
	case m.focused() && m.scrolling() && s != nil && s.shell() != nil:
		// A distinct chip while paused in history: the offset and how far back the
		// scrollback runs, so you can see where in it you are.
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
	// The content is cut to the pane in *both* directions. Height is the obvious one;
	// width is the one that bites, because lipgloss wraps a line wider than the box
	// instead of clipping it — so one over-wide row makes the pane a row taller, the
	// screen a row taller than the window, and the terminal scrolls hop's frame off
	// its own top. A scrollback line rendered at the width the pane had before the
	// sidebar was collapsed (or the window resized) is exactly such a row.
	pane := func(active bool, content string) string {
		style := paneBorder
		if active {
			style = paneBorderActive
		}
		return style.Width(m.paneW).Height(innerH).Render(clampLines(fitLines(content, innerH), m.paneW))
	}

	// A session whose connection dropped keeps its pane: the last screen the host
	// drew, under a banner saying so. The border is drawn inactive even while the
	// pane technically holds the keyboard, because what it holds it no longer
	// forwards — the accent would promise a live shell.
	if s != nil && s.dead && m.active != "" {
		return pane(false, m.deadBanner(s)+"\n"+m.deadContent(s))
	}

	switch {
	// Editing: a tab strip over the open editor's screen.
	case m.editing() && s != nil && s.editor() != nil:
		return pane(true, m.renderEditorTabs(s)+"\n"+m.selectedView(s.editor().pane.View()))

	// Browsing: the session's file browser.
	case m.browsing() && s != nil && s.browser != nil:
		return pane(true, s.browser.View())

	// A live shell, with its strip of tabs once there is a second one to switch to.
	case m.active != "" && s != nil && s.shell() != nil:
		// In scrollback mode the pane shows a window onto its history rather than the
		// live screen, but the same number of lines, so the tab strip and border are
		// unaffected.
		content := s.shell().pane.View()
		if m.focused() && m.scrolling() {
			content = s.shell().pane.ViewScrollback()
		}
		// The highlight goes on before the tab strip, so the selection's rows are the
		// pane's own rows — the same coordinates the drag was measured in.
		content = m.selectedView(content)
		if len(s.shells) > 1 {
			content = m.renderShellTabs(s) + "\n" + content
		}
		return pane(m.focused(), content)
	}

	return pane(false, m.renderDetails(m.paneW))
}

// deadBanner is the line across the top of a dropped session's pane: that the
// connection is gone, why when the transport said, and the two keys that answer it.
// It is on the pane rather than only in the status line because the status line
// expires after a few seconds and a dropped connection does not.
func (m *model) deadBanner(s *session) string {
	head := redText.Bold(true).Render("⚠ connection lost")
	if s.lostWhy != "" {
		// The reason comes off the wire, so it is stripped like any remote string.
		head += faint.Render(" · " + stripControl(s.lostWhy))
	}
	keys := keyHint("r", "reconnect") + "  " + keyHint("d", "drop")
	gap := max(m.paneW-lipgloss.Width(head)-lipgloss.Width(keys), 1)
	return truncate(head+strings.Repeat(" ", gap)+keys, m.paneW)
}

// deadContent is the frozen screen shown under the banner: whichever view the
// session was showing when its connection went. It mirrors the live cases in
// renderRight, with one addition — a session that has been left with nothing but a
// dead connection (every shell and tab already gone) still has a pane to fill.
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

// updateHint is the footer's "a newer hop exists" line, or "" when there is
// nothing to say. It names the command rather than a key: updating swaps the
// running binary, which is not something a single keystroke should do by
// accident mid-session.
func (m *model) updateHint() string {
	if m.updateLatest == "" {
		return ""
	}
	return yellowText.Render("⬆ hop "+m.updateLatest+" available") + " " + dimStyle.Render("· hop self-update")
}

// sidebarHint is the footer's ctrl+b entry. It names the outcome rather than the
// toggle — "hide hosts" or "show hosts" — because a legend that only says "sidebar"
// leaves you to guess which way the key goes.
func (m *model) sidebarHint() string {
	if m.sidebarHidden {
		return keyHint("ctrl+b", "show hosts")
	}
	return keyHint("ctrl+b", "hide hosts")
}

// renderFooter is the key legend for the mode you are in. It is the same list the
// help card holds, cut down to the keys that are live right now — the card is
// there for when that is not enough.
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

	// Above every pane mode: while the leader is open the footer *is* the menu. It has
	// to be, because the leader waits indefinitely — without this the pane would simply
	// stop responding to keys with nothing on screen to say why.
	case m.leaderArmed():
		hints = []string{
			accentText.Render("leader"),
			keyHint("o", "out"), keyHint("1-9", "tab"),
			keyHint("0", "new shell"), keyHint("c", "vs code here"),
			dimStyle.Render("any other key cancels"),
		}

	// Above the three pane modes, the way handleKey routes the keys: a dead pane has
	// its own small keyboard, and a legend still offering shift+←→ would be listing
	// keys that do nothing.
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
		// ctrl+o 0 is named even on a host with one shell — it is the chord that *makes*
		// the second one, so a footer that waited for a second shell to mention it
		// would only ever tell you what you had already worked out.
		hints = []string{keyHint("ctrl+o o", "back"), keyHint("esc esc", "back"), keyHint("ctrl+o", "leader")}
		s := m.sessions[m.active]
		// The chord is only named where it would do the thing it says: with a directory
		// to hand. Without one a second ctrl+o still opens VS Code, but on the host's
		// default directory, and a footer promising "this dir" for that would be lying.
		if m.shellCwd(m.active) != "" {
			hints = append(hints, keyHint("ctrl+o c", "vs code here"))
		}
		if s != nil && len(s.shells) > 1 {
			hints = append(hints, keyHint("shift+←→", "shell"), keyHint("ctrl+o 1-9", "jump"))
		}
		// shift+↑ enters scrollback, but only where there is history to see and no
		// full-screen program owns the screen — the same conditions the entry chord
		// itself checks, so the hint never offers what the key would decline.
		if s != nil && s.shell() != nil && !s.shell().pane.AltScreen() && s.shell().pane.ScrollbackLen() > 0 {
			hints = append(hints, keyHint("shift+↑", "scrollback"))
		}
		hints = append(hints, m.sidebarHint())
		hints = append(hints, dimStyle.Render("keys →")+" "+greenText.Render(m.active))

	case m.filtering:
		hints = []string{keyHint("type", "filter"), keyHint("↑↓", "move"), keyHint("enter", "apply"), keyHint("esc", "clear")}

	default:
		// The per-host keys (S, s, o) are spelled out in the details card beside
		// this line, so the footer keeps to the ones that are always true.
		hints = []string{
			keyHint("↑↓", "move"), keyHint("enter", "connect"), keyHint("f", "sftp"),
			keyHint("t", "tunnels"),
			keyHint("a", "add"), keyHint("i", "import"), keyHint("e", "edit"), keyHint("x", "delete"),
			keyHint("p", "pin"),
			keyHint("/", "filter"), keyHint(",", "settings"), keyHint("?", "keys"), keyHint("q", "quit"),
		}
		// 'r' is bound in the list at all times, but it is only worth a slot in the
		// legend when there is a dropped session under the cursor to spend it on.
		// Same for the reorder keys, which do something only on a pinned host.
		if h, ok := m.selectedHost(); ok {
			if h.Pinned {
				hints = append([]string{keyHint("shift+jk", "reorder")}, hints...)
			}
			if s := m.sessions[h.Alias]; s != nil && s.dead {
				hints = append([]string{keyHint("r", "reconnect")}, hints...)
			}
		}
		// Collapsed, the way back is the only key that matters — so it goes first,
		// where a narrow window's truncation cannot be what drops it.
		if m.sidebarHidden {
			hints = append([]string{m.sidebarHint()}, hints...)
		} else {
			hints = append(hints, m.sidebarHint())
		}
		// News, not a key — so it goes first, where the truncation that trims a
		// long legend on a narrow window can't be what drops it.
		if h := m.updateHint(); h != "" {
			hints = append([]string{h}, hints...)
		}
	}
	return footerStyle.Render(truncate(strings.Join(hints, sep), max(m.width-2, 0)))
}
