package tui

// Paste on the way in. What happens to it afterwards is terminal.Pane.SendPaste
// (see internal/terminal/paste.go). Two routes in, because the platforms differ:
//
//   - macOS and Linux: bracketed paste works, so a paste arrives as one key event
//     with Paste set — see the msg.Paste branches in keys.go.
//   - Windows: Bubble Tea reads console input records, and the console delivers a
//     paste as synthesised key-down events, one per character, with no marker at
//     all. ctrl+shift+v and right-click are handled by the terminal itself, so only
//     the characters reach hop.
//
// So on Windows a paste is recognised by its shape: keys arriving in a burst are
// held for pasteGap and sent as one paste, keys at human speed are sent as
// themselves. The delay is local and an order of magnitude below the SSH round trip
// every keystroke already pays.
//
// The buffer is conservative, because the risk is turning *typing* into a paste and
// a held-down repeating key arrives just as fast. A run of the same character is
// replayed as keystrokes; only a burst with a newline or more than one distinct
// character becomes a paste — which is also the shape that gets mangled without
// this, since the newline is what makes an editor indent.

import (
	"runtime"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"hop/internal/terminal"
)

// handlePaste routes a paste to whichever mode owns the keyboard, in the same order
// handleKey routes a key — but ahead of all of them, because every handler reads a
// key's *name* and a paste's name is the whole clipboard: "q" in the host list is
// not the quit key, "esc" in a form is three characters of a hostname.
//
// A mode with nowhere to put text (the host list, the browser, the confirmations)
// drops the paste rather than guessing at it.
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
		// Only while a field has the keyboard. On the list itself a paste would be a
		// value for a field nobody has opened.
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
		// Pasting into history is pasting into the shell it belongs to: come back to
		// the live screen first, as typing a letter in there does.
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
// newline along, and a field that keeps it submits a value that does not match. The
// remaining lines are dropped rather than joined — a hostname made of three
// concatenated lines is not what was meant either.
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

// pasteGap is how long a burst must go quiet before it is taken to have ended.
// Well above the microseconds between two synthesised keystrokes, well below the
// tens of milliseconds between two typed ones.
const pasteGap = 8 * time.Millisecond

// pasteMax bounds how many keys are held before the buffer is flushed regardless
// of the gap. A large paste is split into several — an editor takes consecutive
// pasted blocks the same way it takes one — rather than held whole in memory.
const pasteMax = 4096

// pasteFlushMsg asks for the buffer to be sent. seq identifies the buffer it was
// armed for: a key arriving after it was scheduled bumps the sequence and arms
// another, so a flush only ever happens after a genuine quiet gap.
type pasteFlushMsg struct{ seq int }

// pasteBuf is the burst being collected. pane is the pane the keys were typed at,
// captured when the burst starts: it is what they are delivered to, whatever the
// focus does later.
type pasteBuf struct {
	keys []tea.KeyMsg
	pane *terminal.Pane
	seq  int
}

// coalescePastes reports whether this platform needs the burst detection above.
// Only Windows does; everywhere else a paste announces itself, and buffering
// would add a delay that bought nothing.
func coalescePastes() bool { return runtime.GOOS == "windows" }

// takeKey offers a key to the paste buffer, reporting whether it was taken; a taken
// key is handled nowhere else, and the caller arms the flush.
//
// Only keys a paste is made of, and only while a pane forwards to a remote program.
// Everywhere else a key is a command, and holding it back would delay that command
// to catch a paste the view has no use for.
//
// An open leader is one of those elsewheres, even though the pane is still focused:
// its second key is a chord (o, c, a digit — all of them pastable runes), and a
// buffered chord is a chord that never lands. Windows is the only platform that
// coalesces at all, so without this the leader would work everywhere but there.
func (m *model) takeKey(msg tea.KeyMsg) bool {
	if !m.pasteCoalesce || !pastable(msg) || m.cardOpen() || m.leaderArmed() || !m.forwardingPane() {
		return false
	}
	pane := m.keyPane()
	if pane == nil {
		return false
	}
	// A burst belongs to one pane. Focus can only move on a key this function does
	// not take (which flushes first), so this is a guard rather than a live case.
	if len(m.paste.keys) > 0 && m.paste.pane != pane {
		m.flushPaste()
	}
	m.paste.pane = pane
	m.paste.keys = append(m.paste.keys, msg)
	m.paste.seq++
	return true
}

// pastable reports whether a key is one a paste can be made of: the characters,
// and the tab and enter between them. A modified key never is — alt+j is a
// command, not a character — and a key event already marked as a paste has no
// business being reassembled from pieces.
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

// cardOpen reports whether one of the modal cards has the keyboard. It is the
// guard the burst buffer cannot do without: a card is answered *above* the panes in
// handleKey, but the buffer is answered above the card — and the two cards that
// open by themselves, the 2FA challenge and the host-key question, arrive from a
// dial while you may well be typing in another host's shell. Without this, the code
// typed into the card that just appeared would be held as a paste and delivered to
// the shell behind it.
func (m *model) cardOpen() bool {
	return m.auth.open || m.help || m.hostKey.open || m.confirm.open ||
		m.hostForm.open || m.importer.open || m.tunnels.open || m.settings.open
}

// forwardingPane reports whether the keyboard currently belongs to a remote
// program: a live shell pane on the live screen, or an editor tab.
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
	// A burst long enough to be worth splitting is sent now: the next key would only
	// grow it, and nothing about waiting improves it.
	if len(m.paste.keys) >= pasteMax {
		m.flushPaste()
		return nil
	}
	seq := m.paste.seq
	return tea.Tick(pasteGap, func(time.Time) tea.Msg { return pasteFlushMsg{seq: seq} })
}

// flushPaste sends what the buffer holds and empties it. A burst that looks like a
// paste goes as one; anything else is replayed key by key, exactly as it arrived,
// so a key held down still repeats and a single character is still a keystroke.
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
// A burst of one key never is. This matters most for Enter: a typed Enter arrives
// alone, and reading it as a one-newline paste sends it *bracketed* to a readline
// that asked for bracketed paste — where it is inserted rather than executed, so
// the command never runs and the terminal looks dead from then on.
//
// Past one key a newline settles it: nothing types two lines in milliseconds, and a
// typed Enter after typed characters has a human-sized gap in front of it that ended
// their burst already. Failing that it takes pasteRun characters, not all the same,
// which rules out both fast-typing shapes — a held-down key repeats one character, a
// fast digraph produces two.
//
// Erring this way is cheap; erring the other way is not. A short paste replayed as
// keystrokes is exactly what typing it would have done, whereas "dw" typed quickly
// in vim and sent as a paste inserts text instead of deleting a word.
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

// pasteRun is how many characters a newline-less burst takes before it reads as a
// paste. Four is past anything a hand produces inside pasteGap, and short enough to
// catch a one-line paste whole.
const pasteRun = 4

// pasteString assembles the buffered keys into text. Enter becomes a newline rather
// than a CR: SendPaste normalises line endings for the pty, and this is text until
// it gets there.
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
