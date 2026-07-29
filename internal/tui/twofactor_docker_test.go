package tui

import (
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"hop/internal/dockerenv"
	"hop/internal/store"
)

// The whole stack, end to end, against a real OpenSSH server running a real
// pam_google_authenticator (see internal/dockerenv): press enter on a host, get
// the card, type a code into it the way a user would, and end up with a live
// remote shell.
//
// internal/sshx has its own Docker tests for the protocol side. These are here
// for the part only the TUI can answer: that the card the user actually types
// into is wired to the dial correctly, that a real dial parks on it without
// deadlocking the event loop, and that what lands afterwards is a working
// session rather than a successful handshake.
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
	os.Exit(code)
}

// Pressing enter on a 2FA host puts the card up, and the code typed into it
// connects. This is the feature, from the keypress that starts it to the shell
// that comes out.
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

// The hardened setup — key, then code — through the card. The key alone gets a
// partial success, so the user is asked exactly once and the connection still
// lands.
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

// A PAM stack that wants the password and then the code drives the card twice,
// each time with the server's own wording. The user answers each in turn.
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

// esc on the card gives up: the dial ends, the spinner clears, and the failure
// is reported as the user's own decision rather than as a broken host.
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

// A wrong code re-prompts on the same connection, and the card comes back with
// an empty field rather than the rejected code still in it.
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

// twoFactorServer skips the test unless the environment opted into Docker, and
// hands back the running container. Every test has to go through it before
// touching a port: without Docker there is no server, and a port read off it
// would be read before any skip could happen.
func twoFactorServer(t *testing.T) *dockerenv.TwoFactor {
	t.Helper()
	if !dockerenv.Enabled() {
		t.Skipf("set %s=1 to run the Docker-backed two-factor tests", dockerenv.EnvVar)
	}
	return server
}

// twoFactorModel builds a model holding one host that points at the container,
// with the host key already trusted and no agent in the way — the state a user
// is in on the second run against a host they have approved once.
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

// pump runs cmd off the UI thread and returns the channel its message will
// arrive on, the way Bubble Tea's event loop would. The auth challenge is
// delivered to the model as it arrives, so the card is up by the time pump
// returns — which is exactly the sequencing a real run has, and the reason the
// dial must not be waited on synchronously.
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

// dispatch runs cmd off the UI thread and puts its message on msgs, the way
// Bubble Tea's event loop does — including unwrapping a tea.Batch into its
// commands, which is what openShell returns when it starts the spinner
// alongside the dial. The spinner's own ticks are dropped: they are a redraw
// clock, and nothing here draws.
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

// waitForCard gives the dial a moment to ask something. It returns as soon as
// the card is up, and also returns quietly if the dial finished instead — a
// connect that never asks is a legitimate outcome for some of these tests.
func waitForCard(t *testing.T, m *model, msgs chan tea.Msg) {
	t.Helper()
	select {
	case msg := <-m.prompts:
		m.openAuthPrompt(msg)
	case <-time.After(20 * time.Second):
		t.Fatal("the dial neither asked for anything nor landed")
	case msg := <-msgs:
		// The dial finished without asking. Put it back for waitForConnect.
		msgs <- msg
	}
}

// waitForConnect collects the connectedMsg the dial eventually produces,
// answering any further challenges the server asks along the way.
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

// trustHostKey records the container's host key in a throwaway ~/.ssh, so these
// tests exercise the authentication card rather than the host-key one. It is the
// TUI's own first-contact flow, run once: dial, take the fingerprint off the
// error, and accept it.
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

	// Refuse whatever it asks: the host key is recorded before authentication is
	// even attempted, so there is no reason to spend a code here.
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
	// Accepting appends the key to known_hosts on the retry; the retry itself is
	// abandoned the same way.
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
