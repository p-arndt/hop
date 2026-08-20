package tui

import (
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"hop/internal/filebrowser"
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
		mode:     modeEditor,
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
	if !m.editing() {
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

// A file that is already open focuses its tab instead of starting a second editor
// on the same file.
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

// fakePaneWith is fakePane at a chosen size with screen already printed onto it by
// the far end, for the tests that care about the relationship between a pane's own
// width and the box it is drawn in. It waits for the emulator to have parsed the
// output before handing the pane back — marker is the text to wait for.
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

// splitModel is editorModel on a wide window with a browser open, which is the only place
// a split can be asked for.
func splitModel(t *testing.T, names ...string) (*model, *session) {
	t.Helper()
	m, s := editorModel(t, names...)
	m.hosts = []store.Host{{Alias: "web"}}
	m.filtered = []int{0}
	m.highlights = map[int][]int{}
	m.width, m.height, m.ready = 200, 34, true
	// A directory and a file. The listing sorts directories first, so row 0 is "logs" and
	// row 1 is "b.conf" — the split only arms on the latter.
	s.browser = fakeBrowserWith(t, "/srv",
		sftpx.Entry{Name: "b.conf", Size: 12},
		sftpx.Entry{Name: "logs", IsDir: true})
	s.browser.Select(1)
	m.relayout()
	return m, s
}

// The whole life of a split: the key arms it, the file that comes back lands in the new
// half, both halves draw from the one tab list, and closing the second file collapses it.
func TestSplitOpenAndClose(t *testing.T) {
	m, s := splitModel(t, "a.conf")
	m.mode = modeBrowser

	// The key goes through the browser's own Activate, so what it leaves behind on the
	// session is the whole of what makes the file that returns a split.
	m.handleKey(key(t, "\\"))
	if !s.splitPending {
		t.Fatal("the split key did not mark the open in flight")
	}

	// The file the browser activated comes back.
	m.openFile("web", filebrowser.OpenFileMsg{Path: "/etc/b.conf", Name: "b.conf"})
	if !s.split || !s.splitRight {
		t.Fatalf("split = %v, splitRight = %v; want the content halved with the keyboard in the new half",
			s.split, s.splitRight)
	}
	if s.splitPending {
		t.Fatal("the flag survived the file it was set for")
	}

	m.Update(editorOpenedMsg{alias: "web", tab: &editorTab{
		id: 2, name: "b.conf", path: "/etc/b.conf", pane: fakePane(),
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

	// Both files on screen at once is the point of the split.
	screen := m.View()
	if !strings.Contains(screen, "a.conf") || !strings.Contains(screen, "b.conf") {
		t.Fatalf("the split does not show both files:\n%s", screen)
	}

	// Closing the second file leaves nothing to put beside the first, so the split goes.
	m.Update(editorExitedMsg{alias: "web", id: 2})
	if s.split {
		t.Fatal("the split survived the second file closing")
	}
	if got := s.editorAt(false); got == nil || got.name != "a.conf" {
		t.Fatalf("after collapsing, the content area shows %v, want a.conf", got)
	}
}

// A split is a second cursor into one tab list, not a second list: with a third file open,
// the half that loses its tab takes the next one along rather than showing the other
// half's file twice.
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

// The tab keys move the half the keyboard is in, not the left one — with two files up,
// "next tab" is a question about the one being read.
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

// Opening a file that is already up cannot be a split: two editors on one file are two
// buffers neither end knows about. The keyboard goes to the half it is already in.
func TestSplitOnAnOpenFileFocusesItInstead(t *testing.T) {
	m, s := splitModel(t, "a.conf", "b.conf")
	s.openSplit()
	s.splitEd = 1
	s.splitRight = false // reading the left half
	m.mode = modeBrowser

	s.splitPending = true
	m.openFile("web", filebrowser.OpenFileMsg{Path: "/etc/b.conf", Name: "b.conf"})

	if len(s.editors) != 2 {
		t.Fatalf("editors = %d, want no second editor on one file", len(s.editors))
	}
	if !s.splitRight {
		t.Fatal("the keyboard did not move to the half already showing b.conf")
	}
}

// A content area with no room for two halves opens the file as a tab and says so, rather
// than halving a pane that is already narrow.
func TestSplitDeclinesOnANarrowContentArea(t *testing.T) {
	m, s := splitModel(t, "a.conf")
	m.paneW = 2*minSplitHalf - 3
	m.mode = modeBrowser

	m.handleKey(key(t, "\\"))
	if s.splitPending {
		t.Fatal("a content area too narrow to halve still armed a split")
	}
	if m.status == "" {
		t.Fatal("the refusal was silent")
	}

	m.openFile("web", filebrowser.OpenFileMsg{Path: "/etc/b.conf", Name: "b.conf"})
	if s.split {
		t.Fatal("the file opened beside on a content area that cannot hold two halves")
	}
}

// An editor that never started leaves the half it was opened for empty. The split is armed
// before the SSH session is, so the failure has to put the content area back.
func TestSplitCollapsesWhenTheEditorFails(t *testing.T) {
	m, s := splitModel(t, "a.conf")
	s.splitPending = true
	m.openFile("web", filebrowser.OpenFileMsg{Path: "/etc/b.conf", Name: "b.conf"})
	if !s.split {
		t.Fatal("the split did not open, so this test proves nothing")
	}

	m.Update(editorOpenedMsg{alias: "web", err: errFakeEditor})

	if s.split {
		t.Fatal("a failed editor left the content area split on one file drawn twice")
	}
}

// A split never arms on a directory. The browser only expands it, so the flag would be
// left standing for whichever file was opened next — including by a double-click, which
// does not go through the key handler that spends it.
func TestSplitDoesNotArmOnADirectory(t *testing.T) {
	m, s := splitModel(t, "a.conf")
	m.mode = modeBrowser
	s.browser.Select(0) // the directory: dirs sort first

	m.handleKey(key(t, "\\"))

	if s.splitPending {
		t.Fatal("the split armed on a directory")
	}
}

// On a file it does arm, and any other browser key spends it — the split can only ever
// apply to the file the split key was pressed on.
func TestSplitArmedOnAFileIsSpentByTheNextKey(t *testing.T) {
	m, s := splitModel(t, "a.conf")
	m.mode = modeBrowser

	m.handleKey(key(t, "\\"))
	if !s.splitPending {
		t.Fatal("the split key did not arm, so this test proves nothing")
	}

	m.handleKey(key(t, "r")) // refresh: any other browser key

	if s.splitPending {
		t.Fatal("the split survived a key that was not the split key")
	}
}
