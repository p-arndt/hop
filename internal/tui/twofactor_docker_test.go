package tui

import (
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"hop/internal/dockerenv"
	"hop/internal/store"
)

// End-to-end 2FA against a real OpenSSH + pam_google_authenticator container:
//
//	HOP_DOCKER_E2E=1 go test ./internal/tui/ -run TwoFactor -v

var server *dockerenv.TwoFactor

func TestMain(m *testing.M) {
	if !dockerenv.Enabled() {
		os.Exit(m.Run())
	}
	s, err := dockerenv.StartTwoFactor()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	server = s
	code := m.Run()
	server.Stop()
	// The shell host is started lazily by vscode_docker_test.go.
	stopShellHost()
	os.Exit(code)
}

// Keypress to shell: the dial raises the card and a correct code connects.
func TestTwoFactorConnectsThroughTheCard(t *testing.T) {
	srv := twoFactorServer(t)
	m := twoFactorModel(t, srv.CodePort, "")

	cmds := pump(t, m, m.openShell(hostUnderTest(t, m), false))

	if !m.auth.open {
		t.Fatal("the dial did not put the authentication card up")
	}
	if card := m.renderAuth(); !strings.Contains(strings.ToLower(card), "verification code") {
		t.Fatalf("the card does not show the server's prompt; card = %q", card)
	}

	typeRunes(t, m, dockerenv.Code())
	if card := m.renderAuth(); strings.Contains(card, dockerenv.Code()) {
		t.Fatal("the card printed the verification code in the clear")
	}
	m.handleKey(key(t, "enter"))

	landed := waitForConnect(t, m, cmds)
	if landed.err != nil {
		t.Fatalf("the connect failed after a correct code: %v", landed.err)
	}
	m.shellLanded(landed)

	s := m.sessions["twofactor"]
	if s == nil || len(s.shells) != 1 {
		t.Fatal("no shell came out of an authenticated connect")
	}
	if m.statusKind == statusErr {
		t.Fatalf("status reports an error after a successful connect: %q", m.status)
	}
	s.close()
}

// Key then code: the key is a partial success, so the card asks exactly once.
func TestTwoFactorAfterKeyConnectsThroughTheCard(t *testing.T) {
	srv := twoFactorServer(t)
	m := twoFactorModel(t, srv.KeyPort, srv.ClientKey)

	cmds := pump(t, m, m.openShell(hostUnderTest(t, m), false))
	if !m.auth.open {
		t.Fatal("the key alone connected, or no card came up")
	}

	typeRunes(t, m, dockerenv.Code())
	m.handleKey(key(t, "enter"))

	landed := waitForConnect(t, m, cmds)
	if landed.err != nil {
		t.Fatalf("connect after key+code: %v", landed.err)
	}
	m.shellLanded(landed)
	if s := m.sessions["twofactor"]; s == nil || len(s.shells) != 1 {
		t.Fatal("no shell came out of a key+code connect")
	} else {
		s.close()
	}
}

// Password then code drives the card twice, each with the server's own wording.
func TestTwoFactorPasswordThenCodeThroughTheCard(t *testing.T) {
	srv := twoFactorServer(t)
	m := twoFactorModel(t, srv.PasswordPort, "")

	cmds := pump(t, m, m.openShell(hostUnderTest(t, m), false))

	var asked []string
	for range 2 {
		if !m.auth.open {
			break
		}
		prompt := strings.ToLower(m.auth.ch.Questions[0].Text)
		asked = append(asked, strings.TrimSpace(prompt))

		if strings.Contains(prompt, "password") {
			typeRunes(t, m, dockerenv.Password)
		} else {
			typeRunes(t, m, dockerenv.Code())
		}
		m.handleKey(key(t, "enter"))
		waitForCard(t, m, cmds)
	}

	if len(asked) != 2 || !strings.Contains(asked[0], "password") || !strings.Contains(asked[1], "verification code") {
		t.Fatalf("the card asked %q, want a password then a verification code", asked)
	}

	landed := waitForConnect(t, m, cmds)
	if landed.err != nil {
		t.Fatalf("connect after password+code: %v", landed.err)
	}
	m.shellLanded(landed)
	if s := m.sessions["twofactor"]; s == nil {
		t.Fatal("no session came out of a password+code connect")
	} else {
		s.close()
	}
}

// esc ends the dial, clears the spinner and does not report an error.
func TestTwoFactorEscapeAbandonsTheConnect(t *testing.T) {
	srv := twoFactorServer(t)
	m := twoFactorModel(t, srv.CodePort, "")

	cmds := pump(t, m, m.openShell(hostUnderTest(t, m), false))
	if !m.auth.open {
		t.Fatal("no card to cancel")
	}
	m.handleKey(key(t, "esc"))

	landed := waitForConnect(t, m, cmds)
	if landed.err == nil {
		t.Fatal("the connect succeeded after the card was dismissed")
	}
	m.shellLanded(landed)

	if m.statusKind == statusErr {
		t.Fatalf("a cancelled connect was reported as an error: %q", m.status)
	}
	if m.connecting["twofactor"] {
		t.Fatal("the connecting spinner was left running")
	}
	if len(m.sessions) != 0 {
		t.Fatal("a session was created for a cancelled connect")
	}
}

// A wrong code re-prompts on the same connection with an empty field.
func TestTwoFactorWrongCodeReopensTheCard(t *testing.T) {
	srv := twoFactorServer(t)
	m := twoFactorModel(t, srv.CodePort, "")

	cmds := pump(t, m, m.openShell(hostUnderTest(t, m), false))
	typeRunes(t, m, "000000")
	m.handleKey(key(t, "enter"))

	waitForCard(t, m, cmds)
	if !m.auth.open {
		t.Fatal("a wrong code did not bring the card back")
	}
	if m.authAnswer() != "" {
		t.Fatalf("the card came back holding %q; the rejected code was not cleared", m.authAnswer())
	}

	typeRunes(t, m, dockerenv.Code())
	m.handleKey(key(t, "enter"))

	landed := waitForConnect(t, m, cmds)
	if landed.err != nil {
		t.Fatalf("the retry after a wrong code failed: %v", landed.err)
	}
	m.shellLanded(landed)
	if s := m.sessions["twofactor"]; s != nil {
		s.close()
	}
}

// ---- harness ----

// twoFactorServer skips unless Docker was opted into, and returns the running container.
func twoFactorServer(t *testing.T) *dockerenv.TwoFactor {
	t.Helper()
	if !dockerenv.Enabled() {
		t.Skipf("set %s=1 to run the Docker-backed two-factor tests", dockerenv.EnvVar)
	}
	return server
}

// twoFactorModel points one host at the container, host key already trusted, no agent.
func twoFactorModel(t *testing.T, port int, identityFile string) *model {
	t.Helper()
	trustHostKey(t, port)

	m := hostMgmtModel(t, store.Host{
		Alias:        "twofactor",
		HostName:     "127.0.0.1",
		Port:         port,
		User:         dockerenv.User,
		IdentityFile: identityFile,
	})
	m.prompts = make(chan authPromptMsg)
	m.connecting = map[string]bool{}
	return m
}

func hostUnderTest(t *testing.T, m *model) store.Host {
	t.Helper()
	h, ok := m.hostByAlias("twofactor")
	if !ok {
		t.Fatal("the seeded host is missing")
	}
	return h
}

// pump runs cmd off the UI thread and returns once the card is up, as the event loop would.
func pump(t *testing.T, m *model, cmd tea.Cmd) chan tea.Msg {
	t.Helper()
	if cmd == nil {
		t.Fatal("openShell dispatched no command")
	}
	msgs := make(chan tea.Msg, 8)
	dispatch(msgs, cmd)

	waitForCard(t, m, msgs)
	return msgs
}

// dispatch runs cmd off the UI thread onto msgs, unwrapping batches and dropping ticks.
func dispatch(msgs chan tea.Msg, cmd tea.Cmd) {
	if cmd == nil {
		return
	}
	go func() {
		switch msg := cmd().(type) {
		case tea.BatchMsg:
			for _, sub := range msg {
				dispatch(msgs, sub)
			}
		case tickMsg:
		case nil:
		default:
			msgs <- msg
		}
	}()
}

func waitForCard(t *testing.T, m *model, msgs chan tea.Msg) {
	t.Helper()
	select {
	case msg := <-m.prompts:
		m.openAuthPrompt(msg)
	case <-time.After(20 * time.Second):
		t.Fatal("the dial neither asked for anything nor landed")
	case msg := <-msgs:
		// Finished without asking: put it back for waitForConnect.
		msgs <- msg
	}
}

func waitForConnect(t *testing.T, m *model, msgs chan tea.Msg) connectedMsg {
	t.Helper()
	deadline := time.After(30 * time.Second)
	for {
		select {
		case msg := <-msgs:
			c, ok := msg.(connectedMsg)
			if !ok {
				t.Fatalf("dial produced %T, want connectedMsg", msg)
			}
			return c
		case p := <-m.prompts:
			m.openAuthPrompt(p)
			typeRunes(t, m, dockerenv.Code())
			m.handleKey(key(t, "enter"))
		case <-deadline:
			t.Fatal("the dial never landed")
		}
	}
}

// trustHostKey records the container's host key in a throwaway ~/.ssh via the real flow.
func trustHostKey(t *testing.T, port int) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("SSH_AUTH_SOCK", "")

	m := hostMgmtModel(t, store.Host{
		Alias: "trust", HostName: "127.0.0.1", Port: port, User: dockerenv.User,
	})
	m.prompts = make(chan authPromptMsg, 1)
	m.connecting = map[string]bool{}

	h, _ := m.hostByAlias("trust")
	cmd := m.openShell(h, false)
	if cmd == nil {
		t.Fatal("the trusting dial dispatched no command")
	}

	msgs := make(chan tea.Msg, 4)
	dispatch(msgs, cmd)

	// Refuse whatever it asks: the host key is recorded before authentication.
	go func() {
		for p := range m.prompts {
			p.reply <- authReply{err: errAbandonTrustDial}
		}
	}()

	select {
	case msg := <-msgs:
		c, ok := msg.(connectedMsg)
		if !ok {
			t.Fatalf("first contact produced %T, want connectedMsg", msg)
		}
		m.shellLanded(c)
	case <-time.After(30 * time.Second):
		t.Fatal("the first-contact dial never landed")
	}

	if !m.hostKey.open {
		t.Fatalf("first contact did not raise the host-key card (status %q)", m.status)
	}
	// Accepting appends to known_hosts on the retry, which is abandoned the same way.
	cmd = m.acceptHostKey(m.hostKey)
	m.closeHostKey()
	dispatch(msgs, cmd)
	select {
	case <-msgs:
	case <-time.After(30 * time.Second):
		t.Fatal("the trusting retry never landed")
	}
}

// errAbandonTrustDial ends a dial that only exists to record a host key.
var errAbandonTrustDial = fmt.Errorf("test: host key recorded, dial abandoned")
