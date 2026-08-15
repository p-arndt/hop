package tui

// Paste on the way in; what happens to it afterwards is terminal.Pane.SendPaste. Two
// routes in, because the platforms differ:
//
//   - macOS and Linux: bracketed paste works, so a paste arrives as one key event with
//     Paste set — see the msg.Paste branches in keys.go.
//   - Windows: the console delivers a paste as synthesised key-down events, one per
//     character, with no marker at all.
//
// So on Windows a paste is recognised by its shape: keys arriving in a burst are held
// for pasteGap and sent as one paste, keys at human speed are sent as themselves.
//
// The buffer is conservative, because the risk is turning typing into a paste and a
// held-down repeating key arrives just as fast. Only a burst with a newline or more than
// one distinct character becomes a paste.

import (
	"runtime"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"hop/internal/terminal"
)

// handlePaste routes a paste to whichever mode owns the keyboard, in handleKey's order
// but ahead of all of it: every handler reads a key's name, and a paste's name is the
// whole clipboard. A mode with nowhere to put text drops the paste.
func (m *model) handlePaste(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	text := string(msg.Runes)
	s := m.sessions[m.active]

	switch {
	case m.auth.open:
		m.setAuthAnswer(m.authAnswer() + pasteInline(text))
	case m.help, m.hostKey.open, m.confirm.open:
		// Nothing on these cards takes text: they are a fingerprint and two questions.
	case m.hostForm.open:
		m.hostForm.buf[m.hostForm.cursor] += pasteInline(text)
	case m.importer.open:
		m.importer.path += pasteInline(text)
	case m.tunnels.open:
		if m.tunnels.editing && m.tunnels.field != tfKind {
			m.tunnels.buf[m.tunnels.field] += pasteInline(text)
		}
	case m.settings.open:
		// Only while a field has the keyboard.
		if m.settings.editing {
			m.settings.buf += pasteInline(text)
		}

	case m.active != "" && m.inPane() && m.activeDead():
		// A picture of a shell. It answers r, d and ctrl+o, and forwards nothing.

	case m.editing() && m.active != "":
		if s != nil && s.editor() != nil {
			s.editor().pane.SendPaste(text)
		}
	case m.browsing() && m.active != "":
		// The browser's keys are commands; there is no field on it to paste into.
	case m.scrolling() && m.focused() && m.active != "":
		// Pasting into history is pasting into its shell: come back to live first.
		if s != nil && s.shell() != nil {
			m.exitScrollback()
			s.shell().pane.SendPaste(text)
		}
	case m.focused() && m.active != "":
		if s != nil && s.shell() != nil {
			s.shell().pane.SendPaste(text)
		}

	case m.filtering:
		m.filter += pasteInline(text)
		m.applyFilter()
	}
	return m, nil
}

// pasteInline is a paste on its way into a single-line field: its first line, with
// control characters dropped. Copying a password out of a manager takes the trailing
// newline along, and the remaining lines are dropped rather than joined.
func pasteInline(text string) string {
	if i := strings.IndexAny(text, "\r\n"); i >= 0 {
		text = text[:i]
	}
	return strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return -1
		}
		return r
	}, text)
}

// pasteGap is how long a burst must go quiet before it is taken to have ended: above
// the microseconds between synthesised keystrokes, below the tens of milliseconds
// between typed ones.
const pasteGap = 8 * time.Millisecond

// pasteMax bounds how many keys are held before the buffer is flushed regardless of the
// gap: a large paste is split into several rather than held whole in memory.
const pasteMax = 4096

// pasteFlushMsg asks for the buffer to be sent. seq identifies the buffer it was armed
// for, so a flush only happens after a genuine quiet gap.
type pasteFlushMsg struct{ seq int }

// pasteBuf is the burst being collected. pane is where the keys were typed, captured
// when the burst starts, so they land there whatever the focus does later.
type pasteBuf struct {
	keys []tea.KeyMsg
	pane *terminal.Pane
	seq  int
}

// coalescePastes reports whether this platform needs the burst detection above. Only
// Windows does; everywhere else a paste announces itself.
func coalescePastes() bool { return runtime.GOOS == "windows" }

// takeKey offers a key to the paste buffer, reporting whether it was taken; a taken key
// is handled nowhere else, and the caller arms the flush.
//
// Only keys a paste is made of, and only while a pane forwards to a remote program —
// everywhere else a key is a command, and holding it back would delay it.
//
// An open leader counts as elsewhere even though the pane is focused: its second key is
// a chord of pastable runes, and a buffered chord is one that never lands.
func (m *model) takeKey(msg tea.KeyMsg) bool {
	if !m.pasteCoalesce || !pastable(msg) || m.cardOpen() || m.leaderArmed() || !m.forwardingPane() {
		return false
	}
	pane := m.keyPane()
	if pane == nil {
		return false
	}
	// A burst belongs to one pane. Focus can only move on a key this does not take, so
	// this is a guard rather than a live case.
	if len(m.paste.keys) > 0 && m.paste.pane != pane {
		m.flushPaste()
	}
	m.paste.pane = pane
	m.paste.keys = append(m.paste.keys, msg)
	m.paste.seq++
	return true
}

// pastable reports whether a key is one a paste can be made of: the characters, and the
// tab and enter between them. A modified key is a command, not a character.
func pastable(msg tea.KeyMsg) bool {
	if msg.Alt || msg.Paste {
		return false
	}
	switch msg.Type {
	case tea.KeyRunes, tea.KeySpace, tea.KeyTab, tea.KeyEnter:
		return true
	}
	return false
}

// cardOpen reports whether one of the modal cards has the keyboard — a guard the burst
// buffer cannot do without. The two cards that open by themselves arrive from a dial
// while you may be typing in another host's shell; without this, the code typed into the
// card that just appeared would be delivered to the shell behind it as a paste.
func (m *model) cardOpen() bool {
	return m.auth.open || m.help || m.hostKey.open || m.confirm.open ||
		m.hostForm.open || m.importer.open || m.tunnels.open || m.settings.open ||
		m.palette.open || m.menu.open || m.guidance.open
}

// forwardingPane reports whether the keyboard belongs to a remote program: a live shell
// pane on the live screen, or an editor tab.
func (m *model) forwardingPane() bool {
	if m.active == "" || m.activeDead() {
		return false
	}
	return (m.focused() && !m.scrolling()) || m.editing()
}

// keyPane is the pane a forwarded key goes to, or nil when there is none.
func (m *model) keyPane() *terminal.Pane {
	s := m.sessions[m.active]
	if s == nil {
		return nil
	}
	switch {
	case m.editing():
		if ed := s.editor(); ed != nil {
			return ed.pane
		}
	case m.focused():
		if sh := s.shell(); sh != nil {
			return sh.pane
		}
	}
	return nil
}

// pasteFlushCmd arms the flush for the buffer as it stands.
func (m *model) pasteFlushCmd() tea.Cmd {
	// A burst long enough to be worth splitting is sent now.
	if len(m.paste.keys) >= pasteMax {
		m.flushPaste()
		return nil
	}
	seq := m.paste.seq
	return tea.Tick(pasteGap, func(time.Time) tea.Msg { return pasteFlushMsg{seq: seq} })
}

// flushPaste sends what the buffer holds and empties it. A burst that looks like a paste
// goes as one; anything else is replayed key by key, exactly as it arrived.
func (m *model) flushPaste() {
	keys, pane := m.paste.keys, m.paste.pane
	m.paste.keys, m.paste.pane = nil, nil
	if len(keys) == 0 || pane == nil {
		return
	}

	if looksPasted(keys) {
		pane.SendPaste(pasteString(keys))
		return
	}
	for _, k := range keys {
		pane.SendKey(k)
	}
}

// looksPasted reports whether a burst is a paste rather than typing.
//
// A burst of one key never is. This matters most for Enter: read as a one-newline paste
// it goes bracketed to a readline that asked for bracketed paste, where it is inserted
// rather than executed and the terminal looks dead from then on.
//
// Past one key a newline settles it — nothing types two lines in milliseconds. Failing
// that it takes pasteRun characters, not all the same, which rules out both fast-typing
// shapes: a held-down key repeats one character, a fast digraph produces two.
//
// Erring this way is cheap: a short paste replayed as keystrokes is what typing it would
// have done, whereas "dw" sent as a paste inserts text instead of deleting a word.
func looksPasted(keys []tea.KeyMsg) bool {
	if len(keys) < 2 {
		return false
	}
	distinct := make(map[string]struct{}, len(keys))
	for _, k := range keys {
		if k.Type == tea.KeyEnter {
			return true
		}
		distinct[k.String()] = struct{}{}
	}
	return len(keys) >= pasteRun && len(distinct) > 1
}

// pasteRun is how many characters a newline-less burst takes before it reads as a paste:
// past anything a hand produces inside pasteGap, short enough to catch a one-line paste.
const pasteRun = 4

// pasteString assembles the buffered keys into text. Enter becomes a newline: SendPaste
// normalises line endings for the pty, and this is text until it gets there.
func pasteString(keys []tea.KeyMsg) string {
	var b strings.Builder
	for _, k := range keys {
		switch k.Type {
		case tea.KeyRunes:
			b.WriteString(string(k.Runes))
		case tea.KeySpace:
			b.WriteByte(' ')
		case tea.KeyTab:
			b.WriteByte('\t')
		case tea.KeyEnter:
			b.WriteByte('\n')
		}
	}
	return b.String()
}
