package tui

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/lipgloss"

	"hop/internal/config"
	"hop/internal/keys"
)

// View composes the screen and overlays the context menu and any modal card. Every return
// carries the alt screen and the mouse mode: the renderer diffs this view against the last,
// so a frame that omits one switches it off.
func (m *model) View() tea.View {
	v := tea.NewView("loading hop…")
	v.AltScreen = true
	v.MouseMode = m.mouseMode()
	if !m.ready {
		return v
	}

	// Widths follow the active session, so measure this frame rather than the last.
	m.recomputeLayout()

	body := m.renderRight(m.frame.content.h)
	if r := m.frame.tree; !r.empty() {
		body = lipgloss.JoinHorizontal(lipgloss.Top, m.renderTree(r), body)
	}
	if r := m.frame.list; !r.empty() {
		body = lipgloss.JoinHorizontal(lipgloss.Top, m.renderList(r.w, r.h), body)
	}

	screen := lipgloss.JoinVertical(lipgloss.Left, m.renderHeader(), body, m.renderStatus(), m.renderFooter())

	// Positioned rather than centred: the menu belongs to a list row.
	if m.menu.open {
		card, x, y := m.menuAt()
		screen = overlay(screen, card, x, y)
	}

	if card := m.modalCard(); card != "" {
		// An over-wide line would push the row's border off screen.
		card = clampLines(card, m.width)
		x, y := centered(m.width, m.height, lipgloss.Width(card), lipgloss.Height(card))
		screen = overlay(screen, card, x, y)
	}
	// Last, so the key trail floats over the cards too.
	v.SetContent(m.keycastDraw(screen))
	return v
}

// mouseMode is the mouse the frame asks for; switching it off is a different view, not a
// command. See settings.toggleMouse.
func (m *model) mouseMode() tea.MouseMode {
	if m.cfg.Mouse {
		return tea.MouseModeCellMotion
	}
	return tea.MouseModeNone
}

// capturing reports whether a modal card, the context menu or a browser prompt owns the input.
func (m *model) capturing() bool {
	return m.modalCard() != "" || m.menu.open || m.browserPrompting()
}

// browserPrompting reports whether the active session's browser is awaiting a typed answer.
func (m *model) browserPrompting() bool {
	if !m.browsing() {
		return false
	}
	s := m.sessions[m.active]
	return s != nil && s.browser != nil && s.browser.Prompting()
}

// modalCard is the popover currently up, or "". Only one can be open at a time.
func (m *model) modalCard() string {
	switch {
	// Before help: an auth dial can be parked on this card.
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

// renderHeader draws the title on the left, session count and last status on the right.
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

// styledStatus colors the status line by statusKind rather than by wording.
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
	return style.Render(truncate(icon+" "+m.status, max(m.width/2, 20)))
}

// ---- the tree column ----

// renderTree draws the SFTP column; the border says whether the keyboard is in it.
func (m *model) renderTree(r rect) string {
	s := m.sessions[m.active]
	if s == nil || s.browser == nil {
		return ""
	}
	innerW, innerH := max(r.innerW(), 1), max(r.innerH(), 1)
	return columnStyle(m.browsing()).
		Width(innerW).Height(innerH).
		Render(clampLines(fitLines(s.browser.View(), innerH), innerW))
}

// columnStyle accents a body column while it holds the keyboard, and dims it otherwise.
func columnStyle(active bool) lipgloss.Style {
	if active {
		return paneBorderActive
	}
	return paneBorderIdle
}

// ---- the content area ----

// contentIsSplit mirrors renderRight's switch: does the content area draw two boxes?
func (m *model) contentIsSplit() bool {
	s := m.sessions[m.active]
	if !m.splitOn(s) {
		return false
	}
	// The renderRight arms that draw one full-width box, in the order that decides which wins.
	switch {
	case s.dead && m.active != "":
		return false
	case m.treeInline() && m.browsing() && s.browser != nil:
		return false
	case m.focused() && s.shell() != nil:
		return false
	}
	return s.editor() != nil
}

// renderRight draws what the active session shows in the content area. Every arm that
// draws a box is mirrored in contentIsSplit.
func (m *model) renderRight(h int) string {
	innerH := max(h-2, 1)
	s := m.sessions[m.active]

	// Inactive border even when focused: the accent would promise a live shell.
	if s != nil && s.dead && m.active != "" {
		return m.contentBox(false, m.paneW, innerH, m.deadBanner(s)+"\n"+m.deadContent(s))
	}

	switch {
	// No column to put it in, so the browser takes the content area. See treeWidth.
	case m.treeInline() && m.browsing() && s != nil && s.browser != nil:
		return m.contentBox(true, m.paneW, innerH, s.browser.View())

	case m.focused() && s != nil && s.shell() != nil:
		return m.renderShellPane(s, innerH)

	case s != nil && s.editor() != nil:
		return m.renderEditorPanes(s, innerH)

	case m.active != "" && s != nil && s.shell() != nil:
		return m.renderShellPane(s, innerH)
	}

	return m.contentBox(false, m.paneW, innerH, m.renderDetails(m.paneW))
}

// contentBox draws one box of the content area. Clipping the width matters: lipgloss wraps
// an over-wide line, which would grow the screen past the window.
func (m *model) contentBox(active bool, w, innerH int, content string) string {
	style := paneBorder
	switch {
	case active:
		style = paneBorderActive
	case !m.frame.tree.empty():
		style = paneBorderIdle
	}
	return style.Width(w).Height(innerH).Render(clampLines(fitLines(content, innerH), w))
}

// renderShellPane draws a live shell in the content area, with its strip of tabs once
// there is a second one to switch to.
func (m *model) renderShellPane(s *session, innerH int) string {
	content := s.shell().pane.View()
	if m.focused() && m.scrolling() {
		content = s.shell().pane.ViewScrollback()
	}
	// Before the tab strip, so the selection's rows match the coordinates the drag used.
	content = m.selectedView(content)
	if len(s.shells) > 1 {
		content = m.renderShellTabs(s) + "\n" + content
	}
	return m.contentBox(m.focused(), m.paneW, innerH, content)
}

// renderEditorPanes draws the open files: one box, or two side by side while split.
func (m *model) renderEditorPanes(s *session, innerH int) string {
	if !m.contentIsSplit() {
		// Asked of contentIsSplit rather than splitOn so this box and the frame agree.
		half := s.focusedHalf()
		ed := s.editorAt(half)
		return m.contentBox(m.editing(), m.paneW, innerH,
			m.renderEditorTabs(s, half)+"\n"+m.selectedView(ed.pane.View()))
	}

	w := m.frame.left.innerW()
	half := func(right bool) string {
		focused := m.editing() && s.splitRight == right
		ed := s.editorAt(right)
		if ed == nil {
			// Reachable only between the last tab closing and dropEditor collapsing the split.
			return m.contentBox(focused, w, innerH, "")
		}
		view := ed.pane.View()
		if focused {
			view = m.selectedView(view)
		}
		return m.contentBox(focused, w, innerH, m.renderEditorTabs(s, right)+"\n"+view)
	}
	return lipgloss.JoinHorizontal(lipgloss.Top, half(false), half(true))
}

// deadBanner sits on the pane rather than the status line, which expires after a few seconds.
func (m *model) deadBanner(s *session) string {
	head := redText.Bold(true).Render("⚠ connection lost")
	if s.lostWhy != "" {
		head += faint.Render(" · " + stripControl(s.lostWhy))
	}
	ways := m.hint(keys.DeadPane, keys.DeadReconnect, "reconnect") + "  " +
		m.hint(keys.DeadPane, keys.DeadDrop, "drop")
	gap := max(m.paneW-lipgloss.Width(head)-lipgloss.Width(ways), 1)
	return truncate(head+strings.Repeat(" ", gap)+ways, m.paneW)
}

// deadContent is the frozen view the session was showing when its connection went.
func (m *model) deadContent(s *session) string {
	switch {
	case m.editing() && s.editor() != nil:
		return m.renderEditorTabs(s, s.focusedHalf()) + "\n" + s.editor().pane.View()
	// Only in the narrow fallback; with a column the browser is drawn beside this pane.
	case m.treeInline() && m.browsing() && s.browser != nil:
		return s.browser.View()
	case s.shell() != nil:
		content := s.shell().pane.View()
		if len(s.shells) > 1 {
			content = m.renderShellTabs(s) + "\n" + content
		}
		return content
	case s.editor() != nil:
		return m.renderEditorTabs(s, s.focusedHalf()) + "\n" + s.editor().pane.View()
	case m.treeInline() && s.browser != nil:
		return s.browser.View()
	}
	return "\n" + dimStyle.Render("  Nothing is left open on this connection.")
}

// ---- footer ----

// updateHint names the command rather than a key: updating swaps the running binary.
func (m *model) updateHint() string {
	if m.updateLatest == "" {
		return ""
	}
	return yellowText.Render("⬆ hop "+m.updateLatest+" available") + " " + dimStyle.Render("· hop self-update")
}

// sidebarHint names the outcome rather than the toggle.
func (m *model) sidebarHint() string {
	// Too narrow for the list means no toggle to name; toggleSidebar declines for the same reason.
	if !m.sidebarFits() {
		return ""
	}
	if m.sidebarHidden {
		return m.hint(keys.Global, keys.Sidebar, "show hosts")
	}
	return m.hint(keys.Global, keys.Sidebar, "hide hosts")
}

// hint is one footer entry: the key bound to an action, and the footer's own word for it.
// An unbound action leaves no hint rather than a dead one.
func (m *model) hint(l keys.Layer, id keys.Action, word string) string {
	b, ok := m.binds.BindingIn(l, id)
	if !ok || b.Keycap() == "" {
		return ""
	}
	return keyHint(b.Keycap(), word)
}

// chordHint is hint for a key behind the leader, drawn as the two keystrokes it is.
func (m *model) chordHint(id keys.Action, word string) string {
	lead := m.binds.Keycap(keys.LeaderKey)
	b, ok := m.binds.BindingIn(keys.Leader, id)
	if !ok || lead == "" || b.Keycap() == "" {
		return ""
	}
	return keyHint(lead+" "+b.Keycap(), word)
}

// leaderRange is the digits behind the leader — a range rather than a binding.
func (m *model) leaderRange(word string) string {
	lead := m.binds.Keycap(keys.LeaderKey)
	if lead == "" {
		return ""
	}
	return keyHint(lead+" 1-9", word)
}

// compact drops the hints that resolved to nothing.
func compact(hints []string) []string {
	out := hints[:0:0]
	for _, h := range hints {
		if h != "" {
			out = append(out, h)
		}
	}
	return out
}

// renderFooter is the key legend, cut to what this mode cannot be worked without. The full
// table is the help card, which opens on the section for the mode you are in.
func (m *model) renderFooter() string {
	core, extra, help := m.footerHints()
	return m.footerLine(core, extra, help)
}

// footerArm is one row of the legend's table. The arms are ordered and the first match wins:
// several predicates are true at once by design, so the order is the rule, not a listing.
type footerArm struct {
	// when is the state this arm speaks for.
	when func(m *model) bool
	// hints is the legend: keys the state needs, then the ones a wide window has room for.
	// A function because most hints are read out of the keyboard; see fixedHints for the rest.
	hints func(m *model) (core, extra []string)
}

// fixedHints is for arms whose keys are spelled out rather than looked up in the registry.
func fixedHints(hints ...string) func(*model) (core, extra []string) {
	return func(*model) ([]string, []string) { return hints, nil }
}

// footerCardArms are the arms that are the whole legend: a core, no extras, no help key. The
// leader is last of them and outranks every pane mode, since it waits indefinitely.
var footerCardArms = []footerArm{
	{
		when:  func(m *model) bool { return m.auth.open },
		hints: fixedHints(keyHint("enter", "submit"), keyHint("esc", "cancel"), keyHint("ctrl+u", "clear")),
	},
	{
		when:  func(m *model) bool { return m.guidance.open },
		hints: fixedHints(keyHint("↑↓", "pick"), keyHint("enter", "start hopping")),
	},
	{
		when:  func(m *model) bool { return m.help },
		hints: fixedHints(keyHint("esc", "close")),
	},
	{
		when:  func(m *model) bool { return m.hostKey.open },
		hints: fixedHints(keyHint("y", "trust"), keyHint("n", "cancel")),
	},
	{
		when:  func(m *model) bool { return m.confirm.open },
		hints: fixedHints(keyHint("y", "delete"), keyHint("n", "cancel")),
	},
	{
		when:  func(m *model) bool { return m.palette.open },
		hints: fixedHints(keyHint("type", "search"), keyHint("enter", "run"), keyHint("esc", "close")),
	},
	{
		when:  func(m *model) bool { return m.menu.open },
		hints: fixedHints(keyHint("↑↓", "move"), keyHint("enter", "run"), keyHint("esc", "close")),
	},
	{
		when: func(m *model) bool { return m.hostForm.open },
		hints: fixedHints(keyHint("tab", "next"), keyHint("enter", "save"),
			keyHint("esc", "cancel"), keyHint("ctrl+u", "clear")),
	},
	{
		// esc skips a step on a first run rather than abandoning one the user asked for.
		when: func(m *model) bool { return m.importer.open },
		hints: func(m *model) ([]string, []string) {
			exit := keyHint("esc", "cancel")
			if m.importer.first {
				exit = keyHint("esc", "skip")
			}
			return []string{keyHint("enter", "import"), exit, keyHint("ctrl+u", "clear")}, nil
		},
	},
	{
		// A card inside a card: esc goes back to the list. Above the manager's own arm.
		when: func(m *model) bool { return m.tunnels.open && m.tunnels.editing },
		hints: fixedHints(keyHint("tab", "next"), keyHint("enter", "save"),
			keyHint("esc", "back"), keyHint("ctrl+u", "clear")),
	},
	{
		when: func(m *model) bool { return m.tunnels.open },
		hints: fixedHints(keyHint("enter", "start / stop"), keyHint("a", "add"),
			keyHint("e", "edit"), keyHint("esc", "close")),
	},
	{
		// The same nesting as the tunnels pair, and the same ordering for the same reason.
		when:  func(m *model) bool { return m.settings.open && m.settings.editing },
		hints: fixedHints(keyHint("enter", "save"), keyHint("esc", "cancel"), keyHint("ctrl+u", "clear")),
	},
	{
		when:  func(m *model) bool { return m.settings.open },
		hints: fixedHints(keyHint("enter", "edit"), keyHint("r", "reset"), keyHint("esc", "close")),
	},
	{
		when: (*model).leaderArmed,
		hints: func(m *model) ([]string, []string) {
			menu := []string{
				accentText.Render("leader"),
				m.hint(keys.Leader, keys.LeaderOut, "out"),
				keyHint("1-9", "tab"),
				m.hint(keys.Leader, keys.LeaderShell, "new shell"),
			}
			// Named only where it would work: without a cwd the chord opens the host's default.
			if m.shellCwd(m.chords.leaderAlias) != "" {
				menu = append(menu, m.hint(keys.Leader, keys.LeaderVSCode, "vs code here"))
			}
			menu = append(menu,
				m.hint(keys.Leader, keys.LeaderPalette, "actions"),
				m.hint(keys.Leader, keys.LeaderHelp, "keys"),
				dimStyle.Render("any other key cancels"))
			return compact(menu), nil
		},
	},
}

// footerModeArms are the modes hop's own keyboard is in. Each keeps the way out, what the
// editorExtras is the editor arm's extra hints. Unsplit is named only while there is a split,
// since naming a key that would decline is worse than not naming it.
func (m *model) editorExtras() []string {
	extra := []string{m.hint(keys.Editor, keys.EditorFocusTree, "tree")}
	if s := m.sessions[m.active]; s != nil && s.split {
		extra = append(extra, m.hint(keys.Editor, keys.EditorUnsplit, "unsplit"))
	}
	return append(extra, m.leaderRange("jump"), m.sidebarHint())
}

// footerModeArms are the modes hop's own keyboard is in. Unlike the cards these keep the
// sidebar hint, the help key and the guidance trim. The last row always matches.
var footerModeArms = []footerArm{
	{
		// Above the mode arms it would fall into: a dropped connection outranks its pane's view.
		when: func(m *model) bool { return m.active != "" && m.inPane() && m.activeDead() },
		hints: func(m *model) ([]string, []string) {
			return []string{
				m.hint(keys.DeadPane, keys.DeadReconnect, "reconnect"),
				m.hint(keys.DeadPane, keys.DeadDrop, "drop session"),
				m.hint(keys.DeadPane, keys.DeadLeave, "back"),
			}, nil
		},
	},
	{
		when: func(m *model) bool { return m.editing() && m.active != "" },
		hints: func(m *model) ([]string, []string) {
			return []string{
				m.chordHint(keys.LeaderOut, "browser"),
				keyHint(":q", "close"), // the remote editor's key, not hop's
				m.hint(keys.Editor, keys.EditorNextTab, "tab"),
			}, m.editorExtras()
		},
	},
	{
		when: func(m *model) bool { return m.browsing() && m.active != "" },
		hints: func(m *model) ([]string, []string) {
			return []string{
				m.hint(keys.Browser, keys.BrowserLeave, "back"),
				m.hint(keys.Browser, keys.In, "edit"),
				m.hint(keys.Browser, keys.BrowserDownload, "download"),
			}, []string{
				m.hint(keys.Browser, keys.BrowserFocusPane, "focus file"),
				m.hint(keys.Browser, keys.BrowserSplit, "open beside"),
				// A copy is three keys nobody guesses, so the selection and target are named.
				m.hint(keys.Browser, keys.BrowserMark, "mark"),
				m.hint(keys.Browser, keys.BrowserTarget, "target"),
				m.hint(keys.Browser, keys.BrowserCopy, "copy there"),
				m.hint(keys.Browser, keys.BrowserMoveTo, "move there"),
				m.hint(keys.Browser, keys.BrowserPalette, "actions"),
				m.hint(keys.Browser, keys.Out, "up"),
				m.hint(keys.Browser, keys.BrowserMarkAll, "mark all"),
				m.hint(keys.Browser, keys.BrowserUpload, "upload"),
				m.hint(keys.Browser, keys.BrowserOpen, "open local"),
				m.hint(keys.Browser, keys.BrowserDelete, "delete"),
				m.hint(keys.Browser, keys.BrowserRename, "rename"),
				m.hint(keys.Browser, keys.BrowserMkdir, "mkdir"),
				m.hint(keys.Browser, keys.BrowserSort, "sort"),
				m.hint(keys.Browser, keys.BrowserRefresh, "refresh"),
				m.hint(keys.Browser, keys.BrowserTree, "tree"),
				m.sidebarHint(),
			}
		},
	},
	{
		// Above the shell's arm: scrolling is focused too, but answers to different keys.
		when: func(m *model) bool { return m.scrolling() && m.focused() && m.active != "" },
		hints: func(m *model) ([]string, []string) {
			return []string{
				m.hint(keys.Scrollback, keys.ScrollLeave, "back to live"),
				keyHint("↑↓", "scroll"),
				keyHint(m.binds.Keycap(keys.ScrollTop)+"/"+m.binds.Keycap(keys.ScrollBottom), "top/live"),
			}, []string{keyHint("pgup/pgdn", "page")}
		},
	},
	{
		when:  func(m *model) bool { return m.focused() && m.active != "" },
		hints: (*model).shellHints,
	},
	{
		// A mode of the list rather than a card, so it keeps the extras; footerHelp drops "?".
		when: func(m *model) bool { return m.filtering },
		hints: func(*model) ([]string, []string) {
			return []string{keyHint("type", "filter"), keyHint("enter", "apply"), keyHint("esc", "clear")},
				[]string{keyHint("↑↓", "move")}
		},
	},
	{
		// Everything else, which is the host list.
		when:  func(*model) bool { return true },
		hints: (*model).listHints,
	},
}

// shellHints is a live shell's legend; three of its keys are offered only where they work.
func (m *model) shellHints() (core, extra []string) {
	core = []string{
		m.chordHint(keys.LeaderOut, "back"),
		m.hint(keys.Pane, keys.LeaderKey, "leader"),
	}
	s := m.sessions[m.active]
	if s != nil && len(s.shells) > 1 {
		core = append(core, m.hint(keys.Pane, keys.PaneNextTab, "shell"))
		extra = append(extra, m.leaderRange("jump"))
	}
	// The same conditions the chords check, so a wide window never names a key that declines.
	if m.shellCwd(m.active) != "" {
		extra = append(extra, m.chordHint(keys.LeaderVSCode, "vs code here"))
	}
	if s != nil && s.shell() != nil && !s.shell().pane.AltScreen() && s.shell().pane.ScrollbackLen() > 0 {
		extra = append(extra, m.hint(keys.Pane, keys.PaneScroll, "scrollback"))
	}
	return core, append(extra, m.hint(keys.Pane, keys.PaneLeave, "back"), m.sidebarHint())
}

// listHints is the host list's legend, and the only one that changes with what is under the
// cursor. The menu key stands in for every per-host key below it.
func (m *model) listHints() (core, extra []string) {
	core = []string{
		m.hint(keys.List, keys.In, "connect"),
		m.hint(keys.List, keys.Menu, "actions"),
		m.hint(keys.List, keys.Filter, "filter"),
	}
	extra = []string{
		m.hint(keys.List, keys.Palette, "search actions"),
		keyHint("↑↓", "move"),
		m.hint(keys.List, keys.HostBrowser, "sftp"),
		m.hint(keys.List, keys.HostAdd, "add"),
		m.hint(keys.List, keys.HostEdit, "edit"),
		m.hint(keys.List, keys.HostDelete, "delete"),
		m.hint(keys.List, keys.HostPin, "pin"),
		m.hint(keys.List, keys.HostTunnels, "tunnels"),
		m.hint(keys.List, keys.HostImport, "import"),
		m.hint(keys.List, keys.Settings, "settings"),
		m.hint(keys.List, keys.Quit, "quit"),
	}
	// Only for a host you can reconnect; otherwise the slot goes to adding one.
	if h, ok := m.selectedHost(); ok {
		if h.Pinned {
			extra = append(extra, keyHint(
				m.binds.Keycap(keys.HostPinUp)+m.binds.Keycap(keys.HostPinDown), "reorder"))
		}
		if s := m.sessions[h.Alias]; s != nil && s.dead {
			core = []string{
				m.hint(keys.List, keys.HostReconnec, "reconnect"),
				m.hint(keys.List, keys.In, "connect"),
				m.hint(keys.List, keys.HostBrowser, "sftp"),
			}
			extra = append([]string{m.hint(keys.List, keys.HostDrop, "drop session")}, extra...)
		}
		return core, extra
	}
	return []string{
		m.hint(keys.List, keys.HostAdd, "add host"),
		m.hint(keys.List, keys.HostImport, "import"),
	}, []string{
		m.hint(keys.List, keys.Palette, "search actions"),
		m.hint(keys.List, keys.Settings, "settings"),
		m.hint(keys.List, keys.Quit, "quit"),
	}
}

// footerHelp is how this mode reaches the help card. Where keys are forwarded a bare "?" is
// text the remote is owed, so there it is the leader chord.
func (m *model) footerHelp() string {
	switch {
	case m.filtering:
		return ""
	case m.activeDead():
		return m.hint(keys.DeadPane, keys.DeadHelp, "keys")
	case m.editing() || m.mode == modeShell:
		return m.chordHint(keys.LeaderHelp, "keys")
	}
	return m.hint(keys.List, keys.Help, "keys")
}

// footerHints is the legend as three lists: the keys this mode needs, the ones a wide window
// has room for, and the one that reaches the help card. Walks the two tables in order and
// stops at the first arm that matches.
func (m *model) footerHints() (core, extra []string, help string) {
	help = m.footerHelp()

	for _, arm := range footerCardArms {
		if arm.when(m) {
			core, _ = arm.hints(m)
			return core, nil, ""
		}
	}

	for _, arm := range footerModeArms {
		if arm.when(m) {
			core, extra = arm.hints(m)
			break
		}
	}

	// Collapsed, the way back to the hosts outranks the mode's own keys.
	if !m.sidebarOn() {
		core = append([]string{m.sidebarHint()}, core...)
	}

	core, extra = compact(core), compact(extra)
	return m.guidedHints(core, extra, help)
}

// guidedHints trims how much of the legend is offered. It never adds or removes a binding —
// every key works in all three profiles.
func (m *model) guidedHints(core, extra []string, help string) ([]string, []string, string) {
	switch m.cfg.Guidance {
	case config.GuidanceKeys:
		return core, nil, help
	case config.GuidanceGuided:
		if h := m.actionsHint(); h != "" {
			// Promoted out of the extras rather than repeated.
			extra = without(extra, h)
			core = append(core, h)
		}
	}
	return core, extra, help
}

// actionsHint is how this mode reaches the action list, or "" for the host list.
// core already says it. In a pane it is behind the leader, for the reason the card is.
func (m *model) actionsHint() string {
	switch m.mode {
	case modeList:
		return m.hint(keys.List, keys.Palette, "search actions")
	case modeBrowser:
		return m.hint(keys.Browser, keys.BrowserPalette, "actions")
	case modeScrollback:
		// Forwards nothing; the palette is a key away once esc has handed the shell back.
		return ""
	default:
		return m.chordHint(keys.LeaderPalette, "actions")
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

// footerLine renders the legend to fit the window: core and trailing hints first, then as
// many extras as the leftover room holds.
func (m *model) footerLine(core, extra []string, tail string) string {
	const sep = "  "

	var keep []string
	// Release news belongs to the list, not to a pane.
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

	// Extras go on only while they fit whole; a half-cut key cannot be read.
	// Copied, since the width probe below appends to it speculatively.
	hints := append([]string{}, core...)
	for _, e := range extra {
		w := lipgloss.Width(strings.Join(append(hints, e), sep)) + fixedW
		if w > room {
			break
		}
		hints = append(hints, e)
	}

	// If even the core overruns, whole hints go from the right rather than a word being cut.
	for len(hints) > 1 && lipgloss.Width(strings.Join(hints, sep))+fixedW > room {
		hints = hints[:len(hints)-1]
	}

	line := strings.Join(hints, sep)
	if len(keep) > 0 {
		// Never cut into: on a very narrow window the way to the card is what survives.
		line = truncate(line, max(room-fixedW, 0))
		if line != "" {
			line += sep
		}
		line += strings.Join(keep, sep)
	}
	return footerStyle.Render(truncate(line, room))
}
