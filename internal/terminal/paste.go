package terminal

// Pasting into a pane.
//
// A paste is not typing, and the difference matters to the program on the far
// end: text arriving as keystrokes is indented again by vim's autoindent, run
// line by line by a shell, and passed through whatever mappings the editor has,
// while the same text arriving as a paste is inserted verbatim. The way a
// terminal says which one this is, is bracketed paste — DECSET 2004:
//
//	ESC[?2004h                  the program asks to be told
//	ESC[200~ <text> ESC[201~    the terminal marks a paste as one
//
// So there are two halves here, and they mirror mouse.go exactly. pasteState
// shadows the mode through the emulator's callbacks, because that is where the
// parsing has already been done and vt's own isModeSet is unexported. SendPaste
// is the write: bracketed when the far end asked for it, plain text when it did
// not, and sanitised either way.
//
// Where the paste comes *from* is the TUI's problem, not this file's: a native
// paste event on macOS and Linux, and the keystroke burst Windows delivers one
// instead (see internal/tui/paste.go). Both arrive here as one string.

import (
	"strings"
	"sync"

	"github.com/charmbracelet/x/ansi"
)

// pasteState is the bracketed-paste mode the far end has asked for. Written by
// the output pump (the emulator's mode callbacks run inside Write), read by the
// UI goroutine on every paste, hence the mutex — the same arrangement as
// mouseState.
type pasteState struct {
	mu sync.Mutex
	on bool
}

// setMode records the mode when it is the one this cares about, and ignores
// every other mode change — which is most of them.
func (s *pasteState) setMode(mode ansi.Mode, on bool) {
	dec, ok := mode.(ansi.DECMode)
	if !ok || dec != ansi.ModeBracketedPaste {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.on = on
}

// clear forgets the mode, which is what a full terminal reset does to it. Called
// from the output pump on the RIS the mode callbacks do not report (see
// oscScanner.ris).
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

// BracketedPaste reports whether the program on the far end has asked to be told
// which of its input is a paste. It is exported for the TUI's status line and for
// tests; SendPaste consults the same state itself.
func (p *Pane) BracketedPaste() bool { return p.paste.enabled() }

// SendPaste writes text to the remote as a paste: bracketed if the far end asked
// to be told, plain otherwise. Empty text — a paste of nothing, or of nothing but
// characters pasteText drops — writes nothing at all, brackets included, so a
// stray paste cannot knock a program into and out of paste mode for no text.
func (p *Pane) SendPaste(text string) {
	// Read the mode once. The output pump can flip it between two reads, and the two
	// halves disagreeing is the bad case: a body built for a bracketed paste keeps the
	// escape sequences in it, and written without the brackets those are keystrokes.
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

// pasteText is the payload SendPaste writes: the pasted text with the characters
// a terminal must not pass on removed, and its line endings turned into the
// carriage returns a pty expects.
//
// Newlines first. A pty's line discipline reads CR as the end of a line — that is
// what the Enter key sends — so both CRLF and a bare LF become a single CR.
// Without that, a paste from a Windows clipboard arrives as a doubled line
// ending, and one from anywhere else as a linefeed that moves the cursor down a
// line without starting one.
//
// Then the control characters, and here the two modes genuinely differ:
//
//   - Bracketed, the far end knows this is a paste and will not act on what is in
//     it, so the text goes through as it is — an ESC in a file being pasted into
//     vim is part of the file. The one thing removed is the bracket sequences
//     themselves, which are the only way a paste can pretend to have ended and
//     have the rest of itself read as keystrokes.
//   - Unbracketed, every byte is a keystroke to whatever is reading, so the ones
//     that would be commands rather than text are dropped: ESC (which is what
//     drops vim out of insert mode mid-paste), and the other C0 controls, which
//     are the ctrl chords — a stray 0x03 in a paste is a ctrl+c. Tab and CR stay,
//     because those are text in a paste.
func pasteText(text string, bracketed bool) string {
	text = strings.ReplaceAll(text, "\r\n", "\r")
	text = strings.ReplaceAll(text, "\n", "\r")

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

// stripAll removes every occurrence of sub from s, including the ones that only
// come into existence as earlier ones are removed.
//
// One pass is not enough, and this is the whole security of a bracketed paste: a
// single ReplaceAll over "\x1b[2\x1b[201~01~" removes the inner terminator and
// splices its neighbours into a fresh one, which then goes to the remote intact and
// ends the paste early — everything after it read as keystrokes. Repeating until
// the string stops changing is what closes that, and it terminates because every
// pass that changes anything makes the string shorter.
func stripAll(s, sub string) string {
	for {
		out := strings.ReplaceAll(s, sub, "")
		if out == s {
			return s
		}
		s = out
	}
}
