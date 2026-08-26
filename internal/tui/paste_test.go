package tui

import (
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"hop/internal/sshx"
	"hop/internal/terminal"
)

// pastePane builds a pane whose input can be read back.
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

// testClock drives burst detection, which reads the wall clock to tell typing from a paste.
type testClock struct{ t time.Time }

// newTestClock installs a clock on m, started past the zero time.
func newTestClock(m *model) *testClock {
	c := &testClock{t: time.Unix(1, 0)}
	m.clock = func() time.Time { return c.t }
	return c
}

// advance moves the clock on, standing in for the gap between two keys.
func (c *testClock) advance(d time.Duration) { c.t = c.t.Add(d) }

// flushPanes waits for what the model queued to reach the panes' sessions.
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

// pasted is the marked, single-event paste a bracketed-paste terminal delivers.
func pasted(text string) tea.PasteMsg {
	return tea.PasteMsg{Content: text}
}

// typed is one typed (or synthesised) character.
func typed(r rune) tea.KeyPressMsg {
	return tea.KeyPressMsg{Code: r, Text: string(r)}
}

// A marked paste reaches the pane as text, not as the bindings its characters spell.
func TestMarkedPasteGoesToTheShell(t *testing.T) {
	m, stdin := pasteModel()
	m.Update(pasted("echo hi\n"))

	flushPanes(m)
	if got := stdin.String(); got != "echo hi\r" {
		t.Fatalf("the pane received %q, want the pasted text", got)
	}
}

// The same in an editor tab, whose handler binds letters of its own.
func TestMarkedPasteGoesToTheEditor(t *testing.T) {
	pane, stdin := pastePane()
	m := &model{sessions: map[string]*session{"web": {editors: []*editorTab{{id: 1, name: "f", pane: pane}}}}, focus: focus{active: "web", mode: modeEditor}}
	m.Update(pasted("line one\nline two"))

	flushPanes(m)
	if got := stdin.String(); got != "line one\rline two" {
		t.Fatalf("the editor received %q, want the pasted text", got)
	}
}

func TestPasteFromScrollbackReturnsLiveFirst(t *testing.T) {
	m, stdin := pasteModel()
	m.mode = modeScrollback

	m.Update(pasted("ls"))
	if m.scrolling() {
		t.Fatal("a paste left the shell in scrollback")
	}
	flushPanes(m)
	if got := stdin.String(); got != "ls" {
		t.Fatalf("the pane received %q, want the pasted text", got)
	}
}

// Windows synthesises a paste as a burst; one carrying a newline is a paste, bar its first key.
func TestBurstWithANewlineIsAPaste(t *testing.T) {
	m, stdin := pasteModel()
	newTestClock(m) // frozen: every key after the first is inside burstGap, as a paste is

	for i, k := range []tea.KeyPressMsg{typed('a'), {Code: tea.KeyEnter}, typed('b')} {
		_, cmd := m.handleKey(k)
		if i > 0 && cmd == nil {
			t.Fatal("a bufferable key did not arm the flush")
		}
	}
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

// Guards the v0.5.0 regression: a lone Enter read as a paste went out bracketed and ran nothing.
func TestALoneEnterIsAKeystrokeNotAPaste(t *testing.T) {
	m, stdin := pasteModel()
	newTestClock(m) // frozen: every key after the first is inside burstGap, as a paste is

	if looksPasted([]tea.KeyPressMsg{{Code: tea.KeyEnter}}) {
		t.Fatal("a lone Enter was taken for a paste")
	}

	m.handleKey(tea.KeyPressMsg{Code: tea.KeyEnter})
	m.flushPaste()
	flushPanes(m)
	if got := stdin.String(); got != "\r" {
		t.Fatalf("the pane received %q, want the bare CR a typed Enter sends", got)
	}
}

// A held key repeats as fast as a paste and is replayed as keystrokes.
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
	if looksPasted([]tea.KeyPressMsg{typed('j'), typed('j'), typed('j')}) {
		t.Fatal("a repeating key was taken for a paste")
	}
}

func TestAnUnbufferableKeyFlushesFirst(t *testing.T) {
	m, stdin := pasteModel()
	newTestClock(m) // frozen: every key after the first is inside burstGap, as a paste is

	m.handleKey(typed('h'))
	m.handleKey(typed('i'))
	m.handleKey(tea.KeyPressMsg{Code: 'o', Mod: tea.ModCtrl})
	m.handleKey(tea.KeyPressMsg{Code: 'o', Text: "o"})

	flushPanes(m)
	if got := stdin.String(); got != "hi" {
		t.Fatalf("the pane received %q, want the buffered keys", got)
	}
	if m.focused() {
		t.Fatal("ctrl+o was swallowed by the flush instead of leaving the pane")
	}
}

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
	if m.takeKey(tea.KeyPressMsg{Code: '0', Mod: tea.ModAlt}) {
		t.Fatal("alt+0 was held back: a modified key is a command, not a character")
	}
}

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

// Holding keys back under a card would deliver a verification code to the shell behind it.
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

func TestPasteInTheHostListIsDropped(t *testing.T) {
	m := &model{sessions: map[string]*session{}, highlights: map[int][]int{}}
	if _, cmd := m.Update(pasted("q")); cmd != nil {
		t.Fatal("a pasted \"q\" in the host list quit hop")
	}
}

// A password copied with its newline must not submit the field.
func TestPasteIntoACardTakesOneLine(t *testing.T) {
	m := &model{auth: authUI{open: true, answers: []string{""}}}
	m.Update(pasted("hunter2\nsecond line"))

	if got := m.auth.answers[0]; got != "hunter2" {
		t.Fatalf("the field holds %q, want just the first line", got)
	}
}

func TestPasteIntoTextFieldsIsText(t *testing.T) {
	m := &model{hosts: nil, highlights: map[int][]int{}, filtering: true}
	m.Update(pasted("esc"))
	if m.filter != "esc" || !m.filtering {
		t.Fatalf("filter = %q, filtering = %v — want the pasted text kept", m.filter, m.filtering)
	}

	m2 := &model{}
	m2.openHostFormAdd()
	m2.Update(pasted("esc"))
	if !m2.hostForm.open {
		t.Fatal("a pasted \"esc\" closed the host form")
	}
	if got := m2.hostForm.buf[m2.hostForm.cursor]; got != "esc" {
		t.Fatalf("the focused field holds %q, want the pasted text", got)
	}
}

// What the buffered keys spell.
func TestPasteString(t *testing.T) {
	keys := []tea.KeyPressMsg{
		typed('a'), {Code: tea.KeySpace, Text: " "}, typed('b'),
		{Code: tea.KeyEnter}, {Code: tea.KeyTab}, typed('c'),
	}
	if got := pasteString(keys); got != "a b\n\tc" {
		t.Fatalf("pasteString = %q, want %q", got, "a b\n\tc")
	}
}

// Without a newline a burst needs a run of differing keys before it reads as a paste.
func TestABurstWithNoNewlineNeedsARun(t *testing.T) {
	if looksPasted([]tea.KeyPressMsg{typed('d'), typed('w')}) {
		t.Fatal("a fast digraph was taken for a paste")
	}
	if looksPasted([]tea.KeyPressMsg{typed('l')}) {
		t.Fatal("a single keystroke was taken for a paste")
	}
	if looksPasted([]tea.KeyPressMsg{typed('j'), typed('j'), typed('j'), typed('j'), typed('j')}) {
		t.Fatal("a key held down until it repeated was taken for a paste")
	}
	if !looksPasted([]tea.KeyPressMsg{typed('l'), typed('s'), typed(' '), typed('-'), typed('l')}) {
		t.Fatal("a pasted command line was not taken for a paste")
	}
}

func TestFlushWithoutAPaneIsANoOp(t *testing.T) {
	m := &model{sessions: map[string]*session{}, pasteCoalesce: true}
	m.flushPaste() // nothing buffered
	m.paste.keys = []tea.KeyPressMsg{typed('a')}
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

// A key typed at human speed goes out at once and arms no flush.
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

// A key inside burstGap is buffered even where the key ahead of it was typed.
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

// Enter is held alone: a clipboard beginning with a newline would submit the line.
func TestEnterIsBufferedEvenAfterAPause(t *testing.T) {
	m, stdin := pasteModel()
	clk := newTestClock(m)

	clk.advance(time.Second) // nothing was typed for a second
	_, cmd := m.handleKey(tea.KeyPressMsg{Code: tea.KeyEnter})
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
