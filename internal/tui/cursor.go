package tui

import (
	tea "charm.land/bubbletea/v2"

	"hop/internal/terminal"
)

// A pane honours the remote program's cursor shape and hidden state on its own
// (internal/terminal/cursor.go); only the blink needs a clock here. It phases every pane at
// once, so switching tabs never leaves a cursor stuck mid-blink.

// applyCursorBlink starts or stops the blink clock. It is the only place a chain is
// started, so the setting cannot end up with two.
func (m *model) applyCursorBlink() tea.Cmd {
	if !m.cfg.CursorBlink {
		// The chain notices on its next frame; the cursors come back up now.
		m.phaseCursors(true)
		return nil
	}
	if m.blinking {
		return nil
	}
	m.blinking = true
	m.blinkGen++
	m.cursorUp = true
	return cursorBlinkCmd(m.blinkGen)
}

// cursorBlinkTick is one frame. A frame from a replaced chain is dropped.
func (m *model) cursorBlinkTick(gen int) tea.Cmd {
	if gen != m.blinkGen {
		return nil
	}
	if !m.cfg.CursorBlink {
		m.blinking = false
		m.phaseCursors(true)
		return nil
	}
	m.cursorUp = !m.cursorUp
	m.phaseCursors(m.cursorUp)
	return cursorBlinkCmd(gen)
}

func (m *model) phaseCursors(up bool) {
	m.eachPane(func(p *terminal.Pane) { p.SetCursorPhase(up) })
}

// eachPane visits every pane hop has open; a dead session still draws its last screen.
func (m *model) eachPane(fn func(*terminal.Pane)) {
	for _, s := range m.sessions {
		for _, sh := range s.shells {
			if sh.pane != nil {
				fn(sh.pane)
			}
		}
		for _, e := range s.editors {
			if e.pane != nil {
				fn(e.pane)
			}
		}
	}
}
