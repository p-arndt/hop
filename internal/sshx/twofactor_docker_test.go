package sshx

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"hop/internal/dockerenv"
	"hop/internal/store"
)

// End-to-end authentication against a real OpenSSH server running a real
// pam_google_authenticator stack, in Docker (see testdata/twofactor). Everything
// else in this package's tests talks to an in-process x/crypto/ssh server, which
// answers whatever we tell it to; these tests answer to Ubuntu's sshd and
// Ubuntu's PAM instead, so they are the ones that can say hop actually works on a
// box with 2FA turned on.
//
// They are opt-in — Docker is not on every machine and the image takes a minute
// to build the first time:
//
//	HOP_DOCKER_E2E=1 go test ./internal/sshx/ -run TwoFactor -v
//
// The container serves four ports, one per shape of two-factor login: the code
// alone, publickey-then-code, password-then-code, and two methods offered as
// alternatives. See the Dockerfile.

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

// ---- the tests ----

// The plain 2FA case: the server asks for nothing but a verification code, and
// hop answers it. This is the setup the Ubuntu guides produce.
func TestTwoFactorCodeOnly(t *testing.T) {
	requireDocker(t)
	p := codePrompter()

	cli := trustAndConnect(t, hostAt(server.CodePort, ""), p)
	defer cli.Close()

	if got := run(t, cli, "id -un"); !strings.Contains(got, dockerenv.User) {
		t.Fatalf("`id -un` on the far side printed %q, want the logged-in user", got)
	}

	asked := p.challenges()
	if len(asked) != 1 {
		t.Fatalf("the user was asked %d times, want once", len(asked))
	}
	if q := asked[0].Questions[0]; !strings.Contains(strings.ToLower(q.Text), "verification code") {
		t.Fatalf("prompt = %q, want the server's own verification-code wording", q.Text)
	}
}

// The hardened setup: `AuthenticationMethods publickey,keyboard-interactive`.
// The key gets a *partial* success and the server then still demands the code.
// This is the case that would break if hop stopped at the first method, and it
// is the reason the code cannot live behind a "retry the dial" prompt.
func TestTwoFactorAfterPublicKey(t *testing.T) {
	requireDocker(t)
	p := codePrompter()

	cli := trustAndConnect(t, hostAt(server.KeyPort, server.ClientKey), p)
	defer cli.Close()

	if got := run(t, cli, "id -un"); !strings.Contains(got, dockerenv.User) {
		t.Fatalf("`id -un` printed %q, want the logged-in user", got)
	}
	if asked := p.challenges(); len(asked) != 1 {
		t.Fatalf("the user was asked %d times after the key was accepted, want once", len(asked))
	}
}

// A PAM stack with both pam_unix and pam_google_authenticator asks two separate
// questions. hop has to answer each with the right thing, which means going by
// the server's prompt text rather than by position.
func TestTwoFactorPasswordThenCode(t *testing.T) {
	requireDocker(t)
	p := codePrompter()

	cli := trustAndConnect(t, hostAt(server.PasswordPort, ""), p)
	defer cli.Close()

	if got := run(t, cli, "id -un"); !strings.Contains(got, dockerenv.User) {
		t.Fatalf("`id -un` printed %q, want the logged-in user", got)
	}

	var prompts []string
	for _, c := range p.challenges() {
		for _, q := range c.Questions {
			prompts = append(prompts, strings.ToLower(strings.TrimSpace(q.Text)))
		}
	}
	if len(prompts) != 2 {
		t.Fatalf("questions asked = %q, want a password and a code", prompts)
	}
	if !strings.Contains(prompts[0], "password") || !strings.Contains(prompts[1], "verification code") {
		t.Fatalf("questions asked = %q, want password then verification code", prompts)
	}
}

// Every question the server asks for a secret comes through with echo off. It is
// what the card keys its masking off, so a server that says "do not show this"
// has to reach hop saying it.
func TestTwoFactorQuestionsAreNotEchoed(t *testing.T) {
	requireDocker(t)
	p := codePrompter()

	cli := trustAndConnect(t, hostAt(server.PasswordPort, ""), p)
	defer cli.Close()

	for _, c := range p.challenges() {
		for _, q := range c.Questions {
			if q.Echo {
				t.Fatalf("the server marked %q echoing; it would be typed in the clear", q.Text)
			}
		}
	}
}

// A mistyped code re-prompts on the *same* connection and the second attempt
// gets in. This is ssh.RetryableAuthMethod doing its job: a fresh dial would
// need a fresh code, and the point of retrying inside the handshake is that the
// one the user is reading off their phone is still valid.
func TestTwoFactorWrongCodeRetriesInTheSameHandshake(t *testing.T) {
	requireDocker(t)

	var n int
	p := &recordingPrompter{}
	p.answer = func(Challenge) ([]string, error) {
		n++
		if n == 1 {
			return []string{"000000"}, nil
		}
		return []string{dockerenv.Code()}, nil
	}

	cli := trustAndConnect(t, hostAt(server.CodePort, ""), p)
	defer cli.Close()

	if n < 2 {
		t.Fatalf("the prompter was called %d times; a wrong code did not re-prompt", n)
	}
	if got := run(t, cli, "id -un"); !strings.Contains(got, dockerenv.User) {
		t.Fatalf("`id -un` printed %q after the retry, want the logged-in user", got)
	}
}

// The negative control, and the test that gives the rest of this file its
// meaning: a wrong code does not get in. Without it, a container whose PAM stack
// silently accepted anything would make every other test here pass while proving
// nothing.
func TestTwoFactorWrongCodeIsRefused(t *testing.T) {
	requireDocker(t)
	p := &recordingPrompter{answers: []string{"000000"}}

	h := hostAt(server.CodePort, "")
	trust(t, h)

	cli, err := Connect(h, p)
	if err == nil {
		cli.Close()
		t.Fatal("the server accepted 000000; this container is not really checking codes")
	}
	if errors.Is(err, ErrAuthCanceled) {
		t.Fatalf("err = %v, want a rejection by the server, not a cancel", err)
	}
	// Three attempts, then it gives up — the bound authRetries puts on it, which
	// is also all pam_google_authenticator's default rate limit would allow.
	if asked := p.challenges(); len(asked) != authRetries {
		t.Fatalf("the user was asked %d times, want %d", len(asked), authRetries)
	}
}

// A code from the wrong point in time is refused too. The window the server
// allows is small; a code from ten minutes ago is not a code.
func TestTwoFactorStaleCodeIsRefused(t *testing.T) {
	requireDocker(t)
	stale := dockerenv.TOTP(dockerenv.Secret, time.Now().Add(-10*time.Minute))
	p := &recordingPrompter{answers: []string{stale}}

	h := hostAt(server.CodePort, "")
	trust(t, h)

	cli, err := Connect(h, p)
	if err == nil {
		cli.Close()
		t.Fatalf("a code from ten minutes ago (%s) was accepted", stale)
	}
}

// Dismissing the prompt ends the dial.
func TestTwoFactorCancelAbandonsTheDial(t *testing.T) {
	requireDocker(t)
	p := &recordingPrompter{err: ErrAuthCanceled}

	h := hostAt(server.CodePort, "")
	trust(t, h)

	cli, err := Connect(h, p)
	if err == nil {
		cli.Close()
		t.Fatal("a cancelled prompt still connected")
	}
	if !errors.Is(err, ErrAuthCanceled) {
		t.Fatalf("err = %v, want it to unwrap to ErrAuthCanceled", err)
	}
}

// One esc is the end of it, against a server that offers both interactive
// methods. This is the case stickyCancel exists for and the only one that shows
// it: an SSH client does not stop at the first method that fails, so without the
// sticky cancel the password method would pick up where the dismissed
// keyboard-interactive prompt left off and ask the same person again.
func TestTwoFactorCancelSkipsRemainingMethods(t *testing.T) {
	requireDocker(t)
	p := &recordingPrompter{err: ErrAuthCanceled}

	h := hostAt(server.EitherPort, "")
	trust(t, h)

	cli, err := Connect(h, p)
	if err == nil {
		cli.Close()
		t.Fatal("a cancelled prompt still connected")
	}
	if !errors.Is(err, ErrAuthCanceled) {
		t.Fatalf("err = %v, want it to unwrap to ErrAuthCanceled", err)
	}
	if asked := p.challenges(); len(asked) != 1 {
		t.Fatalf("the user was asked %d times after cancelling once, want 1 — the cancel did not stick", len(asked))
	}
}

// The same server, answered rather than dismissed: proof that it really does
// offer both methods, so the test above is not passing because there was never a
// second method to fall through to.
func TestTwoFactorEitherPortOffersBothMethods(t *testing.T) {
	requireDocker(t)

	h := hostAt(server.EitherPort, "")
	trust(t, h)

	// Refuse the keyboard-interactive round with a plain error rather than a
	// cancel. A plain error does not stick, so the client moves on — and the
	// password method asking is what shows the server offered it.
	p := &recordingPrompter{}
	p.answer = func(c Challenge) ([]string, error) {
		if len(c.Questions) == 1 && strings.Contains(strings.ToLower(c.Questions[0].Text), "verification code") {
			return nil, fmt.Errorf("not answering this one")
		}
		return []string{"wrong"}, nil
	}

	if cli, err := Connect(h, p); err == nil {
		cli.Close()
		t.Fatal("connected with a refused code and a wrong password")
	}

	var sawPassword bool
	for _, c := range p.challenges() {
		for _, q := range c.Questions {
			if strings.Contains(strings.ToLower(q.Text), "password") {
				sawPassword = true
			}
		}
	}
	if !sawPassword {
		t.Fatal("the password method was never reached; this port does not offer the alternative the cancel test needs")
	}
}

// An unknown host key stops the dial *before* anything is asked. It matters
// beyond tidiness: a code handed over first would be spent on a connection the
// user then declines, and it cannot be used again.
func TestTwoFactorUnknownHostKeyAsksNothing(t *testing.T) {
	requireDocker(t)
	fakeHome(t)
	disableAgent(t)
	p := codePrompter()

	_, err := Connect(hostAt(server.CodePort, ""), p)

	var unknown *UnknownHostKeyError
	if !errors.As(err, &unknown) {
		t.Fatalf("err = %v, want *UnknownHostKeyError", err)
	}
	if asked := p.challenges(); len(asked) != 0 {
		t.Fatalf("the user was asked for a code before the host key was trusted (%d times)", len(asked))
	}
}

// The connection that authenticated is the one everything else runs on: a second
// shell and the SFTP browser are channels on it, so 2FA is paid once per host.
// This is what makes hop usable on a 2FA box at all.
func TestTwoFactorOneCodeServesEveryChannel(t *testing.T) {
	requireDocker(t)
	p := codePrompter()

	cli := trustAndConnect(t, hostAt(server.CodePort, ""), p)
	defer cli.Close()

	for i := range 3 {
		sess, err := cli.Shell(80, 24)
		if err != nil {
			t.Fatalf("shell %d on the authenticated connection: %v", i+1, err)
		}
		sess.Close()
	}
	if asked := p.challenges(); len(asked) != 1 {
		t.Fatalf("opening three shells asked for %d codes, want the one the connection was made with", len(asked))
	}
}

// ---- helpers ----

func requireDocker(t *testing.T) {
	t.Helper()
	if !dockerenv.Enabled() {
		t.Skipf("set %s=1 to run the Docker-backed two-factor tests", dockerenv.EnvVar)
	}
}

// codePrompter answers whatever the server asks: a current TOTP code for a
// verification-code question, the account password for a password question. It
// goes by the prompt text because that is all a client has to go on.
func codePrompter() *recordingPrompter {
	p := &recordingPrompter{}
	p.answer = func(c Challenge) ([]string, error) {
		answers := make([]string, len(c.Questions))
		for i, q := range c.Questions {
			if strings.Contains(strings.ToLower(q.Text), "password") {
				answers[i] = dockerenv.Password
				continue
			}
			answers[i] = dockerenv.Code()
		}
		return answers, nil
	}
	return p
}

// hostAt is the store.Host that reaches one of the container's ports.
func hostAt(port int, identityFile string) store.Host {
	return store.Host{
		Alias:        "twofactor",
		HostName:     "127.0.0.1",
		Port:         port,
		User:         dockerenv.User,
		IdentityFile: identityFile,
	}
}

// trust does the first-contact dance against a fresh ~/.ssh: dial once to be
// told the key is unknown, then record it. It leaves the test with a known host
// and no agent, which is the state a real user is in after approving the card.
//
// Both dials carry a prompter that cancels. One is needed at all — with no keys
// and no prompter there is nothing to offer, and authMethods fails before the
// host key is ever exchanged — and cancelling is what stops the recording dial
// from spending a verification code it has no use for.
func trust(t *testing.T, h store.Host) {
	t.Helper()
	fakeHome(t)
	disableAgent(t)

	refuse := &recordingPrompter{err: ErrAuthCanceled}

	_, err := Connect(h, refuse)
	var unknown *UnknownHostKeyError
	if !errors.As(err, &unknown) {
		t.Fatalf("first contact err = %v, want *UnknownHostKeyError", err)
	}
	// This appends the approved key to known_hosts and then fails the
	// authentication, which is fine: by then the host key has been recorded,
	// which is all this is for.
	if _, err := ConnectTrusting(h, unknown.Fingerprint, refuse); err == nil {
		t.Fatal("connected with an authentication that refuses to answer")
	}
}

// trustAndConnect records the host key, then connects for real with p.
func trustAndConnect(t *testing.T, h store.Host, p Prompter) *Client {
	t.Helper()
	trust(t, h)

	cli, err := Connect(h, p)
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}
	return cli
}

// run executes cmd on the far side and returns what it printed, which is how
// these tests prove the session is a real one and not just a successful
// handshake.
func run(t *testing.T, cli *Client, cmd string) string {
	t.Helper()
	sess, err := cli.Command(cmd, 80, 24)
	if err != nil {
		t.Fatalf("run %q: %v", cmd, err)
	}
	defer sess.Close()

	out, err := io.ReadAll(sess.Stdout)
	if err != nil {
		t.Fatalf("read output of %q: %v", cmd, err)
	}
	return string(out)
}
