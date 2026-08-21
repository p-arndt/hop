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

// accent is the primary highlight color, the only one the user chooses.
var accent = lipgloss.Color(config.DefaultAccent)

const (
	colInk    = lipgloss.Color("16")  // text on an accent fill
	colBright = lipgloss.Color("231") // text on a raised fill
	colText   = lipgloss.Color("252")
	colDim    = lipgloss.Color("245")
	colFaint  = lipgloss.Color("240")

	colSurface = lipgloss.Color("236")
	colRaised  = lipgloss.Color("238")

	colGreen  = lipgloss.Color("42")
	colYellow = lipgloss.Color("214")
	colRed    = lipgloss.Color("203")
)

// ---- styles ----
//
// Accent-derived styles are values, not lazy lookups, so setAccent must rebuild each one.

var (
	// Chrome.
	headerBadge = lipgloss.NewStyle().Bold(true).Foreground(colInk).Background(accent).Padding(0, 1)
	subtitle    = lipgloss.NewStyle().Foreground(colDim)
	footerStyle = lipgloss.NewStyle().Foreground(colDim).Padding(0, 1)
	statusBar   = lipgloss.NewStyle().Foreground(colDim).Background(colSurface).Padding(0, 1)

	// Panes.
	paneBorder = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(colFaint)

	paneBorderActive = lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				BorderForeground(accent)

	// An unfocused column: border and unstyled body both dim.
	paneBorderIdle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(colFaint).
			Foreground(colFaint)

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

	// A bar rather than a full-width fill, which nests badly with the styles inside a row.
	selectedAliasStyle = lipgloss.NewStyle().Bold(true).Foreground(accent)
	selBar             = accentText.Render("▎")

	matchStyle = lipgloss.NewStyle().Bold(true).Underline(true).Foreground(accent)

	// Pills.
	chipStyle   = lipgloss.NewStyle().Bold(true).Foreground(accent).Background(colRaised).Padding(0, 1)
	keycapStyle = lipgloss.NewStyle().Foreground(colBright).Background(colRaised).Padding(0, 1)

	// Tabs.
	tabActive   = lipgloss.NewStyle().Bold(true).Foreground(colInk).Background(accent).Padding(0, 1)
	tabInactive = lipgloss.NewStyle().Foreground(colDim).Background(colSurface).Padding(0, 1)

	// Status dots.
	connectedDot = greenText.Render("●")
	idleDot      = faint.Render("○")
	deadDot      = redText.Render("●")

	// Modal cards.
	cardBox = lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(accent).
		Padding(1, cardPadX)

	// The context menu.
	menuBox = lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(accent).
		Padding(0, 1)

	// The settings card.
	settingsLabel    = lipgloss.NewStyle().Foreground(lipgloss.Color("243"))
	settingsLabelSel = lipgloss.NewStyle().Bold(true).Foreground(accent)

	// Same padding as the filled variants, so text does not jog sideways as the cursor moves.
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

// setAccent re-points the palette at a new highlight color, here and in the file browser.
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
	menuBox = menuBox.BorderForeground(accent)
	settingsLabelSel = settingsLabelSel.Foreground(accent)

	filebrowser.SetAccent(color)
}

// ---- spinner ----

// spinnerFrames is the braille cycle shown beside a host hop is dialing.
var spinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

// spinner returns the frame for tick n.
func spinner(n int) string {
	return yellowText.Render(spinnerFrames[n%len(spinnerFrames)])
}

// ---- text helpers ----

// kc renders a keycap "pill" for legends and help bars.
func kc(key string) string { return keycapStyle.Render(key) }

// keyHint is the keycap-plus-label pair the footer and help card are built out of.
func keyHint(key, label string) string { return kc(key) + " " + dimStyle.Render(label) }

// stripControl removes C0, DEL and C1 so remote text cannot carry an escape into the terminal.
func stripControl(s string) string {
	return strings.Map(func(r rune) rune {
		if r < 0x20 || (r >= 0x7f && r < 0xa0) {
			return -1
		}
		return r
	}, s)
}

// truncate shortens s to at most w display cells, cutting on cell boundaries so an ANSI
// escape is never split.
func truncate(s string, w int) string {
	if w <= 0 {
		return ""
	}
	if lipgloss.Width(s) <= w {
		return s
	}
	return ansi.Truncate(s, w, "…")
}

// clampLines truncates every line to w cells so content cannot wrap out of its box.
func clampLines(s string, w int) string {
	lines := strings.Split(s, "\n")
	for i, ln := range lines {
		lines[i] = truncate(ln, w)
	}
	return strings.Join(lines, "\n")
}

// fitLines cuts s to at most h lines; lipgloss's Height is a floor, not a ceiling.
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

// padTo right-pads s to exactly w display cells, truncating when it is already wider.
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

// relTime renders a unix timestamp as an age ("3m ago"); zero means never.
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
