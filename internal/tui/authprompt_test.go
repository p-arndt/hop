package tui

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/lipgloss"

	"hop/internal/sshx"
	"hop/internal/store"
)

// challenge builds a one-question, non-echoing challenge — what a host running
// pam_google_authenticator asks.
func challenge(text string) sshx.Challenge {
	return sshx.Challenge{
		Instruction: "Two-factor authentication",
		Questions:   []sshx.Question{{Text: text}},
	}
}

// promptFor arms the card for alias and hands back the reply channel the waiting
// dial would be parked on, buffered so the model can answer without a reader.
func promptFor(m *model, alias string, ch sshx.Challenge) chan authReply {
	reply := make(chan authReply, 1)
	m.openAuthPrompt(authPromptMsg{alias: alias, ch: ch, reply: reply})
	return reply
}

// A challenge opens the card, shows the server's own prompt text, and masks what is
// typed: a screen recording of hop must not carry a verification code.
func TestAuthCardOpensAndMasksTheAnswer(t *testing.T) {
	m := hostMgmtModel(t, store.Host{Alias: "web", HostName: "h", Port: 22})
	promptFor(m, "web", challenge("Verification code: "))

	if !m.auth.open {
		t.Fatal("a challenge did not open the card")
	}
	typeRunes(t, m, "123456")

	card := m.renderAuth()
	if !strings.Contains(card, "Verification code") {
		t.Fatalf("the card does not show the server's prompt; card = %q", card)
	}
	if strings.Contains(card, "123456") {
		t.Fatal("the card printed the verification code in the clear")
	}
	if !strings.Contains(card, strings.Repeat("•", 6)) {
		t.Fatalf("the code was not masked one bullet per rune; card = %q", card)
	}
	if !strings.Contains(card, "web") {
		t.Fatal("the card does not say which host is asking")
	}
}

// enter releases the dial with what was typed and takes the card down.
func TestAuthCardSubmitAnswersTheDial(t *testing.T) {
	m := hostMgmtModel(t, store.Host{Alias: "web", HostName: "h", Port: 22})
	reply := promptFor(m, "web", challenge("Verification code: "))

	typeRunes(t, m, "424242")
	m.handleKey(key(t, "enter"))

	if m.auth.open {
		t.Fatal("enter did not close the card")
	}
	r := <-reply
	if r.err != nil {
		t.Fatalf("reply carried an error: %v", r.err)
	}
	if len(r.answers) != 1 || r.answers[0] != "424242" {
		t.Fatalf("answers = %q, want the typed code", r.answers)
	}
}

// esc ends the attempt rather than leaving the dial parked forever: a card with
// no way out would hang the connect in the handshake.
func TestAuthCardCancelReleasesTheDial(t *testing.T) {
	m := hostMgmtModel(t, store.Host{Alias: "web", HostName: "h", Port: 22})
	reply := promptFor(m, "web", challenge("Verification code: "))

	m.handleKey(key(t, "esc"))

	if m.auth.open {
		t.Fatal("esc did not close the card")
	}
	r := <-reply
	if r.err == nil {
		t.Fatal("cancelling did not fail the dial; it would wait forever")
	}
	if r.err != sshx.ErrAuthCanceled {
		t.Fatalf("reply err = %v, want ErrAuthCanceled", r.err)
	}
}

// A multi-question round (a PAM stack asking for password and code together)
// walks field by field on enter and submits both answers at the end, in order.
func TestAuthCardWalksMultipleQuestions(t *testing.T) {
	m := hostMgmtModel(t, store.Host{Alias: "web", HostName: "h", Port: 22})
	reply := promptFor(m, "web", sshx.Challenge{Questions: []sshx.Question{
		{Text: "Password: "},
		{Text: "Verification code: "},
	}})

	typeRunes(t, m, "hunter2")
	m.handleKey(key(t, "enter"))
	if m.auth.open != true || m.auth.idx != 1 {
		t.Fatalf("enter on the first of two questions did not move to the second (idx = %d, open = %v)", m.auth.idx, m.auth.open)
	}
	typeRunes(t, m, "123456")
	m.handleKey(key(t, "enter"))

	r := <-reply
	if len(r.answers) != 2 || r.answers[0] != "hunter2" || r.answers[1] != "123456" {
		t.Fatalf("answers = %q, want both, in question order", r.answers)
	}
}

// Two hosts dialing at once each get their turn: the second challenge waits behind the
// first, whose dial is blocked until it is answered.
func TestAuthCardQueuesASecondChallenge(t *testing.T) {
	m := hostMgmtModel(t,
		store.Host{Alias: "web", HostName: "h", Port: 22},
		store.Host{Alias: "db", HostName: "h2", Port: 22},
	)
	first := promptFor(m, "web", challenge("Verification code: "))
	second := promptFor(m, "db", challenge("Verification code: "))

	if m.auth.alias != "web" {
		t.Fatalf("card is asking for %q, want the first challenge's host", m.auth.alias)
	}
	if len(m.authQueue) != 1 {
		t.Fatalf("queue holds %d challenges, want the second one waiting", len(m.authQueue))
	}

	typeRunes(t, m, "111111")
	m.handleKey(key(t, "enter"))
	if r := <-first; len(r.answers) != 1 || r.answers[0] != "111111" {
		t.Fatalf("first answers = %q", r.answers)
	}

	if !m.auth.open || m.auth.alias != "db" {
		t.Fatalf("the queued challenge did not come up (open = %v, alias = %q)", m.auth.open, m.auth.alias)
	}
	if m.authAnswer() != "" {
		t.Fatal("the second card started with the first one's answer still in it")
	}

	typeRunes(t, m, "222222")
	m.handleKey(key(t, "enter"))
	if r := <-second; len(r.answers) != 1 || r.answers[0] != "222222" {
		t.Fatalf("second answers = %q", r.answers)
	}
	if len(m.authQueue) != 0 {
		t.Fatalf("queue still holds %d challenges", len(m.authQueue))
	}
}

// The card takes every key while it is up. A stray "d" must not reach the list
// behind it and open a delete confirmation over a half-typed code.
func TestAuthCardSwallowsOtherKeys(t *testing.T) {
	m := hostMgmtModel(t, store.Host{Alias: "web", HostName: "h", Port: 22})
	promptFor(m, "web", challenge("Verification code: "))

	m.handleKey(key(t, "d"))

	if m.confirm.open || m.hostForm.open || m.settings.open {
		t.Fatal("a key fell through the auth card to the list behind it")
	}
	if m.authAnswer() != "d" {
		t.Fatalf("answer = %q, want the key to have gone into the field", m.authAnswer())
	}
}

// The card outranks the help card: '?' is bound in navigation mode and a dial takes long
// enough to press it, so a help card opened while connecting would hide the challenge
// that arrives next.
func TestAuthCardOutranksHelp(t *testing.T) {
	m := hostMgmtModel(t, store.Host{Alias: "web", HostName: "h", Port: 22})
	m.help = true
	reply := promptFor(m, "web", challenge("Verification code: "))

	if card := m.modalCard(); card != m.renderAuth() {
		t.Fatal("the help card is drawn over a challenge the dial is waiting on")
	}
	typeRunes(t, m, "123456")
	if m.authAnswer() != "123456" {
		t.Fatalf("keys went to the help card, not the challenge (answer = %q)", m.authAnswer())
	}
	m.handleKey(key(t, "enter"))
	if r := <-reply; len(r.answers) != 1 || r.answers[0] != "123456" {
		t.Fatalf("answers = %q, want the typed code", r.answers)
	}
}

// Answering one host while another is queued leaves the second card on screen
// with its own status, not a line about the host that was just answered.
func TestAuthStatusNamesTheHostOnScreen(t *testing.T) {
	m := hostMgmtModel(t,
		store.Host{Alias: "web", HostName: "h", Port: 22},
		store.Host{Alias: "db", HostName: "h2", Port: 22},
	)
	promptFor(m, "web", challenge("Verification code: "))
	promptFor(m, "db", challenge("Verification code: "))

	typeRunes(t, m, "111111")
	m.handleKey(key(t, "enter"))

	if m.auth.alias != "db" {
		t.Fatalf("card is asking for %q, want the queued host", m.auth.alias)
	}
	if strings.Contains(m.status, "web") {
		t.Fatalf("status = %q while db's card is up", m.status)
	}
}

// A long answer keeps its end in view, so typing past the width of the field
// still looks like typing. With the text masked there is no other feedback.
func TestAuthFieldFollowsTheCaret(t *testing.T) {
	m := hostMgmtModel(t, store.Host{Alias: "web", HostName: "h", Port: 22})
	promptFor(m, "web", challenge("Password: "))

	typeRunes(t, m, strings.Repeat("x", 20))
	short := m.renderAuth()
	typeRunes(t, m, strings.Repeat("x", 200))
	long := m.renderAuth()

	if lipgloss.Width(long) != lipgloss.Width(short) {
		t.Fatalf("the card grew with the answer: %d cells vs %d", lipgloss.Width(long), lipgloss.Width(short))
	}
	if !strings.Contains(long, "•") {
		t.Fatal("a long answer rendered no bullets at all")
	}
}

// A dial the user cancelled reports nothing further: the card already said so.
func TestCanceledAuthIsNotReportedAsFailure(t *testing.T) {
	m := hostMgmtModel(t, store.Host{Alias: "web", HostName: "h", Port: 22})
	m.connecting = map[string]bool{"web": true}

	err := fmt.Errorf("sshx: dial h:22: ssh: handshake failed: %w", sshx.ErrAuthCanceled)
	m.shellLanded(connectedMsg{alias: "web", err: err})

	if m.statusKind == statusErr {
		t.Fatalf("a cancelled auth was reported as an error: %q", m.status)
	}
	if m.connecting["web"] {
		t.Fatal("the connecting spinner was not cleared")
	}
}

// The round trip a real dial makes: the prompter blocks in the handshake, the
// model receives the challenge over the channel, and the answer comes back.
func TestPrompterRoundTrip(t *testing.T) {
	m := hostMgmtModel(t, store.Host{Alias: "web", HostName: "h", Port: 22})
	m.prompts = make(chan authPromptMsg)

	got := make(chan []string, 1)
	fail := make(chan error, 1)
	go func() {
		answers, err := m.prompter("web").Ask(challenge("Verification code: "))
		if err != nil {
			fail <- err
			return
		}
		got <- answers
	}()

	// Stand in for the command Init arms: take the challenge off the channel and
	// give it to the model the way Update does.
	select {
	case msg := <-m.prompts:
		m.openAuthPrompt(msg)
	case <-time.After(2 * time.Second):
		t.Fatal("the prompter never asked; the dial would hang in the handshake")
	}

	typeRunes(t, m, "999999")
	m.handleKey(key(t, "enter"))

	select {
	case answers := <-got:
		if len(answers) != 1 || answers[0] != "999999" {
			t.Fatalf("the dial got %q, want the typed code", answers)
		}
	case err := <-fail:
		t.Fatalf("the dial got an error: %v", err)
	case <-time.After(2 * time.Second):
		t.Fatal("the answer never reached the waiting dial")
	}
}
