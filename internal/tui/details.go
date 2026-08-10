package tui

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"hop/internal/store"
)

// detailsMaxW is the widest the details card lets itself get. Past it the two
// columns of facts drift so far apart that the eye stops pairing them.
const detailsMaxW = 74

// renderDetails is the card shown in the right pane when no session is on screen:
// what the host under the cursor is, what hop is currently holding open on it,
// and what the keys would do to it.
func (m *model) renderDetails(w int) string {
	h, ok := m.selectedHost()
	if !ok {
		return m.renderNoHost(w)
	}

	const pad = "  "
	// The card is a column of text, not a banner: on a wide pane it keeps a
	// readable measure rather than stretching its two columns to opposite edges.
	inner := clamp(w-4, 20, detailsMaxW)

	var b strings.Builder
	b.WriteString("\n")

	// Title row: the alias on the left, its state on the right, pushed apart to
	// the full width of the card.
	// Host fields can arrive from an untrusted SSH config or a paste into the
	// form, so strip escape sequences before they reach the terminal.
	title := titleStyle.Render(stripControl(h.Alias))
	badge := m.hostBadge(h)
	gap := max(inner-lipgloss.Width(title)-lipgloss.Width(badge), 1)
	b.WriteString(pad + title + strings.Repeat(" ", gap) + badge + "\n")
	b.WriteString(pad + rule(inner) + "\n\n")

	// The facts, in two columns: what you connect to on the left, what you know
	// about having connected before on the right.
	port := h.Port
	if port == 0 {
		port = 22
	}
	left := [][2]string{
		{"host", fmt.Sprintf("%s:%d", stripControl(h.HostName), port)},
		{"user", stripControl(h.User)},
		{"identity", stripControl(h.IdentityFile)},
	}
	right := [][2]string{
		{"last", relTime(h.LastConnect)},
		{"visits", strconv.Itoa(h.Visits)},
	}
	switch {
	case h.Group != "":
		right = append(right, [2]string{"group", stripControl(h.Group)})
	case len(h.Tags) > 0:
		right = append(right, [2]string{"tags", stripControl(strings.Join(h.Tags, ", "))})
	}

	colW := inner / 2
	b.WriteString(indent(lipgloss.JoinHorizontal(lipgloss.Top,
		kvColumn(left, colW),
		kvColumn(right, inner-colW),
	), pad))
	b.WriteString("\n")

	// Saved forwards are part of the host's dashboard whether they are running or
	// not. A green dot means the listener is live on this session; a hollow one is
	// defined and ready to start with t.
	if len(h.Forwards) > 0 {
		b.WriteString(pad + sectionCap.Render("TUNNELS") + "\n")
		limit := min(len(h.Forwards), 4)
		for _, f := range h.Forwards[:limit] {
			dot := idleDot
			if s := m.sessions[h.Alias]; s != nil && !s.dead && s.tunnels[f.ID] != nil {
				dot = connectedDot
			}
			b.WriteString(pad + dot + " " + dimStyle.Render(truncate(forwardText(f), inner-2)) + "\n")
		}
		if more := len(h.Forwards) - limit; more > 0 {
			b.WriteString(pad + faint.Render(fmt.Sprintf("  … %d more", more)) + "\n")
		}
		b.WriteString("\n")
	}

	// What is open on the connection. This is the answer to "what am I about to
	// close?", and it is on screen before you reach for 'd' rather than after.
	if s := m.sessions[h.Alias]; s != nil {
		if parts := s.summary(); len(parts) > 0 {
			b.WriteString(pad + sectionCap.Render("OPEN") + "\n")
			b.WriteString(pad + accentText.Render("▸ ") + dimStyle.Render(strings.Join(parts, faint.Render(" · "))) + "\n\n")
		}
	}

	b.WriteString(pad + sectionCap.Render("ACTIONS") + "\n")
	b.WriteString(indent(m.actionGrid(h, inner), pad))
	b.WriteString("\n")
	b.WriteString(pad + keyHint("?", "every key hop knows"))

	return clampLines(b.String(), w)
}

// hostBadge is the host's state, spelled out beside its name: idle, dialing, or
// connected.
func (m *model) hostBadge(h store.Host) string {
	switch {
	case m.sessions[h.Alias] != nil && m.sessions[h.Alias].dead:
		return deadDot + " " + redText.Render("connection lost")
	case m.sessions[h.Alias] != nil:
		return connectedDot + " " + greenText.Render("connected")
	case m.connecting[h.Alias]:
		return spinner(m.frame) + " " + yellowText.Render("connecting…")
	default:
		return idleDot + " " + dimStyle.Render("idle")
	}
}

// actionGrid lays the host keys out in two columns, the ones that open something
// on the left and the ones that act on what is already open on the right. The
// labels track the host's state: there is no "focus shell" on a host with none.
func (m *model) actionGrid(h store.Host, w int) string {
	s := m.sessions[h.Alias]
	live := s != nil

	left := [][2]string{
		{"enter", "connect"},
		{"S", "new shell"},
		{"f", "sftp browser"},
		{"t", "start / stop tunnels"},
	}
	right := [][2]string{{"o", "open in vs code"}, {"T", "manage tunnels"}}
	switch {
	case live && s.dead:
		// The one key that matters on a dropped session goes where "focus shell" would
		// have been: focusing a shell with nothing behind it is not on offer.
		right = append(right, [2]string{"r", "reconnect"})
	case live && s.shell() != nil:
		right = append(right, [2]string{"s", "focus shell"})
	}
	if live {
		right = append(right, [2]string{"d", "disconnect"})
	}

	colW := w / 2
	return lipgloss.JoinHorizontal(lipgloss.Top,
		keyColumn(left, colW),
		keyColumn(right, w-colW),
	)
}

// renderNoHost is what the right pane says with nothing under the cursor: on a
// fresh install there is no host to describe, so it describes hop instead.
func (m *model) renderNoHost(w int) string {
	var b strings.Builder
	b.WriteString("\n\n")
	b.WriteString("  " + titleStyle.Render("hop") + dimStyle.Render(" — jump between your servers") + "\n\n")
	if len(m.hosts) == 0 {
		b.WriteString("  " + dimStyle.Render("No hosts yet. Import the ones you") + "\n")
		b.WriteString("  " + dimStyle.Render("already have in ~/.ssh/config:") + "\n\n")
		b.WriteString("  " + keyHint("i", "import ~/.ssh/config") + "\n\n")
		b.WriteString("  " + faint.Render("…or ") + keyHint("a", "add one by hand") + "\n")
	} else {
		b.WriteString("  " + dimStyle.Render("Select a host on the left.") + "\n\n")
		b.WriteString("  " + keyHint("/", "filter the list") + "\n")
		b.WriteString("  " + keyHint("?", "every key hop knows") + "\n")
	}
	return clampLines(b.String(), w)
}

// ---- column helpers ----

// kvColumn renders label/value pairs as a column, labels aligned, and drops a
// pair whose value is empty — a host with no identity file should not have an
// empty row where one would be.
func kvColumn(pairs [][2]string, w int) string {
	labelW := 0
	for _, p := range pairs {
		if p[1] != "" {
			labelW = max(labelW, len(p[0]))
		}
	}

	var lines []string
	for _, p := range pairs {
		if p[1] == "" {
			continue
		}
		label := kvKey.Render(padTo(p[0], labelW))
		value := truncate(p[1], max(w-labelW-2, 1))
		lines = append(lines, padTo(label+"  "+lipgloss.NewStyle().Foreground(colText).Render(value), w))
	}
	return strings.Join(lines, "\n")
}

// keyColumn renders key/label pairs as a column of keycaps with their labels
// aligned — the keycaps are pills of different widths, so the labels have to be
// padded to a common column rather than merely spaced.
func keyColumn(pairs [][2]string, w int) string {
	capW := 0
	for _, p := range pairs {
		capW = max(capW, lipgloss.Width(kc(p[0])))
	}

	var lines []string
	for _, p := range pairs {
		cell := padTo(kc(p[0]), capW) + " " + dimStyle.Render(p[1])
		lines = append(lines, padTo(cell, w))
	}
	return strings.Join(lines, "\n")
}

// indent prefixes every line of s with pad.
func indent(s, pad string) string {
	lines := strings.Split(s, "\n")
	for i, ln := range lines {
		lines[i] = pad + ln
	}
	return strings.Join(lines, "\n") + "\n"
}
