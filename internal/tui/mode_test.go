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

// scrolledShell is a model on "web" whose shell pane has real history, so enterScrollback
// passes its guard. The emulator is fed on its own goroutine, so the lines are waited for.
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

// wantMode asserts the mode and that the four derived predicates agree with it.
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

// Every way into a pane lands in exactly one mode, and every way out lands in the list.
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
		{"clickIntoPane", func(m *model, s *session) { m.clickIntoPane(s, false) }, modeShell},
		{"backToList", func(m *model, _ *session) { m.backToList() }, modeList},
	}
	m, s := scrolledShell(t)
	s.browser = &filebrowser.Browser{}
	for _, step := range steps {
		step.do(m, s)
		t.Run(step.name, func(t *testing.T) { wantMode(t, m, step.want) })
	}
}

// Scrollback is a mode of the shell, so leaving from inside it leaves the shell too.
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

// The browser and the editor are one step apart in both directions.
func TestBrowserEditorRoundTrip(t *testing.T) {
	m, s := editorModel(t, "a.conf")
	s.browser = &filebrowser.Browser{}
	wantMode(t, m, modeEditor)

	m.leaveEditor()
	wantMode(t, m, modeBrowser)

	// Opening the file that is already open goes straight back to its tab.
	m.openFile("web", filebrowser.OpenFileMsg{Path: "/etc/a.conf", Name: "a.conf"})
	wantMode(t, m, modeEditor)

	s.browser = nil
	m.leaveEditor()
	wantMode(t, m, modeList)
}

// A dropped connection takes the keyboard out of scrollback.
func TestConnectionLossLeavesScrollback(t *testing.T) {
	m, s := scrolledShell(t)
	if !m.enterScrollback(s) {
		t.Fatal("could not enter scrollback")
	}
	m.markDead("web", "kex timeout")
	wantMode(t, m, modeShell)
}

// Tree and file are on screen together; the mode says only which one the keys reach.
func TestFocusCrossesTheColumns(t *testing.T) {
	m, s := columnModel(t, 200, 34)
	s.editors = []*editorTab{{id: 1, name: "a.conf", path: "/etc/a.conf", pane: fakePane()}}
	t.Cleanup(s.closeEditors)
	wantMode(t, m, modeBrowser)

	m.handleKey(key(t, "tab"))
	wantMode(t, m, modeEditor)
	if m.treeWidth() == 0 {
		t.Fatal("focusing the file collapsed the tree column, want it left on screen")
	}

	m.handleKey(altKey("t"))
	wantMode(t, m, modeBrowser)
	if len(s.editors) != 1 {
		t.Fatalf("editors = %d after crossing back to the tree, want the file left open", len(s.editors))
	}
}

// Both columns are drawn whichever one holds the keyboard.
func TestBothColumnsAreDrawn(t *testing.T) {
	m, s := columnModel(t, 200, 34)
	s.editors = []*editorTab{{id: 1, name: "a.conf", path: "/etc/a.conf", pane: fakePane()}}
	t.Cleanup(s.closeEditors)

	for _, mode := range []paneMode{modeBrowser, modeEditor} {
		m.mode = mode
		screen := m.View()
		if !strings.Contains(screen, "/srv") {
			t.Fatalf("in %s the tree column is not on screen:\n%s", modeName(mode), screen)
		}
		if !strings.Contains(screen, "a.conf") {
			t.Fatalf("in %s the open file is not on screen:\n%s", modeName(mode), screen)
		}
	}
}

// A key that would hand the keyboard nowhere does nothing.
func TestFocusKeysDeclineWithNowhereToGo(t *testing.T) {
	t.Run("no content to focus", func(t *testing.T) {
		// No shell of its own: the content area is the details card, which takes no keys.
		m, s := columnModel(t, 200, 34)
		s.closeShells()
		m.handleKey(key(t, "tab"))
		wantMode(t, m, modeBrowser)
	})

	t.Run("no tree to go back to", func(t *testing.T) {
		m, s := editorModel(t, "a.conf")
		if m.focusTree() {
			t.Fatal("focusTree found a tree on a session with no browser")
		}
		wantMode(t, m, modeEditor)
		if s.browser != nil {
			t.Fatal("the session grew a browser")
		}
	})
}
