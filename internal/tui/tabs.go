package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// renderEditorTabs draws the strip of open files above the editor.
func (m *model) renderEditorTabs(s *session) string {
	names := make([]string, len(s.editors))
	for i, e := range s.editors {
		names[i] = e.name
	}
	return m.renderTabStrip(names, s.activeEd)
}

// renderShellTabs draws the strip of shells open on the host. They have no names
// of their own, so they are numbered — which is also how alt+1..9 addresses them.
func (m *model) renderShellTabs(s *session) string {
	names := make([]string, len(s.shells))
	for i := range s.shells {
		names[i] = fmt.Sprintf("shell %d", i+1)
	}
	return m.renderTabStrip(names, s.activeSh)
}

// renderTabStrip draws a row of tab pills, the open one filled with the accent
// and the rest sunk into the surface behind it.
//
// It is always exactly one line — the pane below was sized on that promise — so
// it is truncated to the pane width, and a long row of tabs cannot push the
// layout around. When there are more tabs than fit, the strip scrolls to keep the
// open one on screen: a tab you cannot see is a tab you cannot tell you are on.
func (m *model) renderTabStrip(names []string, active int) string {
	pills := make([]string, len(names))
	for i, n := range names {
		if i == active {
			pills[i] = tabActive.Render(fmt.Sprintf("%d %s", i+1, n))
			continue
		}
		pills[i] = tabInactive.Render(fmt.Sprintf("%d %s", i+1, n))
	}

	// Drop whole pills off the left until the open one fits, rather than letting
	// truncate cut it in half.
	start := 0
	for start < active && stripWidth(pills[start:], m.paneW) > m.paneW {
		start++
	}

	strip := strings.Join(pills[start:], " ")
	if start > 0 {
		strip = faint.Render("‹ ") + strip
	}
	return truncate(strip, m.paneW)
}

// stripWidth is the display width of pills joined by a space, stopping once it
// has exceeded limit — the caller only ever asks whether they fit.
func stripWidth(pills []string, limit int) int {
	total := 0
	for i, p := range pills {
		if i > 0 {
			total++
		}
		total += lipgloss.Width(p)
		if total > limit {
			return total
		}
	}
	return total
}
