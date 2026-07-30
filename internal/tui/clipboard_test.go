package tui

import (
	"encoding/base64"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"hop/internal/config"
	"hop/internal/sshx"
	"hop/internal/terminal"
)

// clipModel builds a model with the clipboard writer replaced, and returns a
// reader for whatever the sink was handed. A test must never write the clipboard
// of the machine it is running on.
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

	pane := terminal.New(&sshx.Session{
		Stdin:  nopWriteCloser{io.Discard},
		Stdout: strings.NewReader("\x1b]52;c;" + base64.StdEncoding.EncodeToString([]byte("yanked")) + "\x07"),
	}, 20, 5, nil)
	m.armClipboard(pane)

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

// With the setting off, the sequence is decoded and dropped. This is the point of
// having the setting: everything running on the far end can reach this channel, not
// only what you started.
func TestRemoteYankIsDroppedWhenTheSettingIsOff(t *testing.T) {
	_, pane, read := clipModel(false)
	defer pane.Close()

	// Nothing to wait for, so wait for the pane to have parsed the sequence at all —
	// a directory report behind it would be the tell, but here the absence has to be
	// given time to be an absence.
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
