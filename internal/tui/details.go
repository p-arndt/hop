package tui

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"hop/internal/config"
	"hop/internal/store"
)

// detailsMaxW is the widest the details card gets: past it the two columns of facts
// drift so far apart that the eye stops pairing them.
const detailsMaxW = 74

// renderDetails is the card shown in the right pane when no session is on screen: what
// the host under the cursor is, what hop holds open on it, and what the keys would do.
func (m *model) renderDetails(w int) string {
	h, ok := m.selectedHost()
	if !ok {
		return m.renderNoHost(w)
	}

	const pad = "  "
	// A column of text, not a banner: on a wide pane it keeps a readable measure.
	inner := clamp(w-4, 20, detailsMaxW)

	var b strings.Builder
	b.WriteString("\n")

	// The alias on the left, its state on the right. Host fields can arrive from an
	// untrusted SSH config or a paste, so escape sequences are stripped.
	title := titleStyle.Render(stripControl(h.Alias))
	badge := m.hostBadge(h)
	gap := max(inner-lipgloss.Width(title)-lipgloss.Width(badge), 1)
	b.WriteString(pad)
	b.WriteString(title)
	b.WriteString(strings.Repeat(" ", gap))
	b.WriteString(badge)
	b.WriteString("\n")
	b.WriteString(pad)
	b.WriteString(rule(inner))
	b.WriteString("\n\n")

	// What you connect to on the left, what you know about having connected on the right.
	port := h.Port
	if port == 0 {
		port = 22
	}
	left := [][2]string{
		{"host", fmt.Sprintf("%s:%d", stripControl(h.HostName), port)},
		{"user", stripControl(h.User)},
		{"identity", stripControl(h.IdentityFile)},
	}
	// Only a non-default route earns a row.
	if h.ProxyJump != "" {
		left = append(left, [2]string{"via", stripControl(h.ProxyJump)})
	} else if h.ProxyCommand != "" {
		left = append(left, [2]string{"via", stripControl(h.ProxyCommand)})
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

	// Saved forwards belong on the dashboard whether running or not: a green dot is a live
	// listener, a hollow one is defined and ready to start with t.
	if len(h.Forwards) > 0 {
		b.WriteString(pad)
		b.WriteString(sectionCap.Render("TUNNELS"))
		b.WriteString("\n")
		limit := min(len(h.Forwards), 4)
		for _, f := range h.Forwards[:limit] {
			dot := idleDot
			if s := m.sessions[h.Alias]; s != nil && !s.dead && s.tunnels[f.ID] != nil {
				dot = connectedDot
			}
			b.WriteString(pad)
			b.WriteString(dot)
			b.WriteString(" ")
			b.WriteString(dimStyle.Render(truncate(forwardText(f), inner-2)))
			b.WriteString("\n")
		}
		if more := len(h.Forwards) - limit; more > 0 {
			b.WriteString(pad)
			b.WriteString(faint.Render(fmt.Sprintf("  … %d more", more)))
			b.WriteString("\n")
		}
		b.WriteString("\n")
	}

	// What is open on the connection — the answer to "what am I about to close?".
	if s := m.sessions[h.Alias]; s != nil {
		if parts := s.summary(); len(parts) > 0 {
			b.WriteString(pad)
			b.WriteString(sectionCap.Render("OPEN"))
			b.WriteString("\n")
			b.WriteString(pad)
			b.WriteString(accentText.Render("▸ "))
			b.WriteString(dimStyle.Render(strings.Join(parts, faint.Render(" · "))))
			b.WriteString("\n\n")
		}
	}

	// How much of the keyboard the card spells out is the guidance profile's one say
	// here: keys leaves the card to the facts about the host, guided also lists what hop
	// itself can do from this screen. See internal/config.Guidance.
	if m.cfg.Guidance != config.GuidanceKeys {
		as := m.availableHostActions()
		// guided spells out hop's own keys beside the host's, in the same grid rather
		// than a block of their own: a second heading would be the first thing cut off
		// on a short pane, and the two columns fill more evenly as one list.
		if m.cfg.Guidance == config.GuidanceGuided {
			as = append(as, available(m, globalActions)...)
		}
		b.WriteString(pad)
		b.WriteString(sectionCap.Render("ACTIONS"))
		b.WriteString("\n")
		b.WriteString(indent(actionGrid(as, inner), pad))
		b.WriteString("\n")
	}

	b.WriteString(pad)
	b.WriteString(keyHint(menuKeyName, "this host's actions"))
	b.WriteString("  ")
	b.WriteString(keyHint("?", "every key hop knows"))

	return clampLines(b.String(), w)
}

// hostBadge is the host's state beside its name: idle, dialing, or connected.
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

// actionGrid lays actions out in two columns of keycaps, filling the left one first so
// the pairs read down the page rather than across it.
//
// It draws whatever it is handed, which is how the card stays true: the same registry
// the menu and the palette are built from (see actions.go), already narrowed to what
// this host's state allows — there is no "focus shell" on a host with none, and no
// second list to keep in step with the first.
func actionGrid(as []action, w int) string {
	if len(as) == 0 {
		return ""
	}
	rows := (len(as) + 1) / 2

	pairs := make([][2]string, 0, len(as))
	for _, a := range as {
		pairs = append(pairs, [2]string{a.keycap(), a.label})
	}

	colW := w / 2
	return lipgloss.JoinHorizontal(lipgloss.Top,
		keyColumn(pairs[:rows], colW),
		keyColumn(pairs[rows:], w-colW),
	)
}

// renderNoHost is what the right pane says with nothing under the cursor: on a fresh
// install there is no host to describe, so it describes hop.
func (m *model) renderNoHost(w int) string {
	var b strings.Builder
	b.WriteString("\n\n")
	b.WriteString("  ")
	b.WriteString(titleStyle.Render("hop"))
	b.WriteString(dimStyle.Render(" — jump between your servers"))
	b.WriteString("\n\n")
	if len(m.hosts) == 0 {
		b.WriteString("  ")
		b.WriteString(dimStyle.Render("No hosts yet. Import the ones you"))
		b.WriteString("\n")
		b.WriteString("  ")
		b.WriteString(dimStyle.Render("already have in ~/.ssh/config:"))
		b.WriteString("\n\n")
		b.WriteString("  ")
		b.WriteString(keyHint("i", "import ~/.ssh/config"))
		b.WriteString("\n\n")
		b.WriteString("  ")
		b.WriteString(faint.Render("…or "))
		b.WriteString(keyHint("a", "add one by hand"))
		b.WriteString("\n")
	} else {
		b.WriteString("  ")
		b.WriteString(dimStyle.Render("Select a host on the left."))
		b.WriteString("\n\n")
		b.WriteString("  ")
		b.WriteString(keyHint("/", "filter the list"))
		b.WriteString("\n")
		b.WriteString("  ")
		b.WriteString(keyHint("?", "every key hop knows"))
		b.WriteString("\n")
	}
	return clampLines(b.String(), w)
}

// ---- column helpers ----

// kvColumn renders label/value pairs as a column with the labels aligned, dropping a pair
// whose value is empty.
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

// keyColumn renders key/label pairs as a column of keycaps with the labels aligned. The
// keycaps are pills of different widths, so the labels are padded to a common column.
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
