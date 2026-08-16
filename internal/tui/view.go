package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"hop/internal/config"
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

	screen := lipgloss.JoinVertical(lipgloss.Left, m.renderHeader(), body, m.renderStatus(), m.renderFooter())

	// The context menu is composited before the cards, and positioned rather than
	// centred: it belongs to a row of the list, not to the middle of the screen. A card
	// opened from it therefore lands on top of it — which cannot happen, since running an
	// action closes the menu first.
	if m.menu.open {
		card, x, y := m.menuAt()
		screen = overlay(screen, card, x, y)
	}

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
	case m.guidance.open:
		return m.renderGuidance()
	case m.help:
		return m.renderHelp()
	case m.hostKey.open:
		return m.renderHostKeyConfirm()
	case m.confirm.open:
		return m.renderConfirm()
	case m.palette.open:
		return m.renderPalette()
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

// renderHeader draws the title on the left and the state of the world on the right: how
// many hosts are connected, and what last happened. Where you are is not here — that is
// the status bar's line, beside the keys that act on it (see renderStatus).
func (m *model) renderHeader() string {
	left := headerBadge.Render("hop")

	var chips []string
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

// renderFooter is the key legend, cut to what this mode cannot be worked without. It is
// deliberately short: the keys hop binds no longer fit a row at 80 columns, and a legend
// that overflows teaches nothing. What it keeps per mode is the way out, the one or two
// keys you would not guess, and "?" — which is the hint that makes every other hint
// optional, so it goes last and truncation is never allowed to reach it.
//
// The full table is the help card, and since 0.9 it opens on the section for the mode you
// are in (see renderHelp). The two are one design: this line is the reminder, the card is
// the reference.
func (m *model) renderFooter() string {
	core, extra, help := m.footerHints()
	return m.footerLine(core, extra, help)
}

// footerHints is the legend as three lists: the keys this mode cannot be worked without,
// the ones worth showing when the window is wide enough to hold them, and the one that
// reaches the help card — which footerLine holds back so truncation cannot eat it.
//
// The split is what makes the row honest at both ends of the range. At 80 columns you get
// the core and nothing else; at 200 you get the keys a wide terminal has room for anyway.
// Nothing is hidden that would not have fit: extra is filled in priority order and cut at
// the first one that does not.
//
// Split out from the rendering because the rule worth testing is "how many keys, and
// which", and that rule is invisible once the row is a styled string.
func (m *model) footerHints() (core, extra []string, help string) {
	// How this mode reaches the card. Where the keys are forwarded — a live shell, an
	// editor — a bare "?" is text the remote is owed, so there it is the leader chord
	// (see handleLeader). Scrollback and a dead pane forward nothing, so they take the
	// plain key, and while filtering the key is part of the filter and there is none.
	help = keyHint("?", "keys")
	switch {
	case m.filtering:
		help = ""
	case m.activeDead():
		// Forwards nothing: the plain key, set above.
	case m.editing() || m.mode == modeShell:
		help = keyHint("ctrl+o ?", "keys")
	}
	// Cards first, and on their own terms: a card's keys are the card, not a reminder of
	// it, and none of them leaves room for a "?" that the card would swallow anyway.
	switch {
	case m.auth.open:
		return []string{keyHint("enter", "submit"), keyHint("esc", "cancel"), keyHint("ctrl+u", "clear")}, nil, ""

	case m.guidance.open:
		return []string{keyHint("↑↓", "pick"), keyHint("enter", "start hopping")}, nil, ""

	case m.help:
		return []string{keyHint("esc", "close")}, nil, ""

	case m.hostKey.open:
		return []string{keyHint("y", "trust"), keyHint("n", "cancel")}, nil, ""

	case m.confirm.open:
		return []string{keyHint("y", "delete"), keyHint("n", "cancel")}, nil, ""

	case m.palette.open:
		return []string{keyHint("type", "search"), keyHint("enter", "run"), keyHint("esc", "close")}, nil, ""

	case m.menu.open:
		return []string{keyHint("↑↓", "move"), keyHint("enter", "run"), keyHint("esc", "close")}, nil, ""

	case m.hostForm.open:
		return []string{keyHint("tab", "next"), keyHint("enter", "save"), keyHint("esc", "cancel"), keyHint("ctrl+u", "clear")}, nil, ""

	case m.importer.open:
		exit := keyHint("esc", "cancel")
		if m.importer.first {
			exit = keyHint("esc", "skip")
		}
		return []string{keyHint("enter", "import"), exit, keyHint("ctrl+u", "clear")}, nil, ""

	case m.tunnels.open && m.tunnels.editing:
		return []string{keyHint("tab", "next"), keyHint("enter", "save"), keyHint("esc", "back"), keyHint("ctrl+u", "clear")}, nil, ""

	case m.tunnels.open:
		return []string{keyHint("enter", "start / stop"), keyHint("a", "add"), keyHint("e", "edit"), keyHint("esc", "close")}, nil, ""

	case m.settings.open && m.settings.editing:
		return []string{keyHint("enter", "save"), keyHint("esc", "cancel"), keyHint("ctrl+u", "clear")}, nil, ""

	case m.settings.open:
		return []string{keyHint("enter", "edit"), keyHint("r", "reset"), keyHint("esc", "close")}, nil, ""

	// Above every pane mode: while the leader is open the footer is the menu. It has to
	// be, since the leader waits indefinitely — this is the one legend that is the whole
	// keyboard rather than a slice of it.
	case m.leaderArmed():
		menu := []string{
			accentText.Render("leader"),
			keyHint("o", "out"), keyHint("1-9", "tab"),
			keyHint("0", "new shell"),
		}
		// Named only where it would do what it says. Without a directory to hand the
		// chord still opens VS Code, but on the host's default one — and this menu is now
		// the only place the chord is written down, so the condition lives here.
		if m.shellCwd(m.chords.leaderAlias) != "" {
			menu = append(menu, keyHint("c", "vs code here"))
		}
		menu = append(menu, keyHint("ctrl+k", "actions"), keyHint("?", "keys"),
			dimStyle.Render("any other key cancels"))
		return menu, nil, ""
	}

	// The modes. Each keeps the way out, what it is for, and nothing you could find on
	// the card instead.
	switch {
	// A dead pane has its own small keyboard; a legend offering shift+←→ would name keys
	// that do nothing.
	case m.active != "" && m.inPane() && m.activeDead():
		core = []string{keyHint("r", "reconnect"), keyHint("d", "drop session"), keyHint("ctrl+o", "back")}

	case m.editing() && m.active != "":
		core = []string{keyHint("ctrl+o o", "browser"), keyHint(":q", "close"), keyHint("shift+←→", "tab")}
		extra = []string{keyHint("ctrl+o 1-9", "jump"), m.sidebarHint()}

	case m.browsing() && m.active != "":
		core = []string{keyHint("ctrl+o", "back"), keyHint("enter", "edit"), keyHint("d", "download")}
		extra = []string{keyHint("ctrl+k", "actions"), keyHint("←", "up"), keyHint("u", "upload"),
			keyHint("o", "open local"), keyHint("x", "delete"), keyHint("R", "rename"),
			keyHint("m", "mkdir"), keyHint("s", "sort"), keyHint("r", "refresh"), m.sidebarHint()}

	case m.scrolling() && m.focused() && m.active != "":
		core = []string{keyHint("esc", "back to live"), keyHint("↑↓", "scroll"), keyHint("g/G", "top/live")}
		extra = []string{keyHint("pgup/pgdn", "page")}

	case m.focused() && m.active != "":
		// The leader earns its slot in every shell: it is the door to the rest, including
		// the card, and the one chord nothing else on screen implies.
		core = []string{keyHint("ctrl+o o", "back"), keyHint("ctrl+o", "leader")}
		s := m.sessions[m.active]
		if s != nil && len(s.shells) > 1 {
			core = append(core, keyHint("shift+←→", "shell"))
			extra = append(extra, keyHint("ctrl+o 1-9", "jump"))
		}
		// The same conditions the chords check, so a wide window never names a key that
		// would decline: VS Code wants a directory, scrollback wants history behind a
		// shell that is not on its alternate screen.
		if m.shellCwd(m.active) != "" {
			extra = append(extra, keyHint("ctrl+o c", "vs code here"))
		}
		if s != nil && s.shell() != nil && !s.shell().pane.AltScreen() && s.shell().pane.ScrollbackLen() > 0 {
			extra = append(extra, keyHint("shift+↑", "scrollback"))
		}
		extra = append(extra, keyHint("esc esc", "back"), m.sidebarHint())

	case m.filtering:
		core = []string{keyHint("type", "filter"), keyHint("enter", "apply"), keyHint("esc", "clear")}
		extra = []string{keyHint("↑↓", "move")}

	default:
		// The list. Its per-host keys are spelled out in the details card beside this
		// line, and all of them are on the help card, so the footer keeps to moving,
		// connecting and the two that make the list itself.
		// The menu key sits in the core beside connect: it is the one hint that stands in
		// for every per-host key below it, so a narrow window that keeps three hints still
		// shows the way to all of them.
		core = []string{keyHint("enter", "connect"), keyHint("space", "actions"), keyHint("/", "filter")}
		extra = []string{
			keyHint("ctrl+k", "search actions"), keyHint("↑↓", "move"), keyHint("f", "sftp"),
			keyHint("a", "add"), keyHint("e", "edit"), keyHint("x", "delete"),
			keyHint("p", "pin"), keyHint("t", "tunnels"), keyHint("i", "import"),
			keyHint(",", "settings"), keyHint("esc esc", "quit"),
		}
		// Only when the host under the cursor is one you can reconnect — otherwise the
		// slot goes to adding a host, which on an empty list is the only thing to do.
		if h, ok := m.selectedHost(); ok {
			if h.Pinned {
				extra = append(extra, keyHint("shift+jk", "reorder"))
			}
			if s := m.sessions[h.Alias]; s != nil && s.dead {
				core = []string{keyHint("r", "reconnect"), keyHint("enter", "connect"), keyHint("f", "sftp")}
				extra = append([]string{keyHint("d", "drop session")}, extra...)
			}
		} else {
			core = []string{keyHint("a", "add host"), keyHint("i", "import")}
			extra = []string{keyHint("ctrl+k", "search actions"), keyHint(",", "settings"), keyHint("esc esc", "quit")}
		}
	}

	// Collapsed, the way back to the hosts outranks the mode's own keys: nothing else on
	// screen says the sidebar is still there.
	if m.sidebarHidden {
		core = append([]string{m.sidebarHint()}, core...)
	}
	return m.guidedHints(core, extra, help)
}

// guidedHints is the guidance profile's only say over the legend: how much of it is
// offered. It cannot add or remove a binding — every key works in all three profiles —
// so a quiet footer is a legend, not a smaller keyboard.
//
// keys keeps the core and drops the extras a wide window would have room for. guided
// adds the way to the action list in the modes where it is a chord nobody would guess;
// the host list already names it in its core.
func (m *model) guidedHints(core, extra []string, help string) ([]string, []string, string) {
	switch m.cfg.Guidance {
	case config.GuidanceKeys:
		return core, nil, help
	case config.GuidanceGuided:
		if h := m.actionsHint(); h != "" {
			// Promoted out of the extras rather than repeated: guided means it is always
			// on the row, not that it is on it twice.
			extra = without(extra, h)
			core = append(core, h)
		}
	}
	return core, extra, help
}

// actionsHint is how this mode reaches the action list, or "" for the host list, whose
// core already says it. In a pane it is behind the leader, for the reason the card is.
func (m *model) actionsHint() string {
	switch m.mode {
	case modeList:
		// The same hint the list offers among its extras, so promoting it moves it
		// rather than doubling it.
		return keyHint(paletteKey, "search actions")
	case modeBrowser:
		return keyHint(paletteKey, "actions")
	case modeScrollback:
		// Forwards nothing and answers to a small keyboard of its own; the palette is a
		// key away once esc has handed the shell back.
		return ""
	default:
		return keyHint(leaderKey+" "+paletteKey, "actions")
	}
}

// without returns hints with every copy of hint removed.
func without(hints []string, hint string) []string {
	out := hints[:0:0]
	for _, h := range hints {
		if h != hint {
			out = append(out, h)
		}
	}
	return out
}

// footerLine renders the legend to fit the window. The core keys and the trailing ones —
// news of an update, the key to the card — are laid out first; whatever room is left over
// goes to the extras, in order, stopping at the first that does not fit. A wide terminal
// therefore shows more of the keyboard without a narrow one showing a cut-off word.
func (m *model) footerLine(core, extra []string, tail string) string {
	const sep = "  "

	var keep []string
	// News of a release belongs to the list, not to a pane: while keystrokes are going to
	// another machine, the row has one job, which is saying how to stop them going there.
	if h := m.updateHint(); h != "" && !m.inPane() {
		keep = append(keep, h)
	}
	if tail != "" {
		keep = append(keep, tail)
	}

	room := max(m.width-2, 0)
	fixedW := 0
	if len(keep) > 0 {
		fixedW = lipgloss.Width(strings.Join(keep, sep)) + len(sep)
	}

	// The extras go on while they fit whole. Cutting one mid-word would spend the room on
	// a key you cannot read, which is worse than not offering it.
	// Copied, since the width probe below appends to it speculatively and core is the
	// caller's slice.
	hints := append([]string{}, core...)
	for _, e := range extra {
		w := lipgloss.Width(strings.Join(append(hints, e), sep)) + fixedW
		if w > room {
			break
		}
		hints = append(hints, e)
	}

	// If even the core overruns, whole hints go rather than a word being cut in half: a
	// legend ending in "shift+←…" names no key. They go from the right, which is why each
	// mode's list starts with its way out.
	for len(hints) > 1 && lipgloss.Width(strings.Join(hints, sep))+fixedW > room {
		hints = hints[:len(hints)-1]
	}

	line := strings.Join(hints, sep)
	if len(keep) > 0 {
		// Whatever is held back is laid down last and never cut into: on a window too
		// narrow even for one hint, the way to the card is what survives.
		line = truncate(line, max(room-fixedW, 0))
		if line != "" {
			line += sep
		}
		line += strings.Join(keep, sep)
	}
	return footerStyle.Render(truncate(line, room))
}
