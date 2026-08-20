// Package fbtest provides a stand-in for filebrowser.Client, so a test that needs a
// browser but does not care what the server does can say only the part it cares about.
//
// It exists because filebrowser.Client is wide — it mirrors most of sftpx.Client — while
// the tests that build a browser are usually testing something else entirely: how a click
// maps to a row, or what a reconnect restores. Without it every such test carries the
// whole interface as filler, and every method added to Client has to be added to each of
// them again.
package fbtest

import "hop/internal/sftpx"

// Stub implements filebrowser.Client and does nothing on purpose. Embed it and override
// what the test is actually about; the func fields cover the two calls a browser makes
// before it can show anything.
type Stub struct {
	// Dir is what Home reports, and Entries what every listing returns.
	Dir     string
	Entries []sftpx.Entry
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
