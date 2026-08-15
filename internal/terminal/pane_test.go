package terminal

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/binary"
	"io"
	"net"
	"runtime"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"golang.org/x/crypto/ssh"

	"hop/internal/sshx"
)

// newSigner generates a fresh ed25519 ssh.Signer for use as either a host key
// or a client key in the in-process test server.
func newSigner(t *testing.T) ssh.Signer {
	t.Helper()
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate ed25519 key: %v", err)
	}
	signer, err := ssh.NewSignerFromSigner(priv)
	if err != nil {
		t.Fatalf("new signer: %v", err)
	}
	return signer
}

// startTestServer spins up an in-process SSH server on 127.0.0.1:0 that accepts any
// public key, answers pty-req and shell, writes a banner and echoes what it receives. It
// returns the listener address and runs until t.Cleanup closes it.
func startTestServer(t *testing.T, hostKey ssh.Signer) string {
	t.Helper()

	cfg := &ssh.ServerConfig{
		PublicKeyCallback: func(ssh.ConnMetadata, ssh.PublicKey) (*ssh.Permissions, error) {
			return nil, nil
		},
	}
	cfg.AddHostKey(hostKey)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { ln.Close() })

	go func() {
		for {
			nc, err := ln.Accept()
			if err != nil {
				return // listener closed
			}
			go serveConn(nc, cfg)
		}
	}()

	return ln.Addr().String()
}

// serveConn performs the SSH handshake and services session channels.
func serveConn(nc net.Conn, cfg *ssh.ServerConfig) {
	sc, chans, reqs, err := ssh.NewServerConn(nc, cfg)
	if err != nil {
		nc.Close()
		return
	}
	defer sc.Close()
	go ssh.DiscardRequests(reqs)

	for newCh := range chans {
		if newCh.ChannelType() != "session" {
			newCh.Reject(ssh.UnknownChannelType, "only session supported")
			continue
		}
		ch, chReqs, err := newCh.Accept()
		if err != nil {
			return
		}
		go handleSession(ch, chReqs)
	}
}

// handleSession accepts pty-req plus shell and exec, writes a banner and echoes input.
// exec echoes the command line it was given, which is how the editor path is checked.
func handleSession(ch ssh.Channel, reqs <-chan *ssh.Request) {
	for req := range reqs {
		switch req.Type {
		case "pty-req":
			if req.WantReply {
				req.Reply(true, nil)
			}
		case "shell":
			if req.WantReply {
				req.Reply(true, nil)
			}
			go banneredEcho(ch, "HOPTEST-READY")
		case "exec":
			if req.WantReply {
				req.Reply(true, nil)
			}
			go banneredEcho(ch, "HOPEXEC:"+execPayload(req.Payload))
		default:
			if req.WantReply {
				req.Reply(false, nil)
			}
		}
	}
}

// execPayload extracts the command string from an exec request: RFC 4254 encodes
// it as a uint32 length followed by the bytes.
func execPayload(p []byte) string {
	if len(p) < 4 {
		return ""
	}
	n := binary.BigEndian.Uint32(p[:4])
	if int(n) > len(p)-4 {
		return ""
	}
	return string(p[4 : 4+n])
}

// banneredEcho writes banner, then echoes everything it receives back.
func banneredEcho(ch ssh.Channel, banner string) {
	io.WriteString(ch, banner+"\r\n")
	buf := make([]byte, 1024)
	for {
		n, err := ch.Read(buf)
		if n > 0 {
			ch.Write(buf[:n])
		}
		if err != nil {
			ch.Close()
			return
		}
	}
}

// waitForView polls term.View() until it contains want or the timeout elapses.
func waitForView(term *Pane, want string, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if strings.Contains(term.View(), want) {
			return true
		}
		time.Sleep(20 * time.Millisecond)
	}
	return strings.Contains(term.View(), want)
}

// TestEmbeddedRoundTrip proves the embedded-terminal chain end to end without an external
// sshd: SSH server -> sshx.Session -> terminal.Pane, keystroke round-trip included.
func TestEmbeddedRoundTrip(t *testing.T) {
	hostKey := newSigner(t)
	clientKey := newSigner(t)

	addr := startTestServer(t, hostKey)

	cfg := &ssh.ClientConfig{
		User:            "tester",
		Auth:            []ssh.AuthMethod{ssh.PublicKeys(clientKey)},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         5 * time.Second,
	}

	cli, err := sshx.ConnectAddr(addr, cfg)
	if err != nil {
		t.Fatalf("ConnectAddr: %v", err)
	}
	defer cli.Close()

	sess, err := cli.Shell(80, 24)
	if err != nil {
		t.Fatalf("Shell: %v", err)
	}

	term := New(sess, 80, 24, nil)
	defer term.Close()

	if !waitForView(term, "HOPTEST-READY", 3*time.Second) {
		t.Fatalf("banner never appeared in emulator; view:\n%s", term.View())
	}

	// Keystroke round-trip: send 'Z', expect the server echo to surface it.
	term.SendKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'Z'}})

	if !waitForView(term, "Z", 3*time.Second) {
		t.Fatalf("echoed keystroke 'Z' never appeared in emulator; view:\n%s", term.View())
	}
}

// Closing a pane while its pumps are running must be quiet and complete: the two
// goroutines New starts go away, and nothing touches the emulator on the way out.
//
// hop closes panes constantly, so one that left its pumps behind would bleed a goroutine
// and a live session per shell. The tear-down was also a data race (see Pane.Close): the
// response pump sits inside emu.Read reading the flag emu.Close() writes. CI runs this
// under -race, which is what makes that half mean anything.
func TestCloseStopsThePumps(t *testing.T) {
	hostKey := newSigner(t)
	clientKey := newSigner(t)
	addr := startTestServer(t, hostKey)

	cfg := &ssh.ClientConfig{
		User:            "tester",
		Auth:            []ssh.AuthMethod{ssh.PublicKeys(clientKey)},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         5 * time.Second,
	}
	cli, err := sshx.ConnectAddr(addr, cfg)
	if err != nil {
		t.Fatalf("ConnectAddr: %v", err)
	}
	defer cli.Close()

	sess, err := cli.Shell(80, 24)
	if err != nil {
		t.Fatalf("Shell: %v", err)
	}

	// Everything the pane will start is started after this count is taken.
	before := runtime.NumGoroutine()

	term := New(sess, 80, 24, nil)
	if !waitForView(term, "HOPTEST-READY", 3*time.Second) {
		t.Fatalf("the shell never came up; view:\n%s", term.View())
	}
	// The pumps are demonstrably live: this keystroke went out through one and its
	// echo came back through the other.
	term.SendKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'Z'}})
	if !waitForView(term, "Z", 3*time.Second) {
		t.Fatalf("the pane is not pumping; view:\n%s", term.View())
	}

	if err := term.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// The goroutines are torn down asynchronously, so this is a wait rather than a
	// reading: what is being asserted is that they end, not when.
	deadline := time.Now().Add(3 * time.Second)
	for runtime.NumGoroutine() > before && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if after := runtime.NumGoroutine(); after > before {
		t.Fatalf("goroutines: %d before the pane, %d after closing it — its pumps outlived it",
			before, after)
	}
}

// TestCommandPaneRoundTrip is the editor-pane chain: sshx.Command runs a program on a
// remote pty and terminal.Pane renders it. The TUI's editor tabs are this with "vi
// <file>" as the command.
func TestCommandPaneRoundTrip(t *testing.T) {
	hostKey := newSigner(t)
	clientKey := newSigner(t)

	addr := startTestServer(t, hostKey)

	cfg := &ssh.ClientConfig{
		User:            "tester",
		Auth:            []ssh.AuthMethod{ssh.PublicKeys(clientKey)},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         5 * time.Second,
	}

	cli, err := sshx.ConnectAddr(addr, cfg)
	if err != nil {
		t.Fatalf("ConnectAddr: %v", err)
	}
	defer cli.Close()

	sess, err := cli.Command("vi /etc/hosts", 80, 24)
	if err != nil {
		t.Fatalf("Command: %v", err)
	}

	term := New(sess, 80, 24, nil)
	defer term.Close()

	// The server echoes back the command line it was asked to exec.
	if !waitForView(term, "HOPEXEC:vi /etc/hosts", 3*time.Second) {
		t.Fatalf("exec'd command never surfaced in emulator; view:\n%s", term.View())
	}

	// The pane drives it like any other: keys go to the running program.
	term.SendKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'i'}})
	if !waitForView(term, "i", 3*time.Second) {
		t.Fatalf("keystroke never reached the exec'd program; view:\n%s", term.View())
	}
}
