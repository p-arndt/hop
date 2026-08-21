package tui

import (
	"io"
	"strings"
	"sync"
	"testing"
	"time"

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
	m := &model{sessions: map[string]*session{"web": {shells: []*shellTab{{id: 1, pane: pane}}}}, pasteCoalesce: true, focus: focus{active: "web", mode: modeShell}}
	return m, stdin
}

// testClock drives a model's burst detection, since a test types a whole burst inside a
// microsecond and hop's gate reads the wall clock to tell typing from a paste.
type testClock struct{ t time.Time }

// newTestClock installs a clock on m and returns it. It starts far enough past the zero
// time that the first key can be made to look either typed or pasted.
func newTestClock(m *model) *testClock {
	c := &testClock{t: time.Unix(1, 0)}
	m.clock = func() time.Time { return c.t }
	return c
}

// advance moves the clock on, standing in for the gap between two keys.
func (c *testClock) advance(d time.Duration) { c.t = c.t.Add(d) }

// flushPanes waits for what the model queued to reach the panes' sessions: a pane writes
// to the far end on its own goroutine (terminal.Pane.send), precisely so the UI never
// waits on the wire, so a test has to.
func flushPanes(m *model) {
	for _, s := range m.sessions {
		for _, sh := range s.shells {
			sh.pane.Flush()
		}
		for _, ed := range s.editors {
			ed.pane.Flush()
		}
	}
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

// A marked paste goes to the pane as a paste, not as the bindings its characters spell:
// pasting "q" into a shell must not be read as anything but a q.
func TestMarkedPasteGoesToTheShell(t *testing.T) {
	m, stdin := pasteModel()
	m.handleKey(pasted("echo hi\n"))

	flushPanes(m)
	if got := stdin.String(); got != "echo hi\r" {
		t.Fatalf("the pane received %q, want the pasted text", got)
	}
}

// The same, in an editor tab: a paste into vim is the case this whole feature is
// for, and the editor handler binds letters of its own the paste must not hit.
func TestMarkedPasteGoesToTheEditor(t *testing.T) {
	pane, stdin := pastePane()
	m := &model{sessions: map[string]*session{"web": {editors: []*editorTab{{id: 1, name: "f", pane: pane}}}}, focus: focus{active: "web", mode: modeEditor}}
	m.handleKey(pasted("line one\nline two"))

	flushPanes(m)
	if got := stdin.String(); got != "line one\rline two" {
		t.Fatalf("the editor received %q, want the pasted text", got)
	}
}

// A paste while the shell is paused in its history goes to the shell, after coming
// back to the live screen — the same thing typing a letter in there does.
func TestPasteFromScrollbackReturnsLiveFirst(t *testing.T) {
	m, stdin := pasteModel()
	m.mode = modeScrollback

	m.handleKey(pasted("ls"))
	if m.scrolling() {
		t.Fatal("a paste left the shell in scrollback")
	}
	flushPanes(m)
	if got := stdin.String(); got != "ls" {
		t.Fatalf("the pane received %q, want the pasted text", got)
	}
}

// Windows has no marked paste: the console synthesises one as a burst of key events. A
// burst carrying a newline is a paste, and reaches the pane as one piece — bar its first
// character, which is gone before there is anything to tell it from typing.
func TestBurstWithANewlineIsAPaste(t *testing.T) {
	m, stdin := pasteModel()
	newTestClock(m) // frozen: every key after the first is inside burstGap, as a paste is

	for i, k := range []tea.KeyMsg{typed('a'), {Type: tea.KeyEnter}, typed('b')} {
		_, cmd := m.handleKey(k)
		if i > 0 && cmd == nil {
			t.Fatal("a bufferable key did not arm the flush")
		}
	}
	// The first key opened the burst and went out as itself: nothing had yet arrived fast
	// enough to say it was pasted. See burstGap.
	flushPanes(m)
	if got := stdin.String(); got != "a" {
		t.Fatalf("the pane received %q before the flush, want the burst's first key alone", got)
	}

	m.flushPaste()
	flushPanes(m)
	if got := stdin.String(); got != "a\rb" {
		t.Fatalf("the pane received %q, want the burst as one paste", got)
	}
}

// A typed Enter arrives alone in its burst and must reach the shell as a keystroke. Read
// as a one-newline paste it goes out bracketed, which a shell inserts instead of
// executing — no command runs from the first Enter on (the v0.5.0 regression).
func TestALoneEnterIsAKeystrokeNotAPaste(t *testing.T) {
	m, stdin := pasteModel()
	newTestClock(m) // frozen: every key after the first is inside burstGap, as a paste is

	if looksPasted([]tea.KeyMsg{{Type: tea.KeyEnter}}) {
		t.Fatal("a lone Enter was taken for a paste")
	}

	m.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	m.flushPaste()
	flushPanes(m)
	if got := stdin.String(); got != "\r" {
		t.Fatalf("the pane received %q, want the bare CR a typed Enter sends", got)
	}
}

// A key held down until it repeats arrives as fast as a paste and is not one: it is
// replayed as keystrokes, so holding j in vim still moves down rather than inserting.
func TestRepeatedKeyIsNotAPaste(t *testing.T) {
	m, stdin := pasteModel()
	newTestClock(m) // frozen: every key after the first is inside burstGap, as a paste is

	for i := 0; i < 3; i++ {
		m.handleKey(typed('j'))
	}
	m.flushPaste()

	flushPanes(m)
	if got := stdin.String(); got != "jjj" {
		t.Fatalf("the pane received %q, want three keystrokes", got)
	}
	if looksPasted([]tea.KeyMsg{typed('j'), typed('j'), typed('j')}) {
		t.Fatal("a repeating key was taken for a paste")
	}
}

// A key that cannot be part of a paste ends the burst before it is handled itself: text
// typed ahead of a ctrl+o reaches the shell before the pane is left.
func TestAnUnbufferableKeyFlushesFirst(t *testing.T) {
	m, stdin := pasteModel()
	newTestClock(m) // frozen: every key after the first is inside burstGap, as a paste is

	m.handleKey(typed('h'))
	m.handleKey(typed('i'))
	m.handleKey(tea.KeyMsg{Type: tea.KeyCtrlO})
	m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'o'}})

	flushPanes(m)
	if got := stdin.String(); got != "hi" {
		t.Fatalf("the pane received %q, want the buffered keys", got)
	}
	if m.focused() {
		t.Fatal("ctrl+o was swallowed by the flush instead of leaving the pane")
	}
}

// Nothing is buffered where a key is a command rather than a character: the host list
// answers a keystroke at once.
func TestNavigationKeysAreNeverBuffered(t *testing.T) {
	m, _ := pasteModel()
	m.mode = modeList

	if m.takeKey(typed('j')) {
		t.Fatal("a key in the host list was held back")
	}
	m.mode = modeScrollback
	if m.takeKey(typed('j')) {
		t.Fatal("a key in scrollback was held back")
	}
	m.mode = modeShell
	if m.takeKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'0'}, Alt: true}) {
		t.Fatal("alt+0 was held back: a modified key is a command, not a character")
	}
}

// The flush only fires for the burst it was armed for. A key arriving in the gap
// arms another, and the stale one must not cut the burst in half.
func TestStaleFlushIsIgnored(t *testing.T) {
	m, stdin := pasteModel()
	newTestClock(m) // frozen: every key after the first is inside burstGap, as a paste is

	m.handleKey(typed('a')) // opens the burst, and goes straight out
	m.handleKey(typed('b'))
	stale := pasteFlushMsg{seq: m.paste.seq}
	m.handleKey(typed('c'))

	m.update(stale)
	flushPanes(m)
	if got := stdin.String(); got != "a" {
		t.Fatalf("a stale flush sent %q on top of the burst's first key", got)
	}

	m.update(pasteFlushMsg{seq: m.paste.seq})
	flushPanes(m)
	if got := stdin.String(); got != "abc" {
		t.Fatalf("the live flush sent %q, want the whole burst", got)
	}
}

// A card that opens under a burst takes its own keys. Both cards that open by themselves
// arrive from a dial that may have started in another host's shell, so holding those keys
// back would deliver a verification code to the shell behind the card.
func TestKeysAreNotBufferedUnderACard(t *testing.T) {
	m, stdin := pasteModel()
	m.auth = authUI{open: true, answers: []string{""}}

	for _, r := range "123" {
		m.handleKey(typed(r))
	}

	if got := m.auth.answers[0]; got != "123" {
		t.Fatalf("the card holds %q, want the typed code", got)
	}
	flushPanes(m)
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

// A password copied out of a manager brings the newline along. The field takes the line,
// not the line ending, which would otherwise submit a value that does not match.
func TestPasteIntoACardTakesOneLine(t *testing.T) {
	m := &model{auth: authUI{open: true, answers: []string{""}}}
	m.handleKey(pasted("hunter2\nsecond line"))

	if got := m.auth.answers[0]; got != "hunter2" {
		t.Fatalf("the field holds %q, want just the first line", got)
	}
}

// Into a text field a paste is text, including one that spells the field's own keys:
// "esc" pasted into the filter or the host form is four characters.
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

// Without a newline it takes a run of characters, not all the same, before a burst reads
// as a paste — ruling out both fast-typing shapes. Being wrong is not symmetric: a short
// paste replayed as keystrokes types what was pasted, while "dw" sent as a paste is
// inserted by vim instead of deleting a word.
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

// The whole point of the gate: a key typed at human speed is never held. Every keystroke
// into a remote shell is on the wire before the next one is read, and none of them arms a
// flush that would have to expire first.
func TestTypingIsNeverHeldBack(t *testing.T) {
	m, stdin := pasteModel()
	clk := newTestClock(m)

	for i, r := range []rune{'l', 's'} {
		clk.advance(80 * time.Millisecond) // a hand between two keys
		if _, cmd := m.handleKey(typed(r)); cmd != nil {
			t.Fatalf("key %d armed a flush: typing was buffered", i)
		}
		flushPanes(m)
		if got, want := stdin.String(), string([]rune{'l', 's'}[:i+1]); got != want {
			t.Fatalf("the pane received %q after key %d, want %q at once", got, i, want)
		}
	}
}

// A key arriving faster than a hand can produce one belongs to a paste, and is buffered
// even where the key ahead of it was typed: that is how a paste into a shell you were
// just typing in is still recognised.
func TestKeysFasterThanAHandAreBuffered(t *testing.T) {
	m, stdin := pasteModel()
	clk := newTestClock(m)

	clk.advance(time.Second)
	m.handleKey(typed('x')) // typed, so it goes straight out
	clk.advance(burstGap / 4)
	if _, cmd := m.handleKey(typed('y')); cmd == nil {
		t.Fatal("a key inside burstGap did not arm a flush: it was not buffered")
	}
	flushPanes(m)
	if got := stdin.String(); got != "x" {
		t.Fatalf("the pane received %q, want only the typed key so far", got)
	}

	m.flushPaste()
	flushPanes(m)
	if got := stdin.String(); got != "xy" {
		t.Fatalf("the pane received %q, want the buffered key after it", got)
	}
}

// Enter is held even when nothing preceded it, because it is the one character whose
// early delivery costs something: a clipboard beginning with a newline would submit the
// line it was pasted at. The delay is pasteGap, and what comes out the other end is
// still the keystroke.
func TestEnterIsBufferedEvenAfterAPause(t *testing.T) {
	m, stdin := pasteModel()
	clk := newTestClock(m)

	clk.advance(time.Second) // nothing was typed for a second
	_, cmd := m.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("Enter did not arm a flush: it went out unbuffered")
	}
	flushPanes(m)
	if got := stdin.String(); got != "" {
		t.Fatalf("the pane received %q before the flush", got)
	}

	m.flushPaste()
	flushPanes(m)
	if got := stdin.String(); got != "\r" {
		t.Fatalf("the pane received %q, want the bare CR a typed Enter sends", got)
	}
}
