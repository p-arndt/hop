package tui

import (
	"strings"
	"sync"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"hop/internal/dockerenv"
	"hop/internal/store"
)

// The VS Code binding's working-directory half, end to end against real shells; only the
// launch is stubbed.
//
//	HOP_DOCKER_E2E=1 go test ./internal/tui/ -run VSCodeE2E -v

var (
	shellHostOnce sync.Once
	shellHost     *dockerenv.ShellHost
	shellHostErr  error
)

// shellHostServer brings the shell host up on first use and shares it across this file.
func shellHostServer(t *testing.T) *dockerenv.ShellHost {
	t.Helper()
	if !dockerenv.Enabled() {
		t.Skipf("set %s=1 to run the Docker end-to-end tests", dockerenv.EnvVar)
	}
	shellHostOnce.Do(func() { shellHost, shellHostErr = dockerenv.StartShellHost() })
	if shellHostErr != nil {
		t.Fatalf("start shell host: %v", shellHostErr)
	}
	return shellHost
}

func stopShellHost() {
	if shellHost != nil {
		shellHost.Stop()
	}
}

// bash and zsh: the hook installs, so VS Code opens where the shell is standing.
func TestVSCodeE2EOpensTheShellsDirectory(t *testing.T) {
	for _, c := range []struct {
		user string
		home string
		cd   string
	}{
		{dockerenv.BashUser, "/home/" + dockerenv.BashUser, dockerenv.SpaceDir},
		{dockerenv.ZshUser, "/home/" + dockerenv.ZshUser, "/etc/ssh"},
	} {
		t.Run(c.user, func(t *testing.T) {
			m, sh := connectShellHost(t, c.user)

			// The hook lands a moment after the shell, over a second channel.
			if dir := waitForPaneCwd(t, m, c.home); dir != c.home {
				t.Fatalf("the shell reported %q on login, want %q\npane:\n%s", dir, c.home, sh.pane.View())
			}

			// The typed line is taken back off the screen once it has run.
			if view := waitForCleanPane(sh); strings.Contains(view, "hop_cwd") {
				t.Fatalf("the hook hop typed is still on screen:\n%s", view)
			}

			typeLine(t, m, "cd "+shellQuote(c.cd))
			if dir := waitForPaneCwd(t, m, c.cd); dir != c.cd {
				t.Fatalf("after cd: the shell reported %q, want %q\npane:\n%s", dir, c.cd, sh.pane.View())
			}

			rec := stubVSCode(t, nil)
			m.handleKey(key(t, "ctrl+o"))
			m.handleKey(key(t, "ctrl+o"))
			if rec.calls != 1 {
				t.Fatalf("the chord asked VS Code to open %d times, want 1", rec.calls)
			}
			if rec.alias != "shellhost" || rec.path != c.cd {
				t.Fatalf("opened (%q, %q), want (%q, %q)", rec.alias, rec.path, "shellhost", c.cd)
			}
		})
	}
}

// fish gets no hook, so the binding falls back to the host's default directory.
func TestVSCodeE2EFallsBackOnAnUnhookableShell(t *testing.T) {
	m, sh := connectShellHost(t, dockerenv.FishUser)

	// Long enough for a hook and a prompt to have shown up, had either happened.
	time.Sleep(5 * time.Second)

	if dir := m.shellCwd("shellhost"); dir != "" {
		t.Fatalf("fish reported a directory (%q); the hook was installed where it should not be", dir)
	}
	if view := sh.pane.View(); strings.Contains(view, "hop_cwd") {
		t.Fatalf("the hook was typed into fish; pane:\n%s", view)
	}

	rec := stubVSCode(t, nil)
	m.handleKey(key(t, "ctrl+o"))
	m.handleKey(key(t, "ctrl+o"))
	if rec.calls != 1 || rec.path != "" {
		t.Fatalf("opened (%q, %q) in %d calls, want the host with no path once",
			rec.alias, rec.path, rec.calls)
	}
	if !strings.Contains(m.status, "default dir") {
		t.Fatalf("status = %q; the fallback should say which directory it opened", m.status)
	}
}

func waitForCleanPane(sh *shellTab) string {
	deadline := time.Now().Add(10 * time.Second)
	view := sh.pane.View()
	for strings.Contains(view, "hop_cwd") && time.Now().Before(deadline) {
		time.Sleep(50 * time.Millisecond)
		view = sh.pane.View()
	}
	return view
}

func connectShellHost(t *testing.T, user string) (*model, *shellTab) {
	t.Helper()
	return connectShellHostIn(t, user, "")
}

// connectShellHostIn is connectShellHost with a default directory on the host.
func connectShellHostIn(t *testing.T, user, defaultDir string) (*model, *shellTab) {
	t.Helper()
	srv := shellHostServer(t)
	trustHostKey(t, srv.Port)

	m := hostMgmtModel(t, store.Host{
		Alias:        "shellhost",
		HostName:     "127.0.0.1",
		Port:         srv.Port,
		User:         user,
		IdentityFile: srv.ClientKey,
		DefaultDir:   defaultDir,
	})
	m.prompts = make(chan authPromptMsg, 1)
	m.connecting = map[string]bool{}
	// A real pane size, so the fish case can read what the shell drew.
	m.paneW, m.paneH = 100, 24

	h, ok := m.hostByAlias("shellhost")
	if !ok {
		t.Fatal("the seeded host is missing")
	}
	cmd := m.openShell(h, false)
	if cmd == nil {
		t.Fatal("openShell dispatched no command")
	}

	msgs := make(chan tea.Msg, 8)
	dispatch(msgs, cmd)

	landed := waitForShellHostConnect(t, msgs)
	if landed.err != nil {
		t.Fatalf("connect as %s: %v\n%s", user, landed.err, srv.Logs())
	}
	m.shellLanded(landed)
	t.Cleanup(func() { m.closeAll() })

	s := m.sessions["shellhost"]
	if s == nil || s.shell() == nil {
		t.Fatal("no shell came out of the connect")
	}
	m.focusShell("shellhost")
	return m, s.shell()
}

// waitForShellHostConnect collects the connectedMsg; the host takes a public key.
func waitForShellHostConnect(t *testing.T, msgs chan tea.Msg) connectedMsg {
	t.Helper()
	deadline := time.After(30 * time.Second)
	for {
		select {
		case msg := <-msgs:
			if c, ok := msg.(connectedMsg); ok {
				return c
			}
		case <-deadline:
			t.Fatal("the dial never landed")
		}
	}
}

func waitForPaneCwd(t *testing.T, m *model, want string) string {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		if dir := m.shellCwd("shellhost"); dir == want {
			return dir
		}
		time.Sleep(50 * time.Millisecond)
	}
	return m.shellCwd("shellhost")
}

func typeLine(t *testing.T, m *model, line string) {
	t.Helper()
	for _, r := range line {
		m.handleKey(tea.KeyPressMsg{Code: r, Text: string(r)})
	}
	m.handleKey(tea.KeyPressMsg{Code: tea.KeyEnter})
}
