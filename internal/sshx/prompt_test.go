package sshx

import (
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/skeema/knownhosts"
	"golang.org/x/crypto/ssh"

	"hop/internal/store"
)

// recordingPrompter answers with fixed replies and records every challenge.
type recordingPrompter struct {
	answers []string
	err     error
	// answer, when set, decides the reply per challenge.
	answer func(Challenge) ([]string, error)

	mu   sync.Mutex
	seen []Challenge
}

func (p *recordingPrompter) Ask(c Challenge) ([]string, error) {
	p.mu.Lock()
	p.seen = append(p.seen, c)
	p.mu.Unlock()
	if p.err != nil {
		return nil, p.err
	}
	if p.answer != nil {
		return p.answer(c)
	}
	return p.answers, nil
}

func (p *recordingPrompter) challenges() []Challenge {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]Challenge(nil), p.seen...)
}

// ---- authMethods wiring ----

func TestAuthMethodsAddsInteractiveMethods(t *testing.T) {
	home := fakeHome(t)
	writeKey(t, filepath.Join(home, ".ssh", "id_ed25519"), "")
	disableAgent(t)

	auths, err := authMethods(store.Host{HostName: "example.com"}, &recordingPrompter{})
	if err != nil {
		t.Fatalf("authMethods: %v", err)
	}
	if len(auths) != 3 {
		t.Fatalf("len(auths) = %d, want publickey + keyboard-interactive + password", len(auths))
	}
}

func TestAuthMethodsWithoutPrompterOffersKeysOnly(t *testing.T) {
	home := fakeHome(t)
	writeKey(t, filepath.Join(home, ".ssh", "id_ed25519"), "")
	disableAgent(t)

	auths, err := authMethods(store.Host{HostName: "example.com"}, nil)
	if err != nil {
		t.Fatalf("authMethods: %v", err)
	}
	if len(auths) != 1 {
		t.Fatalf("len(auths) = %d, want the publickey method alone", len(auths))
	}
}

// Regression: with no agent and no key, an interactive host is still reachable.
func TestAuthMethodsWithNoKeysStillOffersInteractive(t *testing.T) {
	fakeHome(t)
	disableAgent(t)

	auths, err := authMethods(store.Host{HostName: "example.com"}, &recordingPrompter{})
	if err != nil {
		t.Fatalf("authMethods with a prompter and no keys: %v", err)
	}
	if len(auths) != 2 {
		t.Fatalf("len(auths) = %d, want keyboard-interactive + password", len(auths))
	}
}

// ---- the keyboard-interactive callback ----

func TestKeyboardInteractiveCarriesQuestionsAndEchoFlags(t *testing.T) {
	p := &recordingPrompter{answers: []string{"hunter2", "123456"}}
	cb := keyboardInteractive(p)

	answers, err := cb("", "Two-factor required", []string{"Password: ", "Verification code: "}, []bool{false, false})
	if err != nil {
		t.Fatalf("callback: %v", err)
	}
	if len(answers) != 2 || answers[0] != "hunter2" || answers[1] != "123456" {
		t.Fatalf("answers = %q, want the prompter's, in order", answers)
	}

	seen := p.challenges()
	if len(seen) != 1 {
		t.Fatalf("prompter asked %d times, want 1", len(seen))
	}
	if seen[0].Instruction != "Two-factor required" {
		t.Fatalf("instruction = %q, want the server's", seen[0].Instruction)
	}
	if len(seen[0].Questions) != 2 {
		t.Fatalf("questions = %d, want 2", len(seen[0].Questions))
	}
	for i, q := range seen[0].Questions {
		if q.Echo {
			t.Fatalf("question %d is marked echoing; the server said not to show it", i)
		}
	}
}

// A server may say a question is echoable; not everything PAM asks is a secret.
func TestKeyboardInteractiveKeepsEchoTrue(t *testing.T) {
	p := &recordingPrompter{answers: []string{"deploy"}}
	cb := keyboardInteractive(p)

	if _, err := cb("", "", []string{"Username: "}, []bool{true}); err != nil {
		t.Fatalf("callback: %v", err)
	}
	if seen := p.challenges(); !seen[0].Questions[0].Echo {
		t.Fatal("an echoing question was passed on as a secret")
	}
}

func TestKeyboardInteractiveSkipsBannerRounds(t *testing.T) {
	p := &recordingPrompter{answers: []string{"unused"}}
	cb := keyboardInteractive(p)

	answers, err := cb("", "Welcome to prod.", nil, nil)
	if err != nil {
		t.Fatalf("banner round: %v", err)
	}
	if len(answers) != 0 {
		t.Fatalf("answers = %q, want none for a banner", answers)
	}
	if seen := p.challenges(); len(seen) != 0 {
		t.Fatalf("the prompter was asked %d times for a banner round, want 0", len(seen))
	}
}

func TestKeyboardInteractiveRejectsWrongAnswerCount(t *testing.T) {
	cb := keyboardInteractive(&recordingPrompter{answers: []string{"only-one"}})

	if _, err := cb("", "", []string{"Password: ", "Verification code: "}, []bool{false, false}); err == nil {
		t.Fatal("a reply with too few answers was accepted; it must not be sent")
	}
}

// ---- cancellation ----

// One cancel ends the whole dial, so no second prompt follows a dismissed one.
func TestStickyCancelRefusesLaterQuestions(t *testing.T) {
	inner := &recordingPrompter{err: ErrAuthCanceled}
	s := &stickyCancel{p: inner}

	if _, err := s.Ask(Challenge{Questions: []Question{{Text: "Verification code:"}}}); !errors.Is(err, ErrAuthCanceled) {
		t.Fatalf("first Ask = %v, want ErrAuthCanceled", err)
	}
	if _, err := s.Ask(Challenge{Questions: []Question{{Text: "Password:"}}}); !errors.Is(err, ErrAuthCanceled) {
		t.Fatalf("second Ask = %v, want ErrAuthCanceled", err)
	}
	if seen := inner.challenges(); len(seen) != 1 {
		t.Fatalf("the user was asked %d times after cancelling, want 1", len(seen))
	}
}

// A non-cancel error does not stick, which is what lets a mistyped code re-prompt.
func TestStickyCancelDoesNotStickOnOtherErrors(t *testing.T) {
	inner := &recordingPrompter{err: errors.New("something else")}
	s := &stickyCancel{p: inner}

	s.Ask(Challenge{})
	s.Ask(Challenge{})

	if seen := inner.challenges(); len(seen) != 2 {
		t.Fatalf("the prompter was asked %d times, want 2", len(seen))
	}
}

// ---- end to end, against a server that demands a code ----

func TestConnectAnswersKeyboardInteractive(t *testing.T) {
	const code = "123456"
	p := &recordingPrompter{answers: []string{code}}

	h := serveInteractive(t, func(_ ssh.ConnMetadata, client ssh.KeyboardInteractiveChallenge) (*ssh.Permissions, error) {
		answers, err := client("", "", []string{"Verification code: "}, []bool{false})
		if err != nil {
			return nil, err
		}
		if len(answers) != 1 || answers[0] != code {
			return nil, fmt.Errorf("wrong code %q", answers)
		}
		return &ssh.Permissions{}, nil
	})

	cl, err := Connect(h, p)
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}
	cl.Close()

	seen := p.challenges()
	if len(seen) != 1 {
		t.Fatalf("the user was asked %d times, want 1", len(seen))
	}
	if got := seen[0].Questions[0].Text; got != "Verification code: " {
		t.Fatalf("prompt = %q, want the server's own wording", got)
	}
}

func TestConnectReportsCanceledAuth(t *testing.T) {
	p := &recordingPrompter{err: ErrAuthCanceled}

	h := serveInteractive(t, func(_ ssh.ConnMetadata, client ssh.KeyboardInteractiveChallenge) (*ssh.Permissions, error) {
		client("", "", []string{"Verification code: "}, []bool{false})
		return nil, errors.New("denied")
	})

	cl, err := Connect(h, p)
	if err == nil {
		cl.Close()
		t.Fatal("a cancelled prompt still connected")
	}
	if !errors.Is(err, ErrAuthCanceled) {
		t.Fatalf("err = %v, want it to unwrap to ErrAuthCanceled", err)
	}
}

func TestConnectWithoutPrompterOrKeysStillExplains(t *testing.T) {
	h := serveInteractive(t, func(ssh.ConnMetadata, ssh.KeyboardInteractiveChallenge) (*ssh.Permissions, error) {
		return &ssh.Permissions{}, nil
	})

	if _, err := Connect(h, nil); err == nil {
		t.Fatal("connected with nothing to authenticate with")
	} else if !strings.Contains(err.Error(), "ssh-add") {
		t.Fatalf("error %q does not tell the user how to fix it", err)
	}
}

// serveInteractive starts a loopback SSH server authenticating through cb alone.
func serveInteractive(t *testing.T, cb func(ssh.ConnMetadata, ssh.KeyboardInteractiveChallenge) (*ssh.Permissions, error)) store.Host {
	t.Helper()
	disableAgent(t)
	home := fakeHome(t)

	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate host key: %v", err)
	}
	signer, err := ssh.NewSignerFromKey(priv)
	if err != nil {
		t.Fatalf("host key signer: %v", err)
	}

	cfg := &ssh.ServerConfig{KeyboardInteractiveCallback: cb}
	cfg.AddHostKey(signer)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { ln.Close() })

	go func() {
		for {
			nc, err := ln.Accept()
			if err != nil {
				return // the listener closed: the test is over
			}
			go func() {
				sc, chans, reqs, err := ssh.NewServerConn(nc, cfg)
				if err != nil {
					nc.Close()
					return
				}
				go ssh.DiscardRequests(reqs)
				go func() {
					for nch := range chans {
						nch.Reject(ssh.Prohibited, "no channels here")
					}
				}()
				<-make(chan struct{}) // hold the connection open until the process ends
				sc.Close()
			}()
		}
	}()

	host, portStr, err := net.SplitHostPort(ln.Addr().String())
	if err != nil {
		t.Fatalf("split listen addr: %v", err)
	}
	var port int
	fmt.Sscanf(portStr, "%d", &port)

	kh := filepath.Join(home, ".ssh", "known_hosts")
	if err := os.MkdirAll(filepath.Dir(kh), 0o700); err != nil {
		t.Fatalf("known_hosts dir: %v", err)
	}
	line := knownhosts.Line([]string{net.JoinHostPort(host, portStr)}, signer.PublicKey()) + "\n"
	if err := os.WriteFile(kh, []byte(line), 0o600); err != nil {
		t.Fatalf("write known_hosts: %v", err)
	}

	return store.Host{Alias: "twofactor", HostName: host, Port: port, User: "deploy"}
}
