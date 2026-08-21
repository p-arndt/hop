package tui

import (
	"runtime"
	"unicode"
	"unicode/utf8"

	tea "github.com/charmbracelet/bubbletea"
)

// Only Windows reports the keyboard as console key records, so only there is there an
// AltGr modifier to undo; elsewhere the composed character arrives as plain UTF-8.
var altGrKeyboard = runtime.GOOS == "windows"

// normalizeAltGr strips the alt Windows reports on an AltGr composition (@ = AltGr+q), which
// Bubble Tea surfaces as alt+key and hop read as a chord (issue #17). Real alt chords carry
// an ASCII letter or digit, or no character, so only symbols lose the modifier.
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
		// Bubble Tea reads composed '@' as ctrl+@ before it looks at alt; with alt held it is
		// the AltGr composition instead, and hop binds no ctrl+alt chord to lose.
		return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'@'}}
	}
	return msg
}

// composedRunes reports whether an alt-modified key's runes look AltGr-composed.
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
