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

// stalledStdin is a far end that never reads: every write parks forever, which is what an
// SSH channel does once the remote's window is exhausted.
type stalledStdin struct{ gate sync.WaitGroup }

func newStalledStdin() *stalledStdin {
	s := &stalledStdin{}
	s.gate.Add(1)
	return s
}

func (s *stalledStdin) Write(p []byte) (int, error) {
	s.gate.Wait()
	return len(p), nil
}

func (s *stalledStdin) Close() error { return nil }

var _ io.WriteCloser = (*stalledStdin)(nil)

// A pane holds a bounded amount of input for a far end that has stopped reading; past
// that it refuses, and the refusal has to reach the user. Silently dropped keystrokes
// leave a command line that is missing characters from its middle — worse than the delay
// the queue exists to avoid, and invisible until the wrong command runs.
func TestDroppedInputIsReported(t *testing.T) {
	sess := &sshx.Session{Stdin: newStalledStdin(), Stdout: strings.NewReader("")}
	pane := terminal.New(sess, 20, 5, nil)
	m := &model{
		sessions:      map[string]*session{"web": {shells: []*shellTab{{id: 1, pane: pane}}}},
		active:        "web",
		mode:          modeShell,
		pasteCoalesce: true,
	}
	clk := newTestClock(m)

	for i := 0; i < 20000 && m.status == ""; i++ {
		clk.advance(80 * time.Millisecond) // typed, so every key goes straight out
		m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}})
	}

	if m.status == "" {
		t.Fatal("a far end that read nothing at all never produced a warning: input went missing silently")
	}
	if m.statusKind != statusWarn {
		t.Fatalf("dropped input was reported as kind %v, want a warning", m.statusKind)
	}
	if !strings.Contains(m.status, "web") {
		t.Fatalf("the warning is %q, want it to name the host it is about", m.status)
	}
}
