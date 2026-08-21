package terminal

// Pasting into a pane.
//
// Bracketed paste is DECSET 2004: the program asks with ESC[?2004h, the terminal then
// wraps a paste in ESC[200~ ... ESC[201~ so it is inserted verbatim rather than read as
// keystrokes.

import (
	"strings"
	"sync"

	"github.com/charmbracelet/x/ansi"
)

// pasteState is written by the output pump and read by the UI goroutine, hence the mutex.
type pasteState struct {
	mu sync.Mutex
	on bool
}

func (s *pasteState) setMode(mode ansi.Mode, on bool) {
	dec, ok := mode.(ansi.DECMode)
	if !ok || dec != ansi.ModeBracketedPaste {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.on = on
}

// clear forgets the mode; called from the output pump on the RIS the mode callbacks do not
// report (see oscScanner.ris).
func (s *pasteState) clear() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.on = false
}

func (s *pasteState) enabled() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.on
}

// BracketedPaste reports whether the far end has asked to be told which input is a paste.
func (p *Pane) BracketedPaste() bool { return p.paste.enabled() }

// SendPaste writes text to the remote as a paste, reporting whether it was taken.
func (p *Pane) SendPaste(text string) bool {
	// Read the mode once: a body built for a bracketed paste written without the brackets is
	// a body of keystrokes.
	bracketed := p.paste.enabled()

	body := pasteText(text, bracketed)
	if body == "" {
		return true
	}
	if bracketed {
		return p.send([]byte(ansi.BracketedPasteStart + body + ansi.BracketedPasteEnd))
	}
	return p.send([]byte(body))
}

// pasteText sanitises the payload SendPaste writes. A pty's line discipline reads CR as
// the end of a line, so CRLF and bare LF both become CR.
//
// Bracketed, only the bracket sequences are stripped — the one way a paste can pretend to
// have ended. Unbracketed, every byte is a keystroke, so ESC and the other C0 controls go.
func pasteText(text string, bracketed bool) string {
	text = strings.ReplaceAll(text, "\r\n", "\r")
	text = strings.ReplaceAll(text, "\n", "\r")

	// The pty at the far end is UTF-8; applied before the filtering below so no path out of
	// here writes a byte that is not part of a character.
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

// stripAll removes sub repeatedly: one ReplaceAll over "\x1b[2\x1b[201~01~" splices a fresh
// terminator out of the neighbours of the one it removed.
func stripAll(s, sub string) string {
	for {
		out := strings.ReplaceAll(s, sub, "")
		if out == s {
			return s
		}
		s = out
	}
}
