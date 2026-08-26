package tui

import (
	"testing"

	tea "charm.land/bubbletea/v2"
)

// shiftKey builds the tea.KeyPressMsg whose String() is "shift+<name>".
func shiftKey(name string) tea.KeyPressMsg {
	switch name {
	case "left":
		return tea.KeyPressMsg{Code: tea.KeyLeft, Mod: tea.ModShift}
	case "right":
		return tea.KeyPressMsg{Code: tea.KeyRight, Mod: tea.ModShift}
	}
	panic("shiftKey: " + name)
}

// runeKey builds a plain character key.
func runeKey(r rune) tea.KeyPressMsg {
	return tea.KeyPressMsg{Code: r, Text: string(r)}
}

// ctrlO is hop's leader key.
func ctrlO() tea.KeyPressMsg { return tea.KeyPressMsg{Code: 'o', Mod: tea.ModCtrl} }

// Wraps at both ends; the binding that works where alt never arrives.
func TestShellTabCyclingWithShift(t *testing.T) {
	m, s := shellModel(t, 3)

	for _, want := range []int{1, 2, 0} {
		m.handleKey(shiftKey("right"))
		if s.activeSh != want {
			t.Fatalf("after shift+right: activeSh = %d, want %d", s.activeSh, want)
		}
	}
	for _, want := range []int{2, 1, 0} {
		m.handleKey(shiftKey("left"))
		if s.activeSh != want {
			t.Fatalf("after shift+left: activeSh = %d, want %d", s.activeSh, want)
		}
	}
	if !m.focused() {
		t.Fatal("switching shells left the pane")
	}
}

func TestEditorTabCyclingWithShift(t *testing.T) {
	m, s := editorModel(t, "a.txt", "b.txt", "c.txt")

	for _, want := range []int{1, 2, 0} {
		m.handleKey(shiftKey("right"))
		if s.activeEd != want {
			t.Fatalf("after shift+right: activeEd = %d, want %d", s.activeEd, want)
		}
	}
	for _, want := range []int{2, 1, 0} {
		m.handleKey(shiftKey("left"))
		if s.activeEd != want {
			t.Fatalf("after shift+left: activeEd = %d, want %d", s.activeEd, want)
		}
	}
}

// The leader opens without moving anything and without starting a clock.
func TestLeaderOpensWithoutActing(t *testing.T) {
	m, _ := shellModel(t, 3)

	_, cmd := m.handleKey(ctrlO())
	if !m.focused() {
		t.Fatal("the leader left the pane; it is supposed to open and wait")
	}
	if cmd != nil {
		t.Fatal("the leader returned a command; it is on no clock")
	}
	if !m.leaderArmed() {
		t.Fatal("ctrl+o did not open the leader")
	}
}

func TestLeaderOutLeavesThePane(t *testing.T) {
	m, _ := shellModel(t, 2)

	m.handleKey(ctrlO())
	m.handleKey(runeKey('o'))

	if m.focused() {
		t.Fatal("ctrl+o o did not leave the pane")
	}
	if m.leaderArmed() {
		t.Fatal("the leader stayed open after it was resolved")
	}
}

func TestLeaderDigitSelectsShellInPlace(t *testing.T) {
	m, s := shellModel(t, 3)

	m.handleKey(ctrlO())
	m.handleKey(runeKey('3'))

	if s.activeSh != 2 {
		t.Fatalf("ctrl+o 3: activeSh = %d, want 2", s.activeSh)
	}
	if !m.focused() {
		t.Fatal("ctrl+o 3 left the pane")
	}
	if m.leaderArmed() {
		t.Fatal("the leader survived the key that resolved it")
	}
}

// A digit naming a shell that is not open still closes the leader.
func TestLeaderDigitIgnoresMissingShell(t *testing.T) {
	m, s := shellModel(t, 3)

	m.handleKey(ctrlO())
	m.handleKey(runeKey('9'))

	if s.activeSh != 0 {
		t.Fatalf("ctrl+o 9 with 3 shells moved to %d, want to stay on 0", s.activeSh)
	}
	if !m.focused() {
		t.Fatal("ctrl+o 9 left the pane")
	}
	if m.leaderArmed() {
		t.Fatal("a digit with no shell behind it left the leader open")
	}
}

func TestLeaderZeroOpensAnotherShell(t *testing.T) {
	m, _ := shellModel(t, 1)

	m.handleKey(ctrlO())
	_, cmd := m.handleKey(runeKey('0'))

	if cmd == nil {
		t.Fatal("ctrl+o 0 started no second shell")
	}
	if !m.connecting["web"] {
		t.Fatal("ctrl+o 0 did not mark the host as connecting")
	}
	if !m.focused() {
		t.Fatal("ctrl+o 0 left the pane")
	}
}

// An unbound key closes the leader and is swallowed rather than reaching the remote.
func TestLeaderCancelsOnAnUnboundKey(t *testing.T) {
	m, s := shellModel(t, 3)

	m.handleKey(ctrlO())
	m.handleKey(runeKey('z'))

	if !m.focused() {
		t.Fatal("an unbound key after the leader left the pane")
	}
	if m.leaderArmed() {
		t.Fatal("an unbound key left the leader open")
	}
	if s.activeSh != 0 {
		t.Fatalf("an unbound key after the leader selected shell %d", s.activeSh)
	}
}

func TestLeaderOutranksTheSidebarKey(t *testing.T) {
	m, _ := shellModel(t, 2)
	before := m.sidebarHidden

	m.handleKey(ctrlO())
	m.handleKey(key(t, "ctrl+b"))

	if m.sidebarHidden != before {
		t.Fatal("ctrl+b toggled the sidebar while the leader was open")
	}
	if m.leaderArmed() {
		t.Fatal("the leader stayed open after a key resolved it")
	}
}

func TestDigitWithoutLeaderGoesToTheShell(t *testing.T) {
	m, s := shellModel(t, 3)

	m.handleKey(runeKey('3'))

	if s.activeSh != 0 {
		t.Fatalf("a digit with no leader open moved to shell %d", s.activeSh)
	}
	if !m.focused() {
		t.Fatal("a digit with no leader open left the pane")
	}
}

// In the host list a digit focuses that shell of the host under the cursor.
func TestListDigitFocusesShell(t *testing.T) {
	m, s := shellModel(t, 3)
	m.mode = modeList
	m.cursor = 0

	m.handleKey(runeKey('2'))

	if s.activeSh != 1 {
		t.Fatalf("2 in the list: activeSh = %d, want 1", s.activeSh)
	}
	if !m.focused() {
		t.Fatal("a digit in the list did not focus the shell")
	}
}

func TestListDigitIgnoresMissingShell(t *testing.T) {
	m, _ := shellModel(t, 1)
	m.mode = modeList
	m.cursor = 0

	m.handleKey(runeKey('7'))

	if m.focused() {
		t.Fatal("7 in the list focused a shell that is not open")
	}
}

// The leader's second key must not be swallowed by the Windows paste coalescer.
func TestLeaderSurvivesThePasteCoalescer(t *testing.T) {
	m, s := shellModel(t, 3)
	m.pasteCoalesce = true // pretend to be Windows

	m.handleKey(ctrlO())
	if m.takeKey(runeKey('3')) {
		t.Fatal("the paste buffer took the leader's second key")
	}
	m.handleKey(runeKey('3'))

	if s.activeSh != 2 {
		t.Fatalf("ctrl+o 3 under paste coalescing: activeSh = %d, want 2", s.activeSh)
	}
}
