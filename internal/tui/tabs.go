package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

func (m *model) renderEditorTabs(s *session, right bool) string {
	return m.renderTabStrip(editorTabNames(s), s.editorIndex(right), m.contentW(s))
}

func (m *model) renderShellTabs(s *session) string {
	return m.renderTabStrip(shellTabNames(s), s.activeSh, m.paneW)
}

func editorTabNames(s *session) []string {
	names := make([]string, len(s.editors))
	for i, e := range s.editors {
		names[i] = e.name
	}
	return names
}

// shellTabNames numbers each shell, matching the leader's digit chord.
func shellTabNames(s *session) []string {
	names := make([]string, len(s.shells))
	for i := range s.shells {
		names[i] = fmt.Sprintf("shell %d", i+1)
	}
	return names
}

// renderTabStrip draws a row of tab pills, always exactly one line wide. w is passed in
// because a split content area draws two strips, each half the row's width.
func (m *model) renderTabStrip(names []string, active, w int) string {
	pills := tabPills(names, active)
	start := tabStart(pills, active, w)

	strip := strings.Join(pills[start:], " ")
	if start > 0 {
		strip = faint.Render(tabMore) + strip
	}
	return truncate(strip, w)
}

// tabMore stands in for the pills scrolled off the left; tabAt measures its width.
const tabMore = "‹ "

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

// tabStart drops whole pills off the left until the open one fits, rather than letting
// truncate cut it in half.
func tabStart(pills []string, active, w int) int {
	start := 0
	for start < active && stripWidth(pills[start:], w) > w {
		start++
	}
	return start
}

// tabAt maps a column on the strip to the tab drawn there, or false for the gaps.
func (m *model) tabAt(names []string, active, x, w int) (int, bool) {
	pills := tabPills(names, active)
	start := tabStart(pills, active, w)

	col := 0
	if start > 0 {
		col = lipgloss.Width(tabMore)
	}
	for i := start; i < len(pills) && col < w; i++ {
		w := lipgloss.Width(pills[i])
		if x >= col && x < col+w {
			return i, true
		}
		col += w + 1 // the space between pills belongs to neither
	}
	return 0, false
}

// stripWidth is the display width of pills joined by a space, stopping once it exceeds limit.
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
