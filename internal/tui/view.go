package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"hop/internal/config"
	"hop/internal/keys"
)

// View composes the screen: a header rule, the three columns of the body — host list,
// SFTP tree, content — and a context-sensitive key legend along the bottom. The modal
// cards are composited over the finished screen, so the hosts and the panes stay visible
// behind them.
func (m *model) View() string {
	if !m.ready {
		return "loading hop…"
	}

	// Derived, not stored, one more time before anything is measured. Two of the three
	// columns come and go with what the active session is holding, and a frame drawn
	// against the previous frame's widths is a frame that does not add up to the window.
	m.recomputeLayout()

	// The columns are drawn right to left into the boxes the frame placed them in. A
	// collapsed column is not drawn at all — an empty rect, rather than a zero-width box,
	// which would still cost the two columns its border takes.
	body := m.renderRight(m.fr.content.h)
	if r := m.fr.tree; !r.empty() {
		body = lipgloss.JoinHorizontal(lipgloss.Top, m.renderTree(r), body)
	}
	if r := m.fr.list; !r.empty() {
		body = lipgloss.JoinHorizontal(lipgloss.Top, m.renderList(r.w, r.h), body)
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

// capturing reports whether something on screen has taken the input for itself, so a
// click anywhere is not the click it appears to be.
//
// Most of those things render as a centred card, which is why modalCard doubles as the
// test for them. Two do not, and they are the reason this predicate exists rather than
// the renderer standing in for it: the context menu is anchored to a row, and the file
// browser's question lives on its own status line, inside another package. All three are
// the same idea — a click elsewhere would answer the open question by moving the thing it
// was asked about.
func (m *model) capturing() bool {
	return m.modalCard() != "" || m.menu.open || m.browserPrompting()
}

// browserPrompting reports whether the active session's file browser is waiting on a
// typed answer. It walks to the browser because the state lives there: the browser owns
// its own question, and the model only needs to know that one is open.
func (m *model) browserPrompting() bool {
	if !m.browsing() {
		return false
	}
	s := m.sessions[m.active]
	return s != nil && s.browser != nil && s.browser.Prompting()
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

// ---- the tree column ----

// renderTree draws the SFTP column: the active session's browser, in a box of its own
// beside the content area. It is on screen whenever the session has a browser, whether or
// not the keyboard is in it — a tree you cannot see while reading a file is a mode, and
// the column exists so that it is not one — so the border and the dimmed body are what
// say where the keys are going.
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

// columnStyle is the box a column of the body is drawn in: accented while it holds the
// keyboard, and sunk to the faint end of the ramp while it does not. Two columns of
// remote text side by side is more than a border's worth of difference to tell apart.
func columnStyle(active bool) lipgloss.Style {
	if active {
		return paneBorderActive
	}
	return paneBorderIdle
}

// ---- the content area ----

// renderRight draws whatever the active session is showing in the content area — the
// files open in it, a shell, the browser on a window too narrow for a column — and the
// details card when it is showing nothing.
func (m *model) renderRight(h int) string {
	innerH := max(h-2, 1)
	s := m.sessions[m.active]

	// A dropped session keeps its pane: the last screen the host drew, under a banner.
	// The border is drawn inactive even while the pane holds the keyboard, since the
	// accent would promise a live shell.
	if s != nil && s.dead && m.active != "" {
		return m.contentBox(false, m.paneW, innerH, m.deadBanner(s)+"\n"+m.deadContent(s))
	}

	switch {
	// The narrow-window fallback: with no column to put it in, the browser takes the
	// content area while it holds the keyboard, which is the screen hop drew before the
	// column existed. See treeWidth.
	case m.treeInline() && m.browsing() && s != nil && s.browser != nil:
		return m.contentBox(true, m.paneW, innerH, s.browser.View())

	// Which of the two things a session can hold the content area shows is the keyboard's
	// answer while the keyboard is in it — modeShell means the shell — and otherwise the
	// files, since a file is what the tree beside it was used to open.
	case m.focused() && s != nil && s.shell() != nil:
		return m.renderShellPane(s, innerH)

	case s != nil && s.editor() != nil:
		return m.renderEditorPanes(s, innerH)

	case m.active != "" && s != nil && s.shell() != nil:
		return m.renderShellPane(s, innerH)
	}

	return m.contentBox(false, m.paneW, innerH, m.renderDetails(m.paneW))
}

// contentBox draws one box of the content area. The content is cut to it in both
// directions. Width is the one that bites: lipgloss wraps a line wider than the box
// instead of clipping it, so one over-wide row makes the screen a row taller than the
// window and the terminal scrolls hop's frame off its own top.
//
// Without a tree column beside it the box keeps the plain border every pane has always
// had: there is nothing on screen to confuse it with, and dimming the only thing being
// read would be a signal about nothing.
func (m *model) contentBox(active bool, w, innerH int, content string) string {
	style := paneBorder
	switch {
	case active:
		style = paneBorderActive
	case !m.fr.tree.empty():
		style = paneBorderIdle
	}
	return style.Width(w).Height(innerH).Render(clampLines(fitLines(content, innerH), w))
}

// renderShellPane draws a live shell in the content area, with its strip of tabs once
// there is a second one to switch to.
func (m *model) renderShellPane(s *session, innerH int) string {
	// Scrollback shows a window onto history rather than the live screen, but the same
	// number of lines, so the strip and border are unaffected.
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
	return m.contentBox(m.focused(), m.paneW, innerH, content)
}

// renderEditorPanes draws the files open in the content area: one box, or two side by side
// while it is split. Each half is its own tab strip over its own editor, both drawn from
// the one tab list, and only the half the keyboard is in wears the accent.
func (m *model) renderEditorPanes(s *session, innerH int) string {
	if !m.splitOn(s) {
		// Unsplit, or split on a window that has since become too narrow to hold two
		// halves: either way there is one box, showing the half the keyboard is in.
		half := s.focusedHalf()
		ed := s.editorAt(half)
		return m.contentBox(m.editing(), m.paneW, innerH,
			m.renderEditorTabs(s, half)+"\n"+m.selectedView(ed.pane.View()))
	}

	w := m.fr.left.innerW()
	half := func(right bool) string {
		focused := m.editing() && s.splitRight == right
		ed := s.editorAt(right)
		if ed == nil {
			// Only reachable in the frame between the last tab closing and dropEditor
			// collapsing the split; an empty box beats a nil dereference.
			return m.contentBox(focused, w, innerH, "")
		}
		view := ed.pane.View()
		if focused {
			// A selection was made with the pointer in one half, and is meaningful only
			// against the rows it was measured in.
			view = m.selectedView(view)
		}
		return m.contentBox(focused, w, innerH, m.renderEditorTabs(s, right)+"\n"+view)
	}
	// The odd column an odd-width content area leaves over stays blank at the right-hand
	// edge; JoinVertical pads the short row out when the screen is assembled.
	return lipgloss.JoinHorizontal(lipgloss.Top, half(false), half(true))
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
	ways := m.hint(keys.DeadPane, keys.DeadReconnect, "reconnect") + "  " +
		m.hint(keys.DeadPane, keys.DeadDrop, "drop")
	gap := max(m.paneW-lipgloss.Width(head)-lipgloss.Width(ways), 1)
	return truncate(head+strings.Repeat(" ", gap)+ways, m.paneW)
}

// deadContent is the frozen screen under the banner: whichever view the session was
// showing when its connection went. It mirrors renderRight's live cases, plus one for a
// session left with nothing but a dead connection.
func (m *model) deadContent(s *session) string {
	switch {
	case m.editing() && s.editor() != nil:
		return m.renderEditorTabs(s, s.focusedHalf()) + "\n" + s.editor().pane.View()
	// Only in the narrow fallback: with a column of its own the browser is already drawn
	// beside this pane rather than inside it.
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

// updateHint is the footer's "a newer hop exists" line, or "". It names the command
// rather than a key: updating swaps the running binary mid-session.
func (m *model) updateHint() string {
	if m.updateLatest == "" {
		return ""
	}
	return yellowText.Render("⬆ hop "+m.updateLatest+" available") + " " + dimStyle.Render("· hop self-update")
}

// sidebarHint is the footer's sidebar entry. It names the outcome rather than the toggle,
// so the legend does not leave you guessing which way the key goes.
func (m *model) sidebarHint() string {
	if m.sidebarHidden {
		return m.hint(keys.Global, keys.Sidebar, "show hosts")
	}
	return m.hint(keys.Global, keys.Sidebar, "hide hosts")
}

// hint is one footer entry: the key that runs an action, and a word for it.
//
// The word is the footer's own rather than the registry's label. The card has a column
// for "sort by name / size / modified"; this row has space for "sort", and the whole
// design of the legend is that it says less than the card. What it must not invent is the
// key — that comes from the keyboard, so a rebound key moves here too, and an unbound one
// leaves no hint behind rather than a dead one.
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

// compact drops the hints that resolved to nothing, so an unbound key costs the legend a
// slot rather than leaving a hole in it.
func compact(hints []string) []string {
	out := hints[:0:0]
	for _, h := range hints {
		if h != "" {
			out = append(out, h)
		}
	}
	return out
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

// footerArm is one row of the legend's table: the state it speaks for, and the hints that
// state offers.
//
// This is the same shape as the spec tables in actions.go, and it is here for the same
// reason. Those say which actions a mode is worth offering; these say which keys it is
// worth naming — the same kind of data about the same states, and a feature that wants a
// slot in the row should be adding a row rather than another case to a function.
//
// The one thing the table cannot be read without: the arms are ordered and the first match
// wins. Several predicates are true at once by design — a card is open *and* the keyboard
// is in a pane, a shell is focused *and* scrolling — so the order below is not a listing
// but the rule itself. Moving a row moves which legend the user is shown.
type footerArm struct {
	// when is the state this arm speaks for. It takes the whole model because the
	// conditions are more than the mode: the editor's arm wants a session under it as well
	// as modeEditor, and the dead-pane arm wants a connection that has dropped.
	when func(m *model) bool
	// hints is the legend: the keys the state cannot be worked without, then the ones a
	// wide window has room for, in the order the room should be spent on them.
	//
	// A function rather than two literals because nearly every hint is read out of the
	// keyboard rather than written down — a rebound key moves here with it, and an unbound
	// one leaves no hint behind rather than a dead one. Where an arm really is a literal
	// (the cards), fixedHints says so.
	hints func(m *model) (core, extra []string)
}

// fixedHints is hints for an arm whose keys are spelled out rather than looked up. Only
// the cards qualify: a card handles its own keys instead of going through the registry, so
// there is no binding to read and nothing for a rebind to move.
func fixedHints(hints ...string) func(*model) (core, extra []string) {
	return func(*model) ([]string, []string) { return hints, nil }
}

// footerCardArms are the arms that are the whole legend: they answer with a core and
// nothing else, no extras and no way to the help card.
//
// Cards come first, and on their own terms: a card's keys are the card, not a reminder of
// it, and none of them leaves room for a "?" that the card would swallow anyway. The
// leader is last of them and above every pane mode — while it is open the footer is the
// menu, because the leader waits indefinitely, and this is the one legend that is the
// whole keyboard rather than a slice of it.
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
		// The one card with a word that depends on how it was opened: hop opens the
		// importer by itself on a first run, and there esc is skipping a step rather than
		// abandoning one the user asked for.
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
		// Editing a forward is a card inside a card, so esc goes back to the list rather
		// than closing the manager. Above the manager's own arm, which it would match too.
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
			// The leader is open, so its keys are named on their own — the leader key
			// itself is already spent.
			menu := []string{
				accentText.Render("leader"),
				m.hint(keys.Leader, keys.LeaderOut, "out"),
				keyHint("1-9", "tab"),
				m.hint(keys.Leader, keys.LeaderShell, "new shell"),
			}
			// Named only where it would do what it says. Without a directory to hand the
			// chord still opens VS Code, but on the host's default one — and this menu is
			// now the only place the chord is written down, so the condition lives here.
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
// mode is for, and nothing you could find on the card instead. Unlike the cards these are
// not the whole legend: whatever they answer still gets the collapsed sidebar's hint in
// front of it, the help key behind it, and the guidance profile's trim over it.
//
// The last row matches everything, so the walk always has an answer — it is the host list,
// which is where hop is when it is nowhere else.
var footerModeArms = []footerArm{
	{
		// A dead pane has its own small keyboard; a legend offering shift+←→ would name
		// keys that do nothing. Above the mode arms it would otherwise fall into, since a
		// dropped connection outranks whatever its pane was showing when it went.
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
			}, []string{
				// The way back to the tree leads the extras: the column is on screen
				// beside this file, and the one thing the legend has to say about it is
				// how to reach it.
				m.hint(keys.Editor, keys.EditorFocusTree, "tree"),
				m.leaderRange("jump"),
				m.sidebarHint(),
			}
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
				// Crossing to the file beside the tree, and opening one there, come
				// first: they are the two keys the column is for.
				m.hint(keys.Browser, keys.BrowserFocusPane, "focus file"),
				m.hint(keys.Browser, keys.BrowserSplit, "open beside"),
				// The selection and the target next: a copy is three keys nobody
				// guesses, and a key that is never shown is a key that does not exist.
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
		// Above the shell's arm: scrolling is focused too, and the keys it answers to are
		// not the shell's.
		when: func(m *model) bool { return m.scrolling() && m.focused() && m.active != "" },
		hints: func(m *model) ([]string, []string) {
			// Top and bottom share a hint: they are one gesture with two ends, and the row
			// has three slots.
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
		// Filtering is a mode of the list rather than a card, so it keeps the extras — but
		// not the help key, which footerHelp drops: "?" here is filter text.
		when: func(m *model) bool { return m.filtering },
		hints: func(*model) ([]string, []string) {
			return []string{keyHint("type", "filter"), keyHint("enter", "apply"), keyHint("esc", "clear")},
				[]string{keyHint("↑↓", "move")}
		},
	},
	{
		// Everything else, which is the host list. Written as an arm that always matches
		// rather than a fallback outside the table, so the walk has one shape and the last
		// row is as readable as the rest.
		when:  func(*model) bool { return true },
		hints: (*model).listHints,
	},
}

// shellHints is a live shell's legend. It is a function rather than two literals because
// three of its keys are offered only where they would do something, and naming a key that
// declines is worse than not naming it.
func (m *model) shellHints() (core, extra []string) {
	// The leader earns its slot in every shell: it is the door to the rest, including the
	// card, and the one chord nothing else on screen implies.
	core = []string{
		m.chordHint(keys.LeaderOut, "back"),
		m.hint(keys.Pane, keys.LeaderKey, "leader"),
	}
	s := m.sessions[m.active]
	if s != nil && len(s.shells) > 1 {
		core = append(core, m.hint(keys.Pane, keys.PaneNextTab, "shell"))
		extra = append(extra, m.leaderRange("jump"))
	}
	// The same conditions the chords check, so a wide window never names a key that would
	// decline: VS Code wants a directory, scrollback wants history behind a shell that is
	// not on its alternate screen.
	if m.shellCwd(m.active) != "" {
		extra = append(extra, m.chordHint(keys.LeaderVSCode, "vs code here"))
	}
	if s != nil && s.shell() != nil && !s.shell().pane.AltScreen() && s.shell().pane.ScrollbackLen() > 0 {
		extra = append(extra, m.hint(keys.Pane, keys.PaneScroll, "scrollback"))
	}
	return core, append(extra, m.hint(keys.Pane, keys.PaneLeave, "back"), m.sidebarHint())
}

// listHints is the host list's legend, and the only one that changes with what is under
// the cursor rather than with a mode. The list is the one place hop has something selected,
// so the keys worth naming are the ones that would act on it — which is why this is a
// function and not a table row's worth of literals.
//
// Its per-host keys are spelled out in the details card beside this line, and all of them
// are on the help card, so the footer keeps to moving, connecting and the two that make the
// list itself. The menu key sits in the core beside connect: it is the one hint that stands
// in for every per-host key below it, so a narrow window that keeps three hints still shows
// the way to all of them.
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
	// Only when the host under the cursor is one you can reconnect — otherwise the slot
	// goes to adding a host, which on an empty list is the only thing to do.
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

// footerHelp is how this mode reaches the help card. Where the keys are forwarded — a live
// shell, an editor — a bare "?" is text the remote is owed, so there it is the leader chord
// (see handleLeader). Scrollback and a dead pane forward nothing, so they take the plain
// key, and while filtering the key is part of the filter and there is none.
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
//
// The body is a walk over the two tables above, in their order and stopping at the first
// arm that matches. See footerArm for why the order is the rule rather than a listing.
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

	// Collapsed, the way back to the hosts outranks the mode's own keys: nothing else on
	// screen says the sidebar is still there.
	if m.sidebarHidden {
		core = append([]string{m.sidebarHint()}, core...)
	}

	// An unbound key leaves no hint behind, so the row closes over the gap rather than
	// showing one.
	core, extra = compact(core), compact(extra)
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
		return m.hint(keys.List, keys.Palette, "search actions")
	case modeBrowser:
		return m.hint(keys.Browser, keys.BrowserPalette, "actions")
	case modeScrollback:
		// Forwards nothing and answers to a small keyboard of its own; the palette is a
		// key away once esc has handed the shell back.
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
