package tui

import (
	tea "github.com/charmbracelet/bubbletea"

	"hop/internal/terminal"
)

// The cursor's shape and its hidden state belong to the remote program, and a pane
// honours them without being asked (internal/terminal/cursor.go). The blink does not:
// a drawn cursor cannot blink by itself, so hop has to run a clock and repaint, which
// is the one thing here the settings card decides. Off, every cursor stands still.
//
// The clock phases every pane at once rather than the focused one, so switching tabs
// or dropping back to the host list never leaves a cursor stuck mid-blink.

// applyCursorBlink starts or stops the blink clock to match the setting. It is the only
// place a chain is started, so the setting cannot end up with two.
func (m *model) applyCursorBlink() tea.Cmd {
	if !m.cfg.CursorBlink {
		// The chain notices on its next frame; the cursors come back up now, so nothing
		// is left down by a clock that has stopped.
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

// cursorBlinkTick is one frame: flip the phase, phase the panes, arm the next. A frame
// from a chain that has been replaced is dropped, and the setting going off ends it.
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

// phaseCursors puts every live pane on the same blink frame. A pane whose cursor is
// hidden or steady ignores it.
func (m *model) phaseCursors(up bool) {
	m.eachPane(func(p *terminal.Pane) { p.SetCursorPhase(up) })
}

// eachPane visits every pane hop has open: the shells and the editors of every session,
// live or dead — a dead session still draws its last screen.
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
