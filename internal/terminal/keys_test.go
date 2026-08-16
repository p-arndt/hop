package terminal

import (
	"fmt"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// The base mapping keyToBytes documents, key by key: what a pane writes to the remote pty
// for every event that is not modifier-encoded (those live in cursor_test.go). A remote
// program sees only these bytes, so a wrong or missing entry is a key that silently does
// nothing — or the wrong thing — over SSH.
func TestKeyToBytesTable(t *testing.T) {
	cases := []struct {
		name string
		msg  tea.KeyMsg
		want string
	}{
		{"rune", tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}}, "a"},
		{"multi-rune paste", tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("hi")}, "hi"},
		{"non-ascii rune is utf-8", tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'ü'}}, "\xc3\xbc"},
		{"wide rune is utf-8", tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'あ'}}, "\xe3\x81\x82"},
		{"space", tea.KeyMsg{Type: tea.KeySpace}, " "},
		{"enter is CR", tea.KeyMsg{Type: tea.KeyEnter}, "\r"},
		{"tab", tea.KeyMsg{Type: tea.KeyTab}, "\t"},
		{"backspace is DEL", tea.KeyMsg{Type: tea.KeyBackspace}, "\x7f"},
		{"esc", tea.KeyMsg{Type: tea.KeyEsc}, "\x1b"},
		{"up", tea.KeyMsg{Type: tea.KeyUp}, "\x1b[A"},
		{"down", tea.KeyMsg{Type: tea.KeyDown}, "\x1b[B"},
		{"right", tea.KeyMsg{Type: tea.KeyRight}, "\x1b[C"},
		{"left", tea.KeyMsg{Type: tea.KeyLeft}, "\x1b[D"},
		{"home", tea.KeyMsg{Type: tea.KeyHome}, "\x1b[H"},
		{"end", tea.KeyMsg{Type: tea.KeyEnd}, "\x1b[F"},
		{"delete", tea.KeyMsg{Type: tea.KeyDelete}, "\x1b[3~"},
		{"insert", tea.KeyMsg{Type: tea.KeyInsert}, "\x1b[2~"},
		{"pgup", tea.KeyMsg{Type: tea.KeyPgUp}, "\x1b[5~"},
		{"pgdown", tea.KeyMsg{Type: tea.KeyPgDown}, "\x1b[6~"},
		{"shift+tab is back-tab", tea.KeyMsg{Type: tea.KeyShiftTab}, "\x1b[Z"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := string(keyToBytes(c.msg)); got != c.want {
				t.Fatalf("keyToBytes = %q, want %q", got, c.want)
			}
		})
	}
}

// ctrl+<letter> is the letter with its top three bits cleared — the whole alphabet, not
// just the few chords hop happens to name. ctrl+c must reach the remote as 0x03 or
// nothing on the far side can ever be interrupted.
func TestKeyToBytesCtrlLetters(t *testing.T) {
	types := map[byte]tea.KeyType{
		'a': tea.KeyCtrlA, 'b': tea.KeyCtrlB, 'c': tea.KeyCtrlC, 'd': tea.KeyCtrlD,
		'e': tea.KeyCtrlE, 'f': tea.KeyCtrlF, 'g': tea.KeyCtrlG, 'h': tea.KeyCtrlH,
		'i': tea.KeyCtrlI, 'j': tea.KeyCtrlJ, 'k': tea.KeyCtrlK, 'l': tea.KeyCtrlL,
		'm': tea.KeyCtrlM, 'n': tea.KeyCtrlN, 'o': tea.KeyCtrlO, 'p': tea.KeyCtrlP,
		'q': tea.KeyCtrlQ, 'r': tea.KeyCtrlR, 's': tea.KeyCtrlS, 't': tea.KeyCtrlT,
		'u': tea.KeyCtrlU, 'v': tea.KeyCtrlV, 'w': tea.KeyCtrlW, 'x': tea.KeyCtrlX,
		'y': tea.KeyCtrlY, 'z': tea.KeyCtrlZ,
	}
	for letter := byte('a'); letter <= 'z'; letter++ {
		t.Run(fmt.Sprintf("ctrl+%c", letter), func(t *testing.T) {
			got := keyToBytes(tea.KeyMsg{Type: types[letter]})
			want := []byte{letter - 'a' + 1}
			if len(got) != 1 || got[0] != want[0] {
				t.Fatalf("keyToBytes(ctrl+%c) = %q, want %q", letter, got, want)
			}
		})
	}
}

// The control bytes that are not letters — the ones a shell reaches for as ctrl+\ (quit)
// and ctrl+_ (undo) — are the character masked to five bits.
func TestKeyToBytesCtrlSymbols(t *testing.T) {
	cases := []struct {
		typ  tea.KeyType
		want byte
	}{
		{tea.KeyCtrlAt, 0x00},
		{tea.KeyCtrlOpenBracket, 0x1b},
		{tea.KeyCtrlBackslash, 0x1c},
		{tea.KeyCtrlCloseBracket, 0x1d},
		{tea.KeyCtrlCaret, 0x1e},
		{tea.KeyCtrlUnderscore, 0x1f},
	}
	for _, c := range cases {
		msg := tea.KeyMsg{Type: c.typ}
		t.Run(msg.String(), func(t *testing.T) {
			got := keyToBytes(msg)
			if len(got) != 1 || got[0] != c.want {
				t.Fatalf("keyToBytes(%s) = %q, want %#x", msg, got, c.want)
			}
		})
	}
}

// A key hop has no mapping for produces nothing rather than a stray byte: the pane
// writes keyToBytes' result straight to the pty, and a wrong guess is indistinguishable
// from the user having typed it.
func TestKeyToBytesUnmappedKeysProduceNothing(t *testing.T) {
	for _, msg := range []tea.KeyMsg{
		{Type: tea.KeyF1},
		{Type: tea.KeyF12},
		{Type: tea.KeyRunes},
	} {
		if got := keyToBytes(msg); len(got) != 0 {
			t.Fatalf("keyToBytes(%s) = %q, want no bytes", msg, got)
		}
	}
}
