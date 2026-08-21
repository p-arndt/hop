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

// clipModel replaces the clipboard writer with a sink the test can read.
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

	// The pipe holds the yank back until the sink is installed.
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

// waitClip gives the sink a moment to be called.
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

func TestRemoteYankReachesTheClipboard(t *testing.T) {
	_, pane, read := clipModel(true)
	defer pane.Close()

	if !waitClip(read, "yanked") {
		t.Fatalf("the clipboard was written %q, want the yanked text", read())
	}
}

func TestRemoteYankIsDroppedWhenTheSettingIsOff(t *testing.T) {
	_, pane, read := clipModel(false)
	defer pane.Close()

	// The absence has to be given time to be an absence.
	time.Sleep(200 * time.Millisecond)
	if read() != "" {
		t.Fatalf("the clipboard was written %q with the setting off", read())
	}
}

// The setting is read at write time, so it reaches panes already running.
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
