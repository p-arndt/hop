package terminal

import (
	"io"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"hop/internal/sshx"
)

// nopWriteCloser adapts a plain writer to the io.WriteCloser a Session wants.
type nopWriteCloser struct{ io.Writer }

func (nopWriteCloser) Close() error { return nil }

// inputPane builds a Pane over a session with nothing behind it: input goes
// nowhere and there is no output. LineEmpty is answered entirely from the keys hop
// sends, so that is all it needs.
func inputPane() *Pane {
	sess := &sshx.Session{
		Stdin:  nopWriteCloser{io.Discard},
		Stdout: strings.NewReader(""),
	}
	return New(sess, 20, 5, nil)
}

// runes is the KeyMsg for typing s, one key per character.
func runes(s string) []tea.KeyMsg {
	var keys []tea.KeyMsg
	for _, r := range s {
		keys = append(keys, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	return keys
}

// LineEmpty follows what the forwarded keys do to the input line: typing fills it,
// enter and the kill keys empty it, backspace walks it back one character at a
// time. It is the whole basis for hop binding ← at a prompt, so it is pinned here.
func TestLineEmpty(t *testing.T) {
	tests := []struct {
		name string
		keys []tea.KeyMsg
		want bool
	}{
		{"a fresh shell is at a bare prompt", nil, true},
		{"typing fills the line", runes("ls"), false},
		{"enter sends it, leaving a fresh prompt",
			append(runes("ls"), tea.KeyMsg{Type: tea.KeyEnter}), true},
		{"ctrl+c abandons the line",
			append(runes("ls"), tea.KeyMsg{Type: tea.KeyCtrlC}), true},
		{"ctrl+u kills it back to the start",
			append(runes("ls"), tea.KeyMsg{Type: tea.KeyCtrlU}), true},
		{"backspace walks it back",
			append(runes("ls"),
				tea.KeyMsg{Type: tea.KeyBackspace},
				tea.KeyMsg{Type: tea.KeyBackspace}), true},
		{"...but not past empty: the count never goes negative",
			append(runes("l"),
				tea.KeyMsg{Type: tea.KeyBackspace},
				tea.KeyMsg{Type: tea.KeyBackspace},
				tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}}), false},
		{"a space is a character like any other",
			append(runes("ls"), tea.KeyMsg{Type: tea.KeySpace}), false},
		{"tab completes something onto the line",
			[]tea.KeyMsg{{Type: tea.KeyTab}}, false},
		{"the arrows only move within the line",
			append(runes("ls"), tea.KeyMsg{Type: tea.KeyLeft}), false},
		{"...and moving within an empty one leaves it empty",
			[]tea.KeyMsg{{Type: tea.KeyLeft}}, true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			p := inputPane()
			defer p.Close()

			for _, k := range tc.keys {
				p.SendKey(k)
			}
			if got := p.LineEmpty(); got != tc.want {
				t.Fatalf("LineEmpty() = %v, want %v", got, tc.want)
			}
		})
	}
}

// AltScreen is the other half of the question: a full-screen program owns the whole
// keyboard, and hop must not take ← from it even at a "bare" line — vim and htop are
// not typing a line at all.
func TestAltScreen(t *testing.T) {
	out, w := io.Pipe()
	p := New(&sshx.Session{Stdin: nopWriteCloser{io.Discard}, Stdout: out}, 20, 5, nil)
	defer p.Close()

	if p.AltScreen() {
		t.Fatal("a fresh shell reports the alt screen")
	}

	// What vim, htop and less send on the way in (DECSET 1049), and on the way out.
	go io.WriteString(w, "\x1b[?1049h")
	if !waitFor(func() bool { return p.AltScreen() }) {
		t.Fatal("a program taking the alt screen was not noticed")
	}

	go io.WriteString(w, "\x1b[?1049l")
	if !waitFor(func() bool { return !p.AltScreen() }) {
		t.Fatal("quitting back to the shell left hop thinking the alt screen was up")
	}
}

// waitFor polls cond until it holds or the timeout elapses: the emulator is fed by
// a goroutine, so the state a write produces arrives a moment after the write.
func waitFor(cond func() bool) bool {
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return true
		}
		time.Sleep(10 * time.Millisecond)
	}
	return cond()
}
