package sshx

import (
	"errors"
	"fmt"
	"sync"

	"golang.org/x/crypto/ssh"
)

// authRetries bounds each interactive method separately; three is roughly pam_google_authenticator's default rate limit.
const authRetries = 3

// ErrAuthCanceled aborts the dial; it comes back out of Connect wrapped, so callers test it with errors.Is.
var ErrAuthCanceled = errors.New("sshx: authentication canceled")

// Question is one thing the server asks; Echo false means a secret the UI must mask.
type Question struct {
	Text string
	Echo bool
}

// Challenge is one round of interactive authentication; Name and Instruction come off the wire and are untrusted text.
type Challenge struct {
	Name        string
	Instruction string
	Questions   []Question
}

// Prompter answers a challenge; Ask blocks inside the handshake, since a one-time code cannot be replayed on a second dial.
type Prompter interface {
	Ask(Challenge) ([]string, error)
}

// PrompterFunc adapts a plain function to Prompter.
type PrompterFunc func(Challenge) ([]string, error)

func (f PrompterFunc) Ask(c Challenge) ([]string, error) { return f(c) }

// stickyCancel makes a cancel final for the whole dial: the SSH client otherwise moves on to the next method the server offers.
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

// keyboardInteractive wraps p in the callback for the "keyboard-interactive" method.
func keyboardInteractive(p Prompter) ssh.KeyboardInteractiveChallenge {
	return func(name, instruction string, questions []string, echos []bool) ([]string, error) {
		// A round with no questions is the server showing a banner.
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
		// One answer per question, in order: a short reply is read as answering the wrong prompts.
		if len(answers) != len(qs) {
			return nil, fmt.Errorf("sshx: prompter returned %d answers for %d questions", len(answers), len(qs))
		}
		return answers, nil
	}
}

// passwordCallback wraps p in the callback for the "password" method, which carries no prompt text of its own.
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
