package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"hop/internal/keys"
	"hop/internal/store"
)

// renderList draws the host sidebar: a title with the host count, the filter prompt while
// one is being typed, then the rows, with a scrollbar once there are more hosts than fit.
func (m *model) renderList(w, h int) string {
	innerW := max(w-2, 4)
	innerH := max(h-2, 1)

	var b strings.Builder

	// With something pinned the rows carry their own PINNED / HOSTS headings, and a fixed
	// "HOSTS" title above them would say the same word twice about two different things.
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
	// Cut to the pane: on a short window the empty-state text is taller than the sidebar,
	// and a box that grows past its height takes the layout with it.
	return style.Width(innerW).Height(h - 2).Render(fitLines(b.String(), h-2))
}

// listHasFocus is true when keys are going to the host list rather than a pane or card —
// what the accented border on the sidebar means.
func (m *model) listHasFocus() bool {
	return m.mode == modeList
}

// listHeading is the section title with the host count, and how much of the list a filter
// is showing.
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

// filterPrompt is the "/…" line, with a caret while it has the keyboard.
func (m *model) filterPrompt() string {
	// Pasted filter text can carry escape sequences.
	prompt := accentText.Render("/") + stripControl(m.filter)
	if m.filtering {
		return prompt + accentText.Render("▏")
	}
	return prompt + faint.Render("  esc to clear")
}

// listRow is one drawn row of the sidebar: a section heading or a host. Only host rows
// can be selected — the cursor indexes m.filtered, and fi is this row's place in it.
type listRow struct {
	// heading is "PINNED" or "HOSTS" on a section row, and empty on a host row.
	heading string
	// count is the hosts under this heading now, total how many with the filter off — the
	// "3/8" a filtered section shows.
	count int
	total int
	// fi indexes m.filtered, on a host row.
	fi int
}

// renderRows draws the visible slice of the list, headings included, scrolled to keep the
// cursor on screen, with a scrollbar when there is more list than window.
func (m *model) renderRows(w, h int) string {
	start := m.listStart(h)
	end := min(start+h, len(m.rows))

	// The scrollbar earns its column only when the list overflows.
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

// sectionHeading is a PINNED / HOSTS row: the sidebar's own capped title, with the
// section's share of the hosts after it.
func (m *model) sectionHeading(r listRow) string {
	title := sectionCap.Render(r.heading)
	if m.filter != "" && r.count != r.total {
		return title + faint.Render(fmt.Sprintf("  %d/%d", r.count, r.total))
	}
	return title + faint.Render(fmt.Sprintf("  %d", r.count))
}

// listStart is the first row drawn in an h-row viewport, scrolling as little as it can to
// keep the cursor inside. The mouse runs the same arithmetic backwards (see listRowAt).
func (m *model) listStart(h int) int {
	if row := m.cursorRow(); row >= h {
		return row - h + 1
	}
	return 0
}

// scrollbarCell is the character on row i of an h-row viewport: a bright thumb where the
// cursor sits proportionally in the list, a faint track elsewhere.
func (m *model) scrollbarCell(i, h int) string {
	n := len(m.rows)
	// One cell: this is a list of hosts, not a document, so where you are matters and how
	// much is on screen does not.
	thumb := 0
	if n > 1 {
		thumb = m.cursorRow() * (h - 1) / (n - 1)
	}
	if i == thumb {
		return accentText.Render("┃")
	}
	return faint.Render("│")
}

// renderRow draws one host: a status dot, the alias with the filter's matches picked out,
// and who you would be on it. The cursor's row gets an accent bar rather than a
// full-width fill, which nests badly with the styles inside it.
func (m *model) renderRow(h store.Host, hits []int, selected bool, w int) string {
	lead := "  "
	alias := aliasStyle
	if selected {
		lead = selBar + " "
		alias = selectedAliasStyle
	}

	// Host fields can arrive from an untrusted SSH config or a paste into the form, so
	// they are stripped like remote-derived strings elsewhere.
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

	// who@where is the part that gives way when the sidebar is narrow.
	room := w - lipgloss.Width(head) - lipgloss.Width(badge) - 2
	tail := ""
	if room > 3 {
		tail = "  " + dimStyle.Render(truncate(who, room))
	}
	return truncate(head+tail+badge, w)
}

// dotFor is the host's connection state at a glance: green connected, red dropped, a
// spinner while dialing, hollow when idle.
func (m *model) dotFor(alias string) string {
	if s, live := m.sessions[alias]; live {
		if s.dead {
			return deadDot
		}
		return connectedDot
	}
	if m.connecting[alias] {
		return spinner(m.spinFrame)
	}
	return idleDot
}

// highlight renders s in base with the byte offsets in hits picked out in hit, so a
// surprising fuzzy match explains itself.
func highlight(s string, hits []int, base, hit lipgloss.Style) string {
	// The alias gets the same control-character stripping as remote strings, done inside
	// the loop: hits are byte offsets into the original s, so stripping first would
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

// renderEmptyList is the first-run screen: a host list nobody has filled in yet, saying
// how to.
func (m *model) renderEmptyList(w int) string {
	var b strings.Builder
	b.WriteString(dimStyle.Render(truncate("No hosts yet.", w)))
	b.WriteString("\n\n")
	b.WriteString(faint.Render(truncate("Import them from your", w)))
	b.WriteString("\n")
	b.WriteString(faint.Render(truncate("SSH config, or add one:", w)))
	b.WriteString("\n\n")
	b.WriteString(truncate("  "+m.hint(keys.List, keys.HostImport, "import"), w))
	b.WriteString("\n")
	b.WriteString(truncate("  "+m.hint(keys.List, keys.HostAdd, "add a host"), w))
	return b.String()
}
