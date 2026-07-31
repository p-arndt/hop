package tui

// Paste, on the way in — where the text comes from, and how hop can tell a paste
// from typing. What happens to it afterwards is terminal.Pane.SendPaste, which
// marks it as a paste for the remote program (see internal/terminal/paste.go).
//
// There are two routes in, because the platforms genuinely differ:
//
//   - macOS and Linux: the terminal hop runs in supports bracketed paste, Bubble
//     Tea turns it on, and a paste arrives as a single key event carrying the whole
//     clipboard with Paste set. Nothing here has to detect anything — see the
//     msg.Paste branches in keys.go.
//   - Windows: Bubble Tea reads the *console input records* rather than a byte
//     stream (inputreader_windows.go), and the Windows console delivers a paste as
//     a stream of synthesised key-down events — one per character, with no marker
//     of any kind. Paste is never set there, and neither Windows Terminal's
//     ctrl+shift+v nor a right-click in conhost can be intercepted as a key, because
//     the terminal handles them itself and only the characters reach hop.
//
// So on Windows a paste has to be recognised by its shape, and that is what the
// rest of this file does: keys that arrive in a burst are held for a few
// milliseconds and sent as one paste, and keys that arrive at human speed are sent
// as themselves. The delay is bounded by pasteGap and is spent locally — every
// keystroke here is already crossing an SSH connection, which costs an order of
// magnitude more.
//
// The buffer is deliberately conservative: what it is protecting against is
// turning *typing* into a paste, and the one thing that arrives as fast as a paste
// without being one is a key held down until it repeats. A run of the same
// character is therefore replayed as keystrokes, and only a burst carrying a
// newline or more than one distinct character is treated as a paste — which is
// also exactly the shape that gets mangled without this, since it is the newline
// that makes an editor indent the next line.

import (
	"runtime"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"hop/internal/terminal"
)

// handlePaste routes a paste to whichever mode owns the keyboard, in the same
// order handleKey routes a key — and it is answered there, before any of them, for
// one reason: every handler below reads a key's *name* out of the event, and a
// paste's name is the whole clipboard. A clipboard holding "q" in the host list is
// not the quit key, and "esc" in a form is three characters of a hostname.
//
// Where the mode has something to type into, the text goes in; where it has not —
// the host list, the browser, the confirmation cards, a pane whose connection has
// dropped — a paste has nowhere to go and is dropped rather than guessed at.
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
	case m.settings.open:
		// Only while a field has the keyboard. On the list itself a paste would be a
		// value for a field nobody has opened.
		if m.settings.editing {
			m.settings.buf += pasteInline(text)
		}

	case m.active != "" && (m.focused || m.browsing || m.editing) && m.activeDead():
		// A picture of a shell. It answers r, d and ctrl+o, and forwards nothing.

	case m.editing && m.active != "":
		if s != nil && s.editor() != nil {
			s.editor().pane.SendPaste(text)
		}
	case m.browsing && m.active != "":
		// The browser's keys are commands; there is no field on it to paste into.
	case m.scrolling && m.focused && m.active != "":
		// Pasting into history is pasting into the shell it belongs to: come back to
		// the live screen first, as typing a letter in there does.
		if s != nil && s.shell() != nil {
			m.exitScrollback()
			s.shell().pane.SendPaste(text)
		}
	case m.focused && m.active != "":
		if s != nil && s.shell() != nil {
			s.shell().pane.SendPaste(text)
		}

	case m.filtering:
		m.filter += pasteInline(text)
		m.applyFilter()
	}
	return m, nil
}

// pasteInline is a paste on its way into a single-line field: the first line of it,
// with the control characters dropped.
//
// Everything hop pastes into by keyboard is one line — a hostname, a path, a
// password, a filter — and the text on a clipboard often is not: copying a password
// out of a manager takes the newline after it along, and a field that keeps that
// newline submits itself on the next Enter with a value that does not match. The
// rest of a multi-line paste is dropped rather than joined, because a hostname made
// by concatenating three lines is not what was meant either.
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

// takeKey offers a key to the paste buffer, reporting whether it was taken. A
// taken key is not handled anywhere else — it is held, and the caller arms the
// flush.
//
// Only the keys a paste is made of are taken, and only while a pane is forwarding
// to a remote program. Everywhere else — the host list, a card, the filter, the
// file browser, scrollback — a key is a command, and holding one back would delay
// a command to catch a paste that view has no use for anyway.
func (m *model) takeKey(msg tea.KeyMsg) bool {
	if !m.pasteCoalesce || !pastable(msg) || m.cardOpen() || !m.forwardingPane() {
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
		m.hostForm.open || m.importer.open || m.settings.open
}

// forwardingPane reports whether the keyboard currently belongs to a remote
// program: a live shell pane on the live screen, or an editor tab.
func (m *model) forwardingPane() bool {
	if m.active == "" || m.activeDead() {
		return false
	}
	return (m.focused && !m.scrolling) || m.editing
}

// keyPane is the pane a forwarded key goes to, or nil when there is none.
func (m *model) keyPane() *terminal.Pane {
	s := m.sessions[m.active]
	if s == nil {
		return nil
	}
	switch {
	case m.editing:
		if ed := s.editor(); ed != nil {
			return ed.pane
		}
	case m.focused:
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
// A burst of one key never is: it is the key that was pressed, and nothing else.
// This matters most for Enter — a typed Enter usually goes quiet for the gap and
// arrives here alone, and reading it as a one-newline paste sends it *bracketed*
// (ESC[200~ CR ESC[201~) to a shell whose readline has asked for bracketed
// paste. Bracketed text is inserted, not executed: the command line just typed
// never runs, no output ever appears, and every further Enter does the same —
// the terminal looks dead from the first command on.
//
// Past one key, a newline settles it on its own: nothing types two lines in a
// few milliseconds, and multi-line text is what this whole file exists for. It
// is genuinely in the burst with the text before it only during a paste — a
// typed Enter after typed characters has the human-sized gap in front of it,
// which ended their burst already. Failing that it takes
// pasteRun characters, not all of them the same — which rules out both of the ways
// typing can arrive this fast. A key held down until it repeats produces a run of
// one character; a fast digraph produces two.
//
// Being wrong in this direction is cheap and being wrong in the other is not. A
// short paste replayed as keystrokes is exactly what typing it would have done — no
// newline in it means no indenting to get wrong — whereas "dw" typed quickly in
// vim and sent as a paste is inserted as text instead of deleting a word.
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

// pasteRun is how many characters a burst with no newline in it takes before it is
// read as a paste. Four is past anything a hand produces inside pasteGap, and short
// enough to catch a one-line paste — a command, a path, a token — as one.
const pasteRun = 4

// pasteString assembles the buffered keys into the text they stand for. Enter is a
// newline here rather than a carriage return: SendPaste normalises line endings
// for the pty itself, and this is text until it gets there.
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
