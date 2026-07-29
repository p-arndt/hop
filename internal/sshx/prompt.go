package sshx

import (
	"errors"
	"fmt"
	"sync"

	"golang.org/x/crypto/ssh"
)

// authRetries is how many times *each* interactive method may be re-offered
// before the dial gives up on it. Three matches what a mistyped verification
// code deserves, and it is also roughly what pam_google_authenticator's default
// rate limit (three attempts per 30 seconds) allows before the server refuses
// outright — retrying past that only produces failures the user cannot fix by
// typing more carefully.
//
// It is a per-method bound, not a total. A server that offers both
// keyboard-interactive and password as alternatives can therefore ask up to
// twice this many times, because the client moves on to the second method once
// the first is exhausted. That is what plain ssh does too, and the way out of it
// is the same: esc, which cancels for good (see stickyCancel).
const authRetries = 3

// ErrAuthCanceled is what a Prompter returns to abort the dial: the user
// dismissed the question rather than answering it. It travels back out of
// Connect wrapped in the ssh package's own error, so callers test it with
// errors.Is rather than by comparing strings.
var ErrAuthCanceled = errors.New("sshx: authentication canceled")

// Question is one thing the server asks during interactive authentication —
// "Verification code: ", "Password: ", or whatever else the remote PAM stack
// puts in front of the user.
//
// Echo is the server's instruction on whether the answer may appear on screen.
// It is false for secrets, which is what a TOTP prompt and a password prompt
// both are, so the UI must mask anything it is false for.
type Question struct {
	Text string
	Echo bool
}

// Challenge is a single round of interactive authentication. A round can hold
// more than one question — a PAM stack that asks for a password and a
// verification code together sends both at once, and the server expects both
// answers in one reply, in order.
//
// Name and Instruction are the server's own framing for the round; either may be
// empty. Both come off the wire from the remote host, so anything that renders
// them has to treat them as untrusted text.
type Challenge struct {
	Name        string
	Instruction string
	Questions   []Question
}

// Prompter answers a challenge on the user's behalf. Ask is called on the
// goroutine running the dial, from inside the SSH handshake, and blocks it until
// it returns — a TOTP code is only valid for about thirty seconds and may not be
// reused, so there is no way to abort the handshake, ask, and replay it. The
// question has to be answered in place.
//
// Returning an error fails the authentication attempt and unwinds the dial;
// ErrAuthCanceled is the one to return when the user dismissed the prompt.
type Prompter interface {
	Ask(Challenge) ([]string, error)
}

// PrompterFunc adapts a plain function to Prompter.
type PrompterFunc func(Challenge) ([]string, error)

func (f PrompterFunc) Ask(c Challenge) ([]string, error) { return f(c) }

// stickyCancel makes a Prompter's cancel final for the whole dial.
//
// It exists because the SSH client does not stop at the first method that
// errors: with keyboard-interactive dismissed it moves on to any other method
// the server still offers (password, typically) and the error is only surfaced
// once nothing is left to try. Without this, one esc on the verification-code
// prompt would be answered by a password prompt appearing in its place. After a
// cancel every later question is refused with the same error instead of being
// put back in front of the user.
//
// The mutex guards against a future caller driving auth from more than one
// goroutine; today every Ask comes from the single goroutine running the dial.
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
// "keyboard-interactive" method — the method a server running
// pam_google_authenticator uses to ask for the verification code, either on its
// own or as the second factor after a public key has already been accepted.
func keyboardInteractive(p Prompter) ssh.KeyboardInteractiveChallenge {
	return func(name, instruction string, questions []string, echos []bool) ([]string, error) {
		// A round with no questions is the server showing a banner (PAM stacks do
		// this for the login message). There is nothing to answer, and putting an
		// empty card on screen for it would be noise.
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
		// The protocol requires one answer per question, in order. A short reply
		// would be silently misread by the server as answering the wrong prompts.
		if len(answers) != len(qs) {
			return nil, fmt.Errorf("sshx: prompter returned %d answers for %d questions", len(answers), len(qs))
		}
		return answers, nil
	}
}

// passwordCallback wraps p in the callback for the plain "password" method,
// which is what remains on a host that asks for one but does not offer
// keyboard-interactive. The prompt is hop's own wording rather than the
// server's: this method carries no prompt text of its own.
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
