package terminal

import (
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"hop/internal/sshx"
)

// stalledWriter is a far end that has stopped reading: every write parks until the test
// lets it go. An SSH channel does exactly this once the remote's window is full.
type stalledWriter struct {
	mu   sync.Mutex
	b    []byte
	gate chan struct{}
}

func newStalledWriter() *stalledWriter {
	return &stalledWriter{gate: make(chan struct{})}
}

func (w *stalledWriter) Write(p []byte) (int, error) {
	<-w.gate
	w.mu.Lock()
	defer w.mu.Unlock()
	w.b = append(w.b, p...)
	return len(p), nil
}

func (w *stalledWriter) Close() error { return nil }

func (w *stalledWriter) release() { close(w.gate) }

func (w *stalledWriter) String() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return string(w.b)
}

// The reason input is queued at all: SendKey runs on Bubble Tea's update goroutine, and a
// far end that has stopped reading must not hold it there. A blocked write used to freeze
// the whole TUI — no repaint, no other key — for as long as the link was stalled.
func TestSendKeyDoesNotWaitForTheWire(t *testing.T) {
	w := newStalledWriter()
	p := New(&sshx.Session{Stdin: w, Stdout: strings.NewReader("")}, 80, 24, nil)
	defer p.Close()

	typed := make(chan struct{})
	go func() {
		for i := 0; i < 50; i++ {
			p.SendKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}})
		}
		close(typed)
	}()

	select {
	case <-typed:
	case <-time.After(5 * time.Second):
		t.Fatal("SendKey is still waiting for a stalled far end: the UI would be frozen with it")
	}

	// Nothing was lost by not waiting: it goes out, in order, once the link comes back.
	w.release()
	p.Flush()
	if got, want := w.String(), strings.Repeat("x", 50); got != want {
		t.Fatalf("the far end received %q, want %q", got, want)
	}
}

// One queue is also what keeps the order a mutex used to keep: a keystroke and hop's own
// writes cannot interleave halfway through a sequence.
func TestQueuedInputKeepsItsOrder(t *testing.T) {
	stdin := &syncBuf{}
	p := New(&sshx.Session{Stdin: stdin, Stdout: strings.NewReader("")}, 80, 24, nil)
	defer p.Close()

	p.SendKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}})
	p.writeString("bc")
	p.SendPaste("d")
	p.SendKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'e'}})
	p.Flush()

	if got := stdin.String(); got != "abcde" {
		t.Fatalf("the far end received %q, want the writes in the order they were made", got)
	}
}

// A closed pane sends nothing further, and Flush on one returns rather than waiting for a
// session that will never take it.
func TestFlushReturnsOnAClosedPane(t *testing.T) {
	w := newStalledWriter()
	p := New(&sshx.Session{Stdin: w, Stdout: strings.NewReader("")}, 80, 24, nil)

	p.SendKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}})
	_ = p.Close()

	done := make(chan struct{})
	go func() {
		p.Flush()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Flush hung on a closed pane")
	}
}

// A failed write does not end the input path. The session may well still be usable, and
// a pane whose drain has quit is one that looks alive and swallows every key from then
// on — the keys queue until the queue fills and are then dropped, and Flush never
// returns.
func TestAFailedWriteDoesNotStopTheQueue(t *testing.T) {
	w := &flakyWriter{}
	p := New(&sshx.Session{Stdin: w, Stdout: strings.NewReader("")}, 80, 24, nil)
	defer p.Close()

	w.fail(true)
	p.SendKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}})
	p.Flush()

	w.fail(false)
	p.SendKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'b'}})
	p.Flush()

	if got := w.String(); got != "ab" {
		t.Fatalf("the far end received %q, want the key that followed the failed write", got)
	}
}

// flakyWriter records what it is given and can be told to report a failure for it, the
// way a session does over a link that comes and goes.
type flakyWriter struct {
	mu      sync.Mutex
	b       []byte
	failing bool
}

func (w *flakyWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.b = append(w.b, p...)
	if w.failing {
		return 0, errors.New("write: broken pipe")
	}
	return len(p), nil
}

func (w *flakyWriter) Close() error { return nil }

func (w *flakyWriter) fail(on bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.failing = on
}

func (w *flakyWriter) String() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return string(w.b)
}
