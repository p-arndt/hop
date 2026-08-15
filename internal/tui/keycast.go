//go:build hopdemo

// Keycast: the on-screen trail of keys that appears in hop's recorded demo.
//
// It is behind the `hopdemo` build tag, so a released binary carries none of it; every
// other build gets the no-ops in keycast_off.go.
//
// Doing this inside hop rather than as a pass over the GIF is what makes it exact: the
// pill appears on the frame the key was handled on, drawn by the View that handled it.

package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// keycastLen is how many recent events stay on screen: enough to read the chord you just
// pressed and the one before it, not so many that the strip becomes a transcript.
const keycastLen = 5

// keycastState is the trail itself, a field on the model in both builds: a named slice
// here, an empty struct in the no-op build.
type keycastState []keycastEvent

// keycastEvent is one entry in the trail: a named key, or a run of typed text grouped so
// typing a command is one pill rather than twenty.
type keycastEvent struct {
	label string
	typed bool // a run of printable runes, still open for more
}

// keycastRecord notes a keypress for the overlay: runes extend an open typing run,
// anything else becomes its own pill.
func (m *model) keycastRecord(key string) {
	if key == "" {
		return
	}

	// A printable character extends or starts a typing run, the space included.
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

// keycastDraw composites the key trail over the finished screen, bottom-right above the
// footer, using the same overlay splice the modal cards use.
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
