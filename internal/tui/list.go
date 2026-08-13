package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"hop/internal/store"
)

// renderList draws the host sidebar: a title with the host count, the filter
// prompt while one is being typed, then the rows, with a scrollbar down the right
// edge once there are more hosts than fit.
func (m *model) renderList(w, h int) string {
	innerW := max(w-2, 4)
	innerH := max(h-2, 1)

	var b strings.Builder

	// With something pinned the sidebar carries its own PINNED / HOSTS headings
	// inside the scrolling rows, and a second, fixed "HOSTS" title above them would
	// say the same word twice about two different things.
	if !m.hasSections() {
		b.WriteString(truncate(m.listHeading(), innerW))
		b.WriteString("\n")
		innerH--
	}

	if m.filtering || m.filter != "" {
		b.WriteString(truncate(m.filterPrompt(), innerW))
		b.WriteString("\n")
		innerH--
	}
	innerH = max(innerH, 1)

	switch {
	case len(m.hosts) == 0:
		b.WriteString(m.renderEmptyList(innerW))
	case len(m.filtered) == 0:
		b.WriteString(faint.Render(truncate("no host matches "+stripControl(m.filter), innerW)))
	default:
		b.WriteString(m.renderRows(innerW, innerH))
	}

	style := paneBorder
	if m.listHasFocus() {
		style = paneBorderActive
	}
	// Cut to the pane: on a short window the empty-state text is taller than the
	// sidebar, and a box that grows past its height takes the whole layout with it.
	return style.Width(innerW).Height(h - 2).Render(fitLines(b.String(), h-2))
}

// listHasFocus is true when keys are going to the host list rather than to a pane
// or a card — which is what the accented border on the sidebar means.
func (m *model) listHasFocus() bool {
	return m.mode == modeList
}

// listHeading is the section title, with the host count — and, while a filter is
// on, how much of the list it is showing you.
func (m *model) listHeading() string {
	title := sectionCap.Render("HOSTS")
	if len(m.hosts) == 0 {
		return title
	}
	if m.filter != "" {
		return title + faint.Render(fmt.Sprintf("  %d/%d", len(m.filtered), len(m.hosts)))
	}
	return title + faint.Render(fmt.Sprintf("  %d", len(m.hosts)))
}

// filterPrompt is the "/…" line, with a caret while it has the keyboard and a
// hint about the enter that hands it back to the list.
func (m *model) filterPrompt() string {
	// Pasted filter text can carry escape sequences, so strip before it renders.
	prompt := accentText.Render("/") + stripControl(m.filter)
	if m.filtering {
		return prompt + accentText.Render("▏")
	}
	return prompt + faint.Render("  esc to clear")
}

// listRow is one drawn row of the sidebar: either a section heading, or a host.
// Only the host rows can be selected — the cursor indexes m.filtered, and fi is
// where in it this row's host is.
type listRow struct {
	// heading is "PINNED" or "HOSTS" on a section row, and empty on a host row.
	heading string
	// count is the hosts under this heading right now, and total how many there
	// are with the filter off — the "3/8" a filtered section shows.
	count int
	total int
	// fi indexes m.filtered, on a host row.
	fi int
}

// renderRows draws the visible slice of the list, headings included, scrolled so
// the cursor is always on screen, with a scrollbar when there is more list than
// window.
func (m *model) renderRows(w, h int) string {
	start := m.listStart(h)
	end := min(start+h, len(m.rows))

	// The scrollbar earns its column only when the list actually overflows.
	bar := len(m.rows) > h
	roww := w
	if bar {
		roww = w - 1
	}

	lines := make([]string, 0, end-start)
	for i := start; i < end; i++ {
		r := m.rows[i]
		var row string
		if r.heading != "" {
			row = truncate(m.sectionHeading(r), roww)
		} else {
			idx := m.filtered[r.fi]
			row = m.renderRow(m.hosts[idx], m.highlights[idx], r.fi == m.cursor, roww)
		}
		if bar {
			row = padTo(row, roww) + m.scrollbarCell(i-start, h)
		}
		lines = append(lines, row)
	}
	return strings.Join(lines, "\n")
}

// sectionHeading is a PINNED / HOSTS row: the same capped title as the sidebar's
// own, with the section's share of the hosts after it — "3/8" while a filter is
// hiding some of them.
func (m *model) sectionHeading(r listRow) string {
	title := sectionCap.Render(r.heading)
	if m.filter != "" && r.count != r.total {
		return title + faint.Render(fmt.Sprintf("  %d/%d", r.count, r.total))
	}
	return title + faint.Render(fmt.Sprintf("  %d", r.count))
}

// listStart is the first row drawn in an h-row viewport: the scroll window keeps
// the cursor inside it, and scrolls the list as little as it can to do so. It is
// shared with the mouse, which has to run the same arithmetic backwards to say
// which host a clicked row is (see listRowAt).
func (m *model) listStart(h int) int {
	if row := m.cursorRow(); row >= h {
		return row - h + 1
	}
	return 0
}

// scrollbarCell is the character on row i of an h-row viewport: a bright thumb
// where the cursor sits proportionally in the whole list, a faint track elsewhere.
func (m *model) scrollbarCell(i, h int) string {
	n := len(m.rows)
	// The thumb is one cell — the list is a list of hosts, not a document, so where
	// you are matters and how much is on screen does not.
	thumb := 0
	if n > 1 {
		thumb = m.cursorRow() * (h - 1) / (n - 1)
	}
	if i == thumb {
		return accentText.Render("┃")
	}
	return faint.Render("│")
}

// renderRow draws one host: a status dot, the alias (with the characters the
// filter matched picked out), and who you would be on it. The row under the
// cursor gets an accent bar rather than a full-width fill, which nests badly with
// the styles inside it.
func (m *model) renderRow(h store.Host, hits []int, selected bool, w int) string {
	lead := "  "
	alias := aliasStyle
	if selected {
		lead = selBar + " "
		alias = selectedAliasStyle
	}

	// Host fields can arrive from an untrusted SSH config (via hop import) or a
	// paste into the form, so strip any escape sequences before they reach the
	// terminal — the same defense as remote-derived strings elsewhere.
	who := stripControl(h.User)
	if who != "" {
		who += "@"
	}
	who += stripControl(h.HostName)

	// What is open on the host, when it is more than the dot can say.
	badge := ""
	if s := m.sessions[h.Alias]; s != nil {
		if n := len(s.shells); n > 1 {
			badge = " " + faint.Render(fmt.Sprintf("×%d", n))
		}
		if s.browser != nil {
			badge += " " + faint.Render("▤")
		}
		if n := len(s.tunnels); n > 0 {
			badge += " " + faint.Render(fmt.Sprintf("⇄%d", n))
		}
	}
	if h.Group != "" {
		badge += " " + faint.Render("["+stripControl(h.Group)+"]")
	} else if len(h.Tags) > 0 {
		badge += " " + faint.Render("#"+stripControl(h.Tags[0]))
	}

	head := lead + m.dotFor(h.Alias) + " " + highlight(h.Alias, hits, alias, matchStyle)

	// The alias and the badge are what you came for; who@where is the part that
	// gives way when the sidebar is narrow.
	room := w - lipgloss.Width(head) - lipgloss.Width(badge) - 2
	tail := ""
	if room > 3 {
		tail = "  " + dimStyle.Render(truncate(who, room))
	}
	return truncate(head+tail+badge, w)
}

// dotFor is the host's connection state at a glance: green connected, red for a
// session whose connection dropped, a spinner while it dials, a hollow dot when it
// is idle.
func (m *model) dotFor(alias string) string {
	if s, live := m.sessions[alias]; live {
		if s.dead {
			return deadDot
		}
		return connectedDot
	}
	if m.connecting[alias] {
		return spinner(m.frame)
	}
	return idleDot
}

// highlight renders s in base, with the byte offsets in hits picked out in hit —
// the characters the fuzzy filter matched on, so a surprising hit explains itself.
func highlight(s string, hits []int, base, hit lipgloss.Style) string {
	// The alias can come from an untrusted SSH config, so it gets the same
	// control-character stripping as remote strings — done here, inside the loop,
	// because hits are byte offsets into the original s and stripping first would
	// misalign them.
	if len(hits) == 0 {
		return base.Render(stripControl(s))
	}
	at := make(map[int]bool, len(hits))
	for _, i := range hits {
		at[i] = true
	}

	var b strings.Builder
	for i, r := range s {
		if r < 0x20 || (r >= 0x7f && r < 0xa0) {
			continue
		}
		if at[i] {
			b.WriteString(hit.Render(string(r)))
		} else {
			b.WriteString(base.Render(string(r)))
		}
	}
	return b.String()
}

// renderEmptyList is the first-run screen: an empty sidebar is not a failure, it
// is a host list nobody has filled in yet, so it says how.
func (m *model) renderEmptyList(w int) string {
	var b strings.Builder
	b.WriteString(dimStyle.Render(truncate("No hosts yet.", w)))
	b.WriteString("\n\n")
	b.WriteString(faint.Render(truncate("Import them from your", w)))
	b.WriteString("\n")
	b.WriteString(faint.Render(truncate("SSH config, or add one:", w)))
	b.WriteString("\n\n")
	b.WriteString(truncate("  "+keyHint("i", "import"), w))
	b.WriteString("\n")
	b.WriteString(truncate("  "+keyHint("a", "add a host"), w))
	return b.String()
}
