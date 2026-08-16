package filebrowser

import (
	"fmt"
	"os"
	"path"
	"path/filepath"

	tea "github.com/charmbracelet/bubbletea"

	"hop/internal/sftpx"
)

// transfer is a copy in flight. STUB — transfers are still synchronous.
type transfer struct {
	label string
	done  int64
	total int64
}

// upload copies a local file into the current remote directory. STUB.
func (b *Browser) upload() tea.Cmd { return nil }

// handleTransferMsg takes the messages a running transfer produces. STUB.
func (b *Browser) handleTransferMsg(msg tea.Msg) tea.Cmd { return nil }

// progressLine renders the transfer in flight into at most w cells. STUB.
func (b *Browser) progressLine(w int) string { return b.xfer.label }

// openInApp fetches the file under the cursor and hands the local copy to the desktop's
// default application, fire-and-forget. "o" on a directory is a no-op.
func (b *Browser) openInApp() tea.Cmd {
	e, ok := b.selected()
	if !ok || e.IsDir {
		return nil
	}
	if err := checkLocalName(e.Name); err != nil {
		b.fail(err)
		return nil
	}
	// The OS default handler would run an executable-extension file rather than view it,
	// so a server that names a payload like a document could get code executed on a
	// single "o". An explicit OpenWith passes the file to a program the user chose, so
	// that path is left alone.
	if b.opts.OpenWith == "" && executableName(e.Name) {
		b.fail(fmt.Errorf("refusing to open executable file %q — use d to download instead", e.Name))
		return nil
	}

	local, err := b.fetch(e)
	if err != nil {
		b.fail(err)
		return nil
	}
	cmd := openCmd(b.opts.OpenWith, local)
	if err := cmd.Start(); err != nil {
		b.fail(fmt.Errorf("open %s: %w", e.Name, err))
		return nil
	}
	// The launcher exits as soon as the real application is up; reap it.
	go cmd.Wait()
	b.ok("opened " + e.Name)
	return nil
}

// download copies the file under the cursor into downloadDir, where — unlike the scratch
// copy "o" makes — it is meant to be kept.
func (b *Browser) download() tea.Cmd {
	e, ok := b.selected()
	if !ok || e.IsDir {
		return nil
	}
	if err := checkLocalName(e.Name); err != nil {
		b.fail(err)
		return nil
	}

	local := filepath.Join(b.opts.DownloadDir, e.Name)
	if err := os.MkdirAll(b.opts.DownloadDir, 0o755); err != nil {
		b.fail(err)
		return nil
	}
	if _, err := b.client.Download(path.Join(b.cwd, e.Name), local); err != nil {
		b.fail(err)
		return nil
	}
	b.ok(fmt.Sprintf("downloaded %s → %s", e.Name, b.opts.DownloadDir))
	return nil
}

// fetch downloads e into the scratch directory and returns the local path.
func (b *Browser) fetch(e sftpx.Entry) (string, error) {
	dir, err := b.scratch()
	if err != nil {
		return "", err
	}
	local := filepath.Join(dir, e.Name)
	if _, err := b.client.Download(path.Join(b.cwd, e.Name), local); err != nil {
		return "", err
	}
	// Mark the copy the way a browser download would be. On macOS that sets
	// com.apple.quarantine, keeping Gatekeeper in the loop for types the extension guard
	// does not know about; elsewhere it is a no-op.
	if err := quarantine(local); err != nil {
		return "", fmt.Errorf("quarantine %s: %w", e.Name, err)
	}
	return local, nil
}

// scratch returns the browser's temp directory, creating it on first use. Files handed
// to the desktop's default app land here rather than in downloadDir. It is never
// removed: the app may still hold a file open long after the browser closes.
func (b *Browser) scratch() (string, error) {
	if b.tmpDir != "" {
		return b.tmpDir, nil
	}
	dir, err := os.MkdirTemp("", "hop-sftp-*")
	if err != nil {
		return "", err
	}
	b.tmpDir = dir
	return dir, nil
}
