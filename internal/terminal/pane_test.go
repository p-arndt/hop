package terminal

import (
	"crypto/ed25519"
	"crypto/rand"
	"io"
	"net"
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

// startTestServer spins up an in-process SSH server on 127.0.0.1:0 that accepts
// any public key. On a session channel it accepts pty-req and shell requests,
// writes a ready banner, then echoes everything it receives. It returns the
// listener address; the server runs until the listener is closed via t.Cleanup.
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

// handleSession accepts pty-req and shell, then writes a banner and echoes input.
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
			// Banner, then echo loop.
			go func() {
				io.WriteString(ch, "HOPTEST-READY\r\n")
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
			}()
		default:
			if req.WantReply {
				req.Reply(false, nil)
			}
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

// TestEmbeddedRoundTrip proves the full embedded-terminal chain end to end
// without any external sshd: in-process SSH server -> sshx.Client/Session ->
// terminal.Pane VT emulator, including a keystroke round-trip via echo.
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
