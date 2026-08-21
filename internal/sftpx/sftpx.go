// Package sftpx provides a small SFTP client layered over an existing SSH connection.
package sftpx

import (
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
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

// Home returns a best-effort remote starting directory.
func (c *Client) Home() (string, error) {
	if wd, err := c.sc.Getwd(); err == nil && wd != "" {
		return wd, nil
	}
	if rp, err := c.sc.RealPath("."); err == nil && rp != "" {
		return rp, nil
	}
	return "/", nil
}

// List reads dir and returns its entries sorted directories-first, then by name.
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

// Download copies a remote file to localPath, creating parent directories as needed.
func (c *Client) Download(remotePath, localPath string) (int64, error) {
	return c.DownloadProgress(remotePath, localPath, nil)
}

// DownloadProgress is Download; progress may be nil and is called from the calling goroutine.
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

// Upload copies a local file to remotePath.
func (c *Client) Upload(localPath, remotePath string) (int64, error) {
	return c.UploadProgress(localPath, remotePath, nil)
}

// UploadProgress is Upload, with the same progress caveats as DownloadProgress.
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

// counted wraps w to report bytes written, or returns w unchanged when report is nil.
func counted(w io.Writer, report func(int64)) io.Writer {
	if report == nil {
		return w
	}
	return &countingWriter{w: w, report: report}
}

// countingWriter passes bytes through to w and reports the running total.
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

// ReadFrom must exist or io.Copy loses *sftp.File's concurrent write pipeline and falls back
// to sequential 32 KiB writes; the cost is that bytes are counted on read, ahead of the wire.
func (c *countingWriter) ReadFrom(r io.Reader) (int64, error) {
	rf, ok := c.w.(io.ReaderFrom)
	if !ok {
		// onlyWriter hides this method from io.Copy, which would otherwise call it right back.
		return io.Copy(onlyWriter{c}, r)
	}
	n, err := rf.ReadFrom(&countingReader{r: r, base: c.n, report: c.report})
	c.n += n
	return n, err
}

// onlyWriter hides every other method so io.Copy cannot re-select the fast path it is inside.
type onlyWriter struct{ io.Writer }

// countingReader reports bytes as they are read, continuing the running total from base.
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

// Copy copies srcPath into dstDir/<base(srcPath)> on the same host, recursing into directories.
// pkg/sftp exposes no server-side copy extension, so every byte round-trips through this process.
// A failure part way through is not rolled back.
func (c *Client) Copy(srcPath, dstDir string, progress func(int64)) (int64, error) {
	dstPath := path.Join(dstDir, path.Base(path.Clean(srcPath)))
	if err := checkNotIntoItself("copy", srcPath, dstPath); err != nil {
		return 0, err
	}

	fi, err := c.sc.Lstat(srcPath)
	if err != nil {
		return 0, fmt.Errorf("sftpx: copy stat %s: %w", srcPath, err)
	}

	var total int64
	if err := c.copyTree(srcPath, dstPath, fi, &total, progress); err != nil {
		return total, err
	}
	return total, nil
}

// copyTree copies one already-stat'ed source node to dstPath, recursing for directories.
func (c *Client) copyTree(srcPath, dstPath string, fi os.FileInfo, total *int64, progress func(int64)) error {
	// Recreated, not followed: Open refuses a directory link, and following one duplicates a tree.
	if fi.Mode()&fs.ModeSymlink != 0 {
		target, err := c.sc.ReadLink(srcPath)
		if err != nil {
			return fmt.Errorf("sftpx: copy readlink %s: %w", srcPath, err)
		}
		if err := c.sc.Symlink(target, dstPath); err != nil {
			return fmt.Errorf("sftpx: copy symlink %s: %w", dstPath, err)
		}
		return nil
	}
	if !fi.IsDir() {
		return c.copyFile(srcPath, dstPath, fi.Mode(), total, progress)
	}

	if err := c.sc.MkdirAll(dstPath); err != nil {
		return fmt.Errorf("sftpx: copy mkdir %s: %w", dstPath, err)
	}
	if err := c.sc.Chmod(dstPath, fi.Mode().Perm()); err != nil {
		return fmt.Errorf("sftpx: copy chmod %s: %w", dstPath, err)
	}

	children, err := c.sc.ReadDir(srcPath)
	if err != nil {
		return fmt.Errorf("sftpx: copy read %s: %w", srcPath, err)
	}
	for _, child := range children {
		err := c.copyTree(
			path.Join(srcPath, child.Name()),
			path.Join(dstPath, child.Name()),
			child, total, progress,
		)
		if err != nil {
			return err
		}
	}
	return nil
}

// copyFile streams one remote file to another over the same connection, preserving the mode.
func (c *Client) copyFile(srcPath, dstPath string, mode fs.FileMode, total *int64, progress func(int64)) error {
	rf, err := c.sc.Open(srcPath)
	if err != nil {
		return fmt.Errorf("sftpx: copy open %s: %w", srcPath, err)
	}
	defer rf.Close()

	wf, err := c.sc.Create(dstPath)
	if err != nil {
		return fmt.Errorf("sftpx: copy create %s: %w", dstPath, err)
	}
	defer wf.Close()

	// Shift this file's count by earlier files, so the caller sees one cumulative number.
	base := *total
	var report func(int64)
	if progress != nil {
		report = func(n int64) { progress(base + n) }
	}

	n, err := io.Copy(counted(wf, report), rf)
	*total = base + n
	if err != nil {
		return fmt.Errorf("sftpx: copy %s -> %s: %w", srcPath, dstPath, err)
	}
	if err := c.sc.Chmod(dstPath, mode.Perm()); err != nil {
		return fmt.Errorf("sftpx: copy chmod %s: %w", dstPath, err)
	}
	return nil
}

// Move relocates srcPath into dstDir/<base(srcPath)>, falling back to copy-then-delete when a
// server-side rename fails (typically across a mount boundary).
func (c *Client) Move(srcPath, dstDir string, progress func(int64)) error {
	dstPath := path.Join(dstDir, path.Base(path.Clean(srcPath)))
	if err := checkNotIntoItself("move", srcPath, dstPath); err != nil {
		return err
	}
	// Backstop: the copy fallback would otherwise truncate an existing destination.
	if _, err := c.sc.Lstat(dstPath); err == nil {
		return fmt.Errorf("sftpx: move %s: %s already exists", srcPath, dstPath)
	}

	if err := c.sc.Rename(srcPath, dstPath); err == nil {
		return nil
	}
	return c.moveByCopy(srcPath, dstDir, progress)
}

// moveByCopy is Move's cross-filesystem fallback; split out because a test cannot fail a rename.
func (c *Client) moveByCopy(srcPath, dstDir string, progress func(int64)) error {
	if _, err := c.Copy(srcPath, dstDir, progress); err != nil {
		return fmt.Errorf("sftpx: move %s: %w", srcPath, err)
	}
	if err := c.sc.RemoveAll(srcPath); err != nil {
		return fmt.Errorf("sftpx: move remove source %s: %w", srcPath, err)
	}
	return nil
}

// checkNotIntoItself rejects a transfer onto its own source or into its own subtree.
func checkNotIntoItself(op, srcPath, dstPath string) error {
	src, dst := path.Clean(srcPath), path.Clean(dstPath)
	if src == dst {
		return fmt.Errorf("sftpx: %s %s: source and destination are the same path", op, src)
	}
	if strings.HasPrefix(dst, src+"/") {
		return fmt.Errorf("sftpx: %s %s: destination %s is inside the source", op, src, dst)
	}
	return nil
}
