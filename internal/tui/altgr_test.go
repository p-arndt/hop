package tui

import (
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// withAltGrKeyboard runs the Windows branch of normalizeAltGr on any host.
func withAltGrKeyboard(t *testing.T, on bool) {
	t.Helper()
	prev := altGrKeyboard
	altGrKeyboard = on
	t.Cleanup(func() { altGrKeyboard = prev })
}

func TestNormalizeAltGrComposedCharacters(t *testing.T) {
	withAltGrKeyboard(t, true)

	cases := []struct {
		name string
		in   tea.KeyMsg
		want string
	}{
		{"at from altgr+q", tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'@'}, Alt: true}, "@"},
		{"bracket from altgr+8", tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'['}, Alt: true}, "["},
		{"pipe from altgr+<", tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'|'}, Alt: true}, "|"},
		{"tilde from altgr++", tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'~'}, Alt: true}, "~"},
		{"euro from altgr+e", tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'€'}, Alt: true}, "€"},
		{"accented letter", tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'ą'}, Alt: true}, "ą"},
		{"at read as ctrl+@", tea.KeyMsg{Type: tea.KeyCtrlAt, Alt: true}, "@"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := normalizeAltGr(tc.in)
			if got.Alt {
				t.Fatalf("alt still set on %q", got.String())
			}
			if got.String() != tc.want {
				t.Fatalf("got %q, want %q", got.String(), tc.want)
			}
		})
	}
}

func TestNormalizeAltGrKeepsRealChords(t *testing.T) {
	withAltGrKeyboard(t, true)

	cases := []tea.KeyMsg{
		{Type: tea.KeyRunes, Runes: []rune{'1'}, Alt: true},
		{Type: tea.KeyRunes, Runes: []rune{'9'}, Alt: true},
		{Type: tea.KeyRunes, Runes: []rune{'b'}, Alt: true},
		{Type: tea.KeyRunes, Runes: []rune{'F'}, Alt: true},
		{Type: tea.KeyRunes, Runes: []rune{' '}, Alt: true},
		{Type: tea.KeyRunes, Runes: []rune{0}, Alt: true},
		{Type: tea.KeyRunes, Alt: true},
		{Type: tea.KeyLeft, Alt: true},
		{Type: tea.KeyEnter, Alt: true},
	}
	for _, in := range cases {
		got := normalizeAltGr(in)
		if !got.Alt || got.Type != in.Type {
			t.Fatalf("chord %v was rewritten to %v", in, got)
		}
	}
}

func TestNormalizeAltGrLeavesUnmodifiedAndOtherPlatforms(t *testing.T) {
	withAltGrKeyboard(t, true)
	plain := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'@'}}
	if got := normalizeAltGr(plain); got.String() != "@" || got.Alt {
		t.Fatalf("plain @ changed: %v", got)
	}
	paste := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("a@b"), Alt: true, Paste: true}
	if got := normalizeAltGr(paste); !got.Alt || !got.Paste {
		t.Fatalf("paste changed: %v", got)
	}

	withAltGrKeyboard(t, false)
	alt := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'.'}, Alt: true}
	if got := normalizeAltGr(alt); !got.Alt {
		t.Fatalf("alt+. lost its modifier off Windows: %v", got)
	}
	if got := normalizeAltGr(tea.KeyMsg{Type: tea.KeyCtrlAt, Alt: true}); got.Type != tea.KeyCtrlAt {
		t.Fatalf("ctrl+@ rewritten off Windows: %v", got)
	}
}

// End to end: the character AltGr composed reaches the remote shell as itself, not
// behind the ESC prefix an alt chord is forwarded with (issue #17).
func TestAltGrCharacterReachesTheShell(t *testing.T) {
	withAltGrKeyboard(t, true)
	m, stdin := pasteModel()
	clk := newTestClock(m)

	clk.advance(time.Second) // typed, not pasted: delivered on the spot
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'@'}, Alt: true})
	flushPanes(m)

	if got := stdin.String(); got != "@" {
		t.Fatalf("the shell received %q, want %q", got, "@")
	}
}

// And a real alt chord still is one: alt+1 jumps to the first tab rather than typing a 1.
func TestAltDigitStillSwitchesTabs(t *testing.T) {
	withAltGrKeyboard(t, true)
	m, stdin := pasteModel()
	second, _ := pastePane()
	m.sessions["web"].shells = append(m.sessions["web"].shells, &shellTab{id: 2, pane: second})
	m.sessions["web"].activeSh = 1
	clk := newTestClock(m)

	clk.advance(time.Second)
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'1'}, Alt: true})
	flushPanes(m)

	if got := stdin.String(); got != "" {
		t.Fatalf("alt+1 was typed into the shell as %q", got)
	}
	if m.sessions["web"].activeSh != 0 {
		t.Fatalf("alt+1 left the session on tab %d, want the first", m.sessions["web"].activeSh)
	}
}
