package tui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"hop/internal/sshx"
)

// uiPrompter is the sshx.Prompter that puts a server's question on screen — the one place
// where a background command asks the UI something and waits, rather than failing and
// being replayed as the host-key card's dial is. A verification code is valid for about
// thirty seconds and may not be reused, so a dial that aborted to ask would have spent it.
//
// Ask therefore blocks the dial goroutine, inside the handshake, on a round trip through
// the model: out on prompts, which the model permanently waits on, back on a reply channel.
type uiPrompter struct {
	alias   string
	prompts chan authPromptMsg
}

// Ask hands the challenge to the model and waits. The reply channel is buffered so the
// model can answer and move on.
func (p *uiPrompter) Ask(ch sshx.Challenge) ([]string, error) {
	reply := make(chan authReply, 1)
	p.prompts <- authPromptMsg{alias: p.alias, ch: ch, reply: reply}
	r := <-reply
	return r.answers, r.err
}

// prompter builds the Prompter for a dial to alias.
func (m *model) prompter(alias string) sshx.Prompter {
	return &uiPrompter{alias: alias, prompts: m.prompts}
}

// waitAuthPrompt blocks until a dial asks something, then delivers it. Like waitForOutput
// it is re-armed on every message, so a dial never blocks on a send nobody is listening
// for.
func waitAuthPrompt(prompts chan authPromptMsg) tea.Cmd {
	return func() tea.Msg {
		return <-prompts
	}
}

// authUI is the interactive-authentication card's state: the challenge being answered, a
// buffer per question, which one has the keyboard, and the channel the dial is parked on.
//
// A dial left waiting never finishes, so every path out of this card — submit or cancel —
// must send on reply exactly once.
type authUI struct {
	open    bool
	alias   string
	ch      sshx.Challenge
	answers []string
	idx     int
	reply   chan authReply
}

// authReply is what the card sends back to the waiting dial: the answers in question
// order, or the error that ends the attempt.
type authReply struct {
	answers []string
	err     error
}

// openAuthPrompt arms the card for msg. A second challenge arriving while one is up is
// queued rather than dropped: two hosts can be dialing at once, and the dial behind the
// queued one is blocked until it is answered.
func (m *model) openAuthPrompt(msg authPromptMsg) {
	if m.auth.open {
		m.authQueue = append(m.authQueue, msg)
		return
	}
	m.auth = authUI{
		open:    true,
		alias:   msg.alias,
		ch:      msg.ch,
		answers: make([]string, len(msg.ch.Questions)),
		reply:   msg.reply,
	}
	m.status = ""
}

// closeAuth takes the card down and brings up whatever was waiting behind it. The caller
// has already replied to the dial.
func (m *model) closeAuth() {
	m.auth = authUI{}
	if len(m.authQueue) == 0 {
		return
	}
	next := m.authQueue[0]
	m.authQueue = m.authQueue[1:]
	m.openAuthPrompt(next)
}

// handleAuthKey routes a key while the card is up, swallowing everything: a stray key
// must not reach the list behind a prompt being answered. enter walks to the next question
// and submits on the last, so a single-field challenge is type-and-enter.
func (m *model) handleAuthKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc", "ctrl+c":
		m.cancelAuth()

	case "enter":
		if m.auth.idx < len(m.auth.answers)-1 {
			m.auth.idx++
			return m, nil
		}
		m.submitAuth()

	case "tab", "down":
		m.auth.idx = (m.auth.idx + 1) % max(len(m.auth.answers), 1)

	case "shift+tab", "up":
		n := max(len(m.auth.answers), 1)
		m.auth.idx = (m.auth.idx - 1 + n) % n

	case "backspace":
		if v := m.authAnswer(); v != "" {
			r := []rune(v)
			m.setAuthAnswer(string(r[:len(r)-1]))
		}

	case "ctrl+u":
		m.setAuthAnswer("")

	default:
		if len(msg.Runes) > 0 {
			m.setAuthAnswer(m.authAnswer() + string(msg.Runes))
		}
	}
	return m, nil
}

// authAnswer is the buffer for the question that currently has the keyboard.
func (m *model) authAnswer() string {
	if m.auth.idx < 0 || m.auth.idx >= len(m.auth.answers) {
		return ""
	}
	return m.auth.answers[m.auth.idx]
}

func (m *model) setAuthAnswer(v string) {
	if m.auth.idx < 0 || m.auth.idx >= len(m.auth.answers) {
		return
	}
	m.auth.answers[m.auth.idx] = v
}

// submitAuth releases the dial with what was typed, empty answers included: an empty
// answer is a legitimate reply to a PAM prompt, and rejecting it would hide the server's
// verdict.
func (m *model) submitAuth() {
	answers := m.auth.answers
	alias := m.auth.alias
	m.auth.reply <- authReply{answers: answers}

	// Before closing, not after: closeAuth brings up the next queued challenge, which
	// clears the status for its own question.
	m.setStatus(statusInfo, "authenticating %s…", alias)
	m.closeAuth()
}

// cancelAuth ends the attempt. The dial fails with ErrAuthCanceled, which the landings
// report as a cancel rather than a failure.
func (m *model) cancelAuth() {
	alias := m.auth.alias
	m.auth.reply <- authReply{err: sshx.ErrAuthCanceled}
	m.setStatus(statusWarn, "canceled auth for %s", alias)
	m.closeAuth()
}

// Card geometry. A PAM prompt is a sentence, not a path, so the card is prose-wide.
const (
	authMaxW   = 52
	authFloorW = 20
)

// authInnerW is the width available to a rendered line: the box less its border and
// padding, held to the window so the card never spills past the screen.
func (m *model) authInnerW() int {
	room := max(m.width-2*cardPadX-2, authFloorW)
	return clamp(authMaxW, authFloorW, room)
}

// renderAuth draws the card: which host is asking, the server's framing, and one input
// per question — masked unless the server said the answer may be echoed.
//
// Every piece of server-supplied text goes through stripControl first: an escape sequence
// in a PAM prompt would otherwise be executed by the user's terminal.
func (m *model) renderAuth() string {
	w := m.authInnerW()
	var b strings.Builder

	b.WriteString(truncate(titleStyle.Render("AUTHENTICATION"), w))
	b.WriteString("\n\n")

	intro := accentText.Render(m.auth.alias) + dimStyle.Render(" is asking for more than your key.")
	b.WriteString(wrapCard(intro, w))
	b.WriteString("\n\n")

	if s := strings.TrimSpace(stripControl(m.auth.ch.Instruction)); s != "" {
		b.WriteString(wrapDim(s, w))
		b.WriteString("\n\n")
	}

	for i, q := range m.auth.ch.Questions {
		label := strings.TrimSpace(stripControl(q.Text))
		if label == "" {
			label = "Answer"
		}
		style := settingsLabel
		if i == m.auth.idx {
			style = settingsLabelSel
		}
		b.WriteString(truncate(style.Render(label), w))
		b.WriteString("\n")
		b.WriteString(m.renderAuthField(i, q.Echo, w))
		b.WriteString("\n")
	}
	b.WriteString("\n")

	b.WriteString(rule(w))
	b.WriteString("\n")
	b.WriteString(truncate(keyHint("enter", "submit")+"  "+keyHint("esc", "cancel")+"  "+keyHint("ctrl+u", "clear"), w))

	return cardBox.Width(w + 2*cardPadX).Render(b.String())
}

// renderAuthField draws one answer row, in the import card's shape. A field the server
// marked non-echoing shows a bullet per rune: enough to see typing land, nothing readable
// over a shoulder.
func (m *model) renderAuthField(i int, echo bool, w int) string {
	const indent = "    "
	vw := w - lipgloss.Width(indent)

	shown := m.auth.answers[i]
	if !echo {
		shown = strings.Repeat("•", len([]rune(shown)))
	}

	// An answer wider than the field keeps its end: with the text masked, a field that
	// stopped growing would read as keystrokes no longer landing.
	if room := vw - 3; room > 0 {
		if r := []rune(shown); len(r) > room {
			shown = string(r[len(r)-room:])
		}
	}

	text := shown
	if i == m.auth.idx {
		text += accentText.Render("▏")
	}
	return indent + inputStyle.Width(vw).Render(text)
}
