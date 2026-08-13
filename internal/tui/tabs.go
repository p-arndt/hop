package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// renderEditorTabs draws the strip of open files above the editor.
func (m *model) renderEditorTabs(s *session) string {
	return m.renderTabStrip(editorTabNames(s), s.activeEd)
}

// renderShellTabs draws the strip of shells open on the host.
func (m *model) renderShellTabs(s *session) string {
	return m.renderTabStrip(shellTabNames(s), s.activeSh)
}

// editorTabNames labels each open file with its name. Split out of the renderer
// because the mouse needs the same labels to measure the strip it is clicking on.
func editorTabNames(s *session) []string {
	names := make([]string, len(s.editors))
	for i, e := range s.editors {
		names[i] = e.name
	}
	return names
}

// shellTabNames labels each shell. They have no names of their own, so they are
// numbered — which is also how the ctrl+o digit chord addresses them.
func shellTabNames(s *session) []string {
	names := make([]string, len(s.shells))
	for i := range s.shells {
		names[i] = fmt.Sprintf("shell %d", i+1)
	}
	return names
}

// renderTabStrip draws a row of tab pills, the open one filled with the accent
// and the rest sunk into the surface behind it.
//
// It is always exactly one line — the pane below was sized on that promise — so
// it is truncated to the pane width, and a long row of tabs cannot push the
// layout around. When there are more tabs than fit, the strip scrolls to keep the
// open one on screen: a tab you cannot see is a tab you cannot tell you are on.
func (m *model) renderTabStrip(names []string, active int) string {
	pills := tabPills(names, active)
	start := m.tabStart(pills, active)

	strip := strings.Join(pills[start:], " ")
	if start > 0 {
		strip = faint.Render(tabMore) + strip
	}
	return truncate(strip, m.paneW)
}

// tabMore is the marker that stands in for the pills scrolled off the left. Its
// width is part of the strip's geometry, so tabAt measures it rather than assuming.
const tabMore = "‹ "

// tabPills renders each tab as its pill, the open one filled. Split out of
// renderTabStrip so the mouse can measure the same pills the screen is showing.
func tabPills(names []string, active int) []string {
	pills := make([]string, len(names))
	for i, n := range names {
		if i == active {
			pills[i] = tabActive.Render(fmt.Sprintf("%d %s", i+1, n))
			continue
		}
		pills[i] = tabInactive.Render(fmt.Sprintf("%d %s", i+1, n))
	}
	return pills
}

// tabStart is the first pill drawn: whole pills are dropped off the left until the
// open one fits, rather than letting truncate cut it in half.
func (m *model) tabStart(pills []string, active int) int {
	start := 0
	for start < active && stripWidth(pills[start:], m.paneW) > m.paneW {
		start++
	}
	return start
}

// tabAt maps a column on the strip to the tab drawn there, or false for the gaps
// between pills and the empty run past the last one. It walks the same pills
// renderTabStrip lays down, in the same order, so a click lands on the tab the eye
// is on rather than on an index counted from the left.
func (m *model) tabAt(names []string, active, x int) (int, bool) {
	pills := tabPills(names, active)
	start := m.tabStart(pills, active)

	col := 0
	if start > 0 {
		col = lipgloss.Width(tabMore)
	}
	for i := start; i < len(pills) && col < m.paneW; i++ {
		w := lipgloss.Width(pills[i])
		if x >= col && x < col+w {
			return i, true
		}
		col += w + 1 // the space between pills belongs to neither
	}
	return 0, false
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
