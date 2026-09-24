package tui

import (
	"fmt"
	"path"
	"slices"
	"strconv"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// ---- the crumb ----

var statusSep = faint.Render(" › ")

// footerLeft is the footer's left side at w cells: a transient status while one is up —
// it must not be lost, and it speaks for the moment — else the crumb, which says where the
// keystrokes go: the host, the tab, the path.
func (m *model) footerLeft(w int) string {
	if st := m.styledStatus(w); st != "" {
		return st
	}
	return m.statusCrumbs(w)
}

// styledStatus colors the status line by statusKind rather than by wording.
func (m *model) styledStatus(w int) string {
	if m.status == "" || w <= 0 {
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
	return style.Render(truncate(icon+" "+m.status, w))
}

// statusCrumbs renders the trail, eliding the last crumb from the left since the tail of a
// path says more than its root. Crumbs that do not fit go whole from the end: a word cut in
// half cannot be read.
func (m *model) statusCrumbs(w int) string {
	crumbs, tail := m.crumbs()
	// Every tail is a remote path or cwd, so a directory's name could carry an escape.
	tail = stripControl(tail)
	for len(crumbs) > 1 && lipgloss.Width(strings.Join(crumbs, statusSep)) > w {
		crumbs, tail = crumbs[:len(crumbs)-1], ""
	}

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
	host := aliasStyle.Render(stripControl(m.active))

	switch {
	case m.mode == modeList || s == nil:
		crumbs := []string{dimStyle.Render("hosts")}
		if h, ok := m.selectedHost(); ok {
			crumbs = append(crumbs, aliasStyle.Render(stripControl(h.Alias)))
		}
		if t, ok := m.selectedPlace(); ok && t.kind != targetHost && t.kind != targetTunnels {
			crumbs = append(crumbs, dimStyle.Render(m.tabLabel(t)))
		}
		return crumbs, ""

	case s.dead:
		return []string{host, redText.Render("disconnected")}, ""

	case m.mode == modeDrawer && s.drawer != nil:
		return []string{host, dimStyle.Render("terminal")}, s.drawer.pane.Cwd()

	case m.editing() && s.editor() != nil:
		ed := s.editor()
		// The directory as the tail: two tabs on config.yaml in different directories are
		// otherwise the same crumb.
		return []string{host, dimStyle.Render(stripControl(ed.name))}, path.Dir(ed.path)

	case m.browsing() && s.browser != nil:
		return []string{host, dimStyle.Render("files")}, s.browser.Path()

	case m.focused() && s.shell() != nil:
		name := dimStyle.Render(m.shellName(s))
		if m.scrolling() {
			p := s.shell().pane
			return []string{host, name, accentText.Bold(true).Render(
				fmt.Sprintf("scrollback ⇅ %d/%d", p.ScrollOffset(), p.ScrollbackLen()))}, ""
		}
		// The cwd arrives over OSC 7 and only from a shell that emits it, so a quiet shell
		// gets its name alone rather than a stale path.
		return []string{host, name}, m.shellCwd(m.active)
	}
	return []string{host}, ""
}

// shellName is the shell tab in front as the crumb names it, numbered among the shells.
func (m *model) shellName(s *session) string {
	sh := s.shell()
	i := slices.Index(s.shells, sh)
	return "shell " + strconv.Itoa(i+1)
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
