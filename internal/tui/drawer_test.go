package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"hop/internal/sftpx"
	"hop/internal/sshx"
)

// stubDrawer records where the panel was asked to start and lands it with a fake pane.
func stubDrawer(t *testing.T) *startedShell {
	t.Helper()
	rec := &startedShell{}
	prev := startDrawerCmd
	startDrawerCmd = func(alias, dir string, _ *sshx.Client, id, _, _ int, _ chan struct{}) tea.Cmd {
		rec.dir, rec.calls = dir, rec.calls+1
		return func() tea.Msg {
			return drawerLandedMsg{alias: alias, tab: &shellTab{id: id, pane: fakePane()}}
		}
	}
	t.Cleanup(func() { startDrawerCmd = prev })
	return rec
}

// drawerModel is web browsing /srv on a window tall enough for the panel.
func drawerModel(t *testing.T) (*model, *session) {
	t.Helper()
	m, s, _ := deadModel(t, 1, false)
	m.width, m.height = 160, 45
	s.browser = fakeBrowserWith(t, "/srv",
		sftpx.Entry{Name: "app", IsDir: true}, sftpx.Entry{Name: "notes.txt"})
	t.Cleanup(s.closeDrawer)
	m.mode = modeBrowser
	m.relayout()
	return m, s
}

func TestBacktickOpensThePanelInTheCursorDirectory(t *testing.T) {
	rec := stubDrawer(t)
	m, s := drawerModel(t)

	_, cmd := m.handleKey(key(t, "`"))
	run(t, m, cmd)

	if rec.calls != 1 || rec.dir != "/srv/app" {
		t.Fatalf("asked for %d panels starting in %q, want one in /srv/app", rec.calls, rec.dir)
	}
	if s.drawer == nil || !s.drawerOpen || m.mode != modeDrawer {
		t.Fatalf("drawer=%v open=%v mode=%v; want the panel up with the keyboard", s.drawer != nil, s.drawerOpen, m.mode)
	}
	if len(s.shells) != 1 {
		t.Fatalf("shells = %d; the panel must not become a shell tab", len(s.shells))
	}
	if m.frame.drawer.empty() || m.frame.content.h+m.frame.drawer.h != m.bodyHeight() {
		t.Fatalf("content %d + panel %d rows, want the body's %d", m.frame.content.h, m.frame.drawer.h, m.bodyHeight())
	}
}

// One key, three steps: into the panel, out of it hiding it, and back without a new shell.
func TestThePanelToggleHidesAndReturns(t *testing.T) {
	rec := stubDrawer(t)
	m, s := drawerModel(t)
	_, cmd := m.handleKey(key(t, "`"))
	run(t, m, cmd)
	first := s.drawer

	m.handleKey(ctrlO())
	m.handleKey(key(t, "j"))
	if s.drawerOpen || m.mode != modeBrowser || !m.frame.drawer.empty() {
		t.Fatalf("open=%v mode=%v; want the panel hidden and the keys back on the tree", s.drawerOpen, m.mode)
	}

	m.handleKey(key(t, "`"))
	if !s.drawerOpen || m.mode != modeDrawer || s.drawer != first || rec.calls != 1 {
		t.Fatalf("open=%v mode=%v same=%v starts=%d; want the same shell back", s.drawerOpen, m.mode, s.drawer == first, rec.calls)
	}
}

// S on a file puts a running panel in the file's directory instead of starting another.
func TestShiftSMovesTheRunningPanel(t *testing.T) {
	rec := stubDrawer(t)
	m, s := drawerModel(t)
	_, cmd := m.handleKey(key(t, "`"))
	run(t, m, cmd)
	m.focusFiles()
	s.browser.Select(1) // notes.txt

	m.handleKey(key(t, "S"))

	if rec.calls != 1 {
		t.Fatalf("started %d panels, want the running one reused", rec.calls)
	}
	if m.mode != modeDrawer {
		t.Fatalf("mode = %v, want the panel to have the keyboard", m.mode)
	}
}

// The shell view's shell keeps its size whatever the panel does.
func TestThePanelNeverResizesTheShellView(t *testing.T) {
	stubDrawer(t)
	m, s := drawerModel(t)
	before, _ := m.shellSize(len(s.shells))
	_, beforeH := m.shellSize(len(s.shells))

	_, cmd := m.handleKey(key(t, "`"))
	run(t, m, cmd)

	if w, h := m.shellSize(len(s.shells)); w != before || h != beforeH {
		t.Fatalf("the shell would be %dx%d with the panel up, want %dx%d", w, h, before, beforeH)
	}
}

// Its shell exiting takes the panel away and hands the keys back to the files.
func TestThePanelGoesWhenItsShellExits(t *testing.T) {
	stubDrawer(t)
	m, s := drawerModel(t)
	_, cmd := m.handleKey(key(t, "`"))
	run(t, m, cmd)

	m.Update(shellExitedMsg{alias: "web", id: s.drawer.id})

	if s.drawer != nil || m.mode != modeBrowser {
		t.Fatalf("drawer=%v mode=%v; want it gone and the tree in charge", s.drawer != nil, m.mode)
	}
	if len(s.shells) != 1 {
		t.Fatal("the panel's exit took a shell tab with it")
	}
}

func TestThePanelNeedsFilesToSitUnder(t *testing.T) {
	rec := stubDrawer(t)
	m, s := drawerModel(t)
	s.browser = nil
	m.mode = modeShell

	m.handleKey(ctrlO())
	m.handleKey(key(t, "j"))

	if rec.calls != 0 || m.status == "" {
		t.Fatalf("starts=%d status=%q; want a refusal that says why", rec.calls, m.status)
	}
}

// openDrawer puts the panel up on drawerModel, keys in it.
func openDrawer(t *testing.T) (*model, *session) {
	t.Helper()
	stubDrawer(t)
	m, s := drawerModel(t)
	_, cmd := m.handleKey(key(t, "`"))
	run(t, m, cmd)
	return m, s
}

func TestDraggingThePanelEdgeResizesIt(t *testing.T) {
	m, s := openDrawer(t)
	edge := m.frame.drawer.y

	m.handleMouse(mouseEvt{Mouse: tea.Mouse{X: 50, Y: edge, Button: tea.MouseLeft}, action: actPress})
	m.handleMouse(mouseEvt{Mouse: tea.Mouse{X: 50, Y: edge - 6, Button: tea.MouseLeft}, action: actMotion})
	m.handleMouse(mouseEvt{Mouse: tea.Mouse{X: 50, Y: edge - 6, Button: tea.MouseLeft}, action: actRelease})

	if got := m.frame.drawer.y; got != edge-6 {
		t.Fatalf("the edge is at row %d after dragging it up 6, want %d", got, edge-6)
	}
	if _, h := m.drawerSize(s); h != m.frame.drawer.h-3 {
		t.Fatalf("the panel's shell is %d rows in a %d-row panel", h, m.frame.drawer.h)
	}
	if m.resizingDrawer || m.sel.active {
		t.Fatal("the drag outlived its release, or selected text on the way")
	}
}

// One leader, then as many steps as it takes: sizing holds the keyboard until another key.
func TestSizingThePanelNeedsTheLeaderOnce(t *testing.T) {
	m, s := openDrawer(t)
	start := m.frame.drawer.h

	m.handleKey(ctrlO())
	m.handleKey(key(t, "+"))
	m.handleKey(key(t, "up"))
	taller := m.frame.drawer.h
	m.handleKey(key(t, "-"))
	m.handleKey(key(t, "down"))
	m.handleKey(key(t, "down"))

	if taller <= start || m.frame.drawer.h >= start {
		t.Fatalf("rows went %d → %d → %d, want taller then shorter", start, taller, m.frame.drawer.h)
	}
	if !m.sizingDrawer || !strings.Contains(m.renderFooter(), "any other key") {
		t.Fatal("sizing is not held, or the footer does not say how it ends")
	}

	m.handleKey(key(t, "x"))
	if m.sizingDrawer || m.mode != modeDrawer || s.drawer == nil {
		t.Fatal("another key did not end the sizing and go on to the panel")
	}
}

// In the tree the keys are hop's, so + and - need no leader at all.
func TestPlusAndMinusSizeThePanelFromTheTree(t *testing.T) {
	m, _ := openDrawer(t)
	m.focusFiles()
	start := m.frame.drawer.h

	m.handleKey(key(t, "+"))
	if m.frame.drawer.h <= start {
		t.Fatalf("+ in the tree left the panel at %d rows", m.frame.drawer.h)
	}
	m.handleKey(key(t, "-"))
	if m.frame.drawer.h != start {
		t.Fatalf("- in the tree left the panel at %d rows, want %d", m.frame.drawer.h, start)
	}
}

// Neither end lets the files or the panel vanish.
func TestThePanelIsHeldBetweenTheFloors(t *testing.T) {
	m, _ := openDrawer(t)

	m.resizeDrawer(1000)
	if files := m.bodyHeight() - m.frame.drawer.h; files < minFilesRows {
		t.Fatalf("the files kept %d rows, want at least %d", files, minFilesRows)
	}
	m.resizeDrawer(0)
	if m.frame.drawer.h < drawerMinRows {
		t.Fatalf("the panel shrank to %d rows, want at least %d", m.frame.drawer.h, drawerMinRows)
	}
}
