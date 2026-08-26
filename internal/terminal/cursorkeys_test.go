package terminal

import (
	"io"
	"testing"

	tea "charm.land/bubbletea/v2"

	"hop/internal/sshx"
)

// A full-screen program asks for SS3 cursor keys and hop must follow, or its arrows read as
// literal text in vim, less and mc.
func TestCursorKeysFollowTheRemote(t *testing.T) {
	out, w := io.Pipe()
	stdin := &syncBuf{}
	p := New(&sshx.Session{Stdin: stdin, Stdout: out}, 80, 24, nil)
	defer p.Close()

	if p.AppCursorKeys() {
		t.Fatal("a fresh shell claims a program asked for application cursor keys")
	}
	p.SendKey(tea.KeyPressMsg{Code: tea.KeyUp})
	p.Flush()
	if got := stdin.String(); got != "\x1b[A" {
		t.Fatalf("up wrote %q, want the CSI form %q", got, "\x1b[A")
	}

	go io.WriteString(w, "\x1b[?1h")
	if !waitFor(p.AppCursorKeys) {
		t.Fatal("a program asking for application cursor keys was not noticed")
	}

	stdin.reset()
	p.SendKey(tea.KeyPressMsg{Code: tea.KeyUp})
	p.Flush()
	if got := stdin.String(); got != "\x1bOA" {
		t.Fatalf("up wrote %q, want the SS3 form %q", got, "\x1bOA")
	}

	// A modified cursor key is xterm's CSI form in either mode.
	stdin.reset()
	p.SendKey(tea.KeyPressMsg{Code: tea.KeyLeft, Mod: tea.ModCtrl})
	p.Flush()
	if got := stdin.String(); got != "\x1b[1;5D" {
		t.Fatalf("ctrl+left wrote %q under DECCKM, want %q", got, "\x1b[1;5D")
	}

	go io.WriteString(w, "\x1b[?1l")
	if !waitFor(func() bool { return !p.AppCursorKeys() }) {
		t.Fatal("the program giving the mode back was not noticed")
	}

	stdin.reset()
	p.SendKey(tea.KeyPressMsg{Code: tea.KeyHome})
	p.Flush()
	if got := stdin.String(); got != "\x1b[H" {
		t.Fatalf("home wrote %q after the mode was dropped, want %q", got, "\x1b[H")
	}
}

// Leaving the alt screen drops the modes of the program that owned it, DECCKM among them:
// vim exiting must not leave the shell behind it receiving SS3 arrows.
func TestLeavingTheAltScreenDropsApplicationCursorKeys(t *testing.T) {
	out, w := io.Pipe()
	stdin := &syncBuf{}
	p := New(&sshx.Session{Stdin: stdin, Stdout: out}, 80, 24, nil)
	defer p.Close()

	go io.WriteString(w, "\x1b[?1049h\x1b[?1h")
	if !waitFor(p.AppCursorKeys) {
		t.Fatal("the mode was not picked up inside the alt screen")
	}

	go io.WriteString(w, "\x1b[?1049l")
	if !waitFor(func() bool { return !p.AppCursorKeys() }) {
		t.Fatal("leaving the alt screen left hop sending SS3 arrows to the shell")
	}
}
