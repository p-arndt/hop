package tui

import (
	"strings"
	"testing"

	"hop/internal/config"
	"hop/internal/terminal"
)

// blinkModel is a model with two panes on one host and one on another, so a phase change
// has to reach every pane and not only the focused one.
func blinkModel(t *testing.T) (*model, []*terminal.Pane) {
	t.Helper()
	shell, editor, other := fakePane(), fakePane(), fakePane()
	m := &model{
		sessions: map[string]*session{
			"web": {
				shells:  []*shellTab{{id: 1, pane: shell}},
				editors: []*editorTab{{id: 1, name: "hosts", path: "/etc/hosts", pane: editor}},
			},
			"db": {shells: []*shellTab{{id: 1, pane: other}}},
		},
		notify: make(chan struct{}, 1),
		cfg:    config.Default(),
		width:  100,
		height: 30,
	}
	return m, []*terminal.Pane{shell, editor, other}
}

// drawsCursor reports whether a pane is currently painting its cursor.
func drawsCursor(p *terminal.Pane) bool { return strings.Contains(p.View(), "\x1b[7m") }

// Blinking is off unless asked for: hop honours the shape and the hidden state without a
// setting, but the blink is a clock it has to run itself.
func TestCursorBlinkOffByDefault(t *testing.T) {
	if config.Default().CursorBlink {
		t.Fatal("the cursor blinks out of the box; it costs a repaint twice a second")
	}

	m, panes := blinkModel(t)
	if cmd := m.applyCursorBlink(); cmd != nil {
		t.Error("the blink clock started with the setting off")
	}
	if m.blinking {
		t.Error("the model thinks it is blinking with the setting off")
	}
	for i, p := range panes {
		if !drawsCursor(p) {
			t.Errorf("pane %d has no cursor with blinking off", i)
		}
	}
}

// A frame takes every cursor down together and the next brings them back, so switching
// tabs never lands on a pane left mid-blink.
func TestCursorBlinkPhasesEveryPane(t *testing.T) {
	m, panes := blinkModel(t)
	m.cfg.CursorBlink = true

	if cmd := m.applyCursorBlink(); cmd == nil {
		t.Fatal("the blink clock did not start with the setting on")
	}
	gen := m.blinkGen

	if cmd := m.cursorBlinkTick(gen); cmd == nil {
		t.Fatal("a blink frame did not arm the next one")
	}
	for i, p := range panes {
		if drawsCursor(p) {
			t.Errorf("pane %d still drew its cursor on a down frame", i)
		}
	}

	m.cursorBlinkTick(gen)
	for i, p := range panes {
		if !drawsCursor(p) {
			t.Errorf("pane %d did not come back up", i)
		}
	}
}

// Switching the setting off ends the chain and leaves every cursor up: a clock that has
// stopped must not leave one down forever.
func TestCursorBlinkStops(t *testing.T) {
	m, panes := blinkModel(t)
	m.cfg.CursorBlink = true
	m.applyCursorBlink()
	gen := m.blinkGen
	m.cursorBlinkTick(gen) // down

	m.cfg.CursorBlink = false
	if cmd := m.applyCursorBlink(); cmd != nil {
		t.Error("switching the setting off started a clock")
	}
	for i, p := range panes {
		if !drawsCursor(p) {
			t.Errorf("pane %d was left down by a stopped clock", i)
		}
	}

	// The chain notices on its next frame and ends there.
	if cmd := m.cursorBlinkTick(gen); cmd != nil {
		t.Error("a frame arriving after the setting went off armed another")
	}
	if m.blinking {
		t.Error("the model still thinks it is blinking")
	}
}

// One clock, however often the setting is walked: a second chain would blink at half a
// beat apart from the first.
func TestCursorBlinkOneClock(t *testing.T) {
	m, _ := blinkModel(t)
	m.cfg.CursorBlink = true
	m.applyCursorBlink()
	gen := m.blinkGen

	if cmd := m.applyCursorBlink(); cmd != nil {
		t.Error("a second start armed a second clock")
	}
	if m.blinkGen != gen {
		t.Error("a second start renumbered the running chain")
	}

	// A frame from a chain that has been replaced is dropped rather than re-armed.
	if cmd := m.cursorBlinkTick(gen - 1); cmd != nil {
		t.Error("a stale frame armed another")
	}
}

// The card carries the setting, and flipping it there starts the clock — the settings
// path applies it like any other.
func TestSettingsCursorBlinkToggle(t *testing.T) {
	m := settingsModel(t)
	m.settings.cursor = fieldIndex(t, "Cursor blink")

	m.handleKey(key(t, "enter"))
	if !m.cfg.CursorBlink {
		t.Fatal("enter did not switch the blink on")
	}
	if !m.blinking {
		t.Error("switching the setting on did not start the clock")
	}

	m.handleKey(key(t, "enter"))
	if m.cfg.CursorBlink {
		t.Error("enter did not switch the blink back off")
	}
}
