package terminal

import (
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"hop/internal/sshx"
)

// stalledWriter is a far end that has stopped reading: every write parks until released.
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

// SendKey runs on the update goroutine; regression: a blocked write used to freeze the TUI.
func TestSendKeyDoesNotWaitForTheWire(t *testing.T) {
	w := newStalledWriter()
	p := New(&sshx.Session{Stdin: w, Stdout: strings.NewReader("")}, 80, 24, nil)
	defer p.Close()

	typed := make(chan struct{})
	go func() {
		for i := 0; i < 50; i++ {
			p.SendKey(tea.KeyPressMsg{Code: 'x', Text: "x"})
		}
		close(typed)
	}()

	select {
	case <-typed:
	case <-time.After(5 * time.Second):
		t.Fatal("SendKey is still waiting for a stalled far end: the UI would be frozen with it")
	}

	// Nothing was lost: it goes out, in order, once the link comes back.
	w.release()
	p.Flush()
	if got, want := w.String(), strings.Repeat("x", 50); got != want {
		t.Fatalf("the far end received %q, want %q", got, want)
	}
}

// Keystrokes and hop's own writes cannot interleave halfway through a sequence.
func TestQueuedInputKeepsItsOrder(t *testing.T) {
	stdin := &syncBuf{}
	p := New(&sshx.Session{Stdin: stdin, Stdout: strings.NewReader("")}, 80, 24, nil)
	defer p.Close()

	p.SendKey(tea.KeyPressMsg{Code: 'a', Text: "a"})
	p.writeString("bc")
	p.SendPaste("d")
	p.SendKey(tea.KeyPressMsg{Code: 'e', Text: "e"})
	p.Flush()

	if got := stdin.String(); got != "abcde" {
		t.Fatalf("the far end received %q, want the writes in the order they were made", got)
	}
}

func TestFlushReturnsOnAClosedPane(t *testing.T) {
	w := newStalledWriter()
	p := New(&sshx.Session{Stdin: w, Stdout: strings.NewReader("")}, 80, 24, nil)

	p.SendKey(tea.KeyPressMsg{Code: 'x', Text: "x"})
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

// A failed write must not end the drain, leaving a pane that looks alive and eats keys.
func TestAFailedWriteDoesNotStopTheQueue(t *testing.T) {
	w := &flakyWriter{}
	p := New(&sshx.Session{Stdin: w, Stdout: strings.NewReader("")}, 80, 24, nil)
	defer p.Close()

	w.fail(true)
	p.SendKey(tea.KeyPressMsg{Code: 'a', Text: "a"})
	p.Flush()

	w.fail(false)
	p.SendKey(tea.KeyPressMsg{Code: 'b', Text: "b"})
	p.Flush()

	if got := w.String(); got != "ab" {
		t.Fatalf("the far end received %q, want the key that followed the failed write", got)
	}
}

// flakyWriter records what it is given and can be told to report a failure for it.
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

// The queue is bounded and the refusal is reported, so keys never vanish silently.
func TestAFullQueueRefusesInput(t *testing.T) {
	w := newStalledWriter()
	p := New(&sshx.Session{Stdin: w, Stdout: strings.NewReader("")}, 80, 24, nil)
	defer p.Close()

	key := tea.KeyPressMsg{Code: 'x', Text: "x"}
	taken := 0
	for i := 0; i < inQueue*2; i++ {
		if !p.SendKey(key) {
			break
		}
		taken++
	}
	if taken == 0 || taken >= inQueue*2 {
		t.Fatalf("%d keys were taken by a far end reading none of them, want a bounded queue's worth", taken)
	}

	w.release()
	p.Flush()
	if got, want := w.String(), strings.Repeat("x", taken); got != want {
		t.Fatalf("the far end received %d bytes, want the %d keys the queue took", len(got), len(want))
	}
}
