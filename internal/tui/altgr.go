package tui

import (
	"runtime"
	"unicode"
	"unicode/utf8"

	tea "github.com/charmbracelet/bubbletea"
)

// altGrKeyboard reports whether this build has to undo the AltGr confusion below. Only
// Windows reads the keyboard as console key records; every other platform gets the
// composed character as plain UTF-8 bytes, with no modifier attached to undo.
var altGrKeyboard = runtime.GOOS == "windows"

// normalizeAltGr turns an AltGr composition back into the character it typed.
//
// On a non-US layout the third level of a key is reached with AltGr — @ is AltGr+q on a
// German keyboard, [ is AltGr+8, and | is AltGr+<. Windows reports that as a key event
// carrying the composed character *and* the modifiers AltGr is made of (left ctrl +
// right alt), which Bubble Tea hands on as an alt-modified key. hop then reads it as a
// chord: the character is swallowed by an alt binding, or forwarded to the remote shell
// behind an ESC prefix, so typing @ into a connected shell did nothing (issue #17).
// Pressing ctrl+alt+<key> by hand composes the same way and lands here too.
//
// A real alt chord is told apart by what it carries: alt+1 … alt+9, alt+b, alt+f are
// ASCII letters and digits, and an alt chord Windows cannot translate carries no
// character at all. A third-level key is a symbol or an accented letter, so only those
// lose the modifier.
func normalizeAltGr(msg tea.KeyMsg) tea.KeyMsg {
	if !altGrKeyboard || !msg.Alt || msg.Paste {
		return msg
	}

	switch msg.Type {
	case tea.KeyRunes:
		if !composedRunes(msg.Runes) {
			return msg
		}
		msg.Alt = false
		return msg
	case tea.KeyCtrlAt:
		// '@' is the one composed character Bubble Tea reads as a control key before it
		// looks at alt: with ctrl held, the character '@' is ctrl+@. Held with alt, it
		// is the AltGr composition instead — hop binds no ctrl+alt chord to lose.
		return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'@'}}
	}
	return msg
}

// composedRunes reports whether the runes of an alt-modified key look like a character
// AltGr composed rather than the key of a chord.
func composedRunes(runes []rune) bool {
	if len(runes) == 0 {
		return false
	}
	for _, r := range runes {
		if r == ' ' || !unicode.IsGraphic(r) {
			return false
		}
		if r < utf8.RuneSelf && (unicode.IsLetter(r) || unicode.IsDigit(r)) {
			return false
		}
	}
	return true
}
