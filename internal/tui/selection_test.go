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

// selModel builds a focused shell on a pane that has printed text, with the clipboard writer stubbed.
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

	// The screen arrives on the pane's own pump; wait for it before pointing at it.
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

// dragEvents builds the three events one drag arrives as: press, motion, release.
func dragEvents(x1, y1, x2, y2 int) []tea.MouseMsg {
	// The sidebar's outer width plus the borders - the inverse of paneLocal.
	const dx, dy = 33, 2
	return []tea.MouseMsg{
		{X: x1 + dx, Y: y1 + dy, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress},
		{X: x2 + dx, Y: y2 + dy, Button: tea.MouseButtonLeft, Action: tea.MouseActionMotion},
		{X: x2 + dx, Y: y2 + dy, Button: tea.MouseButtonLeft, Action: tea.MouseActionRelease},
	}
}

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
	if !strings.Contains(m.View(), "\x1b[7m") {
		t.Fatal("the selection is not painted on the pane")
	}
}

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

// A selection is a moment: the next key takes it down, and still means whatever it means.
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

func TestScrollCarriesTheSelection(t *testing.T) {
	screen, marker := longScreen(60)
	m, _ := selModel(t, screen, marker)

	for _, e := range dragEvents(0, 4, 6, 4) {
		m.handleMouse(e)
	}
	if !m.sel.active {
		t.Fatal("the drag left no selection to carry")
	}

	m.handleMouse(wheel(40, 6, true))

	if !m.sel.active {
		t.Fatal("scrolling took the highlight down with it")
	}
	if m.sel.anchor.Y != 4+wheelStep || m.sel.head.Y != 4+wheelStep {
		t.Fatalf("selection rows = %d..%d, want both %d — it rides the text it was made on",
			m.sel.anchor.Y, m.sel.head.Y, 4+wheelStep)
	}
}

// Regression: the wheel used to clear a live drag instead of scrolling under it.
func TestWheelDuringDragExtendsTheSelection(t *testing.T) {
	screen, marker := longScreen(60)
	m, copied := selModel(t, screen, marker)
	p := m.sessions["ha"].shell().pane

	m.handleMouse(dragEvents(0, 5, 0, 5)[0]) // press, five rows down
	m.handleMouse(wheel(33, 2+5, true))      // and a notch back into history

	if !m.scrolling() {
		t.Fatal("the wheel did not pause the shell into its history")
	}
	if p.ScrollOffset() != wheelStep {
		t.Fatalf("scroll offset = %d, want %d", p.ScrollOffset(), wheelStep)
	}
	if !m.sel.dragging {
		t.Fatal("the wheel ended the drag it should have scrolled under")
	}
	if m.sel.anchor.Y != 5+wheelStep {
		t.Fatalf("anchor row = %d, want %d — the anchor moves down with the text",
			m.sel.anchor.Y, 5+wheelStep)
	}
	if m.sel.head.Y != 5 {
		t.Fatalf("head row = %d, want 5 — the pointer did not move", m.sel.head.Y)
	}

	m.handleMouse(tea.MouseMsg{X: 33 + 6, Y: 2 + 5, Button: tea.MouseButtonLeft, Action: tea.MouseActionRelease})
	if got := countLines(copied()); got != wheelStep+1 {
		t.Fatalf("copied %d lines (%q), want %d", got, copied(), wheelStep+1)
	}
}

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

// Switching the mouse off mid-drag ends the drag: no release will ever arrive.
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

// Not every terminal names the button that came up, and a drag that never ends never copies.
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

// A drag released off the pane still ends, following the pointer to the edge it left by.
func TestDragReleasedOutsideThePaneEnds(t *testing.T) {
	m, copied := selModel(t, "sudo apt update\r\n", "sudo apt update")

	events := dragEvents(0, 0, 14, 0)
	m.handleMouse(events[0])
	m.handleMouse(events[1])
	// ...and the button comes up over the sidebar.
	m.handleMouse(tea.MouseMsg{X: 4, Y: 5, Button: tea.MouseButtonLeft, Action: tea.MouseActionRelease})

	if m.sel.dragging {
		t.Fatal("a drag released outside the pane is still live")
	}
	// The release landed level with pane row 3, so the copy runs down to it.
	if copied() != "sudo apt update\n\n\n" {
		t.Fatalf("clipboard = %q, want the drag to have copied down to where it ended", copied())
	}

	// A stray release with no press behind it copies nothing.
	m.clipWrite = func(text string) error {
		t.Fatalf("a release with no press behind it copied %q", text)
		return nil
	}
	m.handleMouse(tea.MouseMsg{X: 40, Y: 6, Button: tea.MouseButtonLeft, Action: tea.MouseActionRelease})
}

// ---- autoscroll: a drag held against a pane edge ----

// longScreen is enough output to push lines off the top of a pane.
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

// Regression: a drag reaching the top row used to stop there instead of scrolling into history.
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

// A pointer held still sends no more motion, so the repeat is a tick.
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

	m.handleMouse(tea.MouseMsg{X: 33, Y: 2, Button: tea.MouseButtonLeft, Action: tea.MouseActionRelease})
	if cmd := m.dragScrollTick(m.dragGen); cmd != nil {
		t.Fatal("autoscroll outlived the button that was driving it")
	}
}

func TestDragAtBottomEdgeScrollsBack(t *testing.T) {
	screen, marker := longScreen(60)
	m, _ := selModel(t, screen, marker)
	p := m.sessions["ha"].shell().pane

	m.handleMouse(dragEvents(0, 5, 0, 5)[0])
	m.handleMouse(motion(0, 0)) // up into history
	m.dragScrollTick(m.dragGen)
	m.dragScrollTick(m.dragGen)
	before := p.ScrollOffset()

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

func TestDragToBottomLeavesScrollback(t *testing.T) {
	screen, marker := longScreen(60)
	m, _ := selModel(t, screen, marker)
	p := m.sessions["ha"].shell().pane

	m.handleMouse(dragEvents(0, 5, 0, 5)[0])
	m.handleMouse(motion(0, 0)) // up into history
	for i := 0; i < 4; i++ {
		m.dragScrollTick(m.dragGen)
	}
	if !m.scrolling() {
		t.Fatal("the drag never entered scrollback")
	}

	m.handleMouse(motion(0, m.paneH-1))
	for i := 0; i < 10 && !p.AtBottom(); i++ {
		m.dragScrollTick(m.dragGen)
	}
	if !p.AtBottom() {
		t.Fatalf("scroll offset = %d, want the view back at the live bottom", p.ScrollOffset())
	}
	if m.scrolling() {
		t.Fatal("the pane reached the live bottom but stayed in scrollback mode")
	}
	if !m.sel.dragging || !m.sel.active {
		t.Fatal("leaving scrollback took the drag down with it")
	}
}

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

func TestSelectionTallerThanThePaneCopiesEveryRow(t *testing.T) {
	screen, marker := longScreen(60)
	m, copied := selModel(t, screen, marker)

	// Press on the last row, then wheel back into history so the anchor travels past the bottom.
	m.handleMouse(dragEvents(0, m.paneH-1, 0, m.paneH-1)[0])
	m.handleMouse(wheel(33, 2+m.paneH-1, true))
	m.handleMouse(wheel(33, 2+m.paneH-1, true))
	m.handleMouse(tea.MouseMsg{X: 33, Y: 2, Button: tea.MouseButtonLeft, Action: tea.MouseActionRelease})

	want := m.paneH + 2*wheelStep
	if got := countLines(copied()); got != want {
		t.Fatalf("copied %d lines, want %d — the rows scrolled off the bottom were dropped", got, want)
	}
	if !strings.Contains(copied(), marker) {
		t.Fatalf("copied %q, want it to hold %q — the last line printed is below the window now", copied(), marker)
	}
}

// ---- the width a selection is measured at ----

// splitSelModel splits the content area between two editor tabs, keyboard right, panes wider than a half.
func splitSelModel(t *testing.T, screen, marker string) (*model, *session, func() string) {
	t.Helper()

	var mu sync.Mutex
	var got string

	m := newMouseModel(3)
	m.width, m.height = 200, 20
	m.recomputeLayout()
	m.active = "ha"
	m.mode = modeEditor
	m.clipWrite = func(text string) error {
		mu.Lock()
		defer mu.Unlock()
		got = text
		return nil
	}

	s := &session{}
	for i := 0; i < 2; i++ {
		s.editors = append(s.editors, &editorTab{
			id: i + 1, name: "f.conf", path: "/etc/f.conf",
			pane: fakePaneWith(t, m.paneW-40, 6, screen, marker),
		})
	}
	t.Cleanup(s.closeEditors)
	m.sessions["ha"] = s
	s.openSplit()

	// As openSplit's caller does in production: the halves are boxes of the frame.
	m.recomputeLayout()
	if !m.splitOn(s) {
		t.Fatal("the content area is not split, so nothing here is being tested")
	}
	if m.frame.right.empty() {
		t.Fatal("the frame has no right half, so nothing here is being tested")
	}
	return m, s, func() string {
		mu.Lock()
		defer mu.Unlock()
		return got
	}
}

// wideLine is a row longer than one half of the content area, plus a second row to end on.
func wideLine() (string, string, string) {
	line := strings.Repeat("0123456789", 12)
	return line + "\r\nhello\r\n", "hello", line
}

// Regression: a selection in a split half was measured against the whole content area's width.
func TestSelectionInASplitHalfIsMeasuredAtTheHalfWidth(t *testing.T) {
	screen, marker, line := wideLine()
	m, s, copied := splitSelModel(t, screen, marker)

	box := m.frame.half(s.focusedHalf())
	if box.innerW() != m.splitHalf() {
		t.Fatalf("the half is %d wide, want %d and not the content area's %d",
			box.innerW(), m.splitHalf(), m.paneW)
	}

	// The first row is covered to the edge of the box, the second ends the selection.
	m.startSelection(terminal.Cell{X: 0, Y: 0}, box)
	m.dragSelection(terminal.Cell{X: 4, Y: 1})
	m.endSelection(s.editor().pane.View())

	want := line[:m.splitHalf()] + "\nhello"
	if copied() != want {
		t.Fatalf("clipboard = %q, want %q — the row was read out past the half it was drawn in",
			copied(), want)
	}
}

// A shell is never one of two boxes, so its selection is the full content area even in a split session.
func TestSelectionInAShellIsMeasuredAtTheFullWidth(t *testing.T) {
	screen, marker, line := wideLine()
	m, s, copied := splitSelModel(t, screen, marker)

	// The keyboard moves to a shell; the editors and the split stay open behind it.
	s.shells = []*shellTab{{id: 1, pane: fakePaneWith(t, m.paneW, 6, screen, marker)}}
	t.Cleanup(func() { s.shells[0].pane.Close() })
	m.mode = modeShell

	// A shell is drawn in the content box entire, split session or not.
	box := m.frame.content
	if box.innerW() != m.paneW {
		t.Fatalf("the content box is %d wide, want %d: a shell is never split",
			box.innerW(), m.paneW)
	}

	m.startSelection(terminal.Cell{X: 0, Y: 0}, box)
	m.dragSelection(terminal.Cell{X: 4, Y: 1})
	m.endSelection(s.shell().pane.View())

	want := line + "\nhello"
	if copied() != want {
		t.Fatalf("clipboard = %q, want %q — the shell's row was cut off at half the content area",
			copied(), want)
	}
}

// Regression: box was a rect snapshot, so a mid-drag resize read the row out at the old width.
func TestSelectionResizedMidDragIsMeasuredAtTheNewWidth(t *testing.T) {
	screen, marker, line := wideLine()
	m, s, copied := splitSelModel(t, screen, marker)

	// The button goes down in the half the keyboard is in, at the width the window has.
	was := m.splitHalf()
	m.startSelection(terminal.Cell{X: 0, Y: 0}, m.frame.half(s.focusedHalf()))
	m.dragSelection(terminal.Cell{X: 4, Y: 1})

	// ...and the window is dragged wider before the button comes up.
	m.Update(tea.WindowSizeMsg{Width: 240, Height: 20})

	if m.splitHalf() == was {
		t.Fatalf("the resize left the half at %d columns, so nothing here is being tested", was)
	}
	if len(line) <= m.splitHalf() {
		t.Fatalf("the row is %d columns, not past the new half's %d — the two widths cannot disagree on it",
			len(line), m.splitHalf())
	}

	m.endSelection(s.editor().pane.View())

	want := line[:m.splitHalf()] + "\nhello"
	if copied() != want {
		t.Fatalf("clipboard = %q, want %q — the row was measured at the width the half had before the resize",
			copied(), want)
	}
}
