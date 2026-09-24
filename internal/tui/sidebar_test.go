package tui

import (
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"hop/internal/filebrowser"
	"hop/internal/filebrowser/fbtest"
	"hop/internal/sftpx"
	"hop/internal/sshx"
	"hop/internal/store"
)

// ---- the two columns ----

// With the window wide enough the sidebar is a column beside the shell, whatever has the
// keyboard.
func TestTheSidebarIsAColumnBesideTheShell(t *testing.T) {
	m, _ := statusModel(t, 120, 34)
	for _, mode := range []paneMode{modeShell, modeList} {
		m.mode = mode
		m.relayout()
		if m.frame.list.w != sidebarWidth || m.frame.floats || m.frame.content.x != sidebarWidth {
			t.Fatalf("in %s: list %+v, content at %d; want a docked %d-column sidebar",
				modeName(mode), m.frame.list, m.frame.content.x, sidebarWidth)
		}
		if !strings.Contains(ansi.Strip(m.View().Content), "HOSTS") {
			t.Fatalf("in %s the sidebar is not drawn", modeName(mode))
		}
	}
}

// The shell's size is the window's business alone: moving the keyboard into the sidebar and
// back never reflows a remote program, docked or floating.
func TestTheShellKeepsItsSizeWhereverTheKeyboardIs(t *testing.T) {
	for _, w := range []int{160, 120, dockThreshold, dockThreshold - 1, 88, 60} {
		m, s := statusModel(t, w, 34)
		m.mode = modeShell
		m.relayout()
		sw, sh := m.shellSize()
		content := m.frame.content

		m.toSidebar()
		if gw, gh := m.shellSize(); gw != sw || gh != sh {
			t.Fatalf("at %d columns the sidebar resized the shell to %dx%d, want %dx%d", w, gw, gh, sw, sh)
		}
		if m.frame.content != content {
			t.Fatalf("at %d columns the sidebar moved the shell's box from %+v to %+v", w, content, m.frame.content)
		}
		if m.front() != tabShell || s.shell() == nil {
			t.Fatalf("at %d columns the shell left the content area for the sidebar", w)
		}
		m.backFromSidebar()
		if gw, gh := m.shellSize(); gw != sw || gh != sh {
			t.Fatalf("at %d columns esc resized the shell to %dx%d, want %dx%d", w, gw, gh, sw, sh)
		}
	}
}

// Docked, the shell is the window less the sidebar; floating, the whole window.
func TestTheShellIsTheWindowLessTheDockedSidebar(t *testing.T) {
	for _, c := range []struct{ w, want int }{
		{160, 160 - sidebarWidth - 2},
		{dockThreshold, dockThreshold - sidebarWidth - 2},
		{dockThreshold - 1, dockThreshold - 1 - 2},
	} {
		m, _ := statusModel(t, c.w, 34)
		m.mode = modeShell
		m.relayout()
		if w, h := m.shellSize(); w != c.want || h != m.bodyHeight()-2 {
			t.Fatalf("at %d columns the shell is %dx%d, want %dx%d", c.w, w, h, c.want, m.bodyHeight()-2)
		}
	}
}

// In a narrow window the sidebar is not on screen until esc esc, and then floats over the
// shell rather than resizing it.
func TestANarrowWindowFloatsTheSidebarOverTheShell(t *testing.T) {
	m, _ := statusModel(t, 88, 34)
	m.mode = modeShell
	m.relayout()
	if !m.frame.list.empty() || strings.Contains(ansi.Strip(m.View().Content), "HOSTS") {
		t.Fatal("a narrow window shows the sidebar while the keyboard is in the shell")
	}
	w, h := m.shellSize()

	press(t, m, "esc", "esc")

	if m.mode != modeList || !m.frame.floats || m.frame.list.empty() {
		t.Fatalf("mode %s, floats %v; want the keyboard in a floating sidebar", modeName(m.mode), m.frame.floats)
	}
	if gw, gh := m.shellSize(); gw != w || gh != h || m.frame.content.x != 0 {
		t.Fatalf("the floating sidebar resized the shell to %dx%d at %d, want %dx%d at 0", gw, gh, m.frame.content.x, w, h)
	}
	if !strings.Contains(ansi.Strip(m.View().Content), "HOSTS") {
		t.Fatal("the floating sidebar is not drawn")
	}
	if m.zoneAt(2, 3) != zoneList {
		t.Fatal("the pointer does not find the floating sidebar")
	}
}

// ---- the tree box ----

// An editor tab puts the tree under the hosts, in the sidebar, and the file in the content.
func TestAnEditorTabPutsTheTreeUnderTheHosts(t *testing.T) {
	m, s := statusModel(t, 160, 45)
	s.browser = fakeBrowser(t, "/srv")
	s.editors = []*editorTab{{id: 1, name: "a", path: "/srv/a", pane: fakePane()}}
	m.mode = modeEditor
	m.Update(nil)

	tr, hosts := m.frame.tree, m.frame.list
	if tr.empty() || tr.x != 0 || tr.w != sidebarWidth || tr.y != hosts.h || tr.y+tr.h != m.bodyHeight() {
		t.Fatalf("tree box %+v under hosts box %+v; want it filling the sidebar below the hosts", tr, hosts)
	}
	if ew, _ := m.editorSize(s); ew != 160-sidebarWidth-2 || m.paneW != ew {
		t.Fatalf("editor width = %d, paneW = %d, want %d", ew, m.paneW, 160-sidebarWidth-2)
	}
	if bw, bh := m.browserSize(s); bw != sidebarWidth-2 || bh != tr.h-2 {
		t.Fatalf("browser is %dx%d, want the tree box's interior %dx%d", bw, bh, sidebarWidth-2, tr.h-2)
	}
	if strings.Count(m.View().Content, "╭") != 3 {
		t.Fatalf("want three boxes: hosts, tree and the file:\n%s", ansi.Strip(m.View().Content))
	}
}

// The hosts box takes what its rows need up to its cap, so the tree keeps a usable height.
func TestTheHostsBoxIsCappedAboveTheTree(t *testing.T) {
	m, s := statusModel(t, 160, 45)
	for i := range 40 {
		m.hosts = append(m.hosts, store.Host{Alias: "extra" + string(rune('a'+i%26)) + string(rune('a'+i/26))})
	}
	m.applyFilter()
	s.browser = fakeBrowser(t, "/srv")
	m.mode = modeBrowser
	m.Update(nil)

	if got, max := m.frame.list.h, m.bodyHeight()*hostsBoxPct/100; got > max {
		t.Fatalf("hosts box is %d rows, want at most %d", got, max)
	}
	if m.frame.tree.h < treeBoxMin {
		t.Fatalf("tree box is %d rows, want at least %d", m.frame.tree.h, treeBoxMin)
	}

	// With few hosts it shrinks to them and the tree gets the rest.
	few, fs := statusModel(t, 160, 45)
	fs.browser = fakeBrowser(t, "/srv")
	few.mode = modeBrowser
	few.Update(nil)
	if want := len(few.rows) + few.sidebarChrome() + 2; few.frame.list.h != max(want, hostsBoxMin) {
		t.Fatalf("hosts box is %d rows for %d rows, want %d", few.frame.list.h, len(few.rows), want)
	}
}

// ctrl+o t hides the tree box, and the file keeps its size: the tree was never beside it.
func TestHidingTheTreeBoxLeavesTheFileItsSize(t *testing.T) {
	m, s := statusModel(t, 200, 34)
	s.browser = fakeBrowser(t, "/srv")
	s.editors = []*editorTab{{id: 1, name: "a", path: "/srv/a", pane: fakePane()}}
	m.mode = modeEditor
	m.Update(nil)
	ew, eh := m.editorSize(s)

	press(t, m, "ctrl+o", "t") // into the tree
	if !m.browsing() || !m.treeBoxOn() {
		t.Fatal("ctrl+o t from the file did not put the keyboard in the tree box")
	}
	press(t, m, "ctrl+o", "t") // hides it
	if m.treeBoxOn() || !m.frame.tree.empty() || !m.editing() {
		t.Fatal("ctrl+o t from the tree did not hide the tree box and hand the file the keyboard")
	}
	if m.frame.list.h != m.bodyHeight() {
		t.Fatalf("hosts box is %d rows with the tree hidden, want the body's %d", m.frame.list.h, m.bodyHeight())
	}
	if gw, gh := m.editorSize(s); gw != ew || gh != eh {
		t.Fatalf("hiding the tree resized the file to %dx%d, want %dx%d", gw, gh, ew, eh)
	}
	press(t, m, "ctrl+o", "t") // back, with the keyboard in it
	if !m.treeBoxOn() || !m.browsing() {
		t.Fatal("ctrl+o t with the tree hidden did not bring it back with the keyboard")
	}
}

// The files tab gives the content area to a preview of the file under the tree's cursor.
func TestTheFilesTabPreviewsTheFileUnderTheCursor(t *testing.T) {
	m, s := statusModel(t, 160, 45)
	br, err := filebrowser.New(fbtest.Stub{
		Dir:     "/srv",
		Entries: []sftpx.Entry{{Name: "app.conf", Size: 22}},
		Files:   map[string]string{"/srv/app.conf": "listen 8080\nworkers 4\n"},
	}, "web1", "/srv", filebrowser.Options{DownloadDir: t.TempDir()}, 40, 12)
	if err != nil {
		t.Fatalf("build browser: %v", err)
	}
	s.browser = br
	m.mode = modeBrowser
	_, cmd := m.Update(nil)

	if m.front() != tabFiles || !m.previewOn() || m.browserInContent(s) {
		t.Fatalf("front %v, preview %v; want the tree in the sidebar and a preview beside it", m.front(), m.previewOn())
	}
	if cmd == nil {
		t.Fatal("no preview was asked for the file under the cursor")
	}
	runCmd(m, cmd)

	screen := ansi.Strip(m.View().Content)
	for _, want := range []string{"preview", "app.conf", "listen 8080", "workers 4"} {
		if !strings.Contains(screen, want) {
			t.Fatalf("the preview does not show %q:\n%s", want, screen)
		}
	}
}

// The files tab remembers it is the files tab, even with an editor open.
func TestTheFilesTabIsNotTheEditorsTreeBox(t *testing.T) {
	m, s := statusModel(t, 160, 45)
	s.browser = fakeBrowser(t, "/srv")
	s.editors = []*editorTab{{id: 1, name: "a", path: "/srv/a", pane: fakePane()}}
	m.mode = modeEditor
	m.syncView()

	m.focusTree()
	m.syncView()
	if m.front() != tabEditor {
		t.Fatalf("front = %v, want the tree to be the editor tab's tree box", m.front())
	}

	m.jumpTo(target{alias: "web1", kind: targetBrowser})
	m.syncView()
	if m.front() != tabFiles || !m.previewOn() {
		t.Fatalf("front = %v, want the files tab with its preview", m.front())
	}
}

// With no docked sidebar the files tab draws the tree in the content area, and the editor
// tab is the file alone.
func TestANarrowWindowPutsTheTreeInTheContent(t *testing.T) {
	m, s := statusModel(t, dockThreshold-1, 34)
	s.browser = fakeBrowser(t, "/srv")
	s.editors = []*editorTab{{id: 1, name: "a", path: "/srv/a", pane: fakePane()}}
	m.mode = modeBrowser
	m.relayout()

	if m.treeBoxOn() || !m.browserInContent(s) || m.previewOn() {
		t.Fatalf("tree box %v at %d columns, want the tree across the files tab", m.treeBoxOn(), m.width)
	}
	if bw, _ := m.browserSize(s); bw != m.width-2 {
		t.Fatalf("browser width = %d, want the content area's %d", bw, m.width-2)
	}
	m.mode = modeEditor
	m.relayout()
	if m.treeBoxOn() || m.paneW != m.width-2 {
		t.Fatalf("tree box %v, paneW = %d; want the file across the editor tab", m.treeBoxOn(), m.paneW)
	}
}

// Both halves of a split are the same width; the odd column is given up.
func TestSplitHalvesTheFiles(t *testing.T) {
	m, s := statusModel(t, 200, 34)
	s.browser = fakeBrowser(t, "/srv")
	s.editors = []*editorTab{{id: 1, pane: fakePane()}, {id: 2, pane: fakePane()}}
	m.mode = modeEditor
	m.relayout()
	full, _ := m.editorSize(s)
	if full != m.paneW {
		t.Fatalf("an unsplit editor tab gives an editor %d columns, want the whole %d", full, m.paneW)
	}

	s.openSplit()
	half, _ := m.editorSize(s)
	if want := splitHalfOf(full); half != want {
		t.Fatalf("a split gives an editor %d columns, want %d", half, want)
	}
	if 2*(half+2) > full+2 {
		t.Fatalf("two %d-wide halves do not fit %d columns", half+2, full+2)
	}
}

func TestSplitRefusesANarrowFilesView(t *testing.T) {
	if !splitFitsIn(2*minSplitHalf - 2) {
		t.Fatal("exactly two halves' worth does not fit")
	}
	if splitFitsIn(2*minSplitHalf - 3) {
		t.Fatal("one column short still claims to fit two halves")
	}
}

// ctrl+b is no longer hop's: a shell pane hands it to the remote, where tmux wants it.
func TestCtrlBReachesTheRemote(t *testing.T) {
	m := newPaneModel()

	m.handleKey(tea.KeyPressMsg{Code: 'b', Mod: tea.ModCtrl})

	if !m.focused() {
		t.Fatal("ctrl+b did something in hop")
	}
}

// ---- the rows ----

// sidebarLines is the hosts box's rows as drawn, colour stripped, one per row of m.rows on
// screen.
func sidebarLines(m *model) []string {
	m.recomputeLayout()
	screen := frameOf(m)
	var out []string
	first := m.listFirstRow()
	for y := first; y < first+m.listRows() && y < len(screen); y++ {
		r := screen[y]
		out = append(out, string(r[m.frame.list.x:m.frame.list.x+m.frame.list.w]))
	}
	return out
}

// The host in front opens out into its tabs, in the order they were opened; the other open
// hosts are folded up with a count, and a host with nothing open is dim with a hollow dot.
func TestTheHostInFrontOpensOutIntoItsTabs(t *testing.T) {
	m, _ := placesModel(t)
	m.width, m.height = 160, 40
	m.Update(nil)

	lines := sidebarLines(m)
	want := []string{"▾ ha", "$ shell 1", "▤ files", "✎ a.conf", "✎ nginx.conf", "▸ hb", "hc"}
	for i, w := range want {
		if i >= len(lines) || !strings.Contains(lines[i], w) {
			t.Fatalf("row %d = %q, want %q; rows:\n%s", i, at(lines, i), w, strings.Join(lines, "\n"))
		}
	}
	if !strings.Contains(lines[5], "● 1") {
		t.Fatalf("the folded host does not say how many tabs it holds: %q", lines[5])
	}
	if !strings.Contains(lines[6], "○") {
		t.Fatalf("the host with nothing open has no hollow dot: %q", lines[6])
	}
}

func at(lines []string, i int) string {
	if i < len(lines) {
		return lines[i]
	}
	return ""
}

// Tabs keep the order they were opened in, whatever their kind.
func TestTheSidebarKeepsTheOrderOfOpening(t *testing.T) {
	m, s := placesModel(t)
	m.width, m.height = 160, 40
	s.noteOpened(target{alias: "ha", kind: targetEditor, id: 12})
	s.noteOpened(target{alias: "ha", kind: targetShell, id: 1})
	s.noteOpened(target{alias: "ha", kind: targetBrowser})
	m.Update(nil)

	lines := sidebarLines(m)
	for i, w := range []string{"✎ nginx.conf", "$ shell 1", "▤ files", "✎ a.conf"} {
		if !strings.Contains(at(lines, i+1), w) {
			t.Fatalf("tab %d = %q, want %q", i+1, at(lines, i+1), w)
		}
	}
}

// The tab the content area shows carries the marker while the keyboard is in it; in the
// sidebar the one marker is the cursor, which starts on that same tab.
func TestTheTabInFrontIsMarked(t *testing.T) {
	m, s := placesModel(t)
	m.width, m.height = 160, 40
	s.focusTab(1)
	m.mode = modeEditor
	m.Update(nil)

	marked := func() []string {
		var out []string
		for _, l := range sidebarLines(m) {
			if strings.HasPrefix(l, "│▌") {
				out = append(out, l)
			}
		}
		return out
	}
	for _, where := range []string{"the editor", "the sidebar"} {
		if where == "the sidebar" {
			m.toSidebar()
		}
		got := marked()
		if len(got) != 1 || !strings.Contains(got[0], "nginx.conf") {
			t.Fatalf("keyboard in %s: marked rows %q, want nginx.conf alone", where, got)
		}
	}
	press(t, m, "up")
	if got := marked(); len(got) != 1 || !strings.Contains(got[0], "a.conf") {
		t.Fatalf("after ↑ the marked rows are %q, want the cursor's alone on a.conf", got)
	}
}

// A host's tunnels are a line under its tabs.
func TestTheTunnelsAreALineUnderTheTabs(t *testing.T) {
	m, s := placesModel(t)
	m.width, m.height = 160, 40
	s.tunnels = map[int64]*sshx.Tunnel{1: {}, 2: {}}
	m.Update(nil)

	if !slices.ContainsFunc(sidebarLines(m), func(l string) bool { return strings.Contains(l, "⇄ 2 tunnels") }) {
		t.Fatalf("no tunnels line:\n%s", strings.Join(sidebarLines(m), "\n"))
	}
}

// A dropped host in front keeps its tabs on show, so it is plain what reconnecting brings back.
func TestADroppedHostKeepsItsTabsOnShow(t *testing.T) {
	m, s := placesModel(t)
	m.width, m.height = 160, 40
	s.dead = true
	m.Update(nil)

	if !strings.Contains(strings.Join(sidebarLines(m), "\n"), "$ shell 1") {
		t.Fatal("the dropped host's tabs are gone from the sidebar")
	}
}

// One row model feeds the drawing and the pointer: the row a click lands on is the row drawn
// there, scrolled or not, filtered or not.
func TestSidebarRowsAreWhereTheyAreClicked(t *testing.T) {
	for _, h := range []int{40, 12} {
		for _, filter := range []string{"", "h"} {
			m, _ := placesModel(t)
			m.width, m.height = 160, h
			m.filter = filter
			m.applyFilter()
			m.Update(nil)
			m.toSidebar()

			first := m.listFirstRow()
			for y, line := range sidebarLines(m) {
				r, ok := m.listRowAt(first + y)
				if !ok {
					continue
				}
				want := stripLabel(m, r)
				if !strings.Contains(line, want) {
					t.Fatalf("h=%d filter=%q: row at %d is %+v (%q) but %q is drawn there", h, filter, first+y, r, want, line)
				}
			}
		}
	}
}

// stripLabel is how the sidebar names r, uncoloured.
func stripLabel(m *model, r listRow) string {
	if r.tab.alias != "" {
		if r.tab.kind == targetTunnels {
			return "⇄"
		}
		return m.tabLabel(r.tab)
	}
	return m.hosts[m.filtered[r.fi]].Alias
}

func TestClickingATabRowJumpsThere(t *testing.T) {
	m, s := placesModel(t)
	m.width, m.height = 160, 40
	m.Update(nil)
	m.handleMouse(click(4, rowY(t, m, target{alias: "ha", kind: targetEditor, id: 12})))

	if m.active != "ha" || m.mode != modeEditor || s.editor().id != 12 {
		t.Fatalf("active = %q, mode = %v; want ha's nginx.conf", m.active, m.mode)
	}
}

func TestClickingTheFilesRowShowsTheFilesTab(t *testing.T) {
	m, _ := placesModel(t)
	m.width, m.height = 160, 40
	m.Update(nil)
	m.handleMouse(click(4, rowY(t, m, target{alias: "ha", kind: targetBrowser})))

	if m.mode != modeBrowser || m.front() != tabFiles {
		t.Fatalf("mode = %v, front = %v; want the files tab", m.mode, m.front())
	}
}

func TestClickingAnOpenHostGoesToItsLastPlace(t *testing.T) {
	m, _ := placesModel(t)
	m.width, m.height = 160, 40
	m.Update(nil)
	m.handleMouse(click(4, rowY(t, m, target{alias: "hb"})))

	if m.active != "hb" || m.mode != modeShell {
		t.Fatalf("active = %q, mode = %v; want hb's shell", m.active, m.mode)
	}
}

// Clicking a dropped host shows it as it is; nothing dials until enter.
func TestClickingADroppedHostDoesNotReconnect(t *testing.T) {
	m, _ := placesModel(t)
	m.width, m.height = 160, 40
	m.sessions["hb"].dead = true
	m.Update(nil)

	_, cmd := m.handleMouse(click(4, rowY(t, m, target{alias: "hb"})))

	if cmd != nil || m.connecting["hb"] {
		t.Fatal("clicking a dropped host dialled it")
	}
	if m.active != "hb" || !m.activeDead() {
		t.Fatalf("active = %q; want the dropped hb in front", m.active)
	}
}

// rowY is the screen row the sidebar draws t on: a tab, or with kind targetHost the host.
func rowY(t *testing.T, m *model, want target) int {
	t.Helper()
	m.recomputeLayout()
	for i, r := range m.rows {
		if r.heading != "" {
			continue
		}
		got := r.tab
		if got.alias == "" {
			got = target{alias: m.hosts[m.filtered[r.fi]].Alias}
		}
		if got == want {
			return m.listFirstRow() + i - m.listStart(m.listRows())
		}
	}
	t.Fatalf("no sidebar row for %+v", want)
	return 0
}

// ---- the keyboard ----

// esc esc from a pane puts the keyboard in the sidebar on the tab it was in; esc gives it
// back with nothing changed.
func TestEscEscGoesToTheSidebarAndEscComesBack(t *testing.T) {
	for _, from := range []struct {
		name string
		set  func(m *model, s *session)
		mode paneMode
	}{
		{"shell", func(m *model, _ *session) { m.jumpTo(target{alias: "ha", kind: targetShell, id: 1}) }, modeShell},
		{"editor", func(m *model, _ *session) { m.jumpTo(target{alias: "ha", kind: targetEditor, id: 12}) }, modeEditor},
		{"tree", func(m *model, _ *session) { m.jumpTo(target{alias: "ha", kind: targetBrowser}) }, modeBrowser},
	} {
		t.Run(from.name, func(t *testing.T) {
			m, _ := placesModel(t)
			m.width, m.height = 160, 40
			from.set(m, m.sessions["ha"])
			m.Update(nil)
			here, _ := m.frontTarget()

			press(t, m, "esc", "esc")
			if m.mode != modeList {
				t.Fatalf("esc esc left the keyboard in %s", modeName(m.mode))
			}
			if m.cursorTab != here {
				t.Fatalf("the cursor is on %+v, want the tab the keyboard was in, %+v", m.cursorTab, here)
			}
			if m.active != "ha" || m.front() != modeTab(from.mode) {
				t.Fatalf("the content area changed: active %q, front %v", m.active, m.front())
			}

			m.escGuard = m.escGuard.AddDate(-1, 0, 0) // as if the user paused
			press(t, m, "esc")
			if m.mode != from.mode {
				t.Fatalf("esc gave the keyboard to %s, want %s", modeName(m.mode), modeName(from.mode))
			}
		})
	}
}

// modeTab is the tab kind a mode's keys drive.
func modeTab(mode paneMode) tabKind {
	switch mode {
	case modeEditor:
		return tabEditor
	case modeBrowser:
		return tabFiles
	}
	return tabShell
}

// esc esc in the sidebar is esc: the keyboard goes back, and the second esc does not reach
// the pane to start a double esc of its own there.
func TestEscEscInTheSidebarIsEsc(t *testing.T) {
	m := switchModel(t)
	press(t, m, "esc", "esc")
	if m.mode != modeList {
		t.Fatal("esc esc did not reach the sidebar")
	}

	press(t, m, "esc", "esc")

	if m.mode != modeShell {
		t.Fatalf("esc esc in the sidebar left the keyboard in %s, want the shell", modeName(m.mode))
	}
	if m.reader.Pending() {
		t.Fatal("the second esc reached the shell and armed a double esc there")
	}
}

// ctrl+o o is esc esc behind the leader.
func TestLeaderOIsTheSidebar(t *testing.T) {
	m := switchModel(t)
	press(t, m, "ctrl+o", "o")
	if m.mode != modeList || m.cursorTab != (target{alias: "ha", kind: targetShell, id: 1}) {
		t.Fatalf("mode %s, cursor on %+v; want the sidebar on ha's shell", modeName(m.mode), m.cursorTab)
	}
}

// isQuit reports whether cmd ends the program.
func isQuit(cmd tea.Cmd) bool {
	if cmd == nil {
		return false
	}
	_, ok := cmd().(tea.QuitMsg)
	return ok
}

// esc never quits hop, from anywhere, however many times it is pressed.
func TestEscNeverQuits(t *testing.T) {
	builds := map[string]func() *model{
		"the sidebar, no host in front": func() *model {
			m, _ := statusModel(t, 120, 34)
			m.active, m.mode = "", modeList
			return m
		},
		"the sidebar, a host in front": func() *model {
			m, _ := statusModel(t, 120, 34)
			m.mode = modeList
			return m
		},
		"a shell": func() *model {
			m, _ := statusModel(t, 120, 34)
			m.mode = modeShell
			return m
		},
		"a narrow window": func() *model {
			m, _ := statusModel(t, 60, 20)
			m.mode = modeShell
			return m
		},
	}
	for name, build := range builds {
		t.Run(name, func(t *testing.T) {
			m := build()
			for i := range 6 {
				_, cmd := m.Update(key(t, "esc"))
				if isQuit(cmd) {
					t.Fatalf("esc number %d quit hop", i+1)
				}
			}
		})
	}
}

// q and ctrl+c still quit from the sidebar.
func TestQQuitsFromTheSidebar(t *testing.T) {
	for _, k := range []string{"q", "ctrl+c"} {
		m, _ := statusModel(t, 120, 34)
		m.active, m.mode = "", modeList
		if _, cmd := m.handleKey(key(t, k)); !isQuit(cmd) {
			t.Fatalf("%s in the sidebar did not quit", k)
		}
	}
}

// ↑↓ walk the hosts and the tabs of the opened-out host alike; enter lands on the row.
func TestArrowsWalkHostsAndTabs(t *testing.T) {
	m, s := placesModel(t)
	m.width, m.height = 160, 40
	m.Update(nil)
	press(t, m, "esc", "esc") // on ha's shell

	press(t, m, "down", "down", "down")
	if m.cursorTab != (target{alias: "ha", kind: targetEditor, id: 12}) {
		t.Fatalf("three down from the shell is %+v, want nginx.conf", m.cursorTab)
	}
	press(t, m, "enter")
	if m.mode != modeEditor || s.editor().id != 12 {
		t.Fatalf("enter on a tab row gave mode %s on %v, want nginx.conf", modeName(m.mode), s.editor())
	}

	press(t, m, "esc", "esc", "down")
	if h, _ := m.selectedHost(); h.Alias != "hb" || m.cursorTab.alias != "" {
		t.Fatalf("one down from the last tab is %q/%+v, want the host hb", h.Alias, m.cursorTab)
	}
	press(t, m, "enter")
	if m.active != "hb" || m.mode != modeShell {
		t.Fatalf("enter on hb gave %q in %s, want hb's shell", m.active, modeName(m.mode))
	}
}

// → and l go there as enter does, so the old sidebar's arrow-right hop still works.
func TestRightGoesThereLikeEnter(t *testing.T) {
	for _, k := range []string{"right", "l"} {
		t.Run(k, func(t *testing.T) {
			m, _ := placesModel(t)
			m.cfg.VimKeys = true
			m.width, m.height = 160, 40
			m.Update(nil)
			press(t, m, "esc", "esc")
			m.selectPlace(target{alias: "hb"})

			press(t, m, k)
			if m.active != "hb" || m.mode != modeShell {
				t.Fatalf("%s on hb gave %q in %s, want hb's shell", k, m.active, modeName(m.mode))
			}
		})
	}
}

// ← steps from a tab up to its host, then folds the host up.
func TestLeftFoldsAHostUp(t *testing.T) {
	m, _ := placesModel(t)
	m.width, m.height = 160, 40
	m.Update(nil)
	press(t, m, "esc", "esc") // on ha's shell

	press(t, m, "left")
	if m.cursorTab.alias != "" {
		t.Fatal("← on a tab did not go up to its host")
	}
	press(t, m, "left")
	if m.expanded("ha") {
		t.Fatal("← on an open host did not fold it up")
	}
}

// Every host key still works on the sidebar's host rows.
func TestHostKeysWorkInTheSidebar(t *testing.T) {
	m, _ := statusModel(t, 120, 34)
	m.mode = modeShell
	m.Update(nil)
	press(t, m, "esc", "esc")
	m.selectPlace(target{alias: "raspberrypi"})

	press(t, m, "e")
	if !m.hostForm.open {
		t.Fatal("e in the sidebar did not open the host form")
	}
}

// ---- no host in front ----

// With no host in front the content area offers the recent places, above the details, and
// ↑ from the first host reaches them.
func TestNoHostInFrontOffersTheRecentPlaces(t *testing.T) {
	m := switchModel(t)
	m.width, m.height = 160, 40
	press(t, m, "ctrl+o", "tab") // to hb, so both hosts have been used
	m.leaveAll()
	m.Update(nil)

	screen := ansi.Strip(m.View().Content)
	if !strings.Contains(screen, "RECENT") || m.front() != tabNone {
		t.Fatalf("no recent places with no host in front:\n%s", screen)
	}
	if m.frame.content.empty() || m.frame.list.empty() {
		t.Fatal("want the sidebar beside the content area")
	}

	m.selectPlace(target{alias: "ha"})
	press(t, m, "up")
	if m.recentAt != len(m.recent) {
		t.Fatalf("↑ from the first host is recent %d, want the last recent place %d", m.recentAt, len(m.recent))
	}
	want := m.recent[m.recentAt-1]
	press(t, m, "enter")
	if m.active != want.alias || !m.inPane() {
		t.Fatalf("enter on a recent place went to %q in %s, want %+v", m.active, modeName(m.mode), want)
	}
}

// A click on a recent place lands there.
func TestClickingARecentPlaceLandsThere(t *testing.T) {
	m := switchModel(t)
	m.width, m.height = 160, 40
	m.leaveAll()
	m.Update(nil)
	if len(m.recent) == 0 {
		t.Fatal("no recent places to click")
	}
	want := m.recent[0]

	m.handleMouse(click(m.frame.content.x+4, m.frame.content.y+1+recentTop))

	if m.active != want.alias || !m.inPane() {
		t.Fatalf("the click went to %q in %s, want %+v", m.active, modeName(m.mode), want)
	}
}

// Leaving a pane hands the keyboard to the sidebar, so the sidebar has to be on screen.
func TestLeavingAPaneShowsTheSidebar(t *testing.T) {
	leaves := map[string]func(m *model){
		"toSidebar": (*model).toSidebar,
		"leaveAll":  (*model).leaveAll,
	}
	for name, leave := range leaves {
		t.Run(name, func(t *testing.T) {
			m := viewModel(120, 34)
			m.active, m.mode = "web1", modeShell

			leave(m)

			if m.mode != modeList || !m.sidebarOn() {
				t.Fatalf("leave %s: mode %v, list on %v; want the list on screen", name, m.mode, m.sidebarOn())
			}
		})
	}
}

// Too narrow to hold the list there is nothing to show, and the keys stay quiet.
func TestNarrowWindowLeavesTheListOff(t *testing.T) {
	m := viewModel(24, 24)
	if m.sidebarFits() {
		t.Fatal("a 24-column window fits the sidebar, so this test proves nothing")
	}
	m.active, m.mode = "web1", modeShell

	m.toSidebar()

	if m.sidebarOn() {
		t.Fatal("the list claims to be on screen in a window too narrow for it")
	}
	m.cursor = 1
	m.handleKey(key(t, "e"))
	m.handleKey(key(t, "down"))
	if m.hostForm.open || m.cursor != 1 {
		t.Fatalf("form=%v cursor=%d, want no key to reach the off-screen list", m.hostForm.open, m.cursor)
	}
}

// ---- stepping between hosts ----

// ctrl+o → and ← step along the open hosts in the sidebar's order, wrapping, each landing
// on the host's last place.
func TestLeaderArrowsStepBetweenOpenHosts(t *testing.T) {
	m := newMouseModel(3)
	for i, alias := range []string{"ha", "hb", "hc"} {
		m.sessions[alias] = &session{shells: []*shellTab{{id: i + 1, pane: fakePane()}}}
	}
	m.focusShell("ha")
	press(t, m)

	for _, step := range []struct {
		keys []string
		want string
	}{
		{[]string{"ctrl+o", "right"}, "hb"},
		{[]string{"ctrl+o", "l"}, "hc"},
		{[]string{"ctrl+o", "right"}, "ha"},
		{[]string{"ctrl+o", "left"}, "hc"},
		{[]string{"ctrl+o", "h"}, "hb"},
	} {
		press(t, m, step.keys...)
		if m.active != step.want || m.mode != modeShell {
			t.Fatalf("%v landed on %q in %s, want %s's shell", step.keys, m.active, modeName(m.mode), step.want)
		}
	}
}

// runCmd runs cmd and feeds what it returns back through Update, a batch one by one.
func runCmd(m *model, cmd tea.Cmd) {
	if cmd == nil {
		return
	}
	switch msg := cmd().(type) {
	case tea.BatchMsg:
		for _, c := range msg {
			runCmd(m, c)
		}
	case nil:
	default:
		m.Update(msg)
	}
}

// columnModel is statusModel with an SFTP browser open and the keyboard in it.
func columnModel(t *testing.T, w, h int) (*model, *session) {
	t.Helper()
	m, s := statusModel(t, w, h)
	s.browser = fakeBrowser(t, "/srv")
	m.mode = modeBrowser
	m.relayout()
	return m, s
}

// ctrl+o b hides the docked sidebar: the shell takes the whole width, the sidebar floats on
// esc esc as in a narrow window, and shift+b there or ctrl+o b again docks it back.
func TestTheSidebarCanBeHiddenAndDockedAgain(t *testing.T) {
	m, _ := statusModel(t, 160, 34)
	m.mode = modeShell
	m.Update(nil)
	docked, _ := m.shellSize()

	press(t, m, "ctrl+o", "b")
	if w, _ := m.shellSize(); w != 160-2 || !m.frame.list.empty() || m.docked() {
		t.Fatalf("hidden: shell %d wide, list %+v; want the whole width and no sidebar", w, m.frame.list)
	}

	press(t, m, "esc", "esc")
	if !m.frame.floats || m.frame.list.empty() {
		t.Fatal("esc esc with the sidebar hidden did not float it over the shell")
	}
	if w, _ := m.shellSize(); w != 160-2 {
		t.Fatalf("the floating sidebar resized the shell to %d", w)
	}

	press(t, m, "B")
	if !m.docked() || m.frame.floats {
		t.Fatal("shift+b in the sidebar did not dock it again")
	}
	if w, _ := m.shellSize(); w != docked {
		t.Fatalf("docked again, the shell is %d wide, want %d", w, docked)
	}
}

// The leader works from the sidebar too, so go to and the host steps need no new keys there.
func TestTheLeaderWorksInTheSidebar(t *testing.T) {
	m := switchModel(t)
	press(t, m, "esc", "esc", "ctrl+o", "space")
	if !m.hostSwitch.open {
		t.Fatal("ctrl+o space in the sidebar did not open go to")
	}
}

// esc first clears a filter the prompt says it clears, and only then goes back.
func TestEscClearsAnAppliedFilterFirst(t *testing.T) {
	m := switchModel(t)
	press(t, m, "esc", "esc", "/", "h", "b", "enter")
	if m.filter != "hb" || m.filtering {
		t.Fatalf("filter = %q, filtering %v; want hb applied", m.filter, m.filtering)
	}
	m.escGuard = m.escGuard.AddDate(-1, 0, 0)
	press(t, m, "esc")
	if m.filter != "" || m.mode != modeList {
		t.Fatalf("filter = %q, mode %s; want the filter cleared and the keyboard still here", m.filter, modeName(m.mode))
	}
	press(t, m, "esc")
	if m.mode != modeShell {
		t.Fatalf("the second esc left the keyboard in %s, want the shell", modeName(m.mode))
	}
}

// x on a tab row closes that tab; on a host row it still asks to delete the host.
func TestXOnATabRowClosesTheTab(t *testing.T) {
	m, s := placesModel(t)
	m.width, m.height = 160, 40
	m.Update(nil)
	press(t, m, "esc", "esc") // on ha's shell

	press(t, m, "down") // the files tab
	if m.cursorTab.kind != targetBrowser {
		t.Fatalf("one down from the shell is %+v, want the files tab", m.cursorTab)
	}
	press(t, m, "x")
	if s.browser != nil || m.confirm.open {
		t.Fatalf("x on the files tab left browser=%v confirm=%v, want it closed at once", s.browser != nil, m.confirm.open)
	}
	if m.mode != modeList {
		t.Fatalf("closing a tab from the sidebar moved the keyboard to %s", modeName(m.mode))
	}
}

// An editor may hold unsaved work, so x asks first, and only y closes it.
func TestXOnAnEditorRowAsksFirst(t *testing.T) {
	m, s := placesModel(t)
	m.width, m.height = 160, 40
	m.Update(nil)
	press(t, m, "esc", "esc")
	m.selectPlace(target{alias: "ha", kind: targetEditor, id: 12})

	press(t, m, "x")
	if !m.confirm.open || len(s.editors) != 2 {
		t.Fatalf("x on an editor row: confirm=%v, %d editors; want the card up and both open", m.confirm.open, len(s.editors))
	}
	if card := ansi.Strip(m.modalCard()); !strings.Contains(card, "nginx.conf") {
		t.Fatalf("the card does not name the file:\n%s", card)
	}
	press(t, m, "n")
	if len(s.editors) != 2 {
		t.Fatal("n closed the editor anyway")
	}

	press(t, m, "x", "y")
	if s.findEditorID(12) >= 0 || s.findEditorID(11) < 0 {
		t.Fatal("y did not close exactly the editor under the cursor")
	}
}

func TestXOnAHostRowStillDeletes(t *testing.T) {
	m, _ := placesModel(t)
	m.width, m.height = 160, 40
	m.Update(nil)
	press(t, m, "esc", "esc")
	m.selectPlace(target{alias: "hb"})

	press(t, m, "x")
	if !m.confirm.open || m.confirm.tab.alias != "" || m.confirm.alias != "hb" {
		t.Fatalf("x on a host row gave %+v, want the delete card for hb", m.confirm)
	}
}

// x on a host's only tab ends its session, and the host in front goes with it: nothing may
// keep naming a session that no longer exists.
func TestXOnTheOnlyTabLeavesNoHostInFront(t *testing.T) {
	m, s := statusModel(t, 160, 40)
	s.closeShells()
	s.browser = fakeBrowser(t, "/srv")
	m.mode = modeBrowser
	m.Update(nil)
	press(t, m, "esc", "esc")
	if m.cursorTab.kind != targetBrowser {
		t.Fatalf("the cursor is on %+v, want the files tab", m.cursorTab)
	}

	press(t, m, "x")

	if m.sessions["web1"] != nil || m.active != "" || m.mode != modeList {
		t.Fatalf("session=%v active=%q mode=%s; want the session gone and no host in front",
			m.sessions["web1"] != nil, m.active, modeName(m.mode))
	}
}

// The tunnels row is not a tab: x there neither closes nor deletes anything, and the legend
// does not offer it.
func TestXOnTheTunnelsRowDoesNothing(t *testing.T) {
	m, _ := placesModel(t)
	m.width, m.height = 160, 40
	m.Update(nil)
	press(t, m, "esc", "esc")
	m.cursorTab = target{alias: "ha", kind: targetTunnels}

	if foot := ansi.Strip(m.renderFooter()); strings.Contains(foot, "close tab") || strings.Contains(foot, "delete") {
		t.Fatalf("the tunnels row offers x:\n%s", foot)
	}
	press(t, m, "x")
	if m.confirm.open {
		t.Fatal("x on the tunnels row opened a card")
	}
}

// With no host in front, x on a recent place closes that tab rather than deleting its host.
func TestXOnARecentPlaceClosesItsTab(t *testing.T) {
	m, _ := placesModel(t)
	m.width, m.height = 160, 40
	m.Update(nil)
	m.leaveAll()
	m.buildRows()
	if len(m.recent) == 0 {
		t.Fatal("no recent places to stand on")
	}
	m.recentAt = 1
	place := m.recent[0]

	press(t, m, "x")
	if m.confirm.open && m.confirm.tab.alias == "" {
		t.Fatal("x on a recent place asked to delete its host")
	}
	if slices.Contains(m.hostTabs(place.alias), place) {
		t.Fatalf("x on the recent place %+v left its tab open", place)
	}
}
