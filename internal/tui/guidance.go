package tui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"hop/internal/config"
)

// guidanceUI is the first-run question's state: which profile the cursor is on.
//
// It is asked once, on an install with no config file at all (see config.Exists), and
// never again — a setting added later must not re-open a question for someone who has
// been using hop for months. Everything it decides is reachable afterwards from the
// settings popover, and it decides nothing about what the keys do.
type guidanceUI struct {
	open   bool
	cursor int
}

// openGuidance asks the question, standing on the profile in force — which on a first
// run is the default, hybrid.
func (m *model) openGuidance() {
	m.guidance = guidanceUI{open: true, cursor: guidanceIndex(m.cfg.Guidance)}
}

// guidanceIndex is where a stored profile sits in the list, defaulting to hybrid.
func guidanceIndex(value string) int {
	for i, c := range guidanceChoices {
		if c.value == value {
			return i
		}
	}
	return guidanceIndex(config.GuidanceHybrid)
}

// answerGuidance takes the profile under the cursor, saves it, and hands the first run
// on to the import card when there is a config to read hosts from.
//
// It saves even when the answer is the default, and that is the point: the file existing
// is what says the question was asked. An escape is an answer too — the middle profile,
// which is what an unanswered hop would have done anyway.
func (m *model) answerGuidance() {
	m.cfg.Guidance = guidanceChoices[m.guidance.cursor].value
	m.guidance = guidanceUI{}
	// Saved quietly: "settings saved" is a reply to a settings card, and this one is the
	// first thing hop ever showed. A failure is still worth saying, since the question
	// would otherwise come back on the next start.
	if err := m.cfg.Save(); err != nil {
		m.setStatus(statusErr, "settings: %v", err)
	}

	// The one card hop opens by itself, now that this one is out of its way. Same
	// condition as Run's: hosts hop has not got, in a config it can read.
	if len(m.hosts) == 0 && haveSSHConfig() {
		m.openImport(true)
	}
}

// handleGuidanceKey routes a key while the question is up. Every key that is not a
// motion answers it: this is a fork in a first run, and there is nothing behind it yet
// to leak a keystroke into.
func (m *model) handleGuidanceKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "up", "left", "k", "h":
		m.guidance.cursor = clamp(m.guidance.cursor-1, 0, len(guidanceChoices)-1)
	case "down", "right", "j", "l", "tab":
		m.guidance.cursor = clamp(m.guidance.cursor+1, 0, len(guidanceChoices)-1)
	case "1", "2", "3":
		m.guidance.cursor = int(msg.String()[0] - '1')
		m.answerGuidance()
	case "enter", "esc", "q":
		m.answerGuidance()
	}
	return m, nil
}

// Card geometry: wide enough for a profile's line to stand on one row, since the three
// are read against each other.
const (
	guidanceMaxW   = 62
	guidanceFloorW = 24
)

func (m *model) guidanceInnerW() int {
	room := max(m.width-2*cardPadX-2, guidanceFloorW)
	return clamp(guidanceMaxW, guidanceFloorW, room)
}

// renderGuidance draws the question: the three profiles with what each one shows, and
// the promise that none of them takes a key away.
func (m *model) renderGuidance() string {
	w := m.guidanceInnerW()
	var b strings.Builder

	b.WriteString(truncate(titleStyle.Render("WELCOME TO hop"), w))
	b.WriteString("\n\n")
	b.WriteString(truncate(dimStyle.Render("How much of the keyboard should hop keep on screen?"), w))
	b.WriteString("\n\n")

	for i, c := range guidanceChoices {
		lead, name := "  ", dimStyle.Render(c.value)
		if i == m.guidance.cursor {
			lead, name = selBar+" ", selectedAliasStyle.Render(c.value)
		}
		b.WriteString(padTo(lead+padTo(name, 9)+faint.Render(c.desc), w))
		b.WriteString("\n")
	}

	b.WriteString("\n")
	b.WriteString(truncate(faint.Render("Every key works in all three — this is what hop shows, not what it does."), w))
	b.WriteString("\n")
	b.WriteString(truncate(faint.Render("Change it any time with , → Guidance."), w))
	b.WriteString("\n\n")
	b.WriteString(truncate(keyHint("↑↓", "pick")+"  "+keyHint("enter", "start hopping"), w))

	return cardBox.Width(w + 2*cardPadX).Render(b.String())
}
