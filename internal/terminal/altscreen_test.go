package terminal

import (
	"io"
	"testing"
	"time"

	"hop/internal/sshx"
)

// nopWriteCloser adapts a plain writer to the io.WriteCloser a Session wants.
type nopWriteCloser struct{ io.Writer }

func (nopWriteCloser) Close() error { return nil }

// AltScreen is what the scrollback chords ask before taking a key.
func TestAltScreen(t *testing.T) {
	out, w := io.Pipe()
	p := New(&sshx.Session{Stdin: nopWriteCloser{io.Discard}, Stdout: out}, 20, 5, nil)
	defer p.Close()

	if p.AltScreen() {
		t.Fatal("a fresh shell reports the alt screen")
	}

	// DECSET 1049: what vim, htop and less send on the way in and out.
	go io.WriteString(w, "\x1b[?1049h")
	if !waitFor(func() bool { return p.AltScreen() }) {
		t.Fatal("a program taking the alt screen was not noticed")
	}

	go io.WriteString(w, "\x1b[?1049l")
	if !waitFor(func() bool { return !p.AltScreen() }) {
		t.Fatal("quitting back to the shell left hop thinking the alt screen was up")
	}
}

// waitFor polls cond until it holds: the emulator is fed by a goroutine.
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
