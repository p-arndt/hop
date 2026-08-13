package tui

import (
	"encoding/base64"
	"io"
	"sync"
	"testing"
	"time"

	"hop/internal/config"
	"hop/internal/sshx"
	"hop/internal/terminal"
)

// clipModel builds a model with the clipboard writer replaced and returns a reader for
// whatever the sink was handed: a test must never write the real clipboard.
func clipModel(on bool) (*model, *terminal.Pane, func() string) {
	var mu sync.Mutex
	var got string

	m := &model{cfg: config.Config{Clipboard: on}}
	m.clipWrite = func(text string) error {
		mu.Lock()
		defer mu.Unlock()
		got = text
		return nil
	}
	m.applyClipboard()

	// The output pump starts reading the moment New returns, so the pipe holds the yank
	// back until the sink is installed — otherwise the pump can drop the OSC 52 first.
	pr, pw := io.Pipe()
	pane := terminal.New(&sshx.Session{
		Stdin:  nopWriteCloser{io.Discard},
		Stdout: pr,
	}, 20, 5, nil)
	m.armClipboard(pane)
	go func() {
		_, _ = io.WriteString(pw, "\x1b]52;c;"+base64.StdEncoding.EncodeToString([]byte("yanked"))+"\x07")
		pw.Close()
	}()

	return m, pane, func() string {
		mu.Lock()
		defer mu.Unlock()
		return got
	}
}

// waitClip gives the sink — which runs on the pane's output pump, and then on a
// goroutine of its own — a moment to be called.
func waitClip(read func() string, want string) bool {
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if read() == want {
			return true
		}
		time.Sleep(10 * time.Millisecond)
	}
	return read() == want
}

// A yank on the remote host reaches the local clipboard: the pane decodes the
// sequence, the sink hop installed writes it.
func TestRemoteYankReachesTheClipboard(t *testing.T) {
	_, pane, read := clipModel(true)
	defer pane.Close()

	if !waitClip(read, "yanked") {
		t.Fatalf("the clipboard was written %q, want the yanked text", read())
	}
}

// With the setting off, the sequence is decoded and dropped — the point of the setting,
// since everything on the far end can reach this channel.
func TestRemoteYankIsDroppedWhenTheSettingIsOff(t *testing.T) {
	_, pane, read := clipModel(false)
	defer pane.Close()

	// Nothing to wait for, so the absence has to be given time to be an absence.
	time.Sleep(200 * time.Millisecond)
	if read() != "" {
		t.Fatalf("the clipboard was written %q with the setting off", read())
	}
}

// The setting is consulted when the write happens, not when the pane was opened, so
// switching it off reaches the panes that are already running.
func TestClipboardSettingAppliesToOpenPanes(t *testing.T) {
	m, pane, read := clipModel(true)
	defer pane.Close()

	if !waitClip(read, "yanked") {
		t.Fatal("the first yank never arrived")
	}

	m.cfg.Clipboard = false
	m.applyClipboard()
	if m.clipOK.Load() {
		t.Fatal("the setting was not pushed to the panes")
	}
}
