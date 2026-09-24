package tui

// The sidebar's hosts box: every host in the list's order, and under an opened-out host its
// tabs and its tunnels. One row model (hostlist.go) feeds the drawing here and the pointer in
// mouse.go, so what is clicked is what is drawn.

import (
	"fmt"
	"path"
	"slices"
	"strconv"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"hop/internal/keys"
	"hop/internal/store"
)

// listRow is one drawn row: a section heading, a host whose fi indexes m.filtered, or — with
// tab set — one of that host's tabs or its tunnels.
type listRow struct {
	// heading is empty on a host or tab row.
	heading string
	// count is the hosts under this heading now, total how many with the filter off.
	count int
	total int
	fi    int
	// tab is the place a tab row stands for; its alias is empty on a host row.
	tab target
}

// renderList draws the hosts box at w by h, borders included. Its border is the accent
// while the keyboard is in it.
func (m *model) renderList(w, h int) string {
	innerW := max(w-2, 4)
	innerH := max(h-2, 1)

	var b strings.Builder

	// With sections the rows carry their own headings, so a fixed title would repeat one.
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
	if m.listHasFocus() && m.recentAt == 0 {
		style = paneBorderActive
	}
	// A box grown past its height would take the layout with it.
	return style.Width(innerW).Height(h - 2).Render(fitLines(b.String(), h-2))
}

// listHasFocus is true when keys go to the sidebar rather than a pane or card.
func (m *model) listHasFocus() bool {
	return m.mode == modeList
}

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

func (m *model) filterPrompt() string {
	// Pasted filter text can carry escape sequences.
	prompt := accentText.Render("/") + stripControl(m.filter)
	if m.filtering {
		return prompt + accentText.Render("▏")
	}
	return prompt + faint.Render("  esc to clear")
}

func (m *model) renderRows(w, h int) string {
	start := m.listStart(h)
	end := min(start+h, len(m.rows))

	// The scrollbar earns its column only when the list overflows.
	bar := len(m.rows) > h
	roww := w
	if bar {
		roww = w - 1
	}
	// The cursor is drawn only where the keyboard is: in the sidebar, not in the recent
	// places of the content area.
	cursor := -1
	if m.listHasFocus() && m.recentAt == 0 {
		cursor = m.cursorRow()
	}

	lines := make([]string, 0, end-start)
	for i := start; i < end; i++ {
		r := m.rows[i]
		var row string
		switch {
		case r.heading != "":
			row = truncate(m.sectionHeading(r), roww)
		case r.tab.alias != "":
			row = m.renderTabRow(r.tab, i == cursor, roww)
		default:
			idx := m.filtered[r.fi]
			row = m.renderRow(m.hosts[idx], m.highlights[idx], i == cursor, roww)
		}
		if bar {
			row = padTo(row, roww) + m.scrollbarCell(i-start, h)
		}
		lines = append(lines, row)
	}
	return strings.Join(lines, "\n")
}

func (m *model) sectionHeading(r listRow) string {
	title := sectionCap.Render(r.heading)
	if m.filter != "" && r.count != r.total {
		return title + faint.Render(fmt.Sprintf("  %d/%d", r.count, r.total))
	}
	return title + faint.Render(fmt.Sprintf("  %d", r.count))
}

// listStart is the first drawn row; listRowAt runs the same arithmetic backwards. It keeps
// the anchor row in view: the cursor while the keyboard is here, else the tab in front.
func (m *model) listStart(h int) int {
	if row := m.anchorRow(); row >= h {
		return row - h + 1
	}
	return 0
}

// anchorRow is the row the hosts box scrolls to keep in view.
func (m *model) anchorRow() int {
	if m.listHasFocus() {
		return m.cursorRow()
	}
	t, ok := m.frontTarget()
	for i, r := range m.rows {
		if r.heading != "" || m.hosts[m.filtered[r.fi]].Alias != m.active {
			continue
		}
		if !ok || r.tab == t {
			return i
		}
	}
	for i, r := range m.rows {
		if r.heading == "" && r.tab.alias == "" && m.hosts[m.filtered[r.fi]].Alias == m.active {
			return i
		}
	}
	return 0
}

func (m *model) scrollbarCell(i, h int) string {
	n := len(m.rows)
	// Fixed one-cell thumb: position matters here, the visible fraction does not.
	thumb := 0
	if n > 1 {
		thumb = min(m.anchorRow(), n-1) * (h - 1) / (n - 1)
	}
	if i == thumb {
		return dimStyle.Render("┃")
	}
	return faint.Render("│")
}

// ---- rows ----

// seg is one styled run of a row, measured as plain text so a row can be laid out before it
// is styled.
type seg struct {
	text  string
	style lipgloss.Style
}

// drawSegs lays left against the left edge and right against the right one, w cells in all.
// left gives way first; right is assumed to fit.
func drawSegs(left, right []seg, w int) string {
	rw := 0
	for _, s := range right {
		rw += lipgloss.Width(s.text)
	}
	room := max(w-rw, 0)
	var b strings.Builder
	used := 0
	for _, s := range left {
		t := s.text
		if tw := lipgloss.Width(t); used+tw > room {
			t = truncate(t, room-used)
		}
		if t == "" {
			break
		}
		b.WriteString(s.style.Render(t))
		used += lipgloss.Width(t)
	}
	if gap := w - used - rw; gap > 0 {
		b.WriteString(strings.Repeat(" ", gap))
	}
	if used+rw <= w {
		for _, s := range right {
			b.WriteString(s.style.Render(s.text))
		}
	}
	return b.String()
}

// cursorMark is the sidebar's cursor: the accent bar in the row's first column and its text
// bright. It is drawn only while the keyboard is in the sidebar, so the accent still marks
// only where the keyboard is. No fill: grey text on a grey fill does not read.
var (
	cursorMark = seg{text: "▌"}
	cursorText = lipgloss.NewStyle().Bold(true).Foreground(colBright)
)

// renderRow is one host: whether it is opened out, its alias, and on the right its dot and,
// folded up, how many tabs it holds. A host with nothing open is dim.
func (m *model) renderRow(h store.Host, hits []int, selected bool, w int) string {
	s := m.sessions[h.Alias]
	n := len(m.hostTabs(h.Alias))

	glyph := " "
	switch {
	case m.expanded(h.Alias):
		glyph = "▾"
	case n > 0:
		glyph = "▸"
	}

	name, mark := dimStyle, seg{text: " "}
	if s != nil || m.connecting[h.Alias] {
		name = aliasStyle
	}
	if selected {
		name, mark = cursorText, seg{text: cursorMark.text, style: accentText}
	}
	left := []seg{mark, {text: glyph, style: dimStyle}, {text: " "}}
	// The alias is cut before it is split into runs, so a match past the cut is not drawn.
	room := max(w-3-4, 1)
	left = append(left, highlightSegs(truncate(stripControl(h.Alias), room), hits, name, matchStyle)...)

	right := []seg{m.dotSeg(h.Alias)}
	if n > 0 && !m.expanded(h.Alias) {
		right = append(right, seg{text: " " + strconv.Itoa(n), style: faint})
	}
	right = append(right, seg{text: " "})
	return drawSegs(left, right, w)
}

// renderTabRow is one tab under its host, or the host's tunnels. The tab in front of the
// host in front is bold, and carries the accent marker while the keyboard is in it; while
// the keyboard is in the sidebar the marker is the cursor's alone, so there is one.
func (m *model) renderTabRow(t target, selected bool, w int) string {
	s := m.sessions[t.alias]
	front, ok := m.frontTarget()
	here := ok && t == front

	mark := seg{text: " "}
	if here && !m.listHasFocus() {
		mark = seg{text: cursorMark.text, style: accentText}
	}
	style := dimStyle
	switch {
	case s != nil && s.dead:
		style = faint
	case here:
		style = aliasStyle
	}

	label := m.tabLabel(t)
	if t.kind == targetTunnels {
		n := len(s.tunnels)
		label, style = fmt.Sprintf("⇄ %d %s", n, plural(n, "tunnel", "tunnels")), faint
	}
	if selected {
		mark, style = seg{text: cursorMark.text, style: accentText}, cursorText
	}
	return drawSegs([]seg{mark, {text: "    "}, {text: label, style: style}}, nil, w)
}

// tabLabel is t's name in the sidebar: a glyph for its kind and the last part of what makes
// it this one.
func (m *model) tabLabel(t target) string {
	s := m.sessions[t.alias]
	if s == nil {
		return ""
	}
	var l string
	switch t.kind {
	case targetShell:
		i := slices.IndexFunc(s.shells, func(sh *shellTab) bool { return sh.id == t.id })
		if i < 0 {
			return ""
		}
		l = "$ shell " + strconv.Itoa(i+1)
		if cwd := s.shells[i].pane.Cwd(); cwd != "" {
			l = "$ " + path.Base(cwd)
		}
	case targetBrowser:
		l = "▤ files"
	case targetEditor:
		if i := s.findEditorID(t.id); i >= 0 {
			l = "✎ " + s.editors[i].name
		}
	case targetDrawer:
		l = "▭ terminal"
	}
	return stripControl(l)
}

func (m *model) dotFor(alias string) string {
	d := m.dotSeg(alias)
	return d.style.Render(d.text)
}

// dotSeg is a host's state as one run: connected, dropped, dialling, or idle.
func (m *model) dotSeg(alias string) seg {
	if s, live := m.sessions[alias]; live {
		if s.dead {
			return seg{text: "●", style: redText}
		}
		return seg{text: "●", style: greenText}
	}
	if m.connecting[alias] {
		return seg{text: spinnerFrames[m.spinFrame%len(spinnerFrames)], style: yellowText}
	}
	return seg{text: "○", style: faint}
}

// highlight renders s in base with the byte offsets in hits picked out in hit.
func highlight(s string, hits []int, base, hit lipgloss.Style) string {
	var b strings.Builder
	for _, sg := range highlightSegs(s, hits, base, hit) {
		b.WriteString(sg.style.Render(sg.text))
	}
	return b.String()
}

// highlightSegs is highlight as runs, for a row that lays a fill under them.
func highlightSegs(s string, hits []int, base, hit lipgloss.Style) []seg {
	// Stripped inside the loop: hits are byte offsets into the original s.
	if len(hits) == 0 {
		return []seg{{text: stripControl(s), style: base}}
	}
	at := make(map[int]bool, len(hits))
	for _, i := range hits {
		at[i] = true
	}
	var out []seg
	for i, r := range s {
		if r < 0x20 || (r >= 0x7f && r < 0xa0) {
			continue
		}
		st := base
		if at[i] {
			st = hit
		}
		out = append(out, seg{text: string(r), style: st})
	}
	return out
}

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

// ---- the recent places ----

// recentTop is the content row the first recent place is drawn on: a blank row, then the
// heading. mouse.go reads the same number back.
const recentTop = 2

// renderStart is the content area with no host in front: the recent places across hosts,
// which enter lands on, then the details of the host under the cursor.
func (m *model) renderStart(w int) string {
	var b strings.Builder
	if len(m.recent) > 0 {
		b.WriteString("\n  ")
		b.WriteString(sectionCap.Render("RECENT"))
		b.WriteString("\n")
		for i, t := range m.recent {
			b.WriteString(m.renderRecentRow(t, m.listHasFocus() && m.recentAt == i+1, min(w, detailsMaxW)))
			b.WriteString("\n")
		}
	}
	b.WriteString(m.renderDetails(w))
	return b.String()
}

// renderRecentRow is one recent place: its host, the tab as the sidebar names it, and how
// long ago it had the keyboard.
func (m *model) renderRecentRow(t target, selected bool, w int) string {
	lead, alias := "  ", aliasStyle
	if selected {
		lead, alias = selBar+" ", selectedAliasStyle
	}
	label := m.tabLabel(t)
	ago := faint.Render(m.usedAgo(t))
	head := lead + alias.Render(truncate(stripControl(t.alias), 10)) + " "
	room := max(w-lipgloss.Width(head)-lipgloss.Width(ago)-1, 1)
	left := head + dimStyle.Render(truncate(label, room))
	return left + strings.Repeat(" ", max(w-lipgloss.Width(left)-lipgloss.Width(ago), 1)) + ago
}
