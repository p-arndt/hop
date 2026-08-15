package terminal

// Pasting into a pane.
//
// A paste is not typing, and the difference matters to the far end: text arriving as
// keystrokes is indented again by vim's autoindent and run line by line by a shell,
// while the same text as a paste is inserted verbatim. The way a terminal says which
// this is, is bracketed paste — DECSET 2004:
//
//	ESC[?2004h                  the program asks to be told
//	ESC[200~ <text> ESC[201~    the terminal marks a paste as one
//
// The two halves mirror mouse.go: pasteState shadows the mode through the emulator's
// callbacks, and SendPaste is the write — bracketed when the far end asked for it, plain
// when it did not, sanitised either way. Where the paste comes from is the TUI's problem.

import (
	"strings"
	"sync"

	"github.com/charmbracelet/x/ansi"
)

// pasteState is the bracketed-paste mode the far end has asked for. Written by the
// output pump, read by the UI goroutine on every paste, hence the mutex — mouseState's
// arrangement.
type pasteState struct {
	mu sync.Mutex
	on bool
}

// setMode records the mode when it is the one this cares about.
func (s *pasteState) setMode(mode ansi.Mode, on bool) {
	dec, ok := mode.(ansi.DECMode)
	if !ok || dec != ansi.ModeBracketedPaste {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.on = on
}

// clear forgets the mode, as a full terminal reset does. Called from the output pump on
// the RIS the mode callbacks do not report (see oscScanner.ris).
func (s *pasteState) clear() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.on = false
}

// enabled reports whether the far end is in bracketed-paste mode.
func (s *pasteState) enabled() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.on
}

// BracketedPaste reports whether the far end has asked to be told which of its input is
// a paste. Exported for the TUI and tests; SendPaste consults the same state itself.
func (p *Pane) BracketedPaste() bool { return p.paste.enabled() }

// SendPaste writes text to the remote as a paste: bracketed if the far end asked to be
// told, plain otherwise. Empty text writes nothing at all, brackets included.
func (p *Pane) SendPaste(text string) {
	// Read the mode once: the output pump can flip it between two reads, and a body built
	// for a bracketed paste written without the brackets is a body of keystrokes.
	bracketed := p.paste.enabled()

	body := pasteText(text, bracketed)
	if body == "" {
		return
	}
	if bracketed {
		p.writeString(ansi.BracketedPasteStart + body + ansi.BracketedPasteEnd)
		return
	}
	p.writeString(body)
}

// pasteText is the payload SendPaste writes: the pasted text with the characters a
// terminal must not pass on removed, and its line endings turned into the carriage
// returns a pty expects.
//
// Newlines first: a pty's line discipline reads CR as the end of a line, so both CRLF
// and a bare LF become a single CR.
//
// Then the control characters, where the two modes differ:
//
//   - Bracketed, the far end knows not to act on the contents, so the text goes through
//     as it is. Only the bracket sequences are removed — the one way a paste can pretend
//     to have ended and have its rest read as keystrokes.
//   - Unbracketed, every byte is a keystroke, so the ones that would be commands are
//     dropped: ESC and the other C0 controls, which are the ctrl chords. Tab and CR
//     stay, being text in a paste.
func pasteText(text string, bracketed bool) string {
	text = strings.ReplaceAll(text, "\r\n", "\r")
	text = strings.ReplaceAll(text, "\n", "\r")

	// A Go string can hold any bytes at all, and the pty at the far end is UTF-8. Applied
	// in both modes, before the filtering below, so no path out of here writes a byte that
	// is not part of a character.
	text = strings.ToValidUTF8(text, "")

	if bracketed {
		return stripAll(stripAll(text, ansi.BracketedPasteStart), ansi.BracketedPasteEnd)
	}

	return strings.Map(func(r rune) rune {
		switch {
		case r == '\t' || r == '\r':
			return r
		case r < 0x20 || r == 0x7f:
			return -1
		}
		return r
	}, text)
}

// stripAll removes every occurrence of sub from s, including the ones that only come
// into existence as earlier ones are removed. One pass is not enough — a single
// ReplaceAll over "\x1b[2\x1b[201~01~" splices a fresh terminator out of the neighbours
// of the one it removed. It terminates because every pass that changes anything shortens
// the string.
func stripAll(s, sub string) string {
	for {
		out := strings.ReplaceAll(s, sub, "")
		if out == s {
			return s
		}
		s = out
	}
}
