package tui

import (
	"fmt"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"hop/internal/sshx"
	"hop/internal/terminal"
)

// selModel builds a focused shell on a pane that has printed text, with the clipboard
// writer replaced: a test must never write the clipboard of the machine it runs on. It
// returns a reader for whatever was copied. marker is the text the screen ends with,
// waited for before a test points at it.
func selModel(t *testing.T, screen, marker string) (*model, func() string) {
	t.Helper()

	var mu sync.Mutex
	var got string

	m := newMouseModel(3)
	m.active = "ha"
	m.mode = modeShell
	m.clipWrite = func(text string) error {
		mu.Lock()
		defer mu.Unlock()
		got = text
		return nil
	}

	p := terminal.New(&sshx.Session{
		Stdin:  nopWriteCloser{io.Discard},
		Stdout: strings.NewReader(screen),
	}, m.paneW, m.paneH, nil)
	m.sessions["ha"] = &session{shells: []*shellTab{{id: 1, pane: p}}}

	// The screen arrives on the pane's own pump; wait for it to be parsed onto the
	// emulator before any test points at it.
	deadline := time.Now().Add(2 * time.Second)
	for !strings.Contains(p.View(), marker) && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if !strings.Contains(p.View(), marker) {
		t.Fatalf("the pane never rendered %q", marker)
	}

	return m, func() string {
		mu.Lock()
		defer mu.Unlock()
		return got
	}
}

// drag builds the three events one drag arrives as, in pane-content coordinates
// offset onto the screen: press, motion, release.
func dragEvents(x1, y1, x2, y2 int) []tea.MouseMsg {
	// The sidebar's outer width plus the pane's border, and the header plus the
	// pane's top border — the inverse of paneLocal.
	const dx, dy = 33, 2
	return []tea.MouseMsg{
		{X: x1 + dx, Y: y1 + dy, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress},
		{X: x2 + dx, Y: y2 + dy, Button: tea.MouseButtonLeft, Action: tea.MouseActionMotion},
		{X: x2 + dx, Y: y2 + dy, Button: tea.MouseButtonLeft, Action: tea.MouseActionRelease},
	}
}

// The gesture the mouse took away, given back: drag over a shell pane, and what
// was under the pointer is on the clipboard when the button comes up.
func TestDragOverShellSelectsAndCopies(t *testing.T) {
	m, copied := selModel(t, "sudo apt update\r\n", "sudo apt update")

	for _, e := range dragEvents(0, 0, 14, 0) {
		m.handleMouse(e)
	}

	if copied() != "sudo apt update" {
		t.Fatalf("clipboard = %q, want %q", copied(), "sudo apt update")
	}
	if !m.sel.active {
		t.Fatal("the highlight went down with the button; it should outlive the drag")
	}
	if m.sel.dragging {
		t.Fatal("the drag is still live after the release")
	}
	// And the highlight is on the screen the pane draws.
	if !strings.Contains(m.View(), "\x1b[7m") {
		t.Fatal("the selection is not painted on the pane")
	}
}

// A click that never moved selects nothing and copies nothing: a pointer resting on a
// pane must not clear the clipboard somebody was about to paste from.
func TestClickWithoutDragCopiesNothing(t *testing.T) {
	m, copied := selModel(t, "sudo apt update\r\n", "sudo apt update")

	m.handleMouse(tea.MouseMsg{X: 40, Y: 6, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
	m.handleMouse(tea.MouseMsg{X: 40, Y: 6, Button: tea.MouseButtonLeft, Action: tea.MouseActionRelease})

	if copied() != "" {
		t.Fatalf("a click copied %q", copied())
	}
	if m.sel.active {
		t.Fatal("a click over blank screen left a highlight up")
	}
}

// A selection is a moment. The next key takes it down — and is not spent doing so:
// it still means whatever it means.
func TestAnyKeyClearsTheSelection(t *testing.T) {
	m, _ := selModel(t, "sudo apt update\r\n", "sudo apt update")
	for _, e := range dragEvents(0, 0, 14, 0) {
		m.handleMouse(e)
	}
	if !m.sel.active {
		t.Fatal("the drag left no selection to clear")
	}

	m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}})

	if m.sel.active {
		t.Fatal("a keystroke left the highlight up over a screen that has moved")
	}
}

// The wheel moves the screen under the highlight, so it takes it down with it.
func TestScrollClearsTheSelection(t *testing.T) {
	m, _ := selModel(t, "sudo apt update\r\n", "sudo apt update")
	for _, e := range dragEvents(0, 0, 14, 0) {
		m.handleMouse(e)
	}

	m.handleMouse(wheel(40, 6, true))

	if m.sel.active {
		t.Fatal("scrolling left a stale highlight behind")
	}
}

// A remote program that asked for the mouse keeps it, selection included: vim with
// `set mouse=a` does its own, and two selections for one drag is worse than either.
func TestRemoteMouseKeepsTheDrag(t *testing.T) {
	m, copied := selModel(t, "\x1b[?1002h\x1b[?1006hvim\r\n", "vim")

	p := m.sessions["ha"].shell().pane
	deadline := time.Now().Add(2 * time.Second)
	for !p.MouseEnabled() && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if !p.MouseEnabled() {
		t.Fatal("the pane never saw the program ask for the mouse")
	}

	for _, e := range dragEvents(0, 0, 5, 0) {
		m.handleMouse(e)
	}

	if m.sel.active {
		t.Fatal("hop selected over the top of a program that asked for the mouse")
	}
	if copied() != "" {
		t.Fatalf("hop copied %q out of a program driving its own selection", copied())
	}
}

// ctrl+g hands the pointer back to the terminal and takes it again — the escape hatch for
// the selections hop's own does not cover.
func TestToggleMouseKeyHandsThePointerOver(t *testing.T) {
	m := newMouseModel(3)
	m.mouseOn = true

	_, cmd := m.handleKey(tea.KeyMsg{Type: tea.KeyCtrlG})
	if cmd == nil {
		t.Fatal("ctrl+g sent nothing to the terminal")
	}
	if m.cfg.Mouse || m.mouseOn {
		t.Fatal("ctrl+g left hop still reporting the mouse")
	}

	_, cmd = m.handleKey(tea.KeyMsg{Type: tea.KeyCtrlG})
	if cmd == nil {
		t.Fatal("ctrl+g did not take the mouse back")
	}
	if !m.cfg.Mouse || !m.mouseOn {
		t.Fatal("ctrl+g twice did not leave the mouse where it started")
	}
}

// Switching the mouse off mid-drag ends the drag: hop will see no release, and a
// highlight nothing can finish is a lie about what is on the clipboard.
func TestMouseOffClearsALiveSelection(t *testing.T) {
	m, _ := selModel(t, "sudo apt update\r\n", "sudo apt update")
	m.mouseOn = true
	m.handleMouse(dragEvents(0, 0, 14, 0)[0])
	if !m.sel.active {
		t.Fatal("the press started no selection")
	}

	m.toggleMouse()

	if m.sel.active {
		t.Fatal("the selection outlived the pointer that was making it")
	}
}

// Not every terminal names the button that came up — the X10 encoding has no room
// to — and a drag that never ends is a highlight that never copies.
func TestReleaseWithoutAButtonStillCopies(t *testing.T) {
	m, copied := selModel(t, "sudo apt update\r\n", "sudo apt update")

	events := dragEvents(0, 0, 14, 0)
	m.handleMouse(events[0])
	m.handleMouse(events[1])
	blind := events[2]
	blind.Button = tea.MouseButtonNone
	m.handleMouse(blind)

	if copied() != "sudo apt update" {
		t.Fatalf("clipboard = %q, want the dragged text", copied())
	}
	if m.sel.dragging {
		t.Fatal("the drag outlived a release that did not name its button")
	}
}

// A drag that runs off the pane still ends when the button comes up, and follows the
// pointer to the pane edge it left by on the way — what a terminal's own selection does.
// A drag left live over the sidebar would make the next release finish a gesture nobody
// was making.
func TestDragReleasedOutsideThePaneEnds(t *testing.T) {
	m, copied := selModel(t, "sudo apt update\r\n", "sudo apt update")

	events := dragEvents(0, 0, 14, 0)
	m.handleMouse(events[0]) // press, in the pane
	m.handleMouse(events[1]) // drag across the line
	// ...and the button comes up over the sidebar.
	m.handleMouse(tea.MouseMsg{X: 4, Y: 5, Button: tea.MouseButtonLeft, Action: tea.MouseActionRelease})

	if m.sel.dragging {
		t.Fatal("a drag released outside the pane is still live")
	}
	// The release landed level with pane row 3, left of column 0: the selection runs
	// there, so the copy is the line plus the blank rows under it.
	if copied() != "sudo apt update\n\n\n" {
		t.Fatalf("clipboard = %q, want the drag to have copied down to where it ended", copied())
	}

	// And a stray release over the pane now copies nothing, rather than finishing
	// the drag that was abandoned above.
	m.clipWrite = func(text string) error {
		t.Fatalf("a release with no press behind it copied %q", text)
		return nil
	}
	m.handleMouse(tea.MouseMsg{X: 40, Y: 6, Button: tea.MouseButtonLeft, Action: tea.MouseActionRelease})
}

// ---- autoscroll: a drag held against a pane edge ----

// longScreen is enough output to push lines off the top of a pane, so there is history
// for a drag at the top edge to scroll into.
func longScreen(n int) (string, string) {
	var b strings.Builder
	for i := 0; i < n; i++ {
		fmt.Fprintf(&b, "line %02d\r\n", i)
	}
	return b.String(), fmt.Sprintf("line %02d", n-1)
}

// motion builds the event a drag in progress arrives as, in pane-content coordinates.
func motion(x, y int) tea.MouseMsg {
	const dx, dy = 33, 2
	return tea.MouseMsg{X: x + dx, Y: y + dy, Button: tea.MouseButtonLeft, Action: tea.MouseActionMotion}
}

// The bug this fixes: a drag that reaches the top row used to stop there, so a selection
// could never cover more than the screenful it started on. Now the view scrolls under
// the pointer, and the anchor travels with the text it was put on.
func TestDragAtTopEdgeScrollsIntoHistory(t *testing.T) {
	screen, marker := longScreen(60)
	m, _ := selModel(t, screen, marker)
	p := m.sessions["ha"].shell().pane

	m.handleMouse(dragEvents(0, 5, 0, 5)[0]) // press, five rows down
	_, cmd := m.handleMouse(motion(0, 0))    // and up onto the top row

	if !m.scrolling() {
		t.Fatal("the drag reached the top row without entering scrollback")
	}
	if p.ScrollOffset() != 1 {
		t.Fatalf("scroll offset = %d, want the view a line into history", p.ScrollOffset())
	}
	if m.sel.anchor.Y != 6 {
		t.Fatalf("anchor row = %d, want 6 — the anchor moves down with the text", m.sel.anchor.Y)
	}
	if cmd == nil {
		t.Fatal("no tick armed: a pointer held still sends no more motion, so the scroll would stop")
	}
	if m.sel.edge != -1 {
		t.Fatalf("sel.edge = %d, want -1 while the drag is held at the top", m.sel.edge)
	}
}

// The pointer held still sends nothing more, so the repeat is a tick — and it keeps
// scrolling until the drag ends or history runs out.
func TestDragScrollTickRepeats(t *testing.T) {
	screen, marker := longScreen(60)
	m, _ := selModel(t, screen, marker)
	p := m.sessions["ha"].shell().pane

	m.handleMouse(dragEvents(0, 5, 0, 5)[0])
	m.handleMouse(motion(0, 0))

	if cmd := m.dragScrollTick(m.dragGen); cmd == nil {
		t.Fatal("the tick did not re-arm itself")
	}
	if p.ScrollOffset() != 2 {
		t.Fatalf("scroll offset = %d, want two lines of history", p.ScrollOffset())
	}

	// A tick from a chain the pointer has since left is dropped.
	if cmd := m.dragScrollTick(m.dragGen - 1); cmd != nil {
		t.Fatal("a stale tick kept scrolling")
	}
	if p.ScrollOffset() != 2 {
		t.Fatalf("scroll offset = %d, want the stale tick to have moved nothing", p.ScrollOffset())
	}

	// And the release ends it: the next tick finds no drag behind it.
	m.handleMouse(tea.MouseMsg{X: 33, Y: 2, Button: tea.MouseButtonLeft, Action: tea.MouseActionRelease})
	if cmd := m.dragScrollTick(m.dragGen); cmd != nil {
		t.Fatal("autoscroll outlived the button that was driving it")
	}
}

// The other edge, the one the report was about: dragging down at the bottom row walks
// the view back toward the live screen while paused in history.
func TestDragAtBottomEdgeScrollsBack(t *testing.T) {
	screen, marker := longScreen(60)
	m, _ := selModel(t, screen, marker)
	p := m.sessions["ha"].shell().pane

	m.handleMouse(dragEvents(0, 5, 0, 5)[0])
	m.handleMouse(motion(0, 0)) // up into history
	m.dragScrollTick(m.dragGen)
	m.dragScrollTick(m.dragGen)
	before := p.ScrollOffset()

	// Now back down to the last row of the pane.
	if _, cmd := m.handleMouse(motion(0, m.paneH-1)); cmd == nil {
		t.Fatal("the bottom edge armed no tick")
	}
	if p.ScrollOffset() != before-1 {
		t.Fatalf("scroll offset = %d, want %d — the bottom edge walks back toward live", p.ScrollOffset(), before-1)
	}
	if m.sel.edge != 1 {
		t.Fatalf("sel.edge = %d, want +1 while the drag is held at the bottom", m.sel.edge)
	}
}

// A live screen has nothing below it, so a drag at the bottom of one scrolls nothing and
// arms no clock that would tick forever.
func TestDragAtBottomOfLiveScreenDoesNothing(t *testing.T) {
	screen, marker := longScreen(60)
	m, _ := selModel(t, screen, marker)

	m.handleMouse(dragEvents(0, 5, 0, 5)[0])
	_, cmd := m.handleMouse(motion(0, m.paneH-1))

	if m.scrolling() {
		t.Fatal("a drag at the bottom of the live screen entered scrollback")
	}
	if cmd != nil {
		t.Fatal("a tick was armed for a scroll that cannot happen")
	}
	if m.sel.edge != 0 {
		t.Fatalf("sel.edge = %d, want 0 when there is nowhere to scroll", m.sel.edge)
	}
}

// A drag that wanders over the sidebar keeps its selection: the events belong to the
// drag, not to the region the pointer is over, or the highlight would be cleared halfway
// through making it.
func TestDragOverSidebarKeepsSelection(t *testing.T) {
	m, _ := selModel(t, "sudo apt update\r\n", "sudo apt update")

	m.handleMouse(dragEvents(0, 0, 0, 0)[0])
	m.handleMouse(tea.MouseMsg{X: 4, Y: 4, Button: tea.MouseButtonLeft, Action: tea.MouseActionMotion})

	if !m.sel.active || !m.sel.dragging {
		t.Fatal("crossing the sidebar cleared the drag")
	}
	if m.sel.head.Y != 2 {
		t.Fatalf("head row = %d, want the drag clamped onto the pane row it is level with", m.sel.head.Y)
	}
}
