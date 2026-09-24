//go:build unix

package sftpx

import (
	"path"
	"syscall"
	"testing"
	"time"
)

// A symlink to a FIFO looks like any link in a listing; reading it must be refused rather
// than left blocking the server.
func TestReadHeadRefusesALinkToAFIFO(t *testing.T) {
	c, base := newTestClient(t, "hop_sftp_readhead_fifo")
	fifo := path.Join(base, "pipe")
	if err := syscall.Mkfifo(fifo, 0o600); err != nil {
		t.Skipf("cannot make a FIFO here: %v", err)
	}
	link := path.Join(base, "link")
	if err := c.sc.Symlink(fifo, link); err != nil {
		t.Skipf("server cannot create symlinks: %v", err)
	}

	done := make(chan error, 1)
	go func() {
		_, err := c.ReadHead(link, 64)
		done <- err
	}()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("ReadHead read a FIFO, want a refusal")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("ReadHead blocked on a FIFO")
	}
}

func TestReadHeadReadsARegularFile(t *testing.T) {
	c, base := newTestClient(t, "hop_sftp_readhead_file")
	f := path.Join(base, "a.txt")
	writeRemote(t, c, f, "hello world")

	got, err := c.ReadHead(f, 5)
	if err != nil || string(got) != "hello" {
		t.Fatalf("ReadHead = %q, %v; want the first five bytes", got, err)
	}
}
