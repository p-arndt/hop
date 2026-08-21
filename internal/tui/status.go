package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// ---- status bar ----

var statusSep = faint.Render(" › ")

// renderStatus draws the bar: crumbs left, target and chips right, always exactly m.width
// wide so the fill reaches both edges.
func (m *model) renderStatus() string {
	inner := max(m.width-2, 0)

	right := m.statusChips()
	// The crumbs yield to the target: the path is what gets elided on a narrow window.
	left := m.statusCrumbs(max(inner-lipgloss.Width(right)-2, 8))

	gap := max(inner-lipgloss.Width(left)-lipgloss.Width(right), 0)
	line := truncate(left+strings.Repeat(" ", gap)+right, inner)
	return statusBar.Width(m.width).Render(line)
}

// statusCrumbs renders the trail, eliding the last crumb from the left since the tail of a
// path says more than its root.
func (m *model) statusCrumbs(w int) string {
	crumbs, tail := m.crumbs()

	head := strings.Join(crumbs, statusSep)
	if tail == "" {
		return truncate(head, w)
	}
	// The tail is elided as plain text and coloured after, since cutting a styled string
	// from the left would cut its opening escape off with it.
	room := w - lipgloss.Width(head) - lipgloss.Width(statusSep)
	return truncate(head+statusSep+accentText.Render(elideLeft(tail, room)), w)
}

// crumbs splits the trail into the fixed styled part and the one raw moving crumb, which
// is the one statusCrumbs cuts.
func (m *model) crumbs() ([]string, string) {
	s := m.sessions[m.active]

	switch {
	case s != nil && s.dead && m.active != "":
		return []string{aliasStyle.Render(m.active), redText.Render("disconnected")}, ""

	case m.editing() && s != nil && s.editor() != nil:
		ed := s.editor()
		// The path, not the name: two tabs on config.yaml in different directories are
		// otherwise the same crumb.
		return []string{aliasStyle.Render(m.active), dimStyle.Render("edit")}, ed.path

	case m.browsing() && s != nil && s.browser != nil:
		return []string{aliasStyle.Render(m.active), dimStyle.Render("sftp")}, s.browser.Path()

	case m.scrolling() && m.active != "":
		return []string{aliasStyle.Render(m.active), dimStyle.Render("scrollback")}, ""

	case m.focused() && m.active != "":
		// The cwd arrives over OSC 7 and only from a shell that emits it, so a quiet shell
		// gets the word instead of a stale path.
		if cwd := m.shellCwd(m.active); cwd != "" {
			return []string{aliasStyle.Render(m.active)}, cwd
		}
		return []string{aliasStyle.Render(m.active), dimStyle.Render("shell")}, ""
	}

	crumbs := []string{dimStyle.Render("hosts")}
	if h, ok := m.selectedHost(); ok {
		crumbs = append(crumbs, aliasStyle.Render(h.Alias))
	}
	return crumbs, ""
}

// statusChips is the right-hand end: which of several tabs is up, and the machine as
// user@host:port.
func (m *model) statusChips() string {
	var chips []string
	s := m.sessions[m.active]

	switch {
	case m.scrolling() && s != nil && s.shell() != nil:
		p := s.shell().pane
		chips = append(chips, accentText.Bold(true).Render(fmt.Sprintf("⇅ %d/%d", p.ScrollOffset(), p.ScrollbackLen())))
	case m.editing() && s != nil && len(s.editors) > 1:
		// The half the keyboard is in, since that is the file the crumbs to the left name.
		chips = append(chips, chipStyle.Render(
			fmt.Sprintf("file %d/%d", s.editorIndex(s.focusedHalf())+1, len(s.editors))))
	case m.focused() && s != nil && len(s.shells) > 1:
		chips = append(chips, chipStyle.Render(fmt.Sprintf("shell %d/%d", s.activeSh+1, len(s.shells))))
	}

	if t := m.statusTarget(); t != "" {
		chips = append(chips, faint.Render(t))
	}
	return strings.Join(chips, " ")
}

// statusTarget is user@host:port for the host the screen is about, omitting the parts that
// carry no information.
func (m *model) statusTarget() string {
	alias := m.active
	if alias == "" || !m.inPane() {
		h, ok := m.selectedHost()
		if !ok {
			return ""
		}
		alias = h.Alias
	}
	h, ok := m.hostByAlias(alias)
	if !ok || h.HostName == "" {
		return ""
	}

	t := h.HostName
	if h.User != "" {
		t = h.User + "@" + t
	}
	if h.Port != 0 && h.Port != 22 {
		t = fmt.Sprintf("%s:%d", t, h.Port)
	}
	return t
}

// elideLeft cuts s to w keeping its end, marking the cut with a leading ellipsis.
func elideLeft(s string, w int) string {
	if w <= 0 {
		return ""
	}
	if lipgloss.Width(s) <= w {
		return s
	}
	if w <= 1 {
		return "…"
	}
	// Runes against a width budget: exact for the paths and filenames this sees.
	r := []rune(s)
	if len(r) <= w-1 {
		return "…" + string(r)
	}
	return "…" + string(r[len(r)-(w-1):])
}
