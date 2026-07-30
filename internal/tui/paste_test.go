package tui

import (
	"io"
	"strings"
	"sync"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"hop/internal/sshx"
	"hop/internal/terminal"
)

// pastePane builds a pane whose input can be read back, so a test can see what a
// paste actually put on the wire.
func pastePane() (*terminal.Pane, *syncBuf) {
	stdin := &syncBuf{}
	sess := &sshx.Session{Stdin: stdin, Stdout: strings.NewReader("")}
	return terminal.New(sess, 20, 5, nil), stdin
}

// pasteModel is a model focused on a shell pane, which is where a paste goes.
func pasteModel() (*model, *syncBuf) {
	pane, stdin := pastePane()
	m := &model{
		sessions:      map[string]*session{"web": {shells: []*shellTab{{id: 1, pane: pane}}}},
		active:        "web",
		focused:       true,
		pasteCoalesce: true,
	}
	return m, stdin
}

// pasted is the key event a terminal that supports bracketed paste delivers a
// paste as: the whole clipboard in one event, marked.
func pasted(text string) tea.KeyMsg {
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(text), Paste: true}
}

// runes is one typed (or synthesised) character.
func typed(r rune) tea.KeyMsg {
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}}
}

// A marked paste goes to the pane as a paste — not as the bindings its characters
// happen to spell. Pasting "q" into a shell must not be read as anything but a q,
// and the same text arriving through the shell handler proves the branch sits
// above the switch.
func TestMarkedPasteGoesToTheShell(t *testing.T) {
	m, stdin := pasteModel()
	m.handleKey(pasted("echo hi\n"))

	if got := stdin.String(); got != "echo hi\r" {
		t.Fatalf("the pane received %q, want the pasted text", got)
	}
}

// The same, in an editor tab: a paste into vim is the case this whole feature is
// for, and the editor handler binds letters of its own the paste must not hit.
func TestMarkedPasteGoesToTheEditor(t *testing.T) {
	pane, stdin := pastePane()
	m := &model{
		sessions: map[string]*session{"web": {editors: []*editorTab{{id: 1, name: "f", pane: pane}}}},
		active:   "web",
		editing:  true,
	}
	m.handleKey(pasted("line one\nline two"))

	if got := stdin.String(); got != "line one\rline two" {
		t.Fatalf("the editor received %q, want the pasted text", got)
	}
}

// A paste while the shell is paused in its history goes to the shell, after coming
// back to the live screen — the same thing typing a letter in there does.
func TestPasteFromScrollbackReturnsLiveFirst(t *testing.T) {
	m, stdin := pasteModel()
	m.scrolling = true

	m.handleKey(pasted("ls"))
	if m.scrolling {
		t.Fatal("a paste left the shell in scrollback")
	}
	if got := stdin.String(); got != "ls" {
		t.Fatalf("the pane received %q, want the pasted text", got)
	}
}

// Windows has no marked paste: the console synthesises one as a burst of ordinary
// key events. A burst that carries a newline is a paste, and it reaches the pane as
// one piece rather than as a line of typing an editor would indent.
func TestBurstWithANewlineIsAPaste(t *testing.T) {
	m, stdin := pasteModel()

	for _, k := range []tea.KeyMsg{typed('a'), {Type: tea.KeyEnter}, typed('b')} {
		if _, cmd := m.handleKey(k); cmd == nil {
			t.Fatal("a bufferable key did not arm the flush")
		}
	}
	if got := stdin.String(); got != "" {
		t.Fatalf("the burst was sent before it ended: %q", got)
	}

	m.flushPaste()
	if got := stdin.String(); got != "a\rb" {
		t.Fatalf("the pane received %q, want the burst as one paste", got)
	}
}

// A key held down until it repeats arrives just as fast as a paste and is not one.
// It is replayed as the keystrokes it was, so holding j in vim still moves down
// three lines instead of inserting "jjj".
func TestRepeatedKeyIsNotAPaste(t *testing.T) {
	m, stdin := pasteModel()

	for i := 0; i < 3; i++ {
		m.handleKey(typed('j'))
	}
	m.flushPaste()

	if got := stdin.String(); got != "jjj" {
		t.Fatalf("the pane received %q, want three keystrokes", got)
	}
	if looksPasted([]tea.KeyMsg{typed('j'), typed('j'), typed('j')}) {
		t.Fatal("a repeating key was taken for a paste")
	}
}

// A key that cannot be part of a paste ends the burst, and ends it *before* it is
// handled itself: the text typed ahead of a ctrl+o has to reach the shell before
// the pane is left.
func TestAnUnbufferableKeyFlushesFirst(t *testing.T) {
	m, stdin := pasteModel()

	m.handleKey(typed('h'))
	m.handleKey(typed('i'))
	m.handleKey(tea.KeyMsg{Type: tea.KeyCtrlO})

	if got := stdin.String(); got != "hi" {
		t.Fatalf("the pane received %q, want the buffered keys", got)
	}
	if m.focused {
		t.Fatal("ctrl+o was swallowed by the flush instead of leaving the pane")
	}
}

// Nothing is buffered where a key is a command rather than a character. The host
// list must answer a keystroke at once — and the burst detection has no paste to
// find there anyway.
func TestNavigationKeysAreNeverBuffered(t *testing.T) {
	m, _ := pasteModel()
	m.focused = false

	if m.takeKey(typed('j')) {
		t.Fatal("a key in the host list was held back")
	}
	m.focused = true
	m.scrolling = true
	if m.takeKey(typed('j')) {
		t.Fatal("a key in scrollback was held back")
	}
	m.scrolling = false
	if m.takeKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'0'}, Alt: true}) {
		t.Fatal("alt+0 was held back: a modified key is a command, not a character")
	}
}

// The flush only fires for the burst it was armed for. A key arriving in the gap
// arms another, and the stale one must not cut the burst in half.
func TestStaleFlushIsIgnored(t *testing.T) {
	m, stdin := pasteModel()

	m.handleKey(typed('a'))
	stale := pasteFlushMsg{seq: m.paste.seq}
	m.handleKey(typed('b'))

	m.update(stale)
	if got := stdin.String(); got != "" {
		t.Fatalf("a stale flush sent %q", got)
	}

	m.update(pasteFlushMsg{seq: m.paste.seq})
	if got := stdin.String(); got != "ab" {
		t.Fatalf("the live flush sent %q, want the whole burst", got)
	}
}

// A card that opens under a burst takes its own keys. Both cards that open by
// themselves — the 2FA challenge and the host-key question — arrive from a dial
// that may well have been started from another host's shell, so the buffer must
// stand down the moment one is up. Holding those keys back would deliver a
// verification code to the shell behind the card.
func TestKeysAreNotBufferedUnderACard(t *testing.T) {
	m, stdin := pasteModel()
	m.auth = authUI{open: true, answers: []string{""}}

	for _, r := range "123" {
		m.handleKey(typed(r))
	}

	if got := m.auth.answers[0]; got != "123" {
		t.Fatalf("the card holds %q, want the typed code", got)
	}
	if got := stdin.String(); got != "" {
		t.Fatalf("%q reached the shell behind the card", got)
	}
}

// A paste in a view with nothing to type into is dropped, not read as the keys its
// characters spell. The host list is where that matters most: "q" there is quit.
func TestPasteInTheHostListIsDropped(t *testing.T) {
	m := &model{sessions: map[string]*session{}, highlights: map[int][]int{}}
	if _, cmd := m.handleKey(pasted("q")); cmd != nil {
		t.Fatal("a pasted \"q\" in the host list quit hop")
	}
}

// A password copied out of a manager brings the newline after it along. The field
// takes the line, not the line ending — one that kept it would submit a value that
// does not match on the next Enter.
func TestPasteIntoACardTakesOneLine(t *testing.T) {
	m := &model{auth: authUI{open: true, answers: []string{""}}}
	m.handleKey(pasted("hunter2\nsecond line"))

	if got := m.auth.answers[0]; got != "hunter2" {
		t.Fatalf("the field holds %q, want just the first line", got)
	}
}

// Into a text field, a paste is text — including a paste that spells one of the
// field's own keys. The filter and the host form both read a key's name out of the
// event, and "esc" pasted into either of them is characters.
func TestPasteIntoTextFieldsIsText(t *testing.T) {
	m := &model{hosts: nil, highlights: map[int][]int{}, filtering: true}
	m.handleKey(pasted("esc"))
	if m.filter != "esc" || !m.filtering {
		t.Fatalf("filter = %q, filtering = %v — want the pasted text kept", m.filter, m.filtering)
	}

	m2 := &model{}
	m2.openHostFormAdd()
	m2.handleKey(pasted("esc"))
	if !m2.hostForm.open {
		t.Fatal("a pasted \"esc\" closed the host form")
	}
	if got := m2.hostForm.buf[m2.hostForm.cursor]; got != "esc" {
		t.Fatalf("the focused field holds %q, want the pasted text", got)
	}
}

// What the buffered keys spell.
func TestPasteString(t *testing.T) {
	keys := []tea.KeyMsg{
		typed('a'), {Type: tea.KeySpace}, typed('b'),
		{Type: tea.KeyEnter}, {Type: tea.KeyTab}, typed('c'),
	}
	if got := pasteString(keys); got != "a b\n\tc" {
		t.Fatalf("pasteString = %q, want %q", got, "a b\n\tc")
	}
}

// Without a newline it takes a run of characters, not all the same, before a burst
// is read as a paste. Both ways of typing this fast are ruled out — a key repeating
// (one character over and over) and a fast digraph (two) — because being wrong here
// is not symmetric: a short paste replayed as keystrokes types exactly what was
// pasted, while "dw" typed quickly and sent as a paste is *inserted* by vim instead
// of deleting a word.
func TestABurstWithNoNewlineNeedsARun(t *testing.T) {
	if looksPasted([]tea.KeyMsg{typed('d'), typed('w')}) {
		t.Fatal("a fast digraph was taken for a paste")
	}
	if looksPasted([]tea.KeyMsg{typed('l')}) {
		t.Fatal("a single keystroke was taken for a paste")
	}
	if looksPasted([]tea.KeyMsg{typed('j'), typed('j'), typed('j'), typed('j'), typed('j')}) {
		t.Fatal("a key held down until it repeated was taken for a paste")
	}
	if !looksPasted([]tea.KeyMsg{typed('l'), typed('s'), typed(' '), typed('-'), typed('l')}) {
		t.Fatal("a pasted command line was not taken for a paste")
	}
}

// A pane that goes away under a pending burst takes the burst with it rather than
// delivering it somewhere else.
func TestFlushWithoutAPaneIsANoOp(t *testing.T) {
	m := &model{sessions: map[string]*session{}, pasteCoalesce: true}
	m.flushPaste() // nothing buffered
	m.paste.keys = []tea.KeyMsg{typed('a')}
	m.flushPaste() // buffered, but no pane was ever captured
	if m.paste.keys != nil {
		t.Fatal("the buffer survived a flush")
	}
}

// syncBuf is a writer a test can read while a pane writes to it.
type syncBuf struct {
	mu sync.Mutex
	b  []byte
}

func (s *syncBuf) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.b = append(s.b, p...)
	return len(p), nil
}

func (s *syncBuf) Close() error { return nil }

func (s *syncBuf) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return string(s.b)
}

var _ io.WriteCloser = (*syncBuf)(nil)
