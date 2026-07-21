package tui

import (
	"testing"
)

// These tests exercise the scrollback *mode* — the routing, entry guard and exit
// bookkeeping that live in package tui. The scroll math itself (offsets, clamping,
// the windowed view) belongs to *terminal.Pane and is covered in internal/terminal;
// a real pane needs a live SSH session, so newPaneModel's empty sessions map (s ==
// nil) keeps these honest at the level that does not require one.

// With scrollback armed but no live session, the ways out must clear the mode and
// never panic reaching for a pane that is not there.
func TestScrollbackExitKeysWithoutSession(t *testing.T) {
	for _, k := range []string{"esc", "q", "enter", "left"} {
		t.Run(k, func(t *testing.T) {
			m := newPaneModel()
			m.scrolling = true

			m.handleKey(key(t, k))

			if m.scrolling {
				t.Fatalf("%q did not leave scrollback mode", k)
			}
		})
	}
}

// A motion key with no session behind it is likewise inert rather than a panic: the
// handler guards the nil shell before touching the pane.
func TestScrollbackMotionKeysWithoutSession(t *testing.T) {
	for _, k := range []string{"up", "down", "k", "j", "pgup", "pgdown", "g", "G"} {
		t.Run(k, func(t *testing.T) {
			m := newPaneModel()
			m.scrolling = true

			// Must not panic; with no shell the handler clears the mode and returns.
			m.handleKey(key(t, k))

			if m.scrolling {
				t.Fatalf("%q left scrollback armed with no session behind it", k)
			}
		})
	}
}

// The entry chord on a model with no shell must not enter scrollback: enterScrollback
// declines (s.shell() == nil), and the key falls through to the shell path, which is
// itself a no-op with no session. Either way the mode stays off and nothing panics.
func TestScrollbackEntryChordWithoutShell(t *testing.T) {
	m := newPaneModel()

	m.handleKey(key(t, "shift+up"))

	if m.scrolling {
		t.Fatal("shift+up entered scrollback with no shell to scroll")
	}
}

// leavePane and leaveAll both drop scrollback mode along with focus, so a pane left
// while scrolled back does not come back armed.
func TestLeaveClearsScrolling(t *testing.T) {
	t.Run("leavePane", func(t *testing.T) {
		m := newPaneModel()
		m.scrolling = true
		m.leavePane()
		if m.scrolling {
			t.Fatal("leavePane left scrollback armed")
		}
	})
	t.Run("leaveAll", func(t *testing.T) {
		m := newPaneModel()
		m.scrolling = true
		m.leaveAll()
		if m.scrolling {
			t.Fatal("leaveAll left scrollback armed")
		}
	})
}

// While scrolling, handleKey routes to the scrollback handler rather than the shell:
// an esc leaves scrollback outright instead of arming the double-esc chord the shell
// pane uses, so lastEsc must stay zero.
func TestScrollbackRoutesAwayFromShell(t *testing.T) {
	m := newPaneModel()
	m.scrolling = true

	m.handleKey(key(t, "esc"))

	if m.scrolling {
		t.Fatal("esc did not leave scrollback mode")
	}
	if !m.lastEsc.IsZero() {
		t.Fatal("esc armed the double-esc chord; it should have been the scrollback handler's exit")
	}
}
