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

// startSFTPServer spins up an in-process SSH server on 127.0.0.1:0 serving a real
// sftp.Server, and returns its address.
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

// serveSFTPConn performs the SSH handshake and services session channels.
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

// Bytes pass through unchanged and the running total is reported after each write.
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

	// With nobody to report to there must be no wrapper at all.
	if got := counted(&sink, nil); got != io.Writer(&sink) {
		t.Fatalf("counted(w, nil) = %T, want the writer itself", got)
	}
	if got := counted(&sink, func(int64) {}); got == io.Writer(&sink) {
		t.Fatal("counted(w, report) handed back the bare writer, so nothing would be counted")
	}
}

// A wrapper that is not an io.ReaderFrom would downgrade uploads to 32 KiB writes.
func TestCountingWriterKeepsTheBulkPath(t *testing.T) {
	var w io.Writer = counted(&recordingReaderFrom{}, func(int64) {})
	if _, ok := w.(io.ReaderFrom); !ok {
		t.Fatal("a counted writer is not an io.ReaderFrom, so io.Copy cannot reach the destination's bulk path")
	}

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

// Totals only grow and end on the file's size; the payload exceeds io.Copy's 32 KiB
// block so more than one report has to happen.
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
	// Best-effort only: the removals that matter are the explicit ones at the end.
	t.Cleanup(func() {
		os.Remove(filepath.Join("hop_sftp_progress", "big.bin"))
		os.Remove("hop_sftp_progress")
	})

	const size = 200 * 1024
	src := filepath.Join(t.TempDir(), "big.bin")
	if err := os.WriteFile(src, make([]byte, size), 0o644); err != nil {
		t.Fatalf("write local source: %v", err)
	}

	// check: at least two reports, never going backwards, ending on the size.
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

	// While the client is still open, so the server actually sees them.
	if err := c.Remove(remote); err != nil {
		t.Fatalf("Remove %s: %v", remote, err)
	}
	if err := c.Remove(dir); err != nil {
		t.Fatalf("Remove %s: %v", dir, err)
	}
}

// newTestClient returns a connected sftpx client and a scratch directory of its own,
// removed while the client is still open.
func newTestClient(t *testing.T, name string) (*Client, string) {
	t.Helper()

	addr := startSFTPServer(t, newSigner(t))
	sshClient, err := ssh.Dial("tcp", addr, &ssh.ClientConfig{
		User:            "tester",
		Auth:            []ssh.AuthMethod{ssh.PublicKeys(newSigner(t))},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         5 * time.Second,
	})
	if err != nil {
		t.Fatalf("ssh.Dial: %v", err)
	}
	t.Cleanup(func() { sshClient.Close() })

	c, err := Open(sshClient)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	base, err := c.Home()
	if err != nil {
		t.Fatalf("Home: %v", err)
	}
	dir := path.Join(base, name)
	if err := c.Mkdir(dir); err != nil {
		t.Fatalf("Mkdir %s: %v", dir, err)
	}
	t.Cleanup(func() {
		c.sc.RemoveAll(dir)
		c.Close()
	})

	return c, dir
}

// writeRemote puts content at a remote path, creating its parent directories.
func writeRemote(t *testing.T, c *Client, p, content string) {
	t.Helper()
	if err := c.sc.MkdirAll(path.Dir(p)); err != nil {
		t.Fatalf("MkdirAll %s: %v", path.Dir(p), err)
	}
	f, err := c.sc.Create(p)
	if err != nil {
		t.Fatalf("create %s: %v", p, err)
	}
	if _, err := io.WriteString(f, content); err != nil {
		t.Fatalf("write %s: %v", p, err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("close %s: %v", p, err)
	}
}

// readRemote returns the content of a remote file, failing the test if it is missing.
func readRemote(t *testing.T, c *Client, p string) string {
	t.Helper()
	f, err := c.sc.Open(p)
	if err != nil {
		t.Fatalf("open %s: %v", p, err)
	}
	defer f.Close()
	b, err := io.ReadAll(f)
	if err != nil {
		t.Fatalf("read %s: %v", p, err)
	}
	return string(b)
}

// existsRemote reports whether a remote path is there at all.
func existsRemote(c *Client, p string) bool {
	_, err := c.sc.Stat(p)
	return err == nil
}

// Copy lands the bytes at dstDir/<base>, returns the size, and reports growing totals.
func TestCopyFileWithProgress(t *testing.T) {
	c, base := newTestClient(t, "hop_sftp_copy_file")

	const size = 200 * 1024
	src := path.Join(base, "src", "big.bin")
	writeRemote(t, c, src, strings.Repeat("x", size))
	dstDir := path.Join(base, "dst")
	if err := c.Mkdir(dstDir); err != nil {
		t.Fatalf("Mkdir %s: %v", dstDir, err)
	}

	var seen []int64
	n, err := c.Copy(src, dstDir, func(n int64) { seen = append(seen, n) })
	if err != nil {
		t.Fatalf("Copy: %v", err)
	}
	if n != size {
		t.Fatalf("Copy returned %d, want %d", n, size)
	}
	if got := readRemote(t, c, path.Join(dstDir, "big.bin")); len(got) != size {
		t.Fatalf("copied file is %d bytes, want %d", len(got), size)
	}
	if !existsRemote(c, src) {
		t.Fatal("Copy removed the source")
	}

	if len(seen) < 2 {
		t.Fatalf("progress reported %d times (%v), want several - a bar cannot move on one", len(seen), seen)
	}
	if !slices.IsSorted(seen) {
		t.Fatalf("progress reported %v, want totals that only grow", seen)
	}
	if got := seen[len(seen)-1]; got != size {
		t.Fatalf("progress finished on %d, want the full %d", got, size)
	}
}

// The whole subtree comes across, and progress totals stay cumulative across files.
func TestCopyDirectoryRecursive(t *testing.T) {
	c, base := newTestClient(t, "hop_sftp_copy_dir")

	src := path.Join(base, "tree")
	files := map[string]string{
		"a.txt":          "alpha",
		"sub/b.txt":      "bravo!!",
		"sub/deep/c.txt": "charlie",
		"sub/deep/d.txt": "delta",
		"other/e.txt":    "echo",
	}
	var want int64
	for rel, content := range files {
		writeRemote(t, c, path.Join(src, rel), content)
		want += int64(len(content))
	}
	// An empty directory has to come across too, and no file copy would create it.
	if err := c.sc.MkdirAll(path.Join(src, "empty")); err != nil {
		t.Fatalf("MkdirAll empty: %v", err)
	}

	dstDir := path.Join(base, "dst")
	if err := c.Mkdir(dstDir); err != nil {
		t.Fatalf("Mkdir %s: %v", dstDir, err)
	}

	var seen []int64
	n, err := c.Copy(src, dstDir, func(n int64) { seen = append(seen, n) })
	if err != nil {
		t.Fatalf("Copy: %v", err)
	}
	if n != want {
		t.Fatalf("Copy returned %d bytes, want %d", n, want)
	}

	root := path.Join(dstDir, "tree")
	for rel, content := range files {
		if got := readRemote(t, c, path.Join(root, rel)); got != content {
			t.Fatalf("%s = %q, want %q", rel, got, content)
		}
	}
	if fi, err := c.sc.Stat(path.Join(root, "empty")); err != nil || !fi.IsDir() {
		t.Fatalf("empty subdirectory not copied: %v", err)
	}

	if !slices.IsSorted(seen) {
		t.Fatalf("progress reported %v, want one cumulative sequence that only grows", seen)
	}
	if got := seen[len(seen)-1]; got != want {
		t.Fatalf("progress finished on %d, want the whole tree's %d", got, want)
	}
}

func TestMoveViaRename(t *testing.T) {
	c, base := newTestClient(t, "hop_sftp_move_rename")

	src := path.Join(base, "src", "a.txt")
	writeRemote(t, c, src, "hop-move")
	dstDir := path.Join(base, "dst")
	if err := c.Mkdir(dstDir); err != nil {
		t.Fatalf("Mkdir %s: %v", dstDir, err)
	}

	if err := c.Move(src, dstDir, nil); err != nil {
		t.Fatalf("Move: %v", err)
	}
	if got := readRemote(t, c, path.Join(dstDir, "a.txt")); got != "hop-move" {
		t.Fatalf("moved file = %q, want %q", got, "hop-move")
	}
	if existsRemote(c, src) {
		t.Fatal("source still present after Move")
	}
}

// The pre-existing file surviving alongside the moved one proves the copy path ran.
func TestMoveFallsBackToCopyAndDelete(t *testing.T) {
	c, base := newTestClient(t, "hop_sftp_move_fallback")

	src := path.Join(base, "src", "d")
	writeRemote(t, c, path.Join(src, "a.txt"), "alpha")
	writeRemote(t, c, path.Join(src, "nested", "b.txt"), "bravo")

	dstDir := path.Join(base, "dst")
	writeRemote(t, c, path.Join(dstDir, "d", "keep.txt"), "kept")

	// moveByCopy directly: a mount boundary cannot be staged in-process.
	if err := c.moveByCopy(src, dstDir, nil); err != nil {
		t.Fatalf("moveByCopy: %v", err)
	}

	if got := readRemote(t, c, path.Join(dstDir, "d", "a.txt")); got != "alpha" {
		t.Fatalf("a.txt = %q, want %q", got, "alpha")
	}
	if got := readRemote(t, c, path.Join(dstDir, "d", "nested", "b.txt")); got != "bravo" {
		t.Fatalf("nested/b.txt = %q, want %q", got, "bravo")
	}
	if got := readRemote(t, c, path.Join(dstDir, "d", "keep.txt")); got != "kept" {
		t.Fatalf("keep.txt = %q, want %q - a rename would have replaced the directory", got, "kept")
	}
	if existsRemote(c, src) {
		t.Fatal("source directory still present after the copy-and-delete fallback")
	}
}

// Copy into own subtree, and onto itself, are refused before a byte moves; so is Move.
func TestCopyAndMoveRefuseSelfDestructiveCases(t *testing.T) {
	c, base := newTestClient(t, "hop_sftp_copy_self")

	src := path.Join(base, "tree")
	writeRemote(t, c, path.Join(src, "sub", "a.txt"), "alpha")

	cases := []struct {
		name    string
		srcPath string
		dstDir  string
		want    string
	}{
		{"into own child", src, path.Join(src, "sub"), "inside the source"},
		{"into own deep descendant", src, path.Join(src, "sub", "deeper"), "inside the source"},
		{"onto itself", src, base, "same path"},
		{"file onto itself", path.Join(src, "sub", "a.txt"), path.Join(src, "sub"), "same path"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			n, err := c.Copy(tc.srcPath, tc.dstDir, nil)
			if err == nil {
				t.Fatalf("Copy(%s -> %s) succeeded, want a refusal", tc.srcPath, tc.dstDir)
			}
			if n != 0 {
				t.Fatalf("Copy reported %d bytes written on a refusal, want 0", n)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("Copy error = %q, want it to mention %q", err, tc.want)
			}

			if err := c.Move(tc.srcPath, tc.dstDir, nil); err == nil {
				t.Fatalf("Move(%s -> %s) succeeded, want a refusal", tc.srcPath, tc.dstDir)
			} else if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("Move error = %q, want it to mention %q", err, tc.want)
			}
		})
	}

	if got := readRemote(t, c, path.Join(src, "sub", "a.txt")); got != "alpha" {
		t.Fatalf("source changed after refusals: %q", got)
	}
}

// Refused before anything moves, rather than overwriting the destination.
func TestMoveRefusesAnExistingDestination(t *testing.T) {
	c, base := newTestClient(t, "hop_sftp_move_exists")

	src := path.Join(base, "src", "a.txt")
	writeRemote(t, c, src, "alpha")
	dstDir := path.Join(base, "dst")
	writeRemote(t, c, path.Join(dstDir, "a.txt"), "occupied")

	err := c.Move(src, dstDir, nil)
	if err == nil {
		t.Fatal("Move onto an existing name succeeded, want a refusal")
	}
	if !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("error = %v, want it to name the collision", err)
	}
	if got := readRemote(t, c, path.Join(dstDir, "a.txt")); got != "occupied" {
		t.Fatalf("destination = %q, want it untouched", got)
	}
	if !existsRemote(c, src) {
		t.Fatal("source gone after a refused move")
	}
}

// Regression: following a symlink to a directory aborted the whole copy.
func TestCopyRecreatesSymlinks(t *testing.T) {
	c, base := newTestClient(t, "hop_sftp_copy_links")

	src := path.Join(base, "src")
	writeRemote(t, c, path.Join(src, "real", "a.txt"), "alpha")
	if err := c.sc.Symlink("real", path.Join(src, "current")); err != nil {
		t.Skipf("server cannot create symlinks: %v", err)
	}

	dstDir := path.Join(base, "dst")
	if _, err := c.Copy(src, dstDir, nil); err != nil {
		t.Fatalf("Copy: %v", err)
	}

	link := path.Join(dstDir, "src", "current")
	target, err := c.sc.ReadLink(link)
	if err != nil {
		t.Fatalf("ReadLink(%s): %v, want the link recreated", link, err)
	}
	if target != "real" {
		t.Fatalf("link target = %q, want %q", target, "real")
	}
	if got := readRemote(t, c, path.Join(dstDir, "src", "real", "a.txt")); got != "alpha" {
		t.Fatalf("a.txt = %q, want the rest of the tree copied too", got)
	}
}
