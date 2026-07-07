package sftpx

import (
	"crypto/ed25519"
	"crypto/rand"
	"net"
	"os"
	"path"
	"path/filepath"
	"testing"
	"time"

	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
)

// newSigner generates a fresh ed25519 ssh.Signer for use as a host or client key.
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

// startSFTPServer spins up an in-process SSH server on 127.0.0.1:0 that accepts
// any public key. On a session channel it accepts a "subsystem" request named
// "sftp" and serves it with a real sftp.Server over the real filesystem. It
// returns the listener address; the server runs until t.Cleanup closes it.
func startSFTPServer(t *testing.T, hostKey ssh.Signer) string {
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
			go serveSFTPConn(nc, cfg)
		}
	}()

	return ln.Addr().String()
}

// serveSFTPConn performs the SSH handshake and services session channels,
// answering an "sftp" subsystem request with a real sftp.Server.
func serveSFTPConn(nc net.Conn, cfg *ssh.ServerConfig) {
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
		go handleSFTPSession(ch, chReqs)
	}
}

// handleSFTPSession accepts an "sftp" subsystem request and serves it.
func handleSFTPSession(ch ssh.Channel, reqs <-chan *ssh.Request) {
	for req := range reqs {
		if req.Type != "subsystem" {
			if req.WantReply {
				req.Reply(false, nil)
			}
			continue
		}
		// Subsystem request payload is an SSH string: the subsystem name.
		var payload struct{ Name string }
		if err := ssh.Unmarshal(req.Payload, &payload); err != nil || payload.Name != "sftp" {
			if req.WantReply {
				req.Reply(false, nil)
			}
			continue
		}
		if req.WantReply {
			req.Reply(true, nil)
		}
		server, err := sftp.NewServer(ch)
		if err != nil {
			ch.Close()
			return
		}
		go func() {
			server.Serve()
			server.Close()
			ch.Close()
		}()
	}
}

// TestSFTPRoundTrip proves the sftpx layer end to end against an in-process SFTP
// server rooted at the real filesystem — no external sshd, no admin rights.
func TestSFTPRoundTrip(t *testing.T) {
	hostKey := newSigner(t)
	clientKey := newSigner(t)

	addr := startSFTPServer(t, hostKey)

	cfg := &ssh.ClientConfig{
		User:            "tester",
		Auth:            []ssh.AuthMethod{ssh.PublicKeys(clientKey)},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         5 * time.Second,
	}

	sshClient, err := ssh.Dial("tcp", addr, cfg)
	if err != nil {
		t.Fatalf("ssh.Dial: %v", err)
	}
	defer sshClient.Close()

	c, err := Open(sshClient)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer c.Close()

	// A usable base directory without relying on OS-specific absolute paths.
	base, err := c.Home()
	if err != nil {
		t.Fatalf("Home: %v", err)
	}
	if base == "" {
		t.Fatalf("Home returned empty path")
	}

	p := path.Join(base, "hop_sftp_test")
	if err := c.Mkdir(p); err != nil {
		t.Fatalf("Mkdir %s: %v", p, err)
	}
	// Best-effort cleanup even if a later assertion fails.
	remoteFile := path.Join(p, "a.txt")
	t.Cleanup(func() {
		c.Remove(remoteFile)
		c.Remove(p)
	})

	const payload = "hop-sftp-ok"

	// Local source file to upload.
	src, err := os.CreateTemp(t.TempDir(), "hop-src-*.txt")
	if err != nil {
		t.Fatalf("create local src: %v", err)
	}
	if _, err := src.WriteString(payload); err != nil {
		t.Fatalf("write local src: %v", err)
	}
	src.Close()

	n, err := c.Upload(src.Name(), remoteFile)
	if err != nil {
		t.Fatalf("Upload: %v", err)
	}
	if n != int64(len(payload)) {
		t.Fatalf("Upload wrote %d bytes, want %d", n, len(payload))
	}

	// List must surface the uploaded file as a non-directory with a positive size.
	entries, err := c.List(p)
	if err != nil {
		t.Fatalf("List %s: %v", p, err)
	}
	var found *Entry
	for i := range entries {
		if entries[i].Name == "a.txt" {
			found = &entries[i]
			break
		}
	}
	if found == nil {
		t.Fatalf("List %s did not contain a.txt; got %+v", p, entries)
	}
	if found.IsDir {
		t.Fatalf("a.txt reported as directory")
	}
	if found.Size <= 0 {
		t.Fatalf("a.txt Size = %d, want > 0", found.Size)
	}

	// Download and verify the bytes round-trip exactly.
	dst := filepath.Join(t.TempDir(), "a-downloaded.txt")
	m, err := c.Download(remoteFile, dst)
	if err != nil {
		t.Fatalf("Download: %v", err)
	}
	if m != int64(len(payload)) {
		t.Fatalf("Download read %d bytes, want %d", m, len(payload))
	}
	got, err := os.ReadFile(dst)
	if err != nil {
		t.Fatalf("read downloaded file: %v", err)
	}
	if string(got) != payload {
		t.Fatalf("downloaded content = %q, want %q", string(got), payload)
	}

	// Explicit cleanup exercises Remove for both a file and an empty directory.
	if err := c.Remove(remoteFile); err != nil {
		t.Fatalf("Remove file: %v", err)
	}
	if err := c.Remove(p); err != nil {
		t.Fatalf("Remove dir: %v", err)
	}
}
