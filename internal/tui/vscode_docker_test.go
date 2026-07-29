package tui

import (
	"strings"
	"sync"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"hop/internal/dockerenv"
	"hop/internal/store"
)

// The working-directory half of the VS Code binding, end to end against a real
// OpenSSH server running real shells (see internal/dockerenv/testdata/shellhost):
// connect the way a user does, let hop install its prompt hook into the shell, cd
// somewhere, and press the key. What VS Code is asked to open has to be the
// directory the shell is actually standing in.
//
// Nothing here is stubbed except the launch itself — there is no VS Code on a CI
// box, and the path handed to it is the whole feature, so the launcher is the one
// seam. Everything upstream of it is real: a real sshd, a real bash and zsh
// emitting real OSC 7 because hop asked them to, and a real `cd` moving it.
//
//	HOP_DOCKER_E2E=1 go test ./internal/tui/ -run VSCodeE2E -v
//
// The fish account is here for the other half of the promise: a shell hop cannot
// install the hook into must be left alone, and the binding must fall back to the
// host's default directory rather than to a wrong one.

var (
	shellHostOnce sync.Once
	shellHost     *dockerenv.ShellHost
	shellHostErr  error
)

// shellHostServer brings the shell host up on first use and shares it across the
// tests in this file. It is started lazily rather than from TestMain so a run of
// the two-factor tests does not pay for an image it will not use; TestMain stops
// it if it was ever started.
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

// stopShellHost tears the container down, if it was ever brought up. Called from
// TestMain.
func stopShellHost() {
	if shellHost != nil {
		shellHost.Stop()
	}
}

// bash and zsh: the hook installs, so the directory the shell is standing in is
// the directory VS Code is asked to open — including one with a space in its name.
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

			// The hook lands on its own, a moment after the shell does: hop probes the
			// login shell over a second channel, waits for the shell to say something,
			// and only then types the hook in.
			if dir := waitForPaneCwd(t, m, c.home); dir != c.home {
				t.Fatalf("the shell reported %q on login, want %q\npane:\n%s", dir, c.home, sh.pane.View())
			}

			// And it leaves no trace: the line hop typed into the prompt to install the
			// hook is taken back off the screen once it has run. That happens a moment
			// after the report arrives — the prompt underneath the echo has to be drawn
			// before the rows can be counted — so it is waited for, not asserted on the
			// same instant.
			if view := waitForCleanPane(sh); strings.Contains(view, "hop_cwd") {
				t.Fatalf("the hook hop typed is still on screen:\n%s", view)
			}

			typeLine(t, m, "cd "+shellQuote(c.cd))
			if dir := waitForPaneCwd(t, m, c.cd); dir != c.cd {
				t.Fatalf("after cd: the shell reported %q, want %q\npane:\n%s", dir, c.cd, sh.pane.View())
			}

			// And the chord, from inside the pane, where the user is: ctrl+o out, ctrl+o
			// again to open what that shell was standing in.
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

// fish: the hook is not for it, so it is not sent one — and the binding falls back
// to opening the host with no path, exactly as it did before any of this existed.
func TestVSCodeE2EFallsBackOnAnUnhookableShell(t *testing.T) {
	m, sh := connectShellHost(t, dockerenv.FishUser)

	// Long enough for a hook to have arrived and a prompt to have been drawn: what
	// is being checked is that neither happened.
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

// waitForCleanPane polls the pane until the line hop typed is gone from it, and
// returns the last view it saw either way.
func waitForCleanPane(sh *shellTab) string {
	deadline := time.Now().Add(10 * time.Second)
	view := sh.pane.View()
	for strings.Contains(view, "hop_cwd") && time.Now().Before(deadline) {
		time.Sleep(50 * time.Millisecond)
		view = sh.pane.View()
	}
	return view
}

// connectShellHost connects to the shell host as user, the way a keypress does,
// and returns the model with the landed shell focused.
func connectShellHost(t *testing.T, user string) (*model, *shellTab) {
	t.Helper()
	srv := shellHostServer(t)
	trustHostKey(t, srv.Port)

	m := hostMgmtModel(t, store.Host{
		Alias:        "shellhost",
		HostName:     "127.0.0.1",
		Port:         srv.Port,
		User:         user,
		IdentityFile: srv.ClientKey,
	})
	m.prompts = make(chan authPromptMsg, 1)
	m.connecting = map[string]bool{}
	// A pane with a real size, so what the shell draws into it is there to be
	// looked at: the fish case reads the pane to prove the hook was never typed.
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

// waitForShellHostConnect collects the connectedMsg the dial produces. The host
// takes a public key, so nothing is asked interactively along the way.
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

// waitForPaneCwd polls the model for the shell's reported directory until it is
// want, and returns whatever it ended up being.
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

// typeLine types line into the focused shell pane, a key at a time, and presses
// enter — the same path a user's keystrokes take.
func typeLine(t *testing.T, m *model, line string) {
	t.Helper()
	for _, r := range line {
		m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	m.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
}
