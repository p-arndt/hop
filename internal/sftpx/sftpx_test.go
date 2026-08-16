package sftpx

import (
	"crypto/ed25519"
	"crypto/rand"
	"io"
	"net"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
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

// A counting writer passes every byte through unchanged and reports the running total
// after each write, which is the whole contract the progress callbacks rest on.
func TestCountingWriter(t *testing.T) {
	var sink strings.Builder
	var seen []int64
	cw := &countingWriter{w: &sink, report: func(n int64) { seen = append(seen, n) }}

	for _, part := range []string{"hop", "-", "sftp"} {
		if _, err := io.WriteString(cw, part); err != nil {
			t.Fatalf("write %q: %v", part, err)
		}
	}

	if sink.String() != "hop-sftp" {
		t.Fatalf("passed through %q, want %q", sink.String(), "hop-sftp")
	}
	if want := []int64{3, 4, 8}; !slices.Equal(seen, want) {
		t.Fatalf("reported %v, want the running totals %v", seen, want)
	}

	// With nobody to report to there must be no wrapper at all: that is what keeps the
	// plain Download and Upload byte-for-byte the calls they were before progress existed.
	if got := counted(&sink, nil); got != io.Writer(&sink) {
		t.Fatalf("counted(w, nil) = %T, want the writer itself", got)
	}
	if got := counted(&sink, func(int64) {}); got == io.Writer(&sink) {
		t.Fatal("counted(w, report) handed back the bare writer, so nothing would be counted")
	}
}

// The wrapper must not cost the destination its bulk transfer path: io.Copy reaches it
// through ReadFrom, and a wrapper that is not an io.ReaderFrom silently downgrades an
// upload to sequential 32 KiB writes. This is the assertion that would catch that.
func TestCountingWriterKeepsTheBulkPath(t *testing.T) {
	var w io.Writer = counted(&recordingReaderFrom{}, func(int64) {})
	if _, ok := w.(io.ReaderFrom); !ok {
		t.Fatal("a counted writer is not an io.ReaderFrom, so io.Copy cannot reach the destination's bulk path")
	}

	// And it must actually delegate rather than fall back to its own loop.
	dst := &recordingReaderFrom{}
	var seen []int64
	cw := counted(dst, func(n int64) { seen = append(seen, n) }).(io.ReaderFrom)
	n, err := cw.ReadFrom(strings.NewReader("hop-sftp"))
	if err != nil {
		t.Fatalf("ReadFrom: %v", err)
	}
	if !dst.used {
		t.Fatal("ReadFrom did not delegate to the destination's own ReadFrom")
	}
	if n != 8 {
		t.Fatalf("ReadFrom returned %d, want 8", n)
	}
	if len(seen) == 0 || seen[len(seen)-1] != 8 {
		t.Fatalf("reported %v, want totals ending on 8", seen)
	}
}

// recordingReaderFrom is a destination with a bulk path, which notes whether it was used.
type recordingReaderFrom struct {
	used bool
	buf  strings.Builder
}

func (d *recordingReaderFrom) Write(p []byte) (int, error) { return d.buf.Write(p) }

func (d *recordingReaderFrom) ReadFrom(r io.Reader) (int64, error) {
	d.used = true
	return io.Copy(&d.buf, r)
}

// The progress callback fires during a real transfer in both directions, with totals
// that only ever grow and end on the file's size. The payload is deliberately larger
// than io.Copy's 32 KiB block so more than one report has to happen — a callback that
// only fired once at the end would satisfy a smaller file and tell the user nothing.
func TestTransferProgressReports(t *testing.T) {
	hostKey := newSigner(t)
	clientKey := newSigner(t)
	addr := startSFTPServer(t, hostKey)

	sshClient, err := ssh.Dial("tcp", addr, &ssh.ClientConfig{
		User:            "tester",
		Auth:            []ssh.AuthMethod{ssh.PublicKeys(clientKey)},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         5 * time.Second,
	})
	if err != nil {
		t.Fatalf("ssh.Dial: %v", err)
	}
	defer sshClient.Close()

	c, err := Open(sshClient)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer c.Close()

	base, err := c.Home()
	if err != nil {
		t.Fatalf("Home: %v", err)
	}
	dir := path.Join(base, "hop_sftp_progress")
	if err := c.Mkdir(dir); err != nil {
		t.Fatalf("Mkdir %s: %v", dir, err)
	}
	remote := path.Join(dir, "big.bin")
	// Best-effort only, and it really is only that: t.Cleanup runs after this function's
	// defers, by which time the client is closed and these calls cannot work. The
	// removals that matter are the explicit ones at the end.
	t.Cleanup(func() {
		os.Remove(filepath.Join("hop_sftp_progress", "big.bin"))
		os.Remove("hop_sftp_progress")
	})

	const size = 200 * 1024
	src := filepath.Join(t.TempDir(), "big.bin")
	if err := os.WriteFile(src, make([]byte, size), 0o644); err != nil {
		t.Fatalf("write local source: %v", err)
	}

	// check asserts the shape every progress report must have, whichever way the bytes
	// were going: at least two of them, never going backwards, ending on the size.
	check := func(what string, seen []int64) {
		t.Helper()
		if len(seen) < 2 {
			t.Fatalf("%s reported %d times (%v), want several — a bar cannot move on one", what, len(seen), seen)
		}
		if !slices.IsSorted(seen) {
			t.Fatalf("%s reported %v, want totals that only grow", what, seen)
		}
		if got := seen[len(seen)-1]; got != size {
			t.Fatalf("%s finished reporting %d, want the full %d", what, got, size)
		}
	}

	var upSeen []int64
	n, err := c.UploadProgress(src, remote, func(n int64) { upSeen = append(upSeen, n) })
	if err != nil {
		t.Fatalf("UploadProgress: %v", err)
	}
	if n != size {
		t.Fatalf("UploadProgress returned %d, want %d", n, size)
	}
	check("upload", upSeen)

	var downSeen []int64
	dst := filepath.Join(t.TempDir(), "big-back.bin")
	m, err := c.DownloadProgress(remote, dst, func(n int64) { downSeen = append(downSeen, n) })
	if err != nil {
		t.Fatalf("DownloadProgress: %v", err)
	}
	if m != size {
		t.Fatalf("DownloadProgress returned %d, want %d", m, size)
	}
	check("download", downSeen)

	// While the client is still open, so the server actually sees them. The test server
	// is rooted at the real filesystem, and a leftover 200 KiB file would land in the
	// package directory.
	if err := c.Remove(remote); err != nil {
		t.Fatalf("Remove %s: %v", remote, err)
	}
	if err := c.Remove(dir); err != nil {
		t.Fatalf("Remove %s: %v", dir, err)
	}
}
