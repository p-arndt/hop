package tui

import (
	"fmt"
	"io"
	"strings"
	"testing"
	"time"

	"hop/internal/filebrowser"
	"hop/internal/sshx"
	"hop/internal/terminal"
)

// scrolledShell is a model on host "web" whose shell pane has real history behind it, so
// enterScrollback runs through its own guard. The emulator is fed on its own goroutine,
// so the lines have to be waited for.
func scrolledShell(t *testing.T) (*model, *session) {
	t.Helper()
	var out strings.Builder
	for i := 0; i < 40; i++ {
		fmt.Fprintf(&out, "line %d\r\n", i)
	}
	p := terminal.New(&sshx.Session{
		Stdin:  nopWriteCloser{io.Discard},
		Stdout: strings.NewReader(out.String()),
	}, 20, 5, nil)

	m, s := shellModel(t, 1)
	s.shells[0].pane.Close()
	s.shells[0].pane = p

	deadline := time.Now().Add(2 * time.Second)
	for p.ScrollbackLen() == 0 {
		if time.Now().After(deadline) {
			t.Fatal("the pane never accumulated any scrollback")
		}
		time.Sleep(5 * time.Millisecond)
	}
	return m, s
}

// modeNames is what a paneMode is called in a failure message. A test that says
// "mode = modeBrowser, want modeShell" is worth more than one that says "3, want 1".
var modeNames = map[paneMode]string{
	modeList:       "modeList",
	modeShell:      "modeShell",
	modeScrollback: "modeScrollback",
	modeBrowser:    "modeBrowser",
	modeEditor:     "modeEditor",
}

func modeName(md paneMode) string {
	if n, ok := modeNames[md]; ok {
		return n
	}
	return "paneMode(?)"
}

// wantMode asserts the model's mode and that the four predicates every switch is written
// against agree with it. They derive from the one value, so a disagreement means a
// predicate is wrong.
func wantMode(t *testing.T, m *model, want paneMode) {
	t.Helper()
	if m.mode != want {
		t.Fatalf("mode = %s, want %s", modeName(m.mode), modeName(want))
	}
	type pred struct {
		name string
		got  bool
		want bool
	}
	for _, p := range []pred{
		{"focused", m.focused(), want == modeShell || want == modeScrollback},
		{"scrolling", m.scrolling(), want == modeScrollback},
		{"browsing", m.browsing(), want == modeBrowser},
		{"editing", m.editing(), want == modeEditor},
		{"inPane", m.inPane(), want != modeList},
		{"listHasFocus", m.listHasFocus(), want == modeList},
	} {
		if p.got != p.want {
			t.Errorf("in %s: %s() = %v, want %v", modeName(want), p.name, p.got, p.want)
		}
	}
}

// Every way into a pane lands in exactly one mode, and every way out lands back in the
// list. With four bools, "focused and browsing" was a state you could reach by forgetting
// a line.
func TestModeTransitionsAreExclusive(t *testing.T) {
	steps := []struct {
		name string
		do   func(m *model, s *session)
		want paneMode
	}{
		{"focusShell", func(m *model, _ *session) { m.focusShell("web") }, modeShell},
		{"enterScrollback", func(m *model, s *session) { m.enterScrollback(s) }, modeScrollback},
		{"exitScrollback", func(m *model, _ *session) { m.exitScrollback() }, modeShell},
		{"leavePane", func(m *model, _ *session) { m.leavePane() }, modeList},
		{"clickIntoPane", func(m *model, s *session) { m.clickIntoPane(s) }, modeShell},
		{"backToList", func(m *model, _ *session) { m.backToList() }, modeList},
	}
	m, s := scrolledShell(t)
	s.browser = &filebrowser.Browser{}
	for _, step := range steps {
		step.do(m, s)
		t.Run(step.name, func(t *testing.T) { wantMode(t, m, step.want) })
	}
}

// Scrollback is a mode of the shell, so leaving the pane from inside it leaves the shell
// too. With a separate `scrolling` bool, a call site that cleared `focused` and forgot it
// left the model claiming both.
func TestLeavingFromScrollbackClearsBoth(t *testing.T) {
	for _, out := range []struct {
		name string
		do   func(m *model)
	}{
		{"leavePane", func(m *model) { m.leavePane() }},
		{"backToList", func(m *model) { m.backToList() }},
		{"leaveAll", func(m *model) { m.leaveAll() }},
	} {
		t.Run(out.name, func(t *testing.T) {
			m, s := scrolledShell(t)
			if !m.enterScrollback(s) {
				t.Fatal("could not enter scrollback on a shell with history")
			}
			out.do(m)
			wantMode(t, m, modeList)
		})
	}
}

// The browser and the editor are one step apart in both directions, and neither
// step leaves the other mode standing.
func TestBrowserEditorRoundTrip(t *testing.T) {
	m, s := editorModel(t, "a.conf")
	s.browser = &filebrowser.Browser{}
	wantMode(t, m, modeEditor)

	// Out of the editor is back to the browser the file was opened from...
	m.leaveEditor()
	wantMode(t, m, modeBrowser)

	// ...and opening the file that is already open goes straight back to its tab.
	m.openFile(filebrowser.OpenFileMsg{Path: "/etc/a.conf", Name: "a.conf"})
	wantMode(t, m, modeEditor)

	// With the browser gone, leaving the editor has only the list to fall back to.
	s.browser = nil
	m.leaveEditor()
	wantMode(t, m, modeList)
}

// A dropped connection takes the keyboard out of scrollback: the history is still on
// screen, but the keys now belong to the dead pane's small keyboard.
func TestConnectionLossLeavesScrollback(t *testing.T) {
	m, s := scrolledShell(t)
	if !m.enterScrollback(s) {
		t.Fatal("could not enter scrollback")
	}
	m.markDead("web", "kex timeout")
	wantMode(t, m, modeShell)
}
