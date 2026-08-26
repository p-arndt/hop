package tui

// macOS and Linux deliver a bracketed paste as one key event with Paste set (see the
// msg.Paste branches in keys.go); the Windows console delivers it as synthesised key-down
// events with no marker, so there a paste is recognised by its shape: keys arriving inside
// burstGap are buffered until pasteGap of quiet and sent as one. The first key of a burst
// always goes out immediately, so typing is never delayed.

import (
	"runtime"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"hop/internal/terminal"
)

// handlePaste routes a paste to whichever mode owns the keyboard: a clipboard is text, not
// a key, and no handler below reads it as one.
func (m *model) handlePaste(text string) (tea.Model, tea.Cmd) {
	s := m.sessions[m.active]

	switch {
	case m.auth.open:
		m.setAuthAnswer(m.authAnswer() + pasteInline(text))
	case m.help, m.hostKey.open, m.confirm.open:
	case m.hostForm.open:
		m.hostForm.buf[m.hostForm.cursor] += pasteInline(text)
	case m.importer.open:
		m.importer.path += pasteInline(text)
	case m.tunnels.open:
		if m.tunnels.editing && m.tunnels.field != tfKind {
			m.tunnels.buf[m.tunnels.field] += pasteInline(text)
		}
	case m.settings.open:
		if m.settings.editing {
			m.settings.buf += pasteInline(text)
		}

	case m.active != "" && m.inPane() && m.activeDead():

	case m.editing() && m.active != "":
		if s != nil && s.editor() != nil {
			m.reportInput(s.editor().pane.SendPaste(text))
		}
	case m.browsing() && m.active != "":
		// The browser's keys are commands; there is no field to paste into.
	case m.scrolling() && m.focused() && m.active != "":
		// Pasting into history is pasting into its shell: come back to live first.
		if s != nil && s.shell() != nil {
			m.exitScrollback()
			m.reportInput(s.shell().pane.SendPaste(text))
		}
	case m.focused() && m.active != "":
		if s != nil && s.shell() != nil {
			m.reportInput(s.shell().pane.SendPaste(text))
		}

	case m.filtering:
		m.filter += pasteInline(text)
		m.applyFilter()
	}
	return m, nil
}

// pasteInline is a paste on its way into a single-line field: its first line, with control
// characters dropped — a copied password carries its trailing newline along.
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

// pasteGap is how long a burst must go quiet before it has ended: above the microseconds
// between synthesised keystrokes, below the tens of milliseconds between typed ones.
const pasteGap = 8 * time.Millisecond

// burstGap is the gap below which two keys cannot both have been typed: measured at Update
// (so it must clear hop's own per-key work) yet under Windows' ~30ms key repeat rate.
const burstGap = 20 * time.Millisecond

// pasteMax bounds how many keys are held before the buffer is flushed regardless of the gap.
const pasteMax = 4096

// pasteFlushMsg asks for the buffer to be sent; seq identifies the buffer it was armed for,
// so a flush only happens after a genuine quiet gap.
type pasteFlushMsg struct{ seq int }

// pasteBuf is the burst being collected; pane is captured when the burst starts, so the keys
// land there whatever the focus does later.
type pasteBuf struct {
	keys []tea.KeyPressMsg
	pane *terminal.Pane
	seq  int
	// lastAt is when the previous pastable key was offered — see burstGap.
	lastAt time.Time
}

// coalescePastes reports whether this platform needs the burst detection; only Windows does.
func coalescePastes() bool { return runtime.GOOS == "windows" }

// takeKey offers a key to the paste buffer; a taken key is handled nowhere else, and the
// caller arms the flush. An open leader counts as elsewhere: a buffered chord never lands.
func (m *model) takeKey(msg tea.KeyPressMsg) bool {
	if !m.pasteCoalesce || !pastable(msg) || m.cardOpen() || m.leaderArmed() || !m.forwardingPane() {
		return false
	}
	pane := m.keyPane()
	if pane == nil {
		return false
	}
	// A key that did not follow another inside burstGap cannot be part of a paste. Enter is
	// the exception and always buffers: sent immediately, a clipboard starting with a newline
	// would submit whatever was already on the line.
	now := m.now()
	gap := now.Sub(m.paste.lastAt)
	m.paste.lastAt = now
	if len(m.paste.keys) == 0 && gap > burstGap && msg.Code != tea.KeyEnter {
		return false
	}
	// A burst belongs to one pane; focus can only move on a key this does not take, so this
	// is a guard rather than a live case.
	if len(m.paste.keys) > 0 && m.paste.pane != pane {
		m.flushPaste()
	}
	m.paste.pane = pane
	m.paste.keys = append(m.paste.keys, msg)
	m.paste.seq++
	return true
}

// pastable reports whether a key is one a paste can be made of; a modified key is a command.
func pastable(msg tea.KeyPressMsg) bool {
	if msg.Mod != 0 {
		return false
	}
	// Text covers every printable character, the space among them.
	return msg.Text != "" || msg.Code == tea.KeyTab || msg.Code == tea.KeyEnter
}

// cardOpen guards the buffer: a card that opens by itself while you type into another host's
// shell would otherwise send what you type into it to that shell as a paste.
func (m *model) cardOpen() bool {
	return m.auth.open || m.help || m.hostKey.open || m.confirm.open ||
		m.hostForm.open || m.importer.open || m.tunnels.open || m.settings.open ||
		m.palette.open || m.menu.open || m.guidance.open
}

// forwardingPane reports whether the keyboard belongs to a remote program: a live shell pane
// on the live screen, or an editor tab.
func (m *model) forwardingPane() bool {
	if m.active == "" || m.activeDead() {
		return false
	}
	return (m.focused() && !m.scrolling()) || m.editing()
}

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

func (m *model) pasteFlushCmd() tea.Cmd {
	if len(m.paste.keys) >= pasteMax {
		m.flushPaste()
		return nil
	}
	seq := m.paste.seq
	return tea.Tick(pasteGap, func(time.Time) tea.Msg { return pasteFlushMsg{seq: seq} })
}

// flushPaste sends what the buffer holds and empties it: as one paste if it looks pasted,
// otherwise replayed key by key exactly as it arrived.
func (m *model) flushPaste() {
	keys, pane := m.paste.keys, m.paste.pane
	m.paste.keys, m.paste.pane = nil, nil
	if len(keys) == 0 || pane == nil {
		return
	}

	if looksPasted(keys) {
		m.reportInput(pane.SendPaste(pasteString(keys)))
		return
	}
	m.reportInput(pane.SendKeys(keys))
}

// looksPasted reports whether a burst is a paste. A lone key never is: readline inserts a
// bracketed one-newline paste rather than executing it, leaving the terminal looking dead.
func looksPasted(keys []tea.KeyPressMsg) bool {
	if len(keys) < 2 {
		return false
	}
	distinct := make(map[string]struct{}, len(keys))
	for _, k := range keys {
		if k.Code == tea.KeyEnter {
			return true
		}
		distinct[k.String()] = struct{}{}
	}
	return len(keys) >= pasteRun && len(distinct) > 1
}

// pasteRun is how many characters a newline-less burst takes before it reads as a paste:
// past anything a hand produces inside pasteGap, short enough to catch a one-line paste.
const pasteRun = 4

// pasteString assembles the buffered keys into text; Enter becomes a newline, since
// SendPaste normalises line endings for the pty.
func pasteString(keys []tea.KeyPressMsg) string {
	var b strings.Builder
	for _, k := range keys {
		switch k.Code {
		case tea.KeyTab:
			b.WriteByte('\t')
		case tea.KeyEnter:
			b.WriteByte('\n')
		default:
			// Text covers every printable character, the space among them.
			b.WriteString(k.Text)
		}
	}
	return b.String()
}

// now is the wall clock the burst detection measures gaps with, unless a test injected one.
func (m *model) now() time.Time {
	if m.clock != nil {
		return m.clock()
	}
	return time.Now()
}
