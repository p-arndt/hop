package sshx

import (
	"errors"
	"fmt"
	"sync"

	"golang.org/x/crypto/ssh"
)

// authRetries is how many times each interactive method may be re-offered before the
// dial gives up on it. Three is roughly what pam_google_authenticator's default rate
// limit allows before the server refuses outright.
//
// A per-method bound, not a total: a server offering both keyboard-interactive and
// password can ask twice this often, as plain ssh does too. The way out is esc, which
// cancels for good (see stickyCancel).
const authRetries = 3

// ErrAuthCanceled is what a Prompter returns to abort the dial: the user dismissed the
// question. It comes back out of Connect wrapped in the ssh package's own error, so
// callers test it with errors.Is.
var ErrAuthCanceled = errors.New("sshx: authentication canceled")

// Question is one thing the server asks during interactive authentication. Echo is the
// server's instruction on whether the answer may appear on screen — false for secrets,
// which the UI must mask.
type Question struct {
	Text string
	Echo bool
}

// Challenge is a single round of interactive authentication. A round can hold more than
// one question, and the server expects both answers in one reply, in order.
//
// Name and Instruction are the server's framing for the round, either possibly empty.
// Both come off the wire, so anything rendering them treats them as untrusted text.
type Challenge struct {
	Name        string
	Instruction string
	Questions   []Question
}

// Prompter answers a challenge on the user's behalf. Ask runs on the goroutine driving
// the dial, inside the handshake, and blocks it: a TOTP code cannot be reused, so there
// is no aborting and replaying the handshake.
//
// Returning an error fails the attempt and unwinds the dial; ErrAuthCanceled is the one
// to return when the user dismissed the prompt.
type Prompter interface {
	Ask(Challenge) ([]string, error)
}

// PrompterFunc adapts a plain function to Prompter.
type PrompterFunc func(Challenge) ([]string, error)

func (f PrompterFunc) Ask(c Challenge) ([]string, error) { return f(c) }

// stickyCancel makes a Prompter's cancel final for the whole dial.
//
// The SSH client does not stop at the first method that errors: with keyboard-interactive
// dismissed it moves on to whatever else the server offers, so without this one esc on the
// verification-code prompt would be answered by a password prompt in its place.
//
// The mutex guards against a future caller driving auth from more than one goroutine.
type stickyCancel struct {
	p        Prompter
	mu       sync.Mutex
	canceled bool
}

func (s *stickyCancel) Ask(c Challenge) ([]string, error) {
	s.mu.Lock()
	done := s.canceled
	s.mu.Unlock()
	if done {
		return nil, ErrAuthCanceled
	}

	answers, err := s.p.Ask(c)
	if errors.Is(err, ErrAuthCanceled) {
		s.mu.Lock()
		s.canceled = true
		s.mu.Unlock()
	}
	return answers, err
}

// keyboardInteractive wraps p in the callback x/crypto/ssh drives for the
// "keyboard-interactive" method — how a server running pam_google_authenticator asks for
// the verification code, alone or as the second factor after a key.
func keyboardInteractive(p Prompter) ssh.KeyboardInteractiveChallenge {
	return func(name, instruction string, questions []string, echos []bool) ([]string, error) {
		// A round with no questions is the server showing a banner; there is nothing to
		// answer, and an empty card would be noise.
		if len(questions) == 0 {
			return nil, nil
		}

		qs := make([]Question, len(questions))
		for i, q := range questions {
			qs[i] = Question{Text: q, Echo: i < len(echos) && echos[i]}
		}

		answers, err := p.Ask(Challenge{Name: name, Instruction: instruction, Questions: qs})
		if err != nil {
			return nil, err
		}
		// One answer per question, in order: a short reply would be misread by the server
		// as answering the wrong prompts.
		if len(answers) != len(qs) {
			return nil, fmt.Errorf("sshx: prompter returned %d answers for %d questions", len(answers), len(qs))
		}
		return answers, nil
	}
}

// passwordCallback wraps p in the callback for the plain "password" method, what remains
// on a host that does not offer keyboard-interactive. The wording is hop's: this method
// carries no prompt text of its own.
func passwordCallback(p Prompter) func() (string, error) {
	return func() (string, error) {
		answers, err := p.Ask(Challenge{
			Questions: []Question{{Text: "Password:"}},
		})
		if err != nil {
			return "", err
		}
		if len(answers) != 1 {
			return "", fmt.Errorf("sshx: prompter returned %d answers for the password prompt", len(answers))
		}
		return answers[0], nil
	}
}
