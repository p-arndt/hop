package tui

import (
	"io"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"hop/internal/config"
	"hop/internal/filebrowser"
	"hop/internal/sftpx"
	"hop/internal/sshx"
	"hop/internal/store"
	"hop/internal/terminal"
)

// newMouseModel builds a model in navigation mode with n hosts and a window where
// the sidebar is 32 columns wide, the list shows 15 rows and its first host row is
// screen row 3.
func newMouseModel(n int) *model {
	hosts := make([]store.Host, n)
	filtered := make([]int, n)
	for i := range hosts {
		hosts[i] = store.Host{Alias: "h" + string(rune('a'+i%26)), HostName: "example.test"}
		filtered[i] = i
	}
	m := &model{
		hosts:      hosts,
		filtered:   filtered,
		sessions:   map[string]*session{},
		connecting: map[string]bool{},
		width:      100,
		height:     20,
		ready:      true,
		cfg:        config.Default(),
	}
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

	// Collapsed, the sidebar is not there to point at: the pane owns every column.
	m.sidebarHidden = true
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

// A click stands the cursor on the host it landed on. The rows above the list — the
// heading, and the filter prompt when there is one — are not hosts, and a click on
// them leaves the cursor where it was.
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
		m.lastClick = time.Now().Add(-2 * doubleClickWindow)
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

	// The list re-scrolls around the cursor a click has just moved, so the row under
	// the pointer can hold a different host by the time the second click arrives. Two
	// clicks on one *row* are then two clicks on two hosts, and must connect to
	// neither: the chord is keyed on the host, not the row it was drawn on.
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
				m.focused = true
			case "browsing":
				m.browsing = true
			case "editing":
				m.editing = true
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

// A card takes every event while it is up, the same way it takes every key: an event
// that fell through onto the list behind it would be the trap the modal ordering
// exists to prevent.
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

	if !m.focused {
		t.Fatal("a click on the pane did not focus the shell")
	}
}

// A dropped session's pane is a picture of a shell: it answers r, d and ctrl+o, and
// nothing the pointer could do would look like driving the far end.
func TestMouseOnDeadPaneDoesNothing(t *testing.T) {
	m := newMouseModel(3)
	m.active = "ha"
	m.focused = true
	m.sessions["ha"] = &session{dead: true, shells: []*shellTab{{id: 1, pane: fakePane()}}}

	// Must not panic reaching for the pane behind a dead session's shell tab.
	m.handleMouse(wheel(40, 6, true))
	m.handleMouse(click(40, 6))

	if m.scrolling {
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
		got, ok := m.tabAt(names, 0, col)
		if !ok || got != i {
			t.Fatalf("tabAt at column %d = %d, %v; want tab %d", col, got, ok, i)
		}
		col += lipgloss.Width(pills[i])
		if _, ok := m.tabAt(names, 0, col); ok && i < len(pills)-1 {
			t.Fatalf("the gap at column %d was claimed by a tab", col)
		}
		col++ // the separating space
	}
	if _, ok := m.tabAt(names, 0, m.paneW-1); ok {
		t.Fatal("the empty run past the last pill was claimed by a tab")
	}
}

// A click on a shell tab switches to it, and the row below it is the shell's own
// screen rather than the strip.
func TestClickShellTab(t *testing.T) {
	m := newMouseModel(3)
	m.active = "ha"
	m.focused = true
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

// mouseSFTP is a listing to point at: enough of an SFTP connection to build a real
// browser with entries in it, unlike fakeSFTP's empty directory.
type mouseSFTP struct{ ents []sftpx.Entry }

func (mouseSFTP) Home() (string, error)                { return "/srv", nil }
func (f mouseSFTP) List(string) ([]sftpx.Entry, error) { return f.ents, nil }
func (mouseSFTP) Download(_, _ string) (int64, error)  { return 0, nil }
func (mouseSFTP) Close() error                         { return nil }

// A double-click in the browser opens what it landed on — enter, by pointing. The
// row is mapped through the pane's borders and then through the browser's own header
// and rule, so the entry that opens is the one under the pointer.
func TestDoubleClickInBrowserOpens(t *testing.T) {
	br, err := filebrowser.New(
		mouseSFTP{ents: []sftpx.Entry{{Name: "logs", IsDir: true}, {Name: "app.conf", Size: 12}}},
		"/srv", filebrowser.Options{DownloadDir: t.TempDir()}, 40, 12)
	if err != nil {
		t.Fatalf("build browser: %v", err)
	}

	m := newMouseModel(3)
	m.active = "ha"
	m.browsing = true
	m.sessions["ha"] = &session{browser: br}

	// The browser's first entry: content row 2 (path header, rule), so screen row 4.
	m.handleMouse(click(40, 4))
	m.handleMouse(click(40, 4))

	if br.Path() != "/srv/logs" {
		t.Fatalf("browser path = %q after double-clicking a directory, want /srv/logs", br.Path())
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

// Nothing on the far end asked for the mouse, so the wheel over a shell is hop's:
// it pauses into the history shift+↑ opens, and scrolling back to the live bottom
// hands the shell its keyboard back — the same exit the keys have.
func TestWheelOverShellDrivesScrollback(t *testing.T) {
	m := newMouseModel(3)
	m.active = "ha"
	m.focused = true
	p := scrollbackPane(t)
	m.sessions["ha"] = &session{shells: []*shellTab{{id: 1, pane: p}}}

	// Live, there is nothing newer to scroll to: a wheel down is spent doing nothing.
	m.handleMouse(wheel(40, 6, false))
	if m.scrolling {
		t.Fatal("a wheel down on a live shell entered scrollback")
	}

	m.handleMouse(wheel(40, 6, true))
	if !m.scrolling {
		t.Fatal("a wheel up did not pause the shell into its history")
	}
	if p.ScrollOffset() != wheelStep {
		t.Fatalf("offset = %d after one notch, want %d", p.ScrollOffset(), wheelStep)
	}

	m.handleMouse(wheel(40, 6, false))
	if m.scrolling {
		t.Fatal("scrolling back to the live bottom left the pane paused in history")
	}
	if p.ScrollOffset() != 0 {
		t.Fatalf("offset = %d back at the bottom, want 0", p.ScrollOffset())
	}
}

// Switching the mouse off has to reach the *user's* terminal, which only Bubble Tea
// can address — so the setting comes back as a command. A settings edit that did not
// touch the field sends nothing: mouseOn is what hop last asked the terminal for.
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
