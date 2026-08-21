package tui

import (
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"hop/internal/sshx"
	"hop/internal/store"
	"hop/internal/terminal"
)

type opened struct {
	alias string
	path  string
	calls int
}

// stubVSCode swaps in a recording VS Code launcher for the test.
func stubVSCode(t *testing.T, err error) *opened {
	t.Helper()
	rec := &opened{}
	prev := openVSCode
	openVSCode = func(alias, path string) error {
		rec.alias, rec.path, rec.calls = alias, path, rec.calls+1
		return err
	}
	t.Cleanup(func() { openVSCode = prev })
	return rec
}

// cwdPane builds a pane whose remote reported dir over OSC 7; the returned writer is the remote.
func cwdPane(t *testing.T, dir string) (*terminal.Pane, io.Writer) {
	t.Helper()
	pr, pw := io.Pipe()
	p := terminal.New(&sshx.Session{Stdin: nopWriteCloser{io.Discard}, Stdout: pr}, 20, 5, nil)
	t.Cleanup(func() { p.Close() })

	io.WriteString(pw, "deploy@web:~$ \x1b]7;file://web"+dir+"\x07")

	deadline := time.Now().Add(3 * time.Second)
	for p.Cwd() != dir && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if p.Cwd() != dir {
		t.Fatalf("the pane never picked up the reported cwd %q", dir)
	}
	return p, pw
}

// vscodeModel builds a model on "web" whose shell reports dir; empty dir reports nothing.
func vscodeModel(t *testing.T, shells int, dir string) (*model, io.Writer) {
	t.Helper()
	s := &session{client: &sshx.Client{}}
	var remote io.Writer = io.Discard
	for i := 0; i < shells; i++ {
		pane := fakePane()
		if dir != "" {
			p, feed := cwdPane(t, dir)
			pane = p
			if i == 0 {
				remote = feed
			}
		}
		s.shells = append(s.shells, &shellTab{id: i + 1, pane: pane})
	}
	return &model{hosts: []store.Host{{Alias: "web"}}, filtered: []int{0}, sessions: map[string]*session{"web": s}, connecting: map[string]bool{}, notify: make(chan struct{}, 1), layout: layout{paneW: 40, paneH: 12, height: 20}, focus: focus{active: "web"}}, remote
}

// 'o' opens VS Code where the host's shell is standing, not on its default directory.
func TestVSCodeOpensTheShellsDirectory(t *testing.T) {
	rec := stubVSCode(t, nil)
	m, _ := vscodeModel(t, 1, "/srv/app")

	m.handleKey(key(t, "o"))

	if rec.calls != 1 {
		t.Fatalf("VS Code was asked to open %d times, want 1", rec.calls)
	}
	if rec.alias != "web" || rec.path != "/srv/app" {
		t.Fatalf("opened (%q, %q), want (%q, %q)", rec.alias, rec.path, "web", "/srv/app")
	}
	if m.statusKind != statusOK || !strings.Contains(m.status, "/srv/app") {
		t.Fatalf("status = %q (kind %v); it should name the directory", m.status, m.statusKind)
	}
}

// ctrl+o arms the leader; the second key leaves the pane and opens that shell's directory.
func TestVSCodeChordFromInsideTheShellPane(t *testing.T) {
	rec := stubVSCode(t, nil)
	m, _ := vscodeModel(t, 1, "/var/log/nginx")
	m.mode = modeShell

	m.handleKey(key(t, "ctrl+o"))
	if !m.focused() {
		t.Fatal("the leader left the pane; it is supposed to open and wait")
	}
	if rec.calls != 0 {
		t.Fatal("the leader opened VS Code on its own; it is supposed to wait")
	}

	m.handleKey(runeKey('c'))
	if m.focused() {
		t.Fatal("ctrl+o c did not leave the pane")
	}
	if rec.calls != 1 || rec.alias != "web" || rec.path != "/var/log/nginx" {
		t.Fatalf("opened (%q, %q) in %d calls, want (%q, %q) once",
			rec.alias, rec.path, rec.calls, "web", "/var/log/nginx")
	}
}

// A ctrl+o that completes no chord launches nothing.
func TestVSCodeChordNeedsTheSecondPressInTime(t *testing.T) {
	rec := stubVSCode(t, nil)
	m, _ := vscodeModel(t, 1, "/srv/app")
	m.mode = modeShell

	m.handleKey(key(t, "ctrl+o"))
	m.handleKey(runeKey('z'))
	if rec.calls != 0 {
		t.Fatalf("a key that is not a chord opened VS Code (%d calls)", rec.calls)
	}
	if m.leaderArmed() {
		t.Fatal("a key that is not a chord left the leader open")
	}

	// 'c' with no leader open is the shell's, not hop's.
	m.handleKey(runeKey('c'))
	if rec.calls != 0 {
		t.Fatalf("a bare c opened VS Code with no leader open (%d calls)", rec.calls)
	}
}

// alt+o is the remote's key (vim's ESC-o), not hop's.
func TestVSCodeIsNotOnAltO(t *testing.T) {
	rec := stubVSCode(t, nil)
	m, _ := vscodeModel(t, 1, "/srv/app")
	m.mode = modeShell

	m.handleKey(altKey("o"))

	if rec.calls != 0 {
		t.Fatalf("alt+o opened VS Code (%d calls); it belongs to the remote", rec.calls)
	}
	if !m.focused() {
		t.Fatal("alt+o left the pane")
	}
}

// With no shell to ask, the binding opens the host with no path and says so.
func TestVSCodeFallsBackWithoutAShell(t *testing.T) {
	rec := stubVSCode(t, nil)
	m, _ := vscodeModel(t, 0, "")

	m.handleKey(key(t, "o"))

	if rec.calls != 1 {
		t.Fatalf("VS Code was asked to open %d times, want 1 — the fallback still opens", rec.calls)
	}
	if rec.path != "" {
		t.Fatalf("path = %q, want empty: there is no directory to open", rec.path)
	}
	if m.statusKind != statusOK || !strings.Contains(m.status, "default dir") {
		t.Fatalf("status = %q (kind %v); the fallback should say it is the default directory", m.status, m.statusKind)
	}
}

// A shell with no hook installed reports no directory, and takes the same fallback.
func TestVSCodeFallsBackWhenTheShellReportsNothing(t *testing.T) {
	rec := stubVSCode(t, nil)
	m, _ := vscodeModel(t, 1, "")

	m.handleKey(key(t, "o"))

	if rec.path != "" {
		t.Fatalf("path = %q, want empty", rec.path)
	}
}

// A dropped session's stale directory is not opened.
func TestVSCodeIgnoresADeadSessionsDirectory(t *testing.T) {
	rec := stubVSCode(t, nil)
	m, _ := vscodeModel(t, 1, "/srv/app")
	m.sessions["web"].dead = true

	m.openVSCodeAt("web")

	if rec.path != "" {
		t.Fatalf("path = %q, want empty: the session is gone", rec.path)
	}
}

func TestVSCodeReportsALaunchFailure(t *testing.T) {
	stubVSCode(t, errors.New("exec: \"code\": executable file not found in $PATH"))
	m, _ := vscodeModel(t, 1, "/srv/app")

	m.handleKey(key(t, "o"))

	if m.statusKind != statusErr || !strings.Contains(m.status, "vscode") {
		t.Fatalf("status = %q (kind %v), want an error naming vscode", m.status, m.statusKind)
	}
}

// The leader menu offers "vs code here" only when there is a directory to open.
func TestFooterNamesTheChordOnlyWithADirectory(t *testing.T) {
	m, _ := vscodeModel(t, 1, "/srv/app")
	m.mode, m.width = modeShell, 200
	m.chords.leaderAlias = m.active
	if !strings.Contains(m.renderFooter(), "vs code here") {
		t.Fatalf("the leader menu does not offer the chord with a directory to hand:\n%s", m.renderFooter())
	}

	bare, _ := vscodeModel(t, 1, "")
	bare.mode, bare.width = modeShell, 200
	bare.chords.leaderAlias = bare.active
	if strings.Contains(bare.renderFooter(), "vs code here") {
		t.Fatalf("the leader menu offers the chord with no directory to open:\n%s", bare.renderFooter())
	}

	// A narrow footer drops the chord; a wide one gets it back, still only with a directory.
	m.chords.leaderAlias = ""
	m.width = 60
	if got := m.renderFooter(); strings.Contains(got, "vs code here") {
		t.Fatalf("at 60 columns the shell footer should point at the leader, not list its keys:\n%s", got)
	}
	m.width = 200
	if got := m.renderFooter(); !strings.Contains(got, "vs code here") {
		t.Fatalf("a wide shell footer does not spend its room on the chord:\n%s", got)
	}
	bare.width = 200
	if got := bare.renderFooter(); strings.Contains(got, "vs code here") {
		t.Fatalf("a wide shell footer offers the chord with no directory to open:\n%s", got)
	}
}
