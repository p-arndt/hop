package tui

import (
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"hop/internal/config"
	"hop/internal/filebrowser"
	"hop/internal/filebrowser/fbtest"
	"hop/internal/sftpx"
	"hop/internal/sshx"
	"hop/internal/store"
	"hop/internal/terminal"
)

// newMouseModel builds a model in navigation mode with n hosts, a 32-column sidebar, 15
// list rows and the first host on screen row 3.
func newMouseModel(n int) *model {
	hosts := make([]store.Host, n)
	filtered := make([]int, n)
	for i := range hosts {
		hosts[i] = store.Host{Alias: "h" + string(rune('a'+i%26)), HostName: "example.test"}
		filtered[i] = i
	}
	m := &model{hosts: hosts, filtered: filtered, sessions: map[string]*session{}, connecting: map[string]bool{}, cfg: config.Default(), layout: layout{width: 100, height: 20, ready: true}}
	m.recomputeLayout()
	// applyFilter is what builds the drawn rows the click arithmetic runs backwards;
	// with an empty filter it hands back the same filtered list built above.
	m.applyFilter()
	return m
}

// click builds the event a left press at (x, y) arrives as.
func click(x, y int) tea.MouseMsg {
	return tea.MouseMsg{X: x, Y: y, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress}
}

// wheel builds a wheel event at (x, y). up picks the direction.
func wheel(x, y int, up bool) tea.MouseMsg {
	b := tea.MouseButtonWheelDown
	if up {
		b = tea.MouseButtonWheelUp
	}
	return tea.MouseMsg{X: x, Y: y, Button: b, Action: tea.MouseActionPress}
}

// The whole of hop's hit-testing: which of the four regions a cell belongs to.
func TestZoneAt(t *testing.T) {
	m := newMouseModel(3)

	cases := []struct {
		name string
		x, y int
		want zone
	}{
		{"the header row", 4, 0, zoneHeader},
		{"a host row", 4, 3, zoneList},
		{"the sidebar's last column", 31, 5, zoneList},
		{"the pane's first column", 32, 5, zonePane},
		{"the footer", 4, 19, zoneFooter},
		{"past the window", 200, 5, zoneNone},
		{"above it", 4, -1, zoneNone},
	}
	for _, c := range cases {
		if got := m.zoneAt(c.x, c.y); got != c.want {
			t.Errorf("%s: zoneAt(%d, %d) = %v, want %v", c.name, c.x, c.y, got, c.want)
		}
	}

	// Collapsed, the sidebar is not there to point at: the pane owns every column. The
	// relayout is what toggleSidebar does in production — the pointer hit-tests against
	// the frame that was last laid out, not against state nothing has drawn yet.
	m.sidebarHidden = true
	m.recomputeLayout()
	if got := m.zoneAt(4, 5); got != zonePane {
		t.Errorf("with the sidebar collapsed, zoneAt(4, 5) = %v, want zonePane", got)
	}
}

// The wheel over the list steps the selection one host at a time, and stops at both
// ends — the list does not scroll, so the wheel is ↑/↓ by another name.
func TestWheelOverList(t *testing.T) {
	m := newMouseModel(4)

	m.handleMouse(wheel(4, 5, false))
	if m.cursor != 1 {
		t.Fatalf("cursor = %d after one notch down, want 1", m.cursor)
	}
	for range 8 {
		m.handleMouse(wheel(4, 5, false))
	}
	if m.cursor != 3 {
		t.Fatalf("cursor = %d after scrolling past the end, want the last host 3", m.cursor)
	}
	for range 9 {
		m.handleMouse(wheel(4, 5, true))
	}
	if m.cursor != 0 {
		t.Fatalf("cursor = %d after scrolling back past the top, want 0", m.cursor)
	}
}

// A click stands the cursor on the host it landed on. The rows above the list are not
// hosts, and a click on them leaves the cursor where it was.
func TestClickSelectsHost(t *testing.T) {
	m := newMouseModel(6)

	m.handleMouse(click(4, 5)) // the third host row
	if m.cursor != 2 {
		t.Fatalf("cursor = %d after clicking the third row, want 2", m.cursor)
	}

	m.handleMouse(click(4, 2)) // the HOSTS heading
	if m.cursor != 2 {
		t.Fatalf("a click on the heading moved the cursor to %d", m.cursor)
	}

	// With a filter on, every row is one lower.
	m.filter = "h"
	m.applyFilter()
	m.handleMouse(click(4, 4))
	if m.cursor != 0 {
		t.Fatalf("cursor = %d after clicking the first row under a filter prompt, want 0", m.cursor)
	}
}

// A scrolled list maps rows through the same window it is drawn with: on a list
// standing on its 21st host, the top row is not the first host.
func TestClickSelectsScrolledHost(t *testing.T) {
	m := newMouseModel(30)
	m.cursor = 20 // listRows() == 15, so the window starts at 6

	if start := m.listStart(m.listRows()); start != 6 {
		t.Fatalf("listStart = %d, want 6", start)
	}
	m.handleMouse(click(4, 3))
	if m.cursor != 6 {
		t.Fatalf("cursor = %d after clicking the top row of a scrolled list, want 6", m.cursor)
	}
}

// A second click on the same host connects to it — enter, by pointing. Two clicks on
// *different* hosts are two selections, and a slow second click is not a double.
func TestDoubleClickConnects(t *testing.T) {
	t.Run("same row", func(t *testing.T) {
		m := newMouseModel(3)
		m.handleMouse(click(4, 4))
		m.handleMouse(click(4, 4))
		if !m.connecting["hb"] {
			t.Fatal("a double-click did not start a connect")
		}
	})

	t.Run("different rows", func(t *testing.T) {
		m := newMouseModel(3)
		m.handleMouse(click(4, 3))
		m.handleMouse(click(4, 4))
		if len(m.connecting) != 0 {
			t.Fatalf("clicks on two different hosts started a connect: %v", m.connecting)
		}
	})

	t.Run("outside the window", func(t *testing.T) {
		m := newMouseModel(3)
		m.handleMouse(click(4, 4))
		m.chords.click = time.Now().Add(-2 * doubleClickWindow)
		m.handleMouse(click(4, 4))
		if len(m.connecting) != 0 {
			t.Fatalf("two clicks a window apart started a connect: %v", m.connecting)
		}
	})

	t.Run("a third click is not a second double", func(t *testing.T) {
		m := newMouseModel(3)
		m.handleMouse(click(4, 4))
		if !m.clickChord(zoneList, 1) {
			t.Fatal("the second click on a host did not complete a double")
		}
		if m.clickChord(zoneList, 1) {
			t.Fatal("a third click completed a second double, want the chord spent")
		}
	})

	// The list re-scrolls around the cursor a click has just moved, so two clicks on one
	// row can be two clicks on two hosts. The chord is keyed on the host, not the row.
	t.Run("the same row, a different host", func(t *testing.T) {
		m := newMouseModel(30)
		m.cursor = 20 // the window starts at 6; the top row is host 6

		m.handleMouse(click(4, 3))
		if m.cursor != 6 {
			t.Fatalf("cursor = %d after the first click, want host 6", m.cursor)
		}
		// Selecting host 6 scrolled the window back to the top, so row 3 is now host 0.
		m.handleMouse(click(4, 3))
		if m.cursor != 0 {
			t.Fatalf("cursor = %d after the second click, want host 0", m.cursor)
		}
		if len(m.connecting) != 0 {
			t.Fatalf("two clicks on two different hosts connected to one: %v", m.connecting)
		}
	})
}

// A click in the sidebar is a click away from whatever pane holds the keyboard: it
// hands it back to the list, exactly as ctrl+o does, and keeps the session on screen.
func TestClickInSidebarLeavesThePane(t *testing.T) {
	for _, mode := range []string{"focused", "browsing", "editing"} {
		t.Run(mode, func(t *testing.T) {
			m := newMouseModel(3)
			m.active = "ha"
			m.sessions["ha"] = &session{}
			switch mode {
			case "focused":
				m.mode = modeShell
			case "browsing":
				m.mode = modeBrowser
			case "editing":
				m.mode = modeEditor
			}

			m.handleMouse(click(4, 3))

			if !m.listHasFocus() {
				t.Fatal("a click in the sidebar left the keyboard in the pane")
			}
			if m.active != "ha" {
				t.Fatalf("active = %q, want the session kept on screen", m.active)
			}
		})
	}
}

// A card takes every event while it is up, as it takes every key: one falling through
// onto the list behind it is the trap the modal ordering prevents.
func TestModalCardSwallowsMouse(t *testing.T) {
	m := newMouseModel(6)
	m.help = true

	m.handleMouse(wheel(4, 5, false))
	m.handleMouse(click(4, 6))

	if m.cursor != 0 {
		t.Fatalf("cursor = %d with the help card up, want 0", m.cursor)
	}
}

// paneLocal maps a screen cell into the pane's content box, and rejects the cells
// outside it — the borders, and the sidebar's own columns.
func TestPaneLocal(t *testing.T) {
	m := newMouseModel(3) // listWidth 32, paneW 66, paneH 16

	x, y, ok := m.paneLocal(33, 2)
	if !ok || x != 0 || y != 0 {
		t.Fatalf("paneLocal(33, 2) = %d, %d, %v; want the content origin 0, 0, true", x, y, ok)
	}
	if _, _, ok := m.paneLocal(32, 2); ok {
		t.Fatal("the pane's left border was mapped into its content")
	}
	if _, _, ok := m.paneLocal(33, 1); ok {
		t.Fatal("the pane's top border was mapped into its content")
	}
	if _, _, ok := m.paneLocal(99, 2); ok {
		t.Fatal("a cell past the pane's right edge was mapped into its content")
	}
}

// Clicking a pane the list still has the keyboard in is the way in — the pointer's
// s (a shell) or f (a browser on a session that has no shell).
func TestClickIntoPaneTakesTheKeyboard(t *testing.T) {
	m := newMouseModel(3)
	m.active = "ha"
	m.sessions["ha"] = &session{shells: []*shellTab{{id: 1, pane: fakePane()}}}

	m.handleMouse(click(40, 6))

	if !m.focused() {
		t.Fatal("a click on the pane did not focus the shell")
	}
}

// A dropped session's pane is a picture of a shell: it answers r, d and ctrl+o, and
// nothing the pointer could do would look like driving the far end.
func TestMouseOnDeadPaneDoesNothing(t *testing.T) {
	m := newMouseModel(3)
	m.active = "ha"
	m.mode = modeShell
	m.sessions["ha"] = &session{dead: true, shells: []*shellTab{{id: 1, pane: fakePane()}}}

	// Must not panic reaching for the pane behind a dead session's shell tab.
	m.handleMouse(wheel(40, 6, true))
	m.handleMouse(click(40, 6))

	if m.scrolling() {
		t.Fatal("the wheel scrolled a dropped session's frozen screen")
	}
}

// The tab strip is hop's own row: a click on a pill switches to it, measured off the
// same pills the strip is drawn from. The gaps between pills belong to neither.
func TestTabAt(t *testing.T) {
	m := newMouseModel(3)
	names := []string{"one", "two", "three"}

	// Pill widths: tabActive/tabInactive pad "1 one" etc., so walk the strip rather
	// than hard-coding columns — the point is that the mapping agrees with the render.
	pills := tabPills(names, 0)
	col := 0
	for i := range pills {
		got, ok := m.tabAt(names, 0, col, m.paneW)
		if !ok || got != i {
			t.Fatalf("tabAt at column %d = %d, %v; want tab %d", col, got, ok, i)
		}
		col += lipgloss.Width(pills[i])
		if _, ok := m.tabAt(names, 0, col, m.paneW); ok && i < len(pills)-1 {
			t.Fatalf("the gap at column %d was claimed by a tab", col)
		}
		col++ // the separating space
	}
	if _, ok := m.tabAt(names, 0, m.paneW-1, m.paneW); ok {
		t.Fatal("the empty run past the last pill was claimed by a tab")
	}
}

// A click on a shell tab switches to it, and the row below it is the shell's own
// screen rather than the strip.
func TestClickShellTab(t *testing.T) {
	m := newMouseModel(3)
	m.active = "ha"
	m.mode = modeShell
	s := &session{shells: []*shellTab{
		{id: 1, pane: fakePane()}, {id: 2, pane: fakePane()}, {id: 3, pane: fakePane()},
	}}
	m.sessions["ha"] = s

	// The strip is content row 0, which is screen row 2.
	names := shellTabNames(s)
	pills := tabPills(names, 0)
	x := lipgloss.Width(pills[0]) + 1 // the second pill's first column
	m.handleMouse(click(33+x, 2))

	if s.activeSh != 1 {
		t.Fatalf("activeSh = %d after clicking the second tab, want 1", s.activeSh)
	}
}

// A double-click in the browser opens what it landed on. The row is mapped through the
// pane's borders and the browser's own header and rule.
func TestDoubleClickInBrowserOpens(t *testing.T) {
	br, err := filebrowser.New(
		fbtest.Stub{Dir: "/srv", Entries: []sftpx.Entry{{Name: "logs", IsDir: true}, {Name: "app.conf", Size: 12}}},
		"ha", "/srv", filebrowser.Options{DownloadDir: t.TempDir()}, 40, 12)
	if err != nil {
		t.Fatalf("build browser: %v", err)
	}

	m := newMouseModel(3)
	m.active = "ha"
	m.mode = modeBrowser
	m.sessions["ha"] = &session{browser: br}

	// The browser's first entry: content row 2 (path header, rule), so screen row 4.
	m.handleMouse(click(40, 4))
	m.handleMouse(click(40, 4))

	if br.Path() != "/srv/logs" {
		t.Fatalf("browser path = %q after double-clicking a directory, want /srv/logs", br.Path())
	}
}

// A browser waiting on an answer owns the pointer as well as the keyboard. Without this
// a double-click would walk the listing out from under an open question, and the answer
// — a rename, an upload, a delete — would then be carried out in a directory the user
// never aimed at.
func TestPointerIsIgnoredWhileTheBrowserIsAsking(t *testing.T) {
	br, err := filebrowser.New(
		fbtest.Stub{Dir: "/srv", Entries: []sftpx.Entry{{Name: "logs", IsDir: true}, {Name: "app.conf", Size: 12}}},
		"ha", "/srv", filebrowser.Options{DownloadDir: t.TempDir()}, 40, 12)
	if err != nil {
		t.Fatalf("build browser: %v", err)
	}

	m := newMouseModel(3)
	m.active = "ha"
	m.mode = modeBrowser
	m.sessions["ha"] = &session{browser: br}

	// "m" opens the "new directory:" question.
	m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("m")})
	if !br.Prompting() {
		t.Fatal("m did not open a question, so this test proves nothing")
	}

	m.handleMouse(click(40, 4))
	m.handleMouse(click(40, 4))

	if br.Path() != "/srv" {
		t.Fatalf("the pointer moved the browser to %q while a question was open", br.Path())
	}
	if !br.Prompting() {
		t.Fatal("the pointer closed the open question")
	}
}

// scrollbackPane builds a pane that has history behind it: twelve lines through a
// five-row screen leaves seven in scrollback.
func scrollbackPane(t *testing.T) *terminal.Pane {
	t.Helper()
	sess := &sshx.Session{
		Stdin:  nopWriteCloser{io.Discard},
		Stdout: strings.NewReader(strings.Repeat("line\r\n", 12)),
	}
	p := terminal.New(sess, 20, 5, nil)
	deadline := time.Now().Add(2 * time.Second)
	for p.ScrollbackLen() == 0 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if p.ScrollbackLen() == 0 {
		t.Fatal("the pane never took any scrollback")
	}
	return p
}

// Nothing on the far end asked for the mouse, so the wheel over a shell is hop's: it
// pauses into history, and scrolling back to live hands the keyboard back.
func TestWheelOverShellDrivesScrollback(t *testing.T) {
	m := newMouseModel(3)
	m.active = "ha"
	m.mode = modeShell
	p := scrollbackPane(t)
	m.sessions["ha"] = &session{shells: []*shellTab{{id: 1, pane: p}}}

	// Live, there is nothing newer to scroll to: a wheel down is spent doing nothing.
	m.handleMouse(wheel(40, 6, false))
	if m.scrolling() {
		t.Fatal("a wheel down on a live shell entered scrollback")
	}

	m.handleMouse(wheel(40, 6, true))
	if !m.scrolling() {
		t.Fatal("a wheel up did not pause the shell into its history")
	}
	if p.ScrollOffset() != wheelStep {
		t.Fatalf("offset = %d after one notch, want %d", p.ScrollOffset(), wheelStep)
	}

	m.handleMouse(wheel(40, 6, false))
	if m.scrolling() {
		t.Fatal("scrolling back to the live bottom left the pane paused in history")
	}
	if p.ScrollOffset() != 0 {
		t.Fatalf("offset = %d back at the bottom, want 0", p.ScrollOffset())
	}
}

// Switching the mouse off has to reach the user's terminal, which only Bubble Tea can
// address, so it comes back as a command. An edit that did not touch the field sends
// nothing: mouseOn is what hop last asked for.
func TestApplyMouseOnlySpeaksWhenItChanges(t *testing.T) {
	m := newMouseModel(1)
	m.mouseOn = true // as Init left it, the setting being on by default

	if cmd := m.applyMouse(); cmd != nil {
		t.Fatal("applyMouse spoke to the terminal with nothing changed")
	}

	m.cfg.Mouse = false
	cmd := m.applyMouse()
	if cmd == nil {
		t.Fatal("switching the mouse off sent nothing to the terminal")
	}
	if m.mouseOn {
		t.Fatal("mouseOn still says the mouse is reported after switching it off")
	}
	if cmd := m.applyMouse(); cmd != nil {
		t.Fatal("applyMouse spoke twice for one change")
	}
}

// recordingStdin is a far end that keeps what was typed at it, so a test can read back
// the bytes a gesture put on the wire.
type recordingStdin struct {
	mu  sync.Mutex
	buf strings.Builder
}

func (r *recordingStdin) Write(p []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.buf.Write(p)
}

func (r *recordingStdin) Close() error { return nil }

func (r *recordingStdin) String() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.buf.String()
}

// A full-screen program keeps no scrollback here, so hop has nothing of its own to
// scroll: without this the wheel over less or vim did nothing at all. It is translated
// into the arrow keys the program already answers — xterm's alternate-scroll.
func TestWheelOnAltScreenSendsArrowKeys(t *testing.T) {
	m := newMouseModel(3)
	m.active = "ha"
	m.mode = modeShell

	stdin := &recordingStdin{}
	pr, pw := io.Pipe()
	p := terminal.New(&sshx.Session{Stdin: stdin, Stdout: pr}, m.paneW, m.paneH, nil)
	m.sessions["ha"] = &session{shells: []*shellTab{{id: 1, pane: p}}}

	pw.Write([]byte("\x1b[?1049h"))
	deadline := time.Now().Add(2 * time.Second)
	for !p.AltScreen() && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if !p.AltScreen() {
		t.Fatal("the pane never entered the alt screen")
	}

	m.handleMouse(wheel(40, 6, true))
	m.handleMouse(wheel(40, 6, false))

	want := strings.Repeat("\x1b[A", wheelStep) + strings.Repeat("\x1b[B", wheelStep)
	deadline = time.Now().Add(2 * time.Second)
	for stdin.String() != want && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if got := stdin.String(); got != want {
		t.Fatalf("the wheel sent %q, want %q", got, want)
	}
	if m.scrolling() {
		t.Fatal("a full-screen program's wheel notch put hop into a scrollback it does not keep")
	}
}

// treeMouseModel is newMouseModel on a window wide enough for all three columns, with an
// SFTP browser open on the active host and a shell behind it in the content area.
func treeMouseModel(t *testing.T) (*model, *session) {
	t.Helper()
	m := newMouseModel(3)
	m.width, m.height = 200, 20
	br, err := filebrowser.New(
		fbtest.Stub{Dir: "/srv", Entries: []sftpx.Entry{{Name: "logs", IsDir: true}, {Name: "app.conf", Size: 12}}},
		"ha", "/srv", filebrowser.Options{DownloadDir: t.TempDir()}, 40, 12)
	if err != nil {
		t.Fatalf("build browser: %v", err)
	}
	s := &session{browser: br, shells: []*shellTab{{id: 1, pane: fakePane()}}}
	m.sessions["ha"] = s
	m.active = "ha"
	m.relayout()
	if m.treeWidth() == 0 {
		t.Fatal("the tree column is not on screen, so nothing here is being tested")
	}
	return m, s
}

// Three columns, so three regions across the body. Each starts where the one to its left
// ends, which is the arithmetic View composes with.
func TestZoneAtWithTheTreeColumn(t *testing.T) {
	m, _ := treeMouseModel(t)
	lw, tw := m.listWidth(), m.treeWidth()

	cases := []struct {
		name string
		x    int
		want zone
	}{
		{"the sidebar's last column", lw - 1, zoneList},
		{"the tree column's first", lw, zoneTree},
		{"its last", lw + tw - 1, zoneTree},
		{"the content area's first", lw + tw, zonePane},
		{"its last", m.width - 1, zonePane},
	}
	for _, c := range cases {
		if got := m.zoneAt(c.x, 5); got != c.want {
			t.Errorf("%s: zoneAt(%d, 5) = %v, want %v", c.name, c.x, got, c.want)
		}
	}

	// Collapsed, the host list is not there to point at and the tree starts at column 0.
	m.sidebarHidden = true
	m.recomputeLayout()
	if got := m.zoneAt(0, 5); got != zoneTree {
		t.Errorf("with the sidebar collapsed, zoneAt(0, 5) = %v, want zoneTree", got)
	}
}

// The browser measures rows from its own top-left corner, so the column's border and the
// screen header have to come off a click before it is asked what was under it.
func TestTreeLocalTranslatesPerColumn(t *testing.T) {
	m, _ := treeMouseModel(t)
	lw, tw := m.listWidth(), m.treeWidth()

	x, y, ok := m.treeLocal(lw+1, 2)
	if !ok || x != 0 || y != 0 {
		t.Fatalf("treeLocal at the column's content origin = %d, %d, %v; want 0, 0, true", x, y, ok)
	}
	if _, _, ok := m.treeLocal(lw, 2); ok {
		t.Fatal("the column's left border was mapped into its content")
	}
	if _, _, ok := m.treeLocal(lw+tw-1, 2); ok {
		t.Fatal("the column's right border was mapped into its content")
	}
	if _, _, ok := m.treeLocal(lw+1, 1); ok {
		t.Fatal("the column's top border was mapped into its content")
	}
}

// Clicking a column that does not hold the keyboard is the pointer's tab and alt+t. The
// click that crosses also stands on the row it landed on, exactly as a click in the
// sidebar stands on the host it landed on.
func TestClickingAColumnFocusesIt(t *testing.T) {
	m, s := treeMouseModel(t)
	m.mode = modeShell

	// The browser's first entry: content row 2 (the path header and its rule), so screen
	// row 4, in the tree column rather than the content area.
	m.handleMouse(click(m.listWidth()+2, 4))
	if !m.browsing() {
		t.Fatal("a click in the tree column did not give it the keyboard")
	}
	if i, ok := s.browser.RowAt(2); !ok || i != 0 {
		t.Fatalf("browser row 2 = %d, %v; the listing is not where the test thinks", i, ok)
	}

	// And back the other way: a click on the content area takes the keyboard out of the
	// column without closing it.
	m.handleMouse(click(m.listWidth()+m.treeWidth()+4, 6))
	if !m.focused() {
		t.Fatal("a click on the content area did not take the keyboard out of the tree")
	}
	if s.browser == nil || m.treeWidth() == 0 {
		t.Fatal("crossing back took the tree column off the screen")
	}
}

// The wheel is not a way into a column: a notch aimed at the file being read must not move
// the keyboard out of it.
func TestWheelDoesNotFocusTheTreeColumn(t *testing.T) {
	m, _ := treeMouseModel(t)
	m.mode = modeShell

	m.handleMouse(wheel(m.listWidth()+2, 5, false))

	if m.browsing() {
		t.Fatal("a wheel notch over the tree column gave it the keyboard")
	}
}

// A double-click in the column opens what it landed on, with the row mapped through the
// column's own border rather than the content area's.
func TestDoubleClickInTheTreeColumnOpens(t *testing.T) {
	m, s := treeMouseModel(t)
	m.mode = modeBrowser

	x := m.listWidth() + 2
	m.handleMouse(click(x, 4))
	m.handleMouse(click(x, 4))

	if s.browser.Path() != "/srv/logs" {
		t.Fatalf("browser path = %q after double-clicking a directory in the column, want /srv/logs",
			s.browser.Path())
	}
}

// A click in the tree and a click on the file beside it are two clicks on two things, and
// must never pair up into a double. They are in different regions, which is what the
// chord is keyed on.
func TestClicksInTwoColumnsAreNotADouble(t *testing.T) {
	m, s := treeMouseModel(t)
	m.mode = modeBrowser

	m.handleMouse(click(m.listWidth()+2, 4))
	m.handleMouse(click(m.listWidth()+m.treeWidth()+4, 4))
	m.handleMouse(click(m.listWidth()+2, 4))

	if s.browser.Path() != "/srv" {
		t.Fatalf("browser path = %q; clicks in two columns completed a double", s.browser.Path())
	}
}

// A split content area is two boxes inside the columns the one box had. A click has to
// land in the half it was aimed at, and the half it lands in takes the keyboard.
func TestContentLocalAcrossASplit(t *testing.T) {
	m, s := treeMouseModel(t)
	s.editors = []*editorTab{
		{id: 1, name: "a.conf", path: "/etc/a.conf", pane: fakePane()},
		{id: 2, name: "b.conf", path: "/etc/b.conf", pane: fakePane()},
	}
	t.Cleanup(s.closeEditors)
	s.openSplit()
	s.splitEd = 1
	m.relayout()

	base, w := m.listWidth()+m.treeWidth(), m.splitHalf()
	cases := []struct {
		name  string
		x     int
		right bool
		lx    int
		ok    bool
	}{
		{"the left half's first column", base + 1, false, 0, true},
		{"its last", base + w, false, w - 1, true},
		{"the divider between the halves", base + w + 1, false, 0, false},
		{"the right half's first column", base + w + 3, true, 0, true},
		{"its last", base + 2*w + 2, true, w - 1, true},
	}
	for _, c := range cases {
		right, lx, _, ok := m.contentLocal(c.x, 2)
		if ok != c.ok || (ok && (right != c.right || lx != c.lx)) {
			t.Errorf("%s: contentLocal(%d, 2) = %v, %d, %v; want %v, %d, %v",
				c.name, c.x, right, lx, ok, c.right, c.lx, c.ok)
		}
	}

	// The pointer picks between the halves the way it picks between the columns.
	m.mode = modeEditor
	m.handleMouse(click(base+1, 6))
	if s.splitRight {
		t.Fatal("a click in the left half left the keyboard in the right one")
	}
	if got := s.editor(); got == nil || got.name != "a.conf" {
		t.Fatalf("the keyboard is on %v, want the left half's a.conf", got)
	}
	m.handleMouse(click(base+w+3, 6))
	if !s.splitRight {
		t.Fatal("a click in the right half did not move the keyboard there")
	}
}

// Each half draws the same tab names against a different open one, so a click on a strip
// has to be measured for the half being pointed at.
func TestClickOnASplitHalfsTabStrip(t *testing.T) {
	m, s := treeMouseModel(t)
	s.editors = []*editorTab{
		{id: 1, name: "a.conf", path: "/etc/a.conf", pane: fakePane()},
		{id: 2, name: "b.conf", path: "/etc/b.conf", pane: fakePane()},
	}
	t.Cleanup(s.closeEditors)
	s.openSplit()
	s.splitEd = 1
	m.mode = modeEditor
	m.relayout()

	base, w := m.listWidth()+m.treeWidth(), m.splitHalf()
	// The right half's strip, on its second pill. Screen row 2 is content row 0, which is
	// the strip.
	pills := tabPills(editorTabNames(s), 1)
	x := base + w + 3 + lipgloss.Width(pills[0]) + 1
	m.handleMouse(click(x, 2))

	if s.splitEd != 1 {
		t.Fatalf("splitEd = %d after clicking the right half's second pill, want 1", s.splitEd)
	}
	if s.activeEd != 0 {
		t.Fatalf("activeEd = %d; a click on one half's strip moved the other half", s.activeEd)
	}
}

// The layout keys are hop's, not the browser's, so they are resolved in handleBrowserKey —
// which puts them on the wrong side of the question the browser may be asking. A tab typed
// into a filename is a tab, not a focus change, and a ctrl+t is not a collapse. This is the
// same trap the settings "," fell into once; the gate is one early return, so one test that
// walks every new key is what keeps it that way.
func TestLayoutKeysDoNotEscapeAnOpenQuestion(t *testing.T) {
	for _, k := range []tea.KeyMsg{
		{Type: tea.KeyTab},
		{Type: tea.KeyCtrlT},
		{Type: tea.KeyRunes, Runes: []rune("\\")},
	} {
		t.Run(k.String(), func(t *testing.T) {
			br, err := filebrowser.New(
				fbtest.Stub{Dir: "/srv", Entries: []sftpx.Entry{{Name: "app.conf", Size: 12}}},
				"ha", "/srv", filebrowser.Options{DownloadDir: t.TempDir()}, 40, 12)
			if err != nil {
				t.Fatalf("build browser: %v", err)
			}

			m := newMouseModel(3)
			m.active = "ha"
			m.mode = modeBrowser
			m.sessions["ha"] = &session{browser: br}

			m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("m")})
			if !br.Prompting() {
				t.Fatal("m did not open a question, so this test proves nothing")
			}

			m.handleKey(k)

			if !br.Prompting() {
				t.Fatal("the key closed the open question")
			}
			if m.mode != modeBrowser {
				t.Fatalf("mode = %v, want the keyboard still in the question", m.mode)
			}
			if m.treeHidden {
				t.Fatal("the key collapsed the tree column from inside a question")
			}
		})
	}
}

// The pointer resolves against the box the renderer drew: a split session focused on its
// shell shows ONE full-width box, so a cell on its right is the left half, there being no
// other, measured from that box's border rather than a divider nobody drew.
func TestShellInASplitSessionIsPointedAtAsOneBox(t *testing.T) {
	m := viewModel(200, 20)
	withSplitShell(t, m)

	// Two thirds across the content area: well past where a divider would have been.
	x := m.frame.content.x + m.frame.content.w*2/3
	right, lx, _, ok := m.contentLocal(x, 5)
	if !ok {
		t.Fatalf("contentLocal(%d, 5) declined a cell inside the shell", x)
	}
	if right {
		t.Fatal("a cell in the full-width shell resolved to a right half that was never drawn")
	}
	if want := x - m.frame.content.x - 1; lx != want {
		t.Fatalf("local column = %d, want %d — measured against the wrong box", lx, want)
	}
	// Every column inside the box answers: a phantom divider is two dead columns.
	for cx := m.frame.content.x + 1; cx < m.frame.content.x+m.frame.content.w-1; cx++ {
		if _, _, _, ok := m.contentLocal(cx, 5); !ok {
			t.Fatalf("column %d of the shell declines the pointer", cx)
		}
	}
	// A drag off the edge clamps into the box its anchor was measured in.
	if cx, _ := m.clampToPane(m.width+10, 5); cx != m.frame.content.innerW()-1 {
		t.Fatalf("a drag off the edge clamps to column %d, want %d", cx, m.frame.content.innerW()-1)
	}
}

// A click in a full-width shell must not move the editors' focus to "the other half".
func TestClickInAFullWidthShellKeepsTheEditorHalf(t *testing.T) {
	m := viewModel(200, 20)
	withSplitShell(t, m)
	s := m.sessions["web1"]
	before := s.splitRight

	// A third of the way across, which is the left phantom half — the session's editors
	// are focused on the right one, so a flip is visible.
	m.handleMouse(click(m.frame.content.x+m.frame.content.w/3, 5))

	if s.splitRight != before {
		t.Fatalf("clicking the shell set splitRight to %v, want it left at %v", s.splitRight, before)
	}
}
