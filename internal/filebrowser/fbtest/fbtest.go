// Package fbtest provides a stand-in for filebrowser.Client.
package fbtest

import (
	"errors"

	"hop/internal/sftpx"
)

// Stub implements filebrowser.Client and does nothing on purpose; embed and override.
type Stub struct {
	// Dir is what Home reports, and Entries what every listing returns.
	Dir     string
	Entries []sftpx.Entry
	// Files is what a preview reads, by absolute path.
	Files map[string]string
}

// ReadHead serves Files, cut to n bytes as the real client cuts a read.
func (s Stub) ReadHead(p string, n int64) ([]byte, error) {
	body, ok := s.Files[p]
	if !ok {
		return nil, errors.New("no such file")
	}
	if int64(len(body)) > n {
		body = body[:n]
	}
	return []byte(body), nil
}

func (s Stub) Home() (string, error) { return s.Dir, nil }

func (s Stub) List(string) ([]sftpx.Entry, error) { return s.Entries, nil }

func (Stub) DownloadProgress(_, _ string, _ func(int64)) (int64, error) { return 0, nil }

func (Stub) UploadProgress(_, _ string, _ func(int64)) (int64, error) { return 0, nil }

func (Stub) Mkdir(string) error { return nil }

func (Stub) Remove(string) error { return nil }

func (Stub) Rename(_, _ string) error { return nil }

func (Stub) Copy(_, _ string, _ func(int64)) (int64, error) { return 0, nil }

func (Stub) Move(_, _ string, _ func(int64)) error { return nil }

func (Stub) Close() error { return nil }
