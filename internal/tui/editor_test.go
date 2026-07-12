package tui

import (
	"io"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"hop/internal/filebrowser"
	"hop/internal/sshx"
	"hop/internal/terminal"
)

// nopWriteCloser adapts io.Discard to the io.WriteCloser a Session wants.
type nopWriteCloser struct{ io.Writer }

func (nopWriteCloser) Close() error { return nil }

// fakePane builds a terminal.Pane over a Session with no SSH connection behind
// it: its output stream is immediately at EOF and its input goes nowhere. Enough
// for the tab bookkeeping, which never reads or writes the pane.
func fakePane() *terminal.Pane {
	sess := &sshx.Session{
		Stdin:  nopWriteCloser{io.Discard},
		Stdout: strings.NewReader(""),
	}
	return terminal.New(sess, 20, 5, nil)
}

// editorModel builds a model in editing mode on alias "web" with one tab per name.
func editorModel(t *testing.T, names ...string) (*model, *session) {
	t.Helper()
	s := &session{client: &sshx.Client{}}
	for i, n := range names {
		s.editors = append(s.editors, &editorTab{
			id: i + 1, name: n, path: "/etc/" + n, pane: fakePane(),
		})
	}
	m := &model{
		sessions: map[string]*session{"web": s},
		notify:   make(chan struct{}, 1),
		active:   "web",
		editing:  true,
		paneW:    40,
		paneH:    12,
	}
	t.Cleanup(s.closeEditors)
	return m, s
}

// altKey builds the tea.KeyMsg whose String() is "alt+<name>".
func altKey(name string) tea.KeyMsg {
	switch name {
	case "left":
		return tea.KeyMsg{Type: tea.KeyLeft, Alt: true}
	case "right":
		return tea.KeyMsg{Type: tea.KeyRight, Alt: true}
	default:
		return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(name), Alt: true}
	}
}

// alt+←/→ cycle the open tabs and wrap around at both ends.
func TestEditorTabCycling(t *testing.T) {
	m, s := editorModel(t, "a.conf", "b.conf", "c.conf")

	for _, want := range []int{1, 2, 0} { // right wraps 2 -> 0
		m.handleKey(altKey("right"))
		if s.activeEd != want {
			t.Fatalf("after alt+right: activeEd = %d, want %d", s.activeEd, want)
		}
	}
	for _, want := range []int{2, 1, 0} { // left wraps 0 -> 2
		m.handleKey(altKey("left"))
		if s.activeEd != want {
			t.Fatalf("after alt+left: activeEd = %d, want %d", s.activeEd, want)
		}
	}
}

// alt+1…9 jump straight to a tab; a number with no tab behind it does nothing.
func TestEditorTabJump(t *testing.T) {
	m, s := editorModel(t, "a.conf", "b.conf", "c.conf")

	m.handleKey(altKey("3"))
	if s.activeEd != 2 {
		t.Fatalf("alt+3: activeEd = %d, want 2", s.activeEd)
	}
	m.handleKey(altKey("9")) // no ninth tab
	if s.activeEd != 2 {
		t.Fatalf("alt+9 with 3 tabs moved to %d, want to stay on 2", s.activeEd)
	}
	if !m.editing {
		t.Fatal("tab switching left editing mode")
	}
}

// ctrl+o drops back to the browser, and the editors keep running: their tabs are
// still there, on the same file, when you return.
func TestEditorCtrlOKeepsTabs(t *testing.T) {
	m, s := editorModel(t, "a.conf", "b.conf")
	s.browser = &filebrowser.Browser{}

	m.handleKey(altKey("2"))
	m.handleKey(key(t, "ctrl+o"))

	if m.editing {
		t.Fatal("ctrl+o did not leave editing mode")
	}
	if !m.browsing {
		t.Fatal("ctrl+o did not return to the browser it was opened from")
	}
	if len(s.editors) != 2 || s.activeEd != 1 {
		t.Fatalf("editors = %d, activeEd = %d; want the two tabs intact on the second",
			len(s.editors), s.activeEd)
	}
}

// Quitting the editor closes its tab. The last one closing hands the pane back to
// the browser rather than leaving an empty editing mode on screen.
func TestEditorExitClosesTab(t *testing.T) {
	m, s := editorModel(t, "a.conf", "b.conf")
	s.browser = &filebrowser.Browser{}
	s.activeEd = 1

	m.Update(editorExitedMsg{alias: "web", id: 2}) // b.conf quit
	if len(s.editors) != 1 || s.editors[0].name != "a.conf" {
		t.Fatalf("editors = %+v, want only a.conf", s.editors)
	}
	if s.activeEd != 0 {
		t.Fatalf("activeEd = %d, want 0 after the tab above it closed", s.activeEd)
	}
	if !m.editing {
		t.Fatal("left editing mode while a tab was still open")
	}

	m.Update(editorExitedMsg{alias: "web", id: 1}) // a.conf quit
	if len(s.editors) != 0 {
		t.Fatalf("editors = %+v, want none", s.editors)
	}
	if m.editing || !m.browsing {
		t.Fatalf("editing = %v, browsing = %v; the last tab closing must fall back to the browser",
			m.editing, m.browsing)
	}
}

// A file that is already open focuses its tab instead of starting a second editor
// on the same file.
func TestOpenFileFocusesExistingTab(t *testing.T) {
	m, s := editorModel(t, "a.conf", "b.conf")
	m.editing = false
	m.browsing = true

	_, cmd := m.openFile(filebrowser.OpenFileMsg{Path: "/etc/a.conf", Name: "a.conf"})
	if cmd != nil {
		t.Fatal("openFile started a second editor on a file that is already open")
	}
	if s.activeEd != 0 || !m.editing || m.browsing {
		t.Fatalf("activeEd = %d, editing = %v, browsing = %v; want the existing tab focused",
			s.activeEd, m.editing, m.browsing)
	}
	if len(s.editors) != 2 {
		t.Fatalf("editors = %d, want the original two", len(s.editors))
	}
}

// The command hop asks the remote host to run must survive filenames a shell would
// otherwise mangle — spaces, quotes and glob characters are data, not syntax.
func TestRemoteEditorCmdQuotesPath(t *testing.T) {
	cases := []struct {
		path string
		want string
	}{
		{"/etc/nginx.conf", `'/etc/nginx.conf'`},
		{"/tmp/my notes.md", `'/tmp/my notes.md'`},
		{"/tmp/it's here.txt", `'/tmp/it'\''s here.txt'`},
		{"/tmp/*.conf", `'/tmp/*.conf'`},
	}
	for _, tc := range cases {
		got := remoteEditorCmd("", tc.path)
		if !strings.HasSuffix(got, " "+tc.want) {
			t.Fatalf("remoteEditorCmd(%q) ends %q, want it to end with %q", tc.path, got, tc.want)
		}
		if !strings.Contains(got, "${EDITOR:-") {
			t.Fatalf("remoteEditorCmd(%q) = %q, want it to prefer the remote $EDITOR", tc.path, got)
		}
	}
}
