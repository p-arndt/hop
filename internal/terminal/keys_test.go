package terminal

import (
	"fmt"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// The base mapping, key by key; the modifier-encoded keys live in cursor_test.go.
func TestKeyToBytesTable(t *testing.T) {
	cases := []struct {
		name string
		msg  tea.KeyPressMsg
		want string
	}{
		{"rune", tea.KeyPressMsg{Code: 'a', Text: "a"}, "a"},
		{"multi-rune paste", tea.KeyPressMsg{Code: 'h', Text: "hi"}, "hi"},
		{"non-ascii rune is utf-8", tea.KeyPressMsg{Code: 'ü', Text: "ü"}, "\xc3\xbc"},
		{"wide rune is utf-8", tea.KeyPressMsg{Code: 'あ', Text: "あ"}, "\xe3\x81\x82"},
		{"space", tea.KeyPressMsg{Code: tea.KeySpace}, " "},
		{"enter is CR", tea.KeyPressMsg{Code: tea.KeyEnter}, "\r"},
		{"tab", tea.KeyPressMsg{Code: tea.KeyTab}, "\t"},
		{"backspace is DEL", tea.KeyPressMsg{Code: tea.KeyBackspace}, "\x7f"},
		{"esc", tea.KeyPressMsg{Code: tea.KeyEscape}, "\x1b"},
		{"up", tea.KeyPressMsg{Code: tea.KeyUp}, "\x1b[A"},
		{"down", tea.KeyPressMsg{Code: tea.KeyDown}, "\x1b[B"},
		{"right", tea.KeyPressMsg{Code: tea.KeyRight}, "\x1b[C"},
		{"left", tea.KeyPressMsg{Code: tea.KeyLeft}, "\x1b[D"},
		{"home", tea.KeyPressMsg{Code: tea.KeyHome}, "\x1b[H"},
		{"end", tea.KeyPressMsg{Code: tea.KeyEnd}, "\x1b[F"},
		{"delete", tea.KeyPressMsg{Code: tea.KeyDelete}, "\x1b[3~"},
		{"insert", tea.KeyPressMsg{Code: tea.KeyInsert}, "\x1b[2~"},
		{"pgup", tea.KeyPressMsg{Code: tea.KeyPgUp}, "\x1b[5~"},
		{"pgdown", tea.KeyPressMsg{Code: tea.KeyPgDown}, "\x1b[6~"},
		{"shift+tab is back-tab", tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModShift}, "\x1b[Z"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := string(keyToBytes(c.msg)); got != c.want {
				t.Fatalf("keyToBytes = %q, want %q", got, c.want)
			}
		})
	}
}

// ctrl+<letter> is the letter with its top three bits cleared, for the whole alphabet.
func TestKeyToBytesCtrlLetters(t *testing.T) {
	for letter := byte('a'); letter <= 'z'; letter++ {
		t.Run(fmt.Sprintf("ctrl+%c", letter), func(t *testing.T) {
			got := keyToBytes(tea.KeyPressMsg{Code: rune(letter), Mod: tea.ModCtrl})
			want := letter - 'a' + 1
			if len(got) != 1 || got[0] != want {
				t.Fatalf("keyToBytes(ctrl+%c) = %q, want %#x", letter, got, want)
			}
		})
	}
}

// The non-letter control bytes are the character masked to five bits. ctrl+space and
// ctrl+@ are the same byte, which is the one the AltGr fix used to cost.
func TestKeyToBytesCtrlSymbols(t *testing.T) {
	cases := []struct {
		code rune
		want byte
	}{
		{' ', 0x00},
		{'@', 0x00},
		{'[', 0x1b},
		{'\\', 0x1c},
		{']', 0x1d},
		{'^', 0x1e},
		{'_', 0x1f},
	}
	for _, c := range cases {
		msg := tea.KeyPressMsg{Code: c.code, Mod: tea.ModCtrl}
		t.Run(msg.String(), func(t *testing.T) {
			got := keyToBytes(msg)
			if len(got) != 1 || got[0] != c.want {
				t.Fatalf("keyToBytes(%s) = %q, want %#x", msg, got, c.want)
			}
		})
	}
}

func TestKeyToBytesUnmappedKeysProduceNothing(t *testing.T) {
	for _, msg := range []tea.KeyPressMsg{
		{Code: tea.KeyF1},
		{Code: tea.KeyF12},
		{},
	} {
		if got := keyToBytes(msg); len(got) != 0 {
			t.Fatalf("keyToBytes(%s) = %q, want no bytes", msg, got)
		}
	}
}
