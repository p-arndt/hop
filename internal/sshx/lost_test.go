package sshx

import (
	"crypto/ed25519"
	"crypto/rand"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/skeema/knownhosts"
	"golang.org/x/crypto/ssh"

	"hop/internal/store"
)

// lostWait is how long a test gives a connection to notice it has gone. The notice
// is a goroutine parked on ssh.Client.Wait, so it lands as soon as the transport
// does; the second is slack for a loaded CI machine, not an expectation.
const lostWait = time.Second

// A connection the far end closes is reported lost, and the report is what the UI
// hangs its reconnect offer on: a dropped session that never announced itself is a
// pane that has quietly stopped updating.
func TestLostFiresWhenTheServerGoesAway(t *testing.T) {
	h, stop := serveCloseable(t)

	cl, err := Connect(h, &recordingPrompter{answers: []string{"code"}})
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}
	defer cl.Close()

	if cl.IsLost() {
		t.Fatal("a freshly connected client reports itself lost")
	}
	if err := cl.LostErr(); err != nil {
		t.Fatalf("LostErr on a live connection = %v, want nil", err)
	}

	stop()

	select {
	case <-cl.Lost():
	case <-time.After(lostWait):
		t.Fatal("the server went away and the connection never noticed")
	}
	if !cl.IsLost() {
		t.Fatal("IsLost is false on a connection that has gone")
	}
}

// Closing the connection from this side fires it too. That is deliberate: the model
// tells its own closes apart by *which* connection the loss names, and a close that
// stayed silent would leave a goroutine parked for the life of the process.
func TestLostFiresOnOurOwnClose(t *testing.T) {
	h, _ := serveCloseable(t)

	cl, err := Connect(h, &recordingPrompter{answers: []string{"code"}})
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}
	cl.Close()

	select {
	case <-cl.Lost():
	case <-time.After(lostWait):
		t.Fatal("closing the client left its watcher parked forever")
	}
}

// The keepalive probe is what makes a blackholed connection detectable at all, so
// it has to be answered on a live one — and to fail on a dead one. A *failure*
// reply counts as answered: the request type exists only to be replied to.
func TestKeepalivePing(t *testing.T) {
	h, stop := serveCloseable(t)

	cl, err := Connect(h, &recordingPrompter{answers: []string{"code"}})
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}
	defer cl.Close()

	if !cl.ping() {
		t.Fatal("the server did not answer a keepalive on a live connection")
	}

	stop()
	<-cl.Lost()
	if cl.ping() {
		t.Fatal("a keepalive on a connection that has gone reported an answer")
	}
}

// A zero Client — one that never connected — is not lost, and blocking on it never
// fires. The TUI builds such clients in its own tests, and more to the point a nil
// channel is the honest answer for a connection that does not exist.
func TestZeroClientIsNotLost(t *testing.T) {
	var c Client
	if c.IsLost() {
		t.Fatal("a client that never connected reports itself lost")
	}
	if err := c.LostErr(); err != nil {
		t.Fatalf("LostErr = %v, want nil", err)
	}
	select {
	case <-c.Lost():
		t.Fatal("Lost fired on a client that never connected")
	case <-time.After(20 * time.Millisecond):
	}
}

// serveCloseable starts an SSH server on loopback that lets anything in and hands
// back the host that reaches it, plus a stop function that drops the connection from
// the server's side — which is the event these tests are about. It points $HOME at a
// temp dir holding the server's key, so the dial gets past host-key verification
// without touching the developer's real ~/.ssh.
func serveCloseable(t *testing.T) (store.Host, func()) {
	t.Helper()
	disableAgent(t)
	home := fakeHome(t)

	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate host key: %v", err)
	}
	signer, err := ssh.NewSignerFromKey(priv)
	if err != nil {
		t.Fatalf("host key signer: %v", err)
	}

	cfg := &ssh.ServerConfig{
		KeyboardInteractiveCallback: func(ssh.ConnMetadata, ssh.KeyboardInteractiveChallenge) (*ssh.Permissions, error) {
			return &ssh.Permissions{}, nil
		},
	}
	cfg.AddHostKey(signer)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { ln.Close() })

	// The accepted connections, so the test can close them out from under the client.
	conns := make(chan *ssh.ServerConn, 4)
	go func() {
		for {
			nc, err := ln.Accept()
			if err != nil {
				return // the listener closed: the test is over
			}
			go func() {
				sc, chans, reqs, err := ssh.NewServerConn(nc, cfg)
				if err != nil {
					nc.Close()
					return
				}
				// Answer global requests (the keepalive probe is one) and refuse channels:
				// nothing here needs a shell.
				go ssh.DiscardRequests(reqs)
				go func() {
					for nch := range chans {
						nch.Reject(ssh.Prohibited, "no channels here")
					}
				}()
				conns <- sc
			}()
		}
	}()

	stop := func() {
		select {
		case sc := <-conns:
			sc.Close()
		case <-time.After(lostWait):
			t.Error("no server-side connection to close")
		}
	}

	host, portStr, err := net.SplitHostPort(ln.Addr().String())
	if err != nil {
		t.Fatalf("split listen addr: %v", err)
	}
	var port int
	fmt.Sscanf(portStr, "%d", &port)

	kh := filepath.Join(home, ".ssh", "known_hosts")
	if err := os.MkdirAll(filepath.Dir(kh), 0o700); err != nil {
		t.Fatalf("known_hosts dir: %v", err)
	}
	line := knownhosts.Line([]string{net.JoinHostPort(host, portStr)}, signer.PublicKey()) + "\n"
	if err := os.WriteFile(kh, []byte(line), 0o600); err != nil {
		t.Fatalf("write known_hosts: %v", err)
	}

	return store.Host{Alias: "dropper", HostName: host, Port: port, User: "deploy"}, stop
}
