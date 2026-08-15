package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// ---- status bar ----
//
// The row between the body and the key legend, and the answer to "where am I": on the
// left the place, as a trail of crumbs ending in the thing you are actually looking at —
// a remote directory, a file, a listing — and on the right the machine that place is on,
// spelled as ssh would take it, with the chips that say which of several tabs is up.
//
// It exists so the footer does not have to. Before it, the place lived in the header's
// breadcrumb at the far top-left while the keys sat at the bottom, and the two things you
// read together were a screen apart.

// statusSep is what stands between crumbs. Faint, because the crumbs are the content.
var statusSep = faint.Render(" › ")

// renderStatus draws the bar: crumbs left, target and chips right, one row, always
// exactly m.width wide so the fill reaches both edges.
func (m *model) renderStatus() string {
	// The bar's own padding, which the content has to be cut to.
	inner := max(m.width-2, 0)

	right := m.statusChips()
	// The crumbs yield to the target: which host you are on survives a narrow window,
	// the path is what gets elided.
	left := m.statusCrumbs(max(inner-lipgloss.Width(right)-2, 8))

	gap := max(inner-lipgloss.Width(left)-lipgloss.Width(right), 0)
	line := truncate(left+strings.Repeat(" ", gap)+right, inner)
	return statusBar.Width(m.width).Render(line)
}

// statusCrumbs is the trail: host, what you are doing on it, and where. The last crumb is
// the one that moves — a cwd, a filename, a listing — so it is the one elided when the
// window cannot hold the trail, and elided from the left, since the tail of a path says
// more than its root.
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

// crumbs splits the trail into the fixed part and the one long moving part, so
// statusCrumbs knows which crumb to eat into. The fixed crumbs come styled; the moving
// one comes raw, because it is the one that gets cut.
func (m *model) crumbs() ([]string, string) {
	s := m.sessions[m.active]

	switch {
	// A dropped session says so here as well as on the pane: this line is the one place
	// that is always on screen.
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
		// The cwd arrives over OSC 7 and only from a shell that emits it, so a shell that
		// stays quiet gets the word instead of a stale path.
		if cwd := m.shellCwd(m.active); cwd != "" {
			return []string{aliasStyle.Render(m.active)}, cwd
		}
		return []string{aliasStyle.Render(m.active), dimStyle.Render("shell")}, ""
	}

	// The list. The host under the cursor is a crumb, since it is what every key on this
	// screen is about to act on.
	crumbs := []string{dimStyle.Render("hosts")}
	if h, ok := m.selectedHost(); ok {
		crumbs = append(crumbs, aliasStyle.Render(h.Alias))
	}
	return crumbs, ""
}

// statusChips is the right-hand end: which of several tabs is up, and the machine, as
// user@host:port — the string you would have typed at ssh to get here. The alias is on
// the left already; this says what it resolves to, which is what tells two aliases on one
// box apart.
func (m *model) statusChips() string {
	var chips []string
	s := m.sessions[m.active]

	switch {
	case m.scrolling() && s != nil && s.shell() != nil:
		p := s.shell().pane
		chips = append(chips, accentText.Bold(true).Render(fmt.Sprintf("⇅ %d/%d", p.ScrollOffset(), p.ScrollbackLen())))
	case m.editing() && s != nil && len(s.editors) > 1:
		chips = append(chips, chipStyle.Render(fmt.Sprintf("file %d/%d", s.activeEd+1, len(s.editors))))
	case m.focused() && s != nil && len(s.shells) > 1:
		chips = append(chips, chipStyle.Render(fmt.Sprintf("shell %d/%d", s.activeSh+1, len(s.shells))))
	}

	if t := m.statusTarget(); t != "" {
		chips = append(chips, faint.Render(t))
	}
	return strings.Join(chips, " ")
}

// statusTarget is user@host:port for whichever host the screen is about: the one a pane
// is open on, or the one under the cursor in the list. The parts that carry no
// information are left off — no user when the config named none, no port when it is 22 —
// so the common case reads as the hostname it is.
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

// elideLeft cuts s to w keeping its end, marking the cut with a leading ellipsis. Paths
// are the reason: /var/www/html/current/public says more than /var/www/html/cur….
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
	// Measured in runes against a width budget: exact for the paths and filenames this
	// sees, which is every string it is handed.
	r := []rune(s)
	if len(r) <= w-1 {
		return "…" + string(r)
	}
	return "…" + string(r[len(r)-(w-1):])
}
