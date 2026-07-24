//go:build hopdemo

// Keycast: the on-screen trail of keys that appears in hop's recorded demo.
//
// It is behind the `hopdemo` build tag, so it is not in a released binary at all —
// no flag to check, no code to carry. `just demo` builds with `-tags hopdemo`; every
// other build gets the no-ops in keycast_off.go.
//
// Doing this inside hop rather than as a post-processing pass over the GIF is what
// makes it exact: the pill appears on the frame the key was handled on, because it
// is drawn by the same View that handled it.

package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// keycastLen is how many recent events stay on screen. Enough to read the chord
// you just pressed and the one before it, not so many that the strip becomes a
// transcript.
const keycastLen = 5

// keycastState is the trail itself, a field on the model in both builds — a named
// slice here, an empty struct in the no-op build.
type keycastState []keycastEvent

// keycastEvent is one entry in the trail: either a named key ("ctrl+o", "enter") or
// a run of typed text, which is grouped so typing a command is one pill rather than
// twenty.
type keycastEvent struct {
	label string
	typed bool // a run of printable runes, still open for more
}

// keycastRecord notes a keypress for the overlay. Runes are appended to an open
// typing run; anything else becomes its own pill.
func (m *model) keycastRecord(key string) {
	if key == "" {
		return
	}

	// A single printable character extends (or starts) a typing run — the space
	// included, so "systemctl status caddy" is one pill rather than five.
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

// keycastDraw composites the key trail over the finished screen, bottom-right,
// just above the footer. It uses the same overlay splice the modal cards use, so it
// floats over a terminal pane without disturbing what the pane drew.
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
		// A long typing run is shown by its tail: what is being typed right now is
		// the part worth seeing.
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
		// Drop from the left until it fits: the newest key is the one that must stay.
		for len(pills) > 1 && lipgloss.Width(strings.Join(pills, " ")) > m.width-4 {
			pills = pills[1:]
		}
		strip = strings.Join(pills, " ")
	}

	x := m.width - lipgloss.Width(strip) - 2
	y := m.height - 2 // the row above the footer
	return overlay(screen, strip, x, y)
}
