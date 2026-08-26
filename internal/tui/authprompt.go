package tui

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/lipgloss"

	"hop/internal/sshx"
)

// uiPrompter blocks the dial goroutine inside the handshake and asks the UI, because a
// verification code expires in seconds and cannot be spent on an aborted dial.
type uiPrompter struct {
	alias   string
	prompts chan authPromptMsg
}

// Ask waits on a buffered reply channel, so the model can answer and move on.
func (p *uiPrompter) Ask(ch sshx.Challenge) ([]string, error) {
	reply := make(chan authReply, 1)
	p.prompts <- authPromptMsg{alias: p.alias, ch: ch, reply: reply}
	r := <-reply
	return r.answers, r.err
}

func (m *model) prompter(alias string) sshx.Prompter {
	return &uiPrompter{alias: alias, prompts: m.prompts}
}

// waitAuthPrompt is re-armed on every message, so a dial never blocks on a send nobody is
// listening for.
func waitAuthPrompt(prompts chan authPromptMsg) tea.Cmd {
	return func() tea.Msg {
		return <-prompts
	}
}

// authUI holds a dial parked on reply, so every path out of the card must send on it once.
type authUI struct {
	open    bool
	alias   string
	ch      sshx.Challenge
	answers []string
	idx     int
	reply   chan authReply
}

// authReply carries the answers in question order, or the error that ends the attempt.
type authReply struct {
	answers []string
	err     error
}

// openAuthPrompt queues a second challenge rather than dropping it: its dial stays blocked
// until it is answered.
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

// closeAuth expects the caller to have already replied to the dial.
func (m *model) closeAuth() {
	m.auth = authUI{}
	if len(m.authQueue) == 0 {
		return
	}
	next := m.authQueue[0]
	m.authQueue = m.authQueue[1:]
	m.openAuthPrompt(next)
}

// handleAuthKey swallows every key: a stray one must not reach the list behind the card.
func (m *model) handleAuthKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
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
		if msg.Text != "" {
			m.setAuthAnswer(m.authAnswer() + msg.Text)
		}
	}
	return m, nil
}

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

// submitAuth sends empty answers too: an empty answer is a legitimate reply to a PAM prompt.
func (m *model) submitAuth() {
	answers := m.auth.answers
	alias := m.auth.alias
	m.auth.reply <- authReply{answers: answers}

	// Before closing: closeAuth raises the next challenge, which clears the status.
	m.setStatus(statusInfo, "authenticating %s…", alias)
	m.closeAuth()
}

// cancelAuth fails the dial with ErrAuthCanceled, which the landings report as a cancel.
func (m *model) cancelAuth() {
	alias := m.auth.alias
	m.auth.reply <- authReply{err: sshx.ErrAuthCanceled}
	m.setStatus(statusWarn, "canceled auth for %s", alias)
	m.closeAuth()
}

// Card geometry: a PAM prompt is a sentence, not a path, so the card is prose-wide.
const (
	authMaxW   = 52
	authFloorW = 20
)

func (m *model) authInnerW() int {
	room := max(m.width-2*cardPadX-2, authFloorW)
	return clamp(authMaxW, authFloorW, room)
}

// renderAuth strips every piece of server-supplied text: an escape sequence in a PAM
// prompt would otherwise be executed by the user's terminal.
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

// renderAuthField masks a field the server marked non-echoing with one bullet per rune.
func (m *model) renderAuthField(i int, echo bool, w int) string {
	const indent = "    "
	vw := w - lipgloss.Width(indent)

	shown := m.auth.answers[i]
	if !echo {
		shown = strings.Repeat("•", len([]rune(shown)))
	}

	// Keep the end: with the text masked, a static field reads as keystrokes not landing.
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
