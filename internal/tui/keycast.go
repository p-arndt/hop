//go:build hopdemo

// Keycast: the on-screen trail of keys that appears in hop's recorded demo. Every build
// without the `hopdemo` tag gets the no-ops in keycast_off.go instead.

package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

const keycastLen = 5

// keycastState is the trail itself; an empty struct in the no-op build.
type keycastState []keycastEvent

// keycastEvent is one entry: a named key, or a run of typed text grouped into one pill.
type keycastEvent struct {
	label string
	typed bool // a run of printable runes, still open for more
}

// keycastRecord notes a keypress: runes extend an open typing run, anything else is its own pill.
func (m *model) keycastRecord(key string) {
	if key == "" {
		return
	}

	if len([]rune(key)) == 1 {
		if n := len(m.keycast); n > 0 && m.keycast[n-1].typed {
			m.keycast[n-1].label += key
			return
		}
		m.keycast = append(m.keycast, keycastEvent{label: key, typed: true})
	} else {
		m.keycast = append(m.keycast, keycastEvent{label: key})
	}

	if len(m.keycast) > keycastLen {
		m.keycast = m.keycast[len(m.keycast)-keycastLen:]
	}
}

func (m *model) keycastDraw(screen string) string {
	if len(m.keycast) == 0 || m.width < 30 {
		return screen
	}

	pill := lipgloss.NewStyle().
		Background(lipgloss.Color("236")).
		Foreground(lipgloss.Color("252")).
		Padding(0, 1)
	newest := lipgloss.NewStyle().
		Background(accent).
		Foreground(lipgloss.Color("232")).
		Bold(true).
		Padding(0, 1)

	var pills []string
	for i, ev := range m.keycast {
		label := ev.label
		// A long typing run is shown by its tail, which is what is being typed now.
		if r := []rune(label); len(r) > 24 {
			label = "…" + string(r[len(r)-23:])
		}
		style := pill
		if i == len(m.keycast)-1 {
			style = newest
		}
		pills = append(pills, style.Render(label))
	}

	strip := strings.Join(pills, " ")
	if w := lipgloss.Width(strip); w > m.width-4 {
		// Drop from the left until it fits: the newest key must stay.
		for len(pills) > 1 && lipgloss.Width(strings.Join(pills, " ")) > m.width-4 {
			pills = pills[1:]
		}
		strip = strings.Join(pills, " ")
	}

	x := m.width - lipgloss.Width(strip) - 2
	y := m.height - 2 // the row above the footer
	return overlay(screen, strip, x, y)
}
