package tui

import (
	"fmt"
	"strconv"
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
	if r := m.frame.drawer; !r.empty() {
		body = lipgloss.JoinVertical(lipgloss.Left, body, m.renderDrawer(m.sessions[m.active], r))
	}
	r := m.frame.list
	if !r.empty() && !m.frame.floats {
		side := m.renderList(r.w, r.h)
		if t := m.frame.tree; !t.empty() {
			side = lipgloss.JoinVertical(lipgloss.Left, side, m.renderTree(t))
		}
		body = lipgloss.JoinHorizontal(lipgloss.Top, side, body)
	}

	screen := lipgloss.JoinVertical(lipgloss.Left, body, m.renderFooter())
	if !r.empty() && m.frame.floats {
		// Over the content, which keeps its size: a narrow window shows the sidebar only
		// while it has the keyboard.
		screen = overlay(screen, m.renderList(r.w, r.h), r.x, r.y)
	}

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
	case m.hostSwitch.open:
		return m.renderHostSwitch()
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

// ---- the tree box ----

// browserInContent: the files tab with no tree box in the sidebar draws the tree across the
// content area.
func (m *model) browserInContent(s *session) bool {
	return s != nil && s == m.sessions[m.active] && !s.dead && m.front() == tabFiles && !m.treeBoxOn()
}

// renderTree draws the sidebar's tree box; the border says whether the keyboard is in it.
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

// columnStyle accents a box while it holds the keyboard; every other box is grey.
func columnStyle(active bool) lipgloss.Style {
	if active {
		return paneBorderActive
	}
	return paneBorder
}

// ---- the content area ----

// contentIsSplit mirrors renderRight's switch: does the content area draw two boxes?
func (m *model) contentIsSplit() bool {
	s := m.sessions[m.active]
	return m.front() == tabEditor && !s.dead && m.splitOn(s) && s.editor() != nil
}

// renderRight draws what the tab in front shows in the content area; with no host in front,
// the recent places and the details of the host under the cursor. Every arm that draws a box
// is mirrored in contentIsSplit.
func (m *model) renderRight(h int) string {
	innerH := max(h-2, 1)
	s := m.sessions[m.active]

	switch f := m.front(); {
	case f == tabNone:
		return m.contentBox(m.listHasFocus() && m.recentAt > 0, m.paneW, innerH, m.renderStart(m.paneW))

	// Inactive border even when focused: the accent would promise a live shell.
	case s.dead:
		return m.deadBox(s, innerH)

	case f == tabShell:
		return m.renderShellPane(s, innerH)

	case f == tabEditor:
		return m.renderEditorPanes(s, innerH)

	case m.browserInContent(s):
		return m.contentBox(m.browsing(), m.paneW, innerH, s.browser.View())
	}
	return m.contentBox(false, m.paneW, innerH, s.browser.PreviewView(m.paneW, innerH))
}

// contentBox draws one box of the content area. Clipping the width matters: lipgloss wraps
// an over-wide line, which would grow the screen past the window.
func (m *model) contentBox(active bool, w, innerH int, content string) string {
	return columnStyle(active).Width(w).Height(innerH).Render(clampLines(fitLines(content, innerH), w))
}

// renderShellPane draws a shell tab. It has no strip of its own: the sidebar names it.
func (m *model) renderShellPane(s *session, innerH int) string {
	content := s.shell().pane.View()
	if m.focused() && m.scrolling() {
		content = s.shell().pane.ViewScrollback()
	}
	return m.contentBox(m.focused(), m.paneW, innerH, m.selectedView(content))
}

// editorTitle is the row over an editor: the file's path, since the sidebar has only room
// for its name.
func editorTitle(ed *editorTab, w int) string {
	return dimStyle.Render(elideLeft(stripControl(ed.path), w))
}

// renderEditorPanes draws the open files: one box, or two side by side while split.
func (m *model) renderEditorPanes(s *session, innerH int) string {
	if !m.contentIsSplit() {
		// Asked of contentIsSplit rather than splitOn so this box and the frame agree.
		ed := s.editorAt(s.focusedHalf())
		return m.contentBox(m.editing(), m.paneW, innerH,
			editorTitle(ed, m.paneW)+"\n"+m.selectedView(ed.pane.View()))
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
		return m.contentBox(focused, w, innerH, editorTitle(ed, w)+"\n"+view)
	}
	return lipgloss.JoinHorizontal(lipgloss.Top, half(false), half(true))
}

// deadBox is a dropped host's body: what went, and what reconnecting will put back. It says
// so on the body rather than the status, which expires after a few seconds.
func (m *model) deadBox(s *session, innerH int) string {
	var b strings.Builder
	head := redText.Bold(true).Render(stripControl(m.active) + " is disconnected")
	if s.lostWhy != "" {
		head += faint.Render(" · " + stripControl(s.lostWhy))
	}
	b.WriteString("\n" + head + "\n\n")

	plan := s.plan(false)
	lines := reopenLines(plan)
	if len(lines) == 0 {
		b.WriteString(dimStyle.Render("Nothing is left open on this connection.") + "\n")
	} else {
		b.WriteString(dimStyle.Render("Reconnecting opens again:") + "\n")
		for _, l := range lines {
			b.WriteString("  " + l + "\n")
		}
	}
	if plan.editors > 0 {
		b.WriteString(faint.Render(fmt.Sprintf("  %d %s not reopened: a fresh editor would lose unsaved work",
			plan.editors, plural(plan.editors, "editor is", "editors are"))) + "\n")
	}
	b.WriteString("\n" + strings.Join(compact([]string{
		m.hint(keys.DeadPane, keys.DeadReconnect, "reconnect"),
		m.hint(keys.DeadPane, keys.DeadDrop, "drop"),
		m.chordHint(keys.LeaderLast, "last host"),
		m.hint(keys.DeadPane, keys.DeadLeave, "hosts"),
	}), "  "))

	// Indented as a block rather than centred line by line, so the list stays a list.
	pad := strings.Repeat(" ", max((m.paneW-60)/2, 1))
	return m.contentBox(false, m.paneW, innerH, indentBlock(b.String(), pad))
}

// reopenLines is what a reconnect would put back, one line each.
func reopenLines(plan reconnectPlan) []string {
	var out []string
	// Numbered only: a restored shell starts in the host's default directory, not where
	// the lost one had got to.
	for i := range plan.shells {
		out = append(out, "$ shell "+strconv.Itoa(i+1))
	}
	if plan.browser {
		out = append(out, stripControl("▤ files · "+plan.browserDir))
	}
	if n := len(plan.tunnels); n > 0 {
		out = append(out, fmt.Sprintf("⇄ %d %s", n, plural(n, "tunnel", "tunnels")))
	}
	return out
}

// indentBlock prefixes every line of s with pad.
func indentBlock(s, pad string) string {
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		if l != "" {
			lines[i] = pad + l
		}
	}
	return strings.Join(lines, "\n")
}

// ---- footer ----

// updateHint names the command rather than a key: updating swaps the running binary.
func (m *model) updateHint() string {
	if m.updateLatest == "" {
		return ""
	}
	return yellowText.Render("⬆ hop "+m.updateLatest+" available") + " " + dimStyle.Render("· hop self-update")
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
	return m.chordOf(lead, b.Keycap(), word)
}

// chordPart is a leader chord's own key and its word, without the leader.
type chordPart struct{ key, word string }

// chordOf draws a chord as its two keystrokes and remembers its parts, so a footer with
// several of them can say the leader once. Elsewhere the drawn hint is used as it is.
func (m *model) chordOf(lead, key, word string) string {
	h := keyHint(lead+" "+key, word)
	if m.footerChords == nil {
		m.footerChords = make(map[string]chordPart)
	}
	m.footerChords[h] = chordPart{key: key, word: word}
	return h
}

// chordKeys is a leader chord as the two keystrokes it is, "" when either is unbound.
func (m *model) chordKeys(id keys.Action) string {
	lead := m.binds.Keycap(keys.LeaderKey)
	b, ok := m.binds.BindingIn(keys.Leader, id)
	if !ok || lead == "" || b.Keycap() == "" {
		return ""
	}
	return lead + " " + b.Keycap()
}

// leaderRange is the digits behind the leader — a range rather than a binding.
func (m *model) leaderRange(word string) string {
	lead := m.binds.Keycap(keys.LeaderKey)
	if lead == "" {
		return ""
	}
	return m.chordOf(lead, "1-9", word)
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
//
// The footer is one row: the crumb (or a transient status) on the left, the legend on the
// right. The crumb is held to a third of the row while the legend is measured, then gets
// back whatever the legend left over.
func (m *model) renderFooter() string {
	core, extra, help := m.footerHints()
	room := max(m.width-2, 0)
	reserve := min(lipgloss.Width(m.footerLeft(room)), max(room/3, footerCrumbMin))
	legend := m.footerLine(core, extra, help, max(room-reserve-footerGap, 0))
	leftW := room
	if legend != "" {
		leftW = room - lipgloss.Width(legend) - footerGap
	}
	left := m.footerLeft(max(leftW, 0))
	gap := max(room-lipgloss.Width(left)-lipgloss.Width(legend), 0)
	return footerStyle.Render(truncate(left+strings.Repeat(" ", gap)+legend, room))
}

// footerCrumbMin is the least the crumb is held to while the legend is measured, and
// footerGap what stands between the two.
const (
	footerCrumbMin = 16
	footerGap      = 2
)

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
		when: func(m *model) bool { return m.confirm.open },
		hints: func(m *model) ([]string, []string) {
			if m.confirm.tab.alias != "" {
				return []string{keyHint("y", "close"), keyHint("n", "cancel")}, nil
			}
			return []string{keyHint("y", "delete"), keyHint("n", "cancel")}, nil
		},
	},
	{
		when:  func(m *model) bool { return m.palette.open },
		hints: fixedHints(keyHint("type", "search"), keyHint("enter", "run"), keyHint("esc", "close")),
	},
	{
		when:  func(m *model) bool { return m.hostSwitch.open },
		hints: fixedHints(keyHint("type", "search"), keyHint("enter", "go"), keyHint("esc", "close")),
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
		when:  func(m *model) bool { return m.sizingDrawer },
		hints: fixedHints(keyHint("↑ +", "taller"), keyHint("↓ -", "shorter"), dimStyle.Render("any other key is done")),
	},
	{
		when: (*model).leaderArmed,
		hints: func(m *model) ([]string, []string) {
			alias := m.chords.leaderAlias
			s := m.sessions[alias]
			menu := []string{accentText.Render("leader"), keyHint("1-9", "tab")}
			if twoHosts(m) {
				// With one open host the step lands where it started.
				menu = append(menu, keyHint(m.binds.Keycap(keys.LeaderPrevHost)+m.binds.Keycap(keys.LeaderNextHost), "host"))
			}
			menu = append(menu, m.hint(keys.Leader, keys.LeaderHosts, "go to"))
			if lastHostSpec.ok(m) {
				menu = append(menu, m.hint(keys.Leader, keys.LeaderLast, "last host"))
			}
			menu = append(menu,
				m.hint(keys.Leader, keys.LeaderShell, "new shell"),
				m.hint(keys.Leader, keys.LeaderBrowser, "files"))
			if m.mode != modeShell {
				menu = append(menu, m.hint(keys.Leader, keys.LeaderToShell, "shell"))
			}
			// Named only where it would work: without a cwd the chord opens the host's default.
			if m.mode == modeShell && m.shellCwd(alias) != "" {
				menu = append(menu, m.hint(keys.Leader, keys.LeaderVSCode, "vs code here"))
			}
			if s != nil && s.browser != nil {
				menu = append(menu, m.hint(keys.Leader, keys.LeaderTree, "tree"))
			}
			if s != nil && (s.browser != nil || len(s.editors) > 0) {
				menu = append(menu, m.hint(keys.Leader, keys.LeaderDrawer, "terminal"))
			}
			if m.drawerOnScreen() > 0 {
				menu = append(menu, m.hint(keys.Leader, keys.LeaderGrow, "taller"),
					m.hint(keys.Leader, keys.LeaderShrink, "shorter"))
			}
			menu = append(menu,
				m.hint(keys.Leader, keys.LeaderOut, "hosts"),
				m.hint(keys.Leader, keys.LeaderSidebar, "sidebar"),
				m.hint(keys.Leader, keys.LeaderPalette, "actions"),
				m.hint(keys.Leader, keys.LeaderHelp, "keys"),
				dimStyle.Render("any other key cancels"))
			return compact(menu), nil
		},
	},
}

// editorExtras is the editor arm's extra hints. Unsplit is named only while there is a split,
// since naming a key that would decline is worse than not naming it.
func (m *model) editorExtras() []string {
	extra := []string{m.chordHint(keys.LeaderDrawer, "terminal"), m.chordHint(keys.LeaderToShell, "shell")}
	if s := m.sessions[m.active]; s != nil && s.split {
		extra = append(extra, m.hint(keys.Editor, keys.EditorUnsplit, "unsplit"))
	}
	return append(extra, m.leaderRange("tab"), m.chordHint(keys.LeaderHosts, "go to"),
		m.hint(keys.Editor, keys.EditorLeave, "hosts"))
}

// footerModeArms are the modes hop's own keyboard is in. Unlike the cards these keep the
// help key and the guidance trim. The last row always matches.
var footerModeArms = []footerArm{
	{
		// Above the mode arms it would fall into: a dropped connection outranks its pane's tab.
		when: func(m *model) bool { return m.active != "" && m.inPane() && m.activeDead() },
		hints: func(m *model) ([]string, []string) {
			return []string{
				m.hint(keys.DeadPane, keys.DeadReconnect, "reconnect"),
				m.hint(keys.DeadPane, keys.DeadDrop, "drop session"),
				m.hint(keys.DeadPane, keys.DeadLeave, "hosts"),
			}, []string{m.chordHint(keys.LeaderLast, "last host")}
		},
	},
	{
		when: func(m *model) bool { return m.mode == modeDrawer && m.active != "" },
		hints: func(m *model) ([]string, []string) {
			core := []string{
				m.chordHint(keys.LeaderDrawer, "hide"),
				m.hint(keys.Pane, keys.PaneLeave, "hosts"),
				m.chordHint(keys.LeaderTree, "tree"),
			}
			extra := []string{
				m.chordHint(keys.LeaderGrow, "taller"),
				m.chordHint(keys.LeaderShrink, "shorter"),
				m.chordHint(keys.LeaderToShell, "shell"),
				m.chordHint(keys.LeaderHosts, "go to"),
			}
			return core, extra
		},
	},
	{
		when: func(m *model) bool { return m.editing() && m.active != "" },
		hints: func(m *model) ([]string, []string) {
			return []string{
				keyHint(":q", "close"), // the remote editor's key, not hop's
				m.chordHint(keys.LeaderTree, "tree"),
				m.hint(keys.Editor, keys.EditorNextTab, "tab"),
			}, m.editorExtras()
		},
	},
	{
		when: func(m *model) bool { return m.browsing() && m.active != "" },
		hints: func(m *model) ([]string, []string) {
			core := []string{
				m.hint(keys.Browser, keys.In, "open"),
				m.hint(keys.Browser, keys.BrowserClose, "close"),
				m.hint(keys.Browser, keys.BrowserDrawer, "terminal"),
			}
			extra := []string{
				m.hint(keys.Browser, keys.BrowserHosts, "go to"),
				m.hint(keys.Browser, keys.BrowserLeave, "hosts"),
				m.hint(keys.Browser, keys.BrowserNextTab, "tab"),
				m.hint(keys.Browser, keys.BrowserDownload, "download"),
				m.hint(keys.Browser, keys.BrowserUpload, "upload"),
				m.hint(keys.Browser, keys.BrowserFocusPane, "focus file"),
				m.hint(keys.Browser, keys.BrowserSplit, "open beside"),
				m.hint(keys.Browser, keys.BrowserShell, "terminal here"),
				// A copy is three keys nobody guesses, so the selection and target are named.
				m.hint(keys.Browser, keys.BrowserMark, "mark"),
				m.hint(keys.Browser, keys.BrowserTarget, "target"),
				m.hint(keys.Browser, keys.BrowserCopy, "copy there"),
				m.hint(keys.Browser, keys.BrowserMoveTo, "move there"),
				m.hint(keys.Browser, keys.BrowserPalette, "actions"),
				m.hint(keys.Browser, keys.Out, "up"),
				m.hint(keys.Browser, keys.BrowserMarkAll, "mark all"),
				m.hint(keys.Browser, keys.BrowserOpen, "open local"),
				m.hint(keys.Browser, keys.BrowserDelete, "delete"),
				m.hint(keys.Browser, keys.BrowserRename, "rename"),
				m.hint(keys.Browser, keys.BrowserMkdir, "mkdir"),
				m.hint(keys.Browser, keys.BrowserSort, "sort"),
				m.hint(keys.Browser, keys.BrowserRefresh, "refresh"),
				m.hint(keys.Browser, keys.BrowserTree, "tree"),
				m.hint(keys.Browser, keys.LeaderKey, "leader"),
			}
			return core, extra
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
		// Everything else, which is the sidebar.
		when:  func(*model) bool { return true },
		hints: (*model).listHints,
	},
}

// shellHints is a live shell's legend: the ways out of it, since everything else it does is
// the remote's. Three of its keys are offered only where they work.
func (m *model) shellHints() (core, extra []string) {
	core = []string{
		m.chordHint(keys.LeaderHosts, "go to"),
		m.chordHint(keys.LeaderBrowser, "files"),
	}
	if len(m.hostTabs(m.active)) > 1 {
		core = append(core, m.hint(keys.Pane, keys.PaneNextTab, "tab"))
		extra = append(extra, m.leaderRange("tab"))
	}
	extra = append(extra, m.hint(keys.Pane, keys.PaneLeave, "hosts"), m.chordHint(keys.LeaderShell, "new shell"))
	if lastHostSpec.ok(m) {
		extra = append(extra, m.chordHint(keys.LeaderLast, "last host"))
	}
	if len(m.sessions) > 1 {
		extra = append(extra, m.hostStepHint())
	}
	// The same conditions the chords check, so a wide window never names a key that declines.
	if m.shellCwd(m.active) != "" {
		extra = append(extra, m.chordHint(keys.LeaderVSCode, "vs code here"))
	}
	s := m.sessions[m.active]
	if s != nil && s.shell() != nil && !s.shell().pane.AltScreen() && s.shell().pane.ScrollbackLen() > 0 {
		extra = append(extra, m.hint(keys.Pane, keys.PaneScroll, "scrollback"))
	}
	return core, append(extra, m.hint(keys.Pane, keys.LeaderKey, "leader"))
}

// hostStepHint is the leader's pair of host keys as one hint.
func (m *model) hostStepHint() string {
	lead := m.binds.Keycap(keys.LeaderKey)
	prev, next := m.binds.Keycap(keys.LeaderPrevHost), m.binds.Keycap(keys.LeaderNextHost)
	if lead == "" || prev == "" || next == "" {
		return ""
	}
	return m.chordOf(lead, prev+next, "host")
}

// listHints is the sidebar's legend, and the only one that changes with what is under the
// cursor. The menu key stands in for every per-host key below it. With a host in front the
// way back to it comes first.
func (m *model) listHints() (core, extra []string) {
	enter := "connect"
	if t, ok := m.selectedPlace(); ok && (t.kind != targetHost || m.sessions[t.alias] != nil) {
		enter = "go"
	}
	core = []string{
		m.hint(keys.List, keys.In, enter),
		m.hint(keys.List, keys.Menu, "actions"),
		m.hint(keys.List, keys.Filter, "filter"),
	}
	if m.sessions[m.active] != nil {
		core = []string{
			m.hint(keys.List, keys.In, enter),
			m.hint(keys.List, keys.Back, "back"),
		}
	}
	if _, ok := m.closableUnderCursor(); ok {
		// On a tab row the delete key closes the tab, so the legend says that instead.
		core = append(core, m.hint(keys.List, keys.HostDelete, "close tab"))
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
		m.hint(keys.List, keys.SidebarDock, "dock / hide"),
		m.hint(keys.List, keys.Quit, "quit"),
	}
	if m.sessions[m.active] != nil {
		extra = append([]string{m.hint(keys.List, keys.Menu, "actions"), m.hint(keys.List, keys.Filter, "filter")}, extra...)
	}
	if m.cursorTab.alias != "" || m.recentAt > 0 {
		// x deletes a host only from the host's own row.
		extra = without(extra, m.hint(keys.List, keys.HostDelete, "delete"))
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
	core = []string{
		m.hint(keys.List, keys.HostAdd, "add host"),
		m.hint(keys.List, keys.HostImport, "import"),
	}
	extra = []string{
		m.hint(keys.List, keys.Palette, "search actions"),
		m.hint(keys.List, keys.Settings, "settings"),
		m.hint(keys.List, keys.Quit, "quit"),
	}
	return core, extra
}

// footerHelp is how this mode reaches the help card. Where keys are forwarded a bare "?" is
// text the remote is owed, so there it is the leader chord.
func (m *model) footerHelp() string {
	switch {
	case m.filtering:
		return ""
	case m.activeDead():
		return m.hint(keys.DeadPane, keys.DeadHelp, "keys")
	case m.editing() || m.mode == modeShell || m.mode == modeDrawer:
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

// footerLine renders the legend to fit room: core and trailing hints first, then as many
// extras as the leftover room holds.
func (m *model) footerLine(core, extra []string, tail string, room int) string {
	var keep []string
	// Release news belongs to the list, not to a pane.
	if h := m.updateHint(); h != "" && !m.inPane() {
		keep = append(keep, h)
	}
	if tail != "" {
		keep = append(keep, tail)
	}
	fits := func(hints []string) bool { return lipgloss.Width(m.legend(hints, keep)) <= room }

	// Extras go on only while they fit whole; a half-cut key cannot be read.
	// Copied, since the width probe below appends to it speculatively.
	hints := append([]string{}, core...)
	for _, e := range extra {
		if !fits(append(hints, e)) {
			break
		}
		hints = append(hints, e)
	}

	// If even the core overruns, whole hints go from the right rather than a word being cut.
	for len(hints) > 1 && !fits(hints) {
		hints = hints[:len(hints)-1]
	}
	return truncate(m.legend(hints, keep), room)
}

// legend lays the hints out, gathering every leader chord behind one leader keycap —
// "ctrl+o  space go to  f files" rather than the leader drawn on each — after the plain
// keys. keep is what must never be dropped: the help key and any release news. The bare "leader"
// hint says nothing the gathered chords do not, so it goes when there are any.
func (m *model) legend(hints, keep []string) string {
	const sep = "  "
	// The keys behind the leader are drawn as text, not keycaps: a row of equal keycaps read
	// as keys to press one by one, where these are each the second half of a chord.
	chord := func(c chordPart) string { return accentText.Bold(true).Render(c.key) + " " + dimStyle.Render(c.word) }
	var plain, chords, tail []string
	for _, h := range hints {
		if c, ok := m.footerChords[h]; ok {
			chords = append(chords, chord(c))
			continue
		}
		plain = append(plain, h)
	}
	for _, h := range keep {
		if c, ok := m.footerChords[h]; ok {
			chords = append(chords, chord(c))
			continue
		}
		tail = append(tail, h)
	}
	lead := m.binds.Keycap(keys.LeaderKey)
	if len(chords) > 0 {
		plain = without(plain, keyHint(lead, "leader"))
	}
	// The group goes last, so nothing drawn after it can be misread as behind the leader.
	parts := append(plain, tail...)
	if len(chords) > 0 {
		parts = append(parts, kc(lead)+faint.Render(" › ")+strings.Join(chords, faint.Render(" · ")))
	}
	return strings.Join(parts, sep)
}
