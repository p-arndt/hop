package tui

import (
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"hop/internal/filebrowser"
	"hop/internal/keys"
	"hop/internal/sftpx"
	"hop/internal/sshx"
	"hop/internal/store"
	"hop/internal/terminal"
)

// errFakeEditor stands in for whatever the remote said when an editor would not start.
var errFakeEditor = errors.New("no such file or directory")

// nopWriteCloser adapts io.Discard to the io.WriteCloser a Session wants.
type nopWriteCloser struct{ io.Writer }

func (nopWriteCloser) Close() error { return nil }

// fakePane is a terminal.Pane over a Session with no SSH connection behind it.
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
	m := &model{sessions: map[string]*session{"web": s}, notify: make(chan struct{}, 1), layout: layout{paneW: 40, paneH: 12}, focus: focus{active: "web", mode: modeEditor}}
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
	if !m.editing() {
		t.Fatal("tab switching left editing mode")
	}
}

// ctrl+o drops back to the browser with the editor tabs left running.
func TestEditorCtrlOKeepsTabs(t *testing.T) {
	m, s := editorModel(t, "a.conf", "b.conf")
	s.browser = &filebrowser.Browser{}

	m.handleKey(altKey("2"))
	m.handleKey(key(t, "ctrl+o"))
	m.handleKey(runeKey('o')) // the leader's "out"

	if m.editing() {
		t.Fatal("ctrl+o did not leave editing mode")
	}
	if !m.browsing() {
		t.Fatal("ctrl+o did not return to the browser it was opened from")
	}
	if len(s.editors) != 2 || s.activeEd != 1 {
		t.Fatalf("editors = %d, activeEd = %d; want the two tabs intact on the second",
			len(s.editors), s.activeEd)
	}
}

// The last tab closing hands the content area back to the browser.
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
	if !m.editing() {
		t.Fatal("left editing mode while a tab was still open")
	}

	m.Update(editorExitedMsg{alias: "web", id: 1}) // a.conf quit
	if len(s.editors) != 0 {
		t.Fatalf("editors = %+v, want none", s.editors)
	}
	if m.editing() || !m.browsing() {
		t.Fatalf("editing = %v, browsing = %v; the last tab closing must fall back to the browser",
			m.editing(), m.browsing())
	}
}

func TestOpenFileFocusesExistingTab(t *testing.T) {
	m, s := editorModel(t, "a.conf", "b.conf")
	m.mode = modeBrowser

	cmd := m.openFile("web", filebrowser.OpenFileMsg{Path: "/etc/a.conf", Name: "a.conf"})
	if cmd != nil {
		t.Fatal("openFile started a second editor on a file that is already open")
	}
	if s.activeEd != 0 || !m.editing() || m.browsing() {
		t.Fatalf("activeEd = %d, editing = %v, browsing = %v; want the existing tab focused",
			s.activeEd, m.editing(), m.browsing())
	}
	if len(s.editors) != 2 {
		t.Fatalf("editors = %d, want the original two", len(s.editors))
	}
}

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

// fakePaneWith is fakePane at a chosen size with screen parsed onto it; marker is what to wait for.
func fakePaneWith(t *testing.T, w, h int, screen, marker string) *terminal.Pane {
	t.Helper()
	p := terminal.New(&sshx.Session{
		Stdin:  nopWriteCloser{io.Discard},
		Stdout: strings.NewReader(screen),
	}, w, h, nil)

	deadline := time.Now().Add(2 * time.Second)
	for !strings.Contains(p.View(), marker) && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if !strings.Contains(p.View(), marker) {
		t.Fatalf("the pane never rendered %q", marker)
	}
	return p
}

// splitModel is editorModel on a wide window with a browser open.
func splitModel(t *testing.T, names ...string) (*model, *session) {
	t.Helper()
	m, s := editorModel(t, names...)
	m.hosts = []store.Host{{Alias: "web"}}
	m.filtered = []int{0}
	m.highlights = map[int][]int{}
	m.width, m.height, m.ready = 200, 34, true
	// Dirs sort first, so row 1 is b.conf — the only row the split arms on.
	s.browser = fakeBrowserWith(t, "/srv",
		sftpx.Entry{Name: "b.conf", Size: 12},
		sftpx.Entry{Name: "logs", IsDir: true})
	s.browser.Select(1)
	m.relayout()
	return m, s
}

// activated unwraps the OpenFileMsg a browser key's command carries; ok is false when it produced none.
func activated(t *testing.T, cmd tea.Cmd) (filebrowser.OpenFileMsg, bool) {
	t.Helper()
	if cmd == nil {
		return filebrowser.OpenFileMsg{}, false
	}
	wrapped, ok := cmd().(filebrowser.Msg)
	if !ok {
		return filebrowser.OpenFileMsg{}, false
	}
	open, ok := wrapped.Body.(filebrowser.OpenFileMsg)
	return open, ok
}

// The whole life of a split: open beside, both halves drawn, collapse on close.
func TestSplitOpenAndClose(t *testing.T) {
	m, s := splitModel(t, "a.conf")
	m.mode = modeBrowser

	_, cmd := m.handleKey(key(t, "\\"))
	open, ok := activated(t, cmd)
	if !ok || !open.Beside {
		t.Fatalf("the split key produced %+v (ok=%v), want a file marked to open beside", open, ok)
	}

	m.openFile("web", open)
	if !s.split || !s.splitRight {
		t.Fatalf("split = %v, splitRight = %v; want the content halved with the keyboard in the new half",
			s.split, s.splitRight)
	}

	m.Update(editorOpenedMsg{alias: "web", tab: &editorTab{
		id: 2, name: "b.conf", path: open.Path, pane: fakePane(),
	}})

	if got := s.editorAt(false); got == nil || got.name != "a.conf" {
		t.Fatalf("the left half shows %v, want a.conf", got)
	}
	if got := s.editorAt(true); got == nil || got.name != "b.conf" {
		t.Fatalf("the right half shows %v, want b.conf", got)
	}
	if got := s.editor(); got == nil || got.name != "b.conf" {
		t.Fatalf("the keyboard is in %v, want the half just opened", got)
	}

	screen := m.View()
	if !strings.Contains(screen, "a.conf") || !strings.Contains(screen, "b.conf") {
		t.Fatalf("the split does not show both files:\n%s", screen)
	}

	m.Update(editorExitedMsg{alias: "web", id: 2})
	if s.split {
		t.Fatal("the split survived the second file closing")
	}
	if got := s.editorAt(false); got == nil || got.name != "a.conf" {
		t.Fatalf("after collapsing, the content area shows %v, want a.conf", got)
	}
}

func TestSplitHalvesNeverShowOneFileTwice(t *testing.T) {
	m, s := splitModel(t, "a.conf", "b.conf", "c.conf")
	s.openSplit()
	s.splitEd = 1 // b.conf on the right, a.conf on the left

	m.Update(editorExitedMsg{alias: "web", id: 2}) // b.conf quit

	if !s.split {
		t.Fatal("the split collapsed with two files still open")
	}
	if s.activeEd == s.splitEd {
		t.Fatalf("both halves are on tab %d, want two different files", s.activeEd)
	}
}

func TestSplitTabKeysMoveTheFocusedHalf(t *testing.T) {
	m, s := splitModel(t, "a.conf", "b.conf", "c.conf")
	s.openSplit()
	s.splitEd = 1
	m.mode = modeEditor

	m.handleKey(altKey("right"))
	if s.splitEd != 2 || s.activeEd != 0 {
		t.Fatalf("splitEd/activeEd = %d/%d after alt+right in the right half, want 2/0", s.splitEd, s.activeEd)
	}

	s.splitRight = false
	m.handleKey(altKey("3"))
	if s.activeEd != 2 || s.splitEd != 2 {
		t.Fatalf("activeEd/splitEd = %d/%d after alt+3 in the left half, want 2/2", s.activeEd, s.splitEd)
	}
}

// Two editors on one file would be two buffers neither end knows about.
func TestSplitOnAnOpenFileFocusesItInstead(t *testing.T) {
	m, s := splitModel(t, "a.conf", "b.conf")
	s.openSplit()
	s.splitEd = 1
	s.splitRight = false // reading the left half
	m.mode = modeBrowser

	m.openFile("web", filebrowser.OpenFileMsg{Path: "/etc/b.conf", Name: "b.conf", Beside: true})

	if len(s.editors) != 2 {
		t.Fatalf("editors = %d, want no second editor on one file", len(s.editors))
	}
	if !s.splitRight {
		t.Fatal("the keyboard did not move to the half already showing b.conf")
	}
}

// A content area too narrow to halve opens the file as a tab and says so.
func TestSplitDeclinesOnANarrowContentArea(t *testing.T) {
	m, s := splitModel(t, "a.conf")
	m.paneW = 2*minSplitHalf - 3
	m.mode = modeBrowser

	_, cmd := m.handleKey(key(t, "\\"))
	open, ok := activated(t, cmd)
	if !ok || open.Beside {
		t.Fatalf("a content area too narrow to halve still asked for %+v (ok=%v)", open, ok)
	}
	if m.status == "" {
		t.Fatal("the refusal was silent")
	}

	m.openFile("web", open)
	if s.split {
		t.Fatal("the file opened beside on a content area that cannot hold two halves")
	}
}

// The split is armed before the SSH session, so a failed editor has to put the content area back.
func TestSplitCollapsesWhenTheEditorFails(t *testing.T) {
	m, s := splitModel(t, "a.conf")
	m.openFile("web", filebrowser.OpenFileMsg{Path: "/etc/b.conf", Name: "b.conf", Beside: true})
	if !s.split {
		t.Fatal("the split did not open, so this test proves nothing")
	}

	m.Update(editorOpenedMsg{alias: "web", err: errFakeEditor})

	if s.split {
		t.Fatal("a failed editor left the content area split on one file drawn twice")
	}
}

// The split key on a directory expands it in place and leaves no intent to land later.
func TestSplitOnADirectoryOpensNothing(t *testing.T) {
	m, s := splitModel(t, "a.conf")
	m.mode = modeBrowser
	s.browser.Select(0) // the directory: dirs sort first

	_, cmd := m.handleKey(key(t, "\\"))

	if open, ok := activated(t, cmd); ok {
		t.Fatalf("the split key on a directory produced %+v, want no file to open", open)
	}
	if s.split {
		t.Fatal("descending into a directory halved the content area")
	}
}

func TestSplitStillSplitsAfterAnotherKey(t *testing.T) {
	m, s := splitModel(t, "a.conf")
	m.mode = modeBrowser

	m.handleKey(key(t, "r")) // refresh: any other browser key
	_, cmd := m.handleKey(key(t, "\\"))

	open, ok := activated(t, cmd)
	if !ok || !open.Beside {
		t.Fatalf("the second split key produced %+v (ok=%v), want a file marked to open beside", open, ok)
	}
	m.openFile("web", open)
	if !s.split || !s.splitRight {
		t.Fatalf("split = %v, splitRight = %v; want the content halved with the keyboard in the new half",
			s.split, s.splitRight)
	}
}

func TestUnsplitKeepsTheFocusedHalfsFile(t *testing.T) {
	m, s := splitModel(t, "a.conf", "b.conf")
	s.openSplit()
	s.splitEd = 1 // b.conf on the right, a.conf on the left, keyboard on the right
	m.mode = modeEditor

	m.handleKey(key(t, `ctrl+\`))

	if s.split || s.splitRight {
		t.Fatalf("split = %v, splitRight = %v; want the content area back to one box",
			s.split, s.splitRight)
	}
	if got := s.editor(); got == nil || got.name != "b.conf" {
		t.Fatalf("the content area shows %v, want the file the focused half was reading", got)
	}
	if len(s.editors) != 2 {
		t.Fatalf("editors = %d; unsplitting closed a file, and it must only close the split",
			len(s.editors))
	}
	// The layout has to drop the halves too, not just the state.
	w, _ := m.editorSize(s)
	if w != m.paneW {
		t.Fatalf("the editor is %d columns wide in a %d-column content area; unsplit did not relayout",
			w, m.paneW)
	}
	if m.splitOn(s) {
		t.Fatal("the layout still reports two halves")
	}
}

func TestUnsplitFromTheLeftHalf(t *testing.T) {
	m, s := splitModel(t, "a.conf", "b.conf")
	s.openSplit()
	s.splitEd = 1
	s.splitRight = false
	m.mode = modeEditor

	m.handleKey(key(t, `ctrl+\`))

	if got := s.editor(); got == nil || got.name != "a.conf" {
		t.Fatalf("the content area shows %v, want a.conf", got)
	}
}

// With nothing split the key is not hop's: the remote editor is owed it.
func TestUnsplitWithNoSplitFallsThroughToTheEditor(t *testing.T) {
	m, s := splitModel(t, "a.conf", "b.conf")
	m.mode = modeEditor
	s.activeEd = 1

	handled, _, cmd := m.doEditor(keys.EditorUnsplit)

	if handled {
		t.Fatal("the unsplit key was swallowed with nothing split; the remote editor is owed it")
	}
	if cmd != nil {
		t.Fatal("a no-op returned a command")
	}
	if s.split || s.activeEd != 1 || m.mode != modeEditor {
		t.Fatalf("split = %v, activeEd = %d, mode = %v; the key must change nothing visible",
			s.split, s.activeEd, m.mode)
	}
}
