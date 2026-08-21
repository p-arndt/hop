package tui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"hop/internal/config"
)

// guidanceUI is the first-run question's state. It is asked once, on an install with no
// config file at all (config.Exists), and never again.
type guidanceUI struct {
	open   bool
	cursor int
}

// openGuidance asks the question, standing on the profile in force.
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

// answerGuidance saves the profile under the cursor and hands the first run on to the
// import card. It saves even the default: the file existing is what records the answer.
func (m *model) answerGuidance() {
	m.cfg.Guidance = guidanceChoices[m.guidance.cursor].value
	m.guidance = guidanceUI{}
	// Saved quietly; a failure is still worth saying, since the question would come back.
	if err := m.cfg.Save(); err != nil {
		m.setStatus(statusErr, "settings: %v", err)
	}

	// Same condition as Run's: no hosts, and a config hop can read.
	if len(m.hosts) == 0 && haveSSHConfig() {
		m.openImport(true)
	}
}

// handleGuidanceKey routes a key while the question is up; anything but a motion answers it.
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

// Card geometry.
const (
	guidanceMaxW   = 62
	guidanceFloorW = 24
)

func (m *model) guidanceInnerW() int {
	room := max(m.width-2*cardPadX-2, guidanceFloorW)
	return clamp(guidanceMaxW, guidanceFloorW, room)
}

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
