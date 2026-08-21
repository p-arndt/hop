package tui

import (
	"testing"
)

// These tests cover scrollback mode routing and guards; the scroll math lives in internal/terminal.

// With no live session the ways out must clear the mode and never panic.
func TestScrollbackExitKeysWithoutSession(t *testing.T) {
	for _, k := range []string{"esc", "q", "enter", "left"} {
		t.Run(k, func(t *testing.T) {
			m := newPaneModel()
			m.mode = modeScrollback

			m.handleKey(key(t, k))

			if m.scrolling() {
				t.Fatalf("%q did not leave scrollback mode", k)
			}
		})
	}
}

func TestScrollbackMotionKeysWithoutSession(t *testing.T) {
	for _, k := range []string{"up", "down", "k", "j", "pgup", "pgdown", "g", "G"} {
		t.Run(k, func(t *testing.T) {
			m := newPaneModel()
			m.mode = modeScrollback

			m.handleKey(key(t, k))

			if m.scrolling() {
				t.Fatalf("%q left scrollback armed with no session behind it", k)
			}
		})
	}
}

// With no shell, enterScrollback declines and the key falls through; the mode stays off.
func TestScrollbackEntryChordWithoutShell(t *testing.T) {
	m := newPaneModel()

	m.handleKey(key(t, "shift+up"))

	if m.scrolling() {
		t.Fatal("shift+up entered scrollback with no shell to scroll")
	}
}

// leavePane and leaveAll drop scrollback mode along with focus.
func TestLeaveClearsScrolling(t *testing.T) {
	t.Run("leavePane", func(t *testing.T) {
		m := newPaneModel()
		m.mode = modeScrollback
		m.leavePane()
		if m.scrolling() {
			t.Fatal("leavePane left scrollback armed")
		}
	})
	t.Run("leaveAll", func(t *testing.T) {
		m := newPaneModel()
		m.mode = modeScrollback
		m.leaveAll()
		if m.scrolling() {
			t.Fatal("leaveAll left scrollback armed")
		}
	})
}

// esc leaves scrollback outright instead of arming the shell's double-esc chord.
func TestScrollbackRoutesAwayFromShell(t *testing.T) {
	m := newPaneModel()
	m.mode = modeScrollback

	m.handleKey(key(t, "esc"))

	if m.scrolling() {
		t.Fatal("esc did not leave scrollback mode")
	}
	if m.reader.Pending() {
		t.Fatal("esc armed the double-esc chord; it should have been the scrollback handler's exit")
	}
}
