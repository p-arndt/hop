package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// shiftKey builds the tea.KeyMsg whose String() is "shift+<name>".
func shiftKey(name string) tea.KeyMsg {
	switch name {
	case "left":
		return tea.KeyMsg{Type: tea.KeyShiftLeft}
	case "right":
		return tea.KeyMsg{Type: tea.KeyShiftRight}
	}
	panic("shiftKey: " + name)
}

// runeKey builds a plain character key, the way a digit arrives.
func runeKey(r rune) tea.KeyMsg {
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}}
}

// ctrlO is hop's leader, and the key that leaves a pane.
func ctrlO() tea.KeyMsg { return tea.KeyMsg{Type: tea.KeyCtrlO} }

// shift+←/→ cycle the shells open on the host and wrap at both ends. This is the
// binding that has to work on a stock macOS terminal, where alt never arrives.
func TestShellTabCyclingWithShift(t *testing.T) {
	m, s := shellModel(t, 3)

	for _, want := range []int{1, 2, 0} { // right wraps 2 -> 0
		m.handleKey(shiftKey("right"))
		if s.activeSh != want {
			t.Fatalf("after shift+right: activeSh = %d, want %d", s.activeSh, want)
		}
	}
	for _, want := range []int{2, 1, 0} { // left wraps 0 -> 2
		m.handleKey(shiftKey("left"))
		if s.activeSh != want {
			t.Fatalf("after shift+left: activeSh = %d, want %d", s.activeSh, want)
		}
	}
	if !m.focused() {
		t.Fatal("switching shells left the pane")
	}
}

// shift+←/→ cycle editor tabs the same way, alongside the alt aliases.
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

// The leader opens without moving anything and without starting a clock: the pane
// stays focused, and hop waits for the second key however long it takes.
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

// ctrl+o o is the way out — what a bare ctrl+o used to be, now that ctrl+o leads.
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

// ctrl+o then a digit selects that shell in place: no focus change, no leaving.
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

// A digit naming a shell that is not open changes nothing, and still closes the
// leader rather than leaving it open for the next keystroke.
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

// ctrl+o 0 opens another shell on this host, without leaving the pane to do it.
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

// A key that names no chord closes the leader and is swallowed: it must not reach
// the remote, which would otherwise act on the tail of an abandoned chord.
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

// While the leader is open hop owns the keyboard outright — above ctrl+b, which is
// otherwise held in every mode. Half a chord is no moment to toggle the sidebar and
// leave the other half hanging.
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

// A digit with no leader open is the shell's, not hop's.
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

// In the host list a digit is a way *in*: it focuses that shell of the host under
// the cursor. No leader, no window — in the list a digit has nothing else to be.
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

// A digit naming a shell the host has not got leaves the list alone.
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
// Every chord key is an ordinary pastable rune, and the pane is still focused while
// the leader is open — so without an explicit guard the burst buffer holds it and
// the chord never lands. Windows is the only platform that coalesces, which is
// exactly why this needs a test rather than a try.
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
