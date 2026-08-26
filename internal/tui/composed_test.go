package tui

import (
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"hop/internal/store"
)

// What a German layout's AltGr+q arrives as under v2: the console composes the character
// and reports it as itself, with no modifier and no phantom key ahead of it. Under v1 it
// arrived as an alt chord preceded by the NUL key-downs of ctrl and alt, which is what
// internal/tui/altgr.go existed to undo — see issue #17 and the ledger entry on password
// prompts.
func composedAt() tea.KeyPressMsg {
	return tea.KeyPressMsg{Code: '@', Text: "@"}
}

func TestComposedCharacterReachesTheShell(t *testing.T) {
	m, stdin := pasteModel()
	clk := newTestClock(m)

	clk.advance(time.Second) // typed, not pasted: delivered on the spot
	m.Update(composedAt())
	flushPanes(m)

	if got := stdin.String(); got != "@" {
		t.Fatalf("the remote program received %q, want %q", got, "@")
	}
}

func TestComposedCharacterTypesOneCharacterIntoTheAuthCard(t *testing.T) {
	m := hostMgmtModel(t, store.Host{Alias: "web", HostName: "h", Port: 22})
	promptFor(m, "web", challenge("Password:"))

	m.Update(composedAt())

	if got := m.auth.answers[0]; got != "@" {
		t.Fatalf("the answer holds %q, want %q", got, "@")
	}
}

// The NUL byte is what the v1 fix cost: ctrl+space and the phantom key-down of a modifier
// were the same event there, so hop dropped both. v2 tells them apart, so it goes out again.
func TestCtrlSpaceReachesTheRemoteAsNUL(t *testing.T) {
	m, stdin := pasteModel()
	clk := newTestClock(m)

	clk.advance(time.Second)
	m.Update(tea.KeyPressMsg{Code: tea.KeySpace, Mod: tea.ModCtrl})
	flushPanes(m)

	if got := stdin.String(); got != "\x00" {
		t.Fatalf("the remote program received %q, want a single NUL", got)
	}
}

func TestAltDigitStillSwitchesTabs(t *testing.T) {
	m, stdin := pasteModel()
	second, _ := pastePane()
	m.sessions["web"].shells = append(m.sessions["web"].shells, &shellTab{id: 2, pane: second})
	m.sessions["web"].activeSh = 1
	clk := newTestClock(m)

	clk.advance(time.Second)
	m.Update(tea.KeyPressMsg{Code: '1', Mod: tea.ModAlt})
	flushPanes(m)

	if got := stdin.String(); got != "" {
		t.Fatalf("alt+1 was typed into the shell as %q", got)
	}
	if m.sessions["web"].activeSh != 0 {
		t.Fatalf("alt+1 left the session on tab %d, want the first", m.sessions["web"].activeSh)
	}
}
