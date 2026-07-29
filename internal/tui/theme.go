package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"hop/internal/config"
	"hop/internal/filebrowser"
)

// accent is the primary highlight color, and the only one the user chooses. The
// rest of the palette is a fixed neutral ramp, so it has to stay legible beside
// any accent the settings popover can produce.
var accent = lipgloss.Color(config.DefaultAccent)

const (
	colInk    = lipgloss.Color("16")  // text on an accent fill
	colBright = lipgloss.Color("231") // text on a raised fill
	colText   = lipgloss.Color("252")
	colDim    = lipgloss.Color("245")
	colFaint  = lipgloss.Color("240")

	colSurface = lipgloss.Color("236") // the fill under the row you are standing in
	colRaised  = lipgloss.Color("238") // keycaps, chips, text inputs

	colGreen  = lipgloss.Color("42")
	colYellow = lipgloss.Color("214")
	colRed    = lipgloss.Color("203")
)

// ---- styles ----
//
// The accent-derived ones are values rather than lazy lookups, so setAccent has
// to rebuild them — which is also what lets a color picked in the popover apply
// without a restart.

var (
	// Chrome.
	headerBadge = lipgloss.NewStyle().Bold(true).Foreground(colInk).Background(accent).Padding(0, 1)
	subtitle    = lipgloss.NewStyle().Foreground(colDim)
	footerStyle = lipgloss.NewStyle().Foreground(colDim).Padding(0, 1)

	// Panes.
	paneBorder = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(colFaint)

	paneBorderActive = lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				BorderForeground(accent)

	// Text.
	titleStyle = lipgloss.NewStyle().Bold(true).Foreground(accent)
	accentText = lipgloss.NewStyle().Foreground(accent)
	aliasStyle = lipgloss.NewStyle().Bold(true).Foreground(colText)
	dimStyle   = lipgloss.NewStyle().Foreground(colDim)
	faint      = lipgloss.NewStyle().Foreground(colFaint)
	sectionCap = lipgloss.NewStyle().Bold(true).Foreground(colDim)
	kvKey      = lipgloss.NewStyle().Foreground(colFaint)

	greenText  = lipgloss.NewStyle().Foreground(colGreen)
	yellowText = lipgloss.NewStyle().Foreground(colYellow)
	redText    = lipgloss.NewStyle().Foreground(colRed)

	// The host under the cursor: a bright bold alias behind an accent bar. Not a
	// full-width background block — that nests badly with the styles inside a row.
	selectedAliasStyle = lipgloss.NewStyle().Bold(true).Foreground(accent)
	selBar             = accentText.Render("▎")

	// A matched character while filtering, so a fuzzy hit shows *why* it matched.
	matchStyle = lipgloss.NewStyle().Bold(true).Underline(true).Foreground(accent)

	// Pills.
	chipStyle   = lipgloss.NewStyle().Bold(true).Foreground(accent).Background(colRaised).Padding(0, 1)
	keycapStyle = lipgloss.NewStyle().Foreground(colBright).Background(colRaised).Padding(0, 1)

	// Tabs: the open one is a filled pill, the rest are quiet text.
	tabActive   = lipgloss.NewStyle().Bold(true).Foreground(colInk).Background(accent).Padding(0, 1)
	tabInactive = lipgloss.NewStyle().Foreground(colDim).Background(colSurface).Padding(0, 1)

	// Status dots. The dead one is a filled dot like the connected one, in red: it
	// is a host hop is still holding something for, unlike an idle one, and the
	// shape says so before the color does.
	connectedDot = greenText.Render("●")
	idleDot      = faint.Render("○")
	deadDot      = redText.Render("●")

	// Modal cards.
	cardBox = lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(accent).
		Padding(1, cardPadX)

	// The settings card: a quiet label above each value, the selected value filled
	// so it reads as a field you are standing in, and the one being edited filled
	// brighter still with a caret in it.
	settingsLabel    = lipgloss.NewStyle().Foreground(lipgloss.Color("243"))
	settingsLabelSel = lipgloss.NewStyle().Bold(true).Foreground(accent)

	// The unselected values carry the same padding as the filled one, so the text
	// does not jog sideways by a cell as the cursor moves down the card.
	settingsValue       = lipgloss.NewStyle().Foreground(colText).Padding(0, 1)
	settingsPlaceholder = lipgloss.NewStyle().Italic(true).Foreground(colFaint).Padding(0, 1)

	settingsValueSel = lipgloss.NewStyle().
				Foreground(colBright).
				Background(colSurface).
				Padding(0, 1)

	inputStyle = lipgloss.NewStyle().
			Foreground(colBright).
			Background(colRaised).
			Padding(0, 1)
)

// cardPadX is the horizontal padding inside a modal card's border.
const cardPadX = 3

// setAccent re-points the palette at a new highlight color, here and in the file
// browser, which draws its own rows.
func setAccent(color string) {
	if color == "" {
		color = config.DefaultAccent
	}
	accent = lipgloss.Color(color)

	headerBadge = headerBadge.Background(accent)
	paneBorderActive = paneBorderActive.BorderForeground(accent)
	titleStyle = titleStyle.Foreground(accent)
	accentText = accentText.Foreground(accent)
	selectedAliasStyle = selectedAliasStyle.Foreground(accent)
	selBar = accentText.Render("▎")
	matchStyle = matchStyle.Foreground(accent)
	chipStyle = chipStyle.Foreground(accent)
	tabActive = tabActive.Background(accent)
	cardBox = cardBox.BorderForeground(accent)
	settingsLabelSel = settingsLabelSel.Foreground(accent)

	filebrowser.SetAccent(color)
}

// ---- spinner ----

// spinnerFrames is the braille cycle shown beside a host hop is dialing. It runs
// only while a connect is in flight — nothing animates on an idle screen.
var spinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

// spinner returns the frame for tick n, in the "working on it" yellow the
// connecting host wears elsewhere.
func spinner(n int) string {
	return yellowText.Render(spinnerFrames[n%len(spinnerFrames)])
}

// ---- text helpers ----

// kc renders a keycap "pill" for legends and help bars.
func kc(key string) string { return keycapStyle.Render(key) }

// keyHint is the keycap-plus-label pair the footer and the help card are built
// out of.
func keyHint(key, label string) string { return kc(key) + " " + dimStyle.Render(label) }

// stripControl removes control characters (C0, DEL and C1) from s, so a string
// that originated on a remote host — a file name, an error text — cannot carry
// an escape sequence into the user's terminal.
func stripControl(s string) string {
	return strings.Map(func(r rune) rune {
		if r < 0x20 || (r >= 0x7f && r < 0xa0) {
			return -1
		}
		return r
	}, s)
}

// truncate shortens s (measured by display width) to at most w cells, adding an
// ellipsis when it must cut.
//
// It is style-aware: a naive rune-by-rune cut can land in the middle of an ANSI
// escape, which then leaks into the terminal as garbage (and drops the color it
// was opening). ansi.Truncate cuts on cell boundaries and keeps the escapes
// balanced.
func truncate(s string, w int) string {
	if w <= 0 {
		return ""
	}
	if lipgloss.Width(s) <= w {
		return s
	}
	return ansi.Truncate(s, w, "…")
}

// clampLines truncates every line to w cells so styled content can never wrap and
// break out of its bordered box.
func clampLines(s string, w int) string {
	lines := strings.Split(s, "\n")
	for i, ln := range lines {
		lines[i] = truncate(ln, w)
	}
	return strings.Join(lines, "\n")
}

// fitLines cuts s to at most h lines. lipgloss's Height is a floor rather than a
// ceiling — a box whose content is taller simply grows — so anything rendered
// into a fixed-height pane has to be cut to it here, or the screen ends up taller
// than the window and the terminal scrolls.
func fitLines(s string, h int) string {
	if h < 1 {
		return ""
	}
	lines := strings.Split(s, "\n")
	if len(lines) <= h {
		return s
	}
	return strings.Join(lines[:h], "\n")
}

// padTo right-pads s with spaces to exactly w display cells (truncating when it
// is already wider), so styled cells can be laid out in columns without lipgloss
// having to measure them again.
func padTo(s string, w int) string {
	s = truncate(s, w)
	if gap := w - lipgloss.Width(s); gap > 0 {
		s += strings.Repeat(" ", gap)
	}
	return s
}

// rule draws a horizontal divider w cells wide.
func rule(w int) string {
	if w < 1 {
		return ""
	}
	return faint.Render(strings.Repeat("─", w))
}

// plural picks the singular or plural word for n.
func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}

// relTime renders a unix timestamp as an age ("3m ago"), because when you last
// used a host is the useful fact, not the wall-clock time you used it at. A zero
// timestamp is a host that has never been connected to.
func relTime(unix int64) string {
	if unix <= 0 {
		return "never"
	}
	d := time.Since(time.Unix(unix, 0))
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	case d < 30*24*time.Hour:
		return fmt.Sprintf("%dd ago", int(d.Hours()/24))
	default:
		return time.Unix(unix, 0).Format("2 Jan 2006")
	}
}
