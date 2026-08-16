// Package sftpx provides a small SFTP client for hop, layered over an existing
// SSH connection. It exposes directory listing, transfer and basic mutation
// operations used by the remote file browser. Remote paths always use forward
// slashes and are manipulated with the stdlib "path" package.
package sftpx

import (
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
)

// Entry describes a single remote directory entry.
type Entry struct {
	Name    string
	IsDir   bool
	Size    int64
	Mode    fs.FileMode
	ModTime int64 // unix seconds
}

// Client wraps an *sftp.Client bound to a single SSH connection.
type Client struct {
	sc *sftp.Client
}

// Open starts an SFTP subsystem over the supplied SSH client.
func Open(sshClient *ssh.Client) (*Client, error) {
	sc, err := sftp.NewClient(sshClient)
	if err != nil {
		return nil, fmt.Errorf("sftpx: open: %w", err)
	}
	return &Client{sc: sc}, nil
}

// Close shuts down the SFTP subsystem.
func (c *Client) Close() error {
	return c.sc.Close()
}

// Home returns a best-effort remote starting directory: the working directory
// if the server reports one, otherwise the real path of ".", falling back to
// "/".
func (c *Client) Home() (string, error) {
	if wd, err := c.sc.Getwd(); err == nil && wd != "" {
		return wd, nil
	}
	if rp, err := c.sc.RealPath("."); err == nil && rp != "" {
		return rp, nil
	}
	return "/", nil
}

// List reads dir and returns its entries sorted directories-first, then by
// case-insensitive name. It does not inject a ".." entry.
func (c *Client) List(dir string) ([]Entry, error) {
	infos, err := c.sc.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("sftpx: list %s: %w", dir, err)
	}

	entries := make([]Entry, 0, len(infos))
	for _, fi := range infos {
		entries = append(entries, Entry{
			Name:    fi.Name(),
			IsDir:   fi.IsDir(),
			Size:    fi.Size(),
			Mode:    fi.Mode(),
			ModTime: fi.ModTime().Unix(),
		})
	}

	sort.Slice(entries, func(i, j int) bool {
		if entries[i].IsDir != entries[j].IsDir {
			return entries[i].IsDir
		}
		return strings.ToLower(entries[i].Name) < strings.ToLower(entries[j].Name)
	})

	return entries, nil
}

// Download copies a remote file to localPath, creating parent directories as
// needed, and returns the number of bytes written.
func (c *Client) Download(remotePath, localPath string) (int64, error) {
	return c.DownloadProgress(remotePath, localPath, nil)
}

// DownloadProgress is Download, reporting the running byte count to progress as the copy
// proceeds. progress may be nil, and is called from the calling goroutine — a caller
// showing the count on another one has to publish it safely itself.
func (c *Client) DownloadProgress(remotePath, localPath string, progress func(int64)) (int64, error) {
	if err := os.MkdirAll(filepath.Dir(localPath), 0o755); err != nil {
		return 0, fmt.Errorf("sftpx: download: %w", err)
	}

	rf, err := c.sc.Open(remotePath)
	if err != nil {
		return 0, fmt.Errorf("sftpx: download open remote %s: %w", remotePath, err)
	}
	defer rf.Close()

	lf, err := os.Create(localPath)
	if err != nil {
		return 0, fmt.Errorf("sftpx: download create local %s: %w", localPath, err)
	}
	defer lf.Close()

	n, err := io.Copy(counted(lf, progress), rf)
	if err != nil {
		return n, fmt.Errorf("sftpx: download copy: %w", err)
	}
	return n, nil
}

// Upload copies a local file to remotePath and returns the number of bytes
// written.
func (c *Client) Upload(localPath, remotePath string) (int64, error) {
	return c.UploadProgress(localPath, remotePath, nil)
}

// UploadProgress is Upload, reporting the running byte count to progress as the copy
// proceeds. The same caveats as DownloadProgress apply.
func (c *Client) UploadProgress(localPath, remotePath string, progress func(int64)) (int64, error) {
	lf, err := os.Open(localPath)
	if err != nil {
		return 0, fmt.Errorf("sftpx: upload open local %s: %w", localPath, err)
	}
	defer lf.Close()

	rf, err := c.sc.Create(remotePath)
	if err != nil {
		return 0, fmt.Errorf("sftpx: upload create remote %s: %w", remotePath, err)
	}
	defer rf.Close()

	n, err := io.Copy(counted(rf, progress), lf)
	if err != nil {
		return n, fmt.Errorf("sftpx: upload copy: %w", err)
	}
	return n, nil
}

// counted wraps w so the bytes going through it are reported, or hands back w untouched
// when nobody is listening. The plain Download and Upload take that second path, so they
// are exactly what they were before progress existed — wrapper included, they would not
// be.
func counted(w io.Writer, report func(int64)) io.Writer {
	if report == nil {
		return w
	}
	return &countingWriter{w: w, report: report}
}

// countingWriter passes bytes through to w and reports the running total as they go. It
// sits on the writing side because that is the side that knows what has actually landed:
// a read io.Copy has buffered but not yet written is not progress worth showing.
//
// io.Copy works in 32 KiB blocks, so report fires about that often — frequently enough
// for a bar to move on a slow link, rarely enough that the callback need not be cheap to
// the point of inlining.
type countingWriter struct {
	w      io.Writer
	n      int64
	report func(int64)
}

func (c *countingWriter) Write(p []byte) (int, error) {
	n, err := c.w.Write(p)
	c.n += int64(n)
	c.report(c.n)
	return n, err
}

// ReadFrom keeps the wrapped writer's bulk path reachable, and exists because losing it
// costs far more than it looks like it should.
//
// io.Copy prefers the source's WriteTo, and *os.File has one — which then re-enters
// io.Copy looking for a ReadFrom on the destination. Unwrapped, that destination is a
// *sftp.File, whose ReadFrom is pkg/sftp's concurrent write pipeline: the documented way
// to get throughput out of a high-latency link. A wrapper without this method is not a
// ReaderFrom, so the whole upload would quietly fall back to sequential 32 KiB writes,
// one round trip each.
//
// The price is where the counting happens. Delegating means the bytes are counted as
// they are read out of the local file rather than as the server acknowledges them, so an
// upload's progress runs slightly ahead of the wire. That is the honest trade: a bar a
// little optimistic beats an upload an order of magnitude slower.
func (c *countingWriter) ReadFrom(r io.Reader) (int64, error) {
	rf, ok := c.w.(io.ReaderFrom)
	if !ok {
		// Nothing to preserve. onlyWriter hides this method from io.Copy, which would
		// otherwise call it right back.
		return io.Copy(onlyWriter{c}, r)
	}
	n, err := rf.ReadFrom(&countingReader{r: r, base: c.n, report: c.report})
	c.n += n
	return n, err
}

// onlyWriter is a writer with every other method hidden, so io.Copy cannot re-select the
// fast path it is already inside.
type onlyWriter struct{ io.Writer }

// countingReader reports bytes as they are read, continuing the running total from base.
// It is the counter of last resort, used only where writing side counting would cost the
// bulk transfer path — see countingWriter.ReadFrom.
type countingReader struct {
	r      io.Reader
	n      int64
	base   int64
	report func(int64)
}

func (c *countingReader) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	c.n += int64(n)
	c.report(c.base + c.n)
	return n, err
}

// Mkdir creates p and any necessary parents on the remote host.
func (c *Client) Mkdir(p string) error {
	if err := c.sc.MkdirAll(p); err != nil {
		return fmt.Errorf("sftpx: mkdir %s: %w", p, err)
	}
	return nil
}

// Remove deletes a remote file or empty directory.
func (c *Client) Remove(p string) error {
	if err := c.sc.Remove(p); err != nil {
		return fmt.Errorf("sftpx: remove %s: %w", p, err)
	}
	return nil
}

// Rename moves oldp to newp on the remote host.
func (c *Client) Rename(oldp, newp string) error {
	if err := c.sc.Rename(oldp, newp); err != nil {
		return fmt.Errorf("sftpx: rename %s -> %s: %w", oldp, newp, err)
	}
	return nil
}
