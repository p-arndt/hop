package tui

import (
	"errors"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"hop/internal/filebrowser"
	"hop/internal/filebrowser/fbtest"
	"hop/internal/sftpx"
	"hop/internal/sshx"
	"hop/internal/store"
)

func fakeBrowser(t *testing.T, dir string) *filebrowser.Browser {
	t.Helper()
	return fakeBrowserWith(t, dir)
}

// fakeBrowserWith is fakeBrowser over a listing rather than an empty directory.
func fakeBrowserWith(t *testing.T, dir string, entries ...sftpx.Entry) *filebrowser.Browser {
	t.Helper()
	br, err := filebrowser.New(fbtest.Stub{Dir: dir, Entries: entries}, "ha", dir,
		filebrowser.Options{DownloadDir: t.TempDir()}, 40, 12)
	if err != nil {
		t.Fatalf("build browser: %v", err)
	}
	return br
}

// deadModel builds a focused model on host "web" with n shells and optionally a browser.
func deadModel(t *testing.T, n int, browser bool) (*model, *session, *sshx.Client) {
	t.Helper()
	cli := &sshx.Client{}
	s := &session{client: cli}
	for i := 0; i < n; i++ {
		s.shells = append(s.shells, &shellTab{id: i + 1, pane: fakePane()})
	}
	if browser {
		s.browser = fakeBrowser(t, "/srv/www")
	}
	host := store.Host{Alias: "web", HostName: "web.example.com", User: "deploy"}
	st := testStore(t)
	if _, err := st.Add(host); err != nil {
		t.Fatalf("add host: %v", err)
	}
	m := &model{st: st, hosts: []store.Host{host}, filtered: []int{0}, highlights: map[int][]int{}, sessions: map[string]*session{"web": s}, connecting: map[string]bool{}, pending: map[string]reconnectPlan{}, notify: make(chan struct{}, 1), layout: layout{width: 100, height: 30, paneW: 60, paneH: 20, ready: true}, focus: focus{active: "web", mode: paneModeIf(n > 0, modeShell)}}
	m.recomputeLayout()
	t.Cleanup(func() {
		if s, live := m.sessions["web"]; live {
			s.close()
		}
	})
	return m, s, cli
}

// Everything stays on screen: tearing it down would take the reconnect offer with it.
func TestDroppedConnectionMarksTheSessionDead(t *testing.T) {
	m, s, cli := deadModel(t, 2, true)

	m.Update(sessionLostMsg{alias: "web", client: cli, err: errors.New("ssh: unexpected packet")})

	if !s.dead {
		t.Fatal("a dropped connection did not mark the session dead")
	}
	if _, live := m.sessions["web"]; !live {
		t.Fatal("the session was torn down; there is nothing left to reconnect")
	}
	if len(s.shells) != 2 || s.browser == nil {
		t.Fatalf("shells = %d, browser = %v; want what was open kept on screen", len(s.shells), s.browser != nil)
	}
	if !strings.Contains(m.status, "connection lost") || !strings.Contains(m.status, "r to reconnect") {
		t.Fatalf("status = %q, want it to say the link dropped and how to answer", m.status)
	}
	if s.lostWhy != "ssh: unexpected packet" {
		t.Fatalf("lostWhy = %q, want what the transport reported", s.lostWhy)
	}
}

// Every channel ends at a drop, so those shell exits are not exits.
func TestShellExitsAfterADropKeepTheSession(t *testing.T) {
	m, s, cli := deadModel(t, 2, false)
	m.Update(sessionLostMsg{alias: "web", client: cli})

	m.Update(shellExitedMsg{alias: "web", id: 1})
	m.Update(shellExitedMsg{alias: "web", id: 2})

	if _, live := m.sessions["web"]; !live {
		t.Fatal("the cut-off shells were treated as exits and ended the session")
	}
	if len(s.shells) != 2 {
		t.Fatalf("shells = %d, want both tabs kept as the last screen the host drew", len(s.shells))
	}
}

func TestEditorExitAfterADropKeepsTheSession(t *testing.T) {
	m, s, cli := deadModel(t, 1, true)
	s.editors = append(s.editors, &editorTab{id: 1, name: "nginx.conf", path: "/etc/nginx.conf", pane: fakePane()})
	m.Update(sessionLostMsg{alias: "web", client: cli})

	m.Update(editorExitedMsg{alias: "web", id: 1})

	if len(s.editors) != 1 {
		t.Fatalf("editors = %d, want the tab kept on a dead session", len(s.editors))
	}
}

// The loss names a connection, since the alias may already hold a new one.
func TestLossOfAReplacedConnectionIsIgnored(t *testing.T) {
	m, s, _ := deadModel(t, 1, false)

	m.Update(sessionLostMsg{alias: "web", client: &sshx.Client{}, err: errors.New("closed")})

	if s.dead {
		t.Fatal("a loss reported for a connection the session no longer holds marked it dead")
	}
}

// A dead pane forwards nothing and swallows nothing: what is left is reconnect, leave, drop.
func TestDeadPaneKeyboard(t *testing.T) {
	m, _, cli := deadModel(t, 1, false)
	m.Update(sessionLostMsg{alias: "web", client: cli})

	if _, cmd := m.handleKey(key(t, "j")); cmd != nil {
		t.Fatal("a plain key on a dead pane ran a command")
	}
	if m.connecting["web"] || !m.focused() {
		t.Fatal("a plain key on a dead pane reconnected or left the pane")
	}

	m.handleKey(key(t, "ctrl+o"))
	if m.focused() || m.active != "web" {
		t.Fatalf("focused = %v, active = %q; want the list focused with the pane still shown",
			m.focused(), m.active)
	}
}

// r drops the dead session, starts a dial, and parks what was open as a plan.
func TestDeadPaneReconnects(t *testing.T) {
	m, _, cli := deadModel(t, 2, true)
	m.Update(sessionLostMsg{alias: "web", client: cli})

	_, cmd := m.handleKey(key(t, "r"))
	if cmd == nil {
		t.Fatal("r on a dead pane started no reconnect")
	}
	if _, live := m.sessions["web"]; live {
		t.Fatal("the dead session survived the reconnect; the new connection would share its pane")
	}
	if !m.connecting["web"] {
		t.Fatal("the reconnecting host is not marked connecting, so it shows no spinner")
	}
	plan, ok := m.pending["web"]
	if !ok {
		t.Fatal("the reconnect kept no plan of what to put back")
	}
	if plan.shells != 2 || !plan.browser {
		t.Fatalf("plan = %+v, want the two shells and the browser", plan)
	}
	if plan.browserDir != "/srv/www" {
		t.Fatalf("browserDir = %q, want the directory the browser was standing in", plan.browserDir)
	}
	if !strings.Contains(m.status, "reconnecting") {
		t.Fatalf("status = %q, want it to say a reconnect is in flight", m.status)
	}
}

func TestReconnectComesBackToTheBrowser(t *testing.T) {
	m, _, cli := deadModel(t, 1, true)
	m.mode = modeBrowser
	m.Update(sessionLostMsg{alias: "web", client: cli})

	if _, cmd := m.handleKey(key(t, "r")); cmd == nil {
		t.Fatal("r in a dead browser started no reconnect")
	}
	if !m.pending["web"].browsingFirst {
		t.Fatal("the plan does not record that the browser is the half to come back to")
	}
}

// Editor tabs are not reopened, and the status says so.
func TestReconnectLandingRestoresTheRest(t *testing.T) {
	m, s, cli := deadModel(t, 3, true)
	s.editors = append(s.editors, &editorTab{id: 1, name: "a.conf", path: "/etc/a.conf", pane: fakePane()})
	m.Update(sessionLostMsg{alias: "web", client: cli})
	m.handleKey(key(t, "r"))

	fresh := &sshx.Client{}
	_, cmd := m.Update(connectedMsg{alias: "web", client: fresh, tab: &shellTab{id: 9, pane: fakePane()}})

	if cmd == nil {
		t.Fatal("the landing issued no commands to put the rest of the session back")
	}
	if _, still := m.pending["web"]; still {
		t.Fatal("the plan is still pending after it was applied; a later connect would replay it")
	}
	if !strings.Contains(m.status, "reconnected to web") {
		t.Fatalf("status = %q, want it to report the reconnect", m.status)
	}
	if !strings.Contains(m.status, "1 editor not reopened") {
		t.Fatalf("status = %q, want it to name the editor tab it could not restore", m.status)
	}
	if !m.focused() {
		t.Fatal("the reconnected shell did not take the pane back")
	}
}

// The reconnect has already decided where the keyboard goes.
func TestRestoredShellLandsQuietly(t *testing.T) {
	m, s, _ := deadModel(t, 1, false)
	m.mode = modeBrowser
	m.clearStatus()

	m.Update(connectedMsg{alias: "web", tab: &shellTab{id: 5, pane: fakePane()}, restore: true})

	if len(s.shells) != 2 {
		t.Fatalf("shells = %d, want the restored tab appended", len(s.shells))
	}
	if m.focused() || !m.browsing() {
		t.Fatalf("focused = %v, browsing = %v; want the restored shell to leave the keyboard alone",
			m.focused(), m.browsing())
	}
	if m.status != "" {
		t.Fatalf("status = %q, want a restored shell to say nothing", m.status)
	}
}

// A leftover plan would restore tabs on the next ordinary connect.
func TestFailedReconnectDropsThePlan(t *testing.T) {
	m, _, cli := deadModel(t, 2, false)
	m.Update(sessionLostMsg{alias: "web", client: cli})
	m.handleKey(key(t, "r"))

	m.Update(connectedMsg{alias: "web", err: errors.New("dial tcp: connection refused")})

	if _, still := m.pending["web"]; still {
		t.Fatal("a failed reconnect left its plan pending")
	}
}

func TestReconnectKeyInTheList(t *testing.T) {
	m, _, cli := deadModel(t, 1, false)
	m.Update(sessionLostMsg{alias: "web", client: cli})
	m.mode = modeList
	m.active = ""

	if _, cmd := m.handleKey(key(t, "r")); cmd == nil {
		t.Fatal("r in the list did not reconnect the dropped session under the cursor")
	}
	if !m.connecting["web"] {
		t.Fatal("r in the list started no dial")
	}
}

func TestReconnectKeyOnALiveHostExplains(t *testing.T) {
	m, _, _ := deadModel(t, 1, false)
	m.mode = modeList
	m.active = ""

	if _, cmd := m.handleKey(key(t, "r")); cmd != nil {
		t.Fatal("r reconnected a host whose session is still live")
	}
	if !strings.Contains(m.status, "still connected") {
		t.Fatalf("status = %q, want it to say the session is live", m.status)
	}

	delete(m.sessions, "web")
	m.handleKey(key(t, "r"))
	if !strings.Contains(m.status, "no dropped session") {
		t.Fatalf("status = %q, want it to say there is nothing to reconnect", m.status)
	}
}

// The drop shows in the pane's banner, the header, the footer and the details card.
func TestDeadSessionOnScreen(t *testing.T) {
	m, _, cli := deadModel(t, 1, false)
	m.Update(sessionLostMsg{alias: "web", client: cli, err: errors.New("EOF")})

	view := m.View()
	for _, want := range []string{"connection lost", "disconnected", "reconnect"} {
		if !strings.Contains(view, want) {
			t.Fatalf("the screen never says %q:\n%s", want, view)
		}
	}
	if !strings.Contains(m.renderFooter(), "drop session") {
		t.Fatalf("the dead pane's footer does not offer the way out:\n%s", m.renderFooter())
	}
	m.mode, m.active = modeList, ""
	if !strings.Contains(m.renderDetails(m.paneW), "connection lost") {
		t.Fatal("the details card does not say the host's session dropped")
	}
}

func TestDropADeadSession(t *testing.T) {
	m, _, cli := deadModel(t, 1, false)
	m.Update(sessionLostMsg{alias: "web", client: cli})

	m.handleKey(key(t, "d"))

	if _, live := m.sessions["web"]; live {
		t.Fatal("d on a dead pane kept the session")
	}
	if m.focused() {
		t.Fatal("d on a dead pane left the keyboard in it")
	}
}

func TestOpeningAnythingOnADeadSessionReconnects(t *testing.T) {
	for _, k := range []string{"enter", "s", "S", "f"} {
		t.Run(k, func(t *testing.T) {
			m, _, cli := deadModel(t, 1, true)
			m.Update(sessionLostMsg{alias: "web", client: cli})
			m.mode, m.active = modeList, ""

			if _, cmd := m.handleKey(key(t, k)); cmd == nil {
				t.Fatalf("%q on a dropped session did nothing", k)
			}
			if !m.connecting["web"] {
				t.Fatalf("%q on a dropped session started no reconnect", k)
			}
		})
	}
}

// ---- the size a reconnected browser is built at ----

// watchBrowserSize swaps the browser command out and reports the size a reconnect asked for.
func watchBrowserSize(t *testing.T) func() (int, int) {
	t.Helper()
	var w, h int
	restore := reconnectBrowserCmd
	reconnectBrowserCmd = func(_ store.Host, _ *sshx.Client, _ string, _ sshx.Prompter,
		_ filebrowser.Options, _ string, pw, ph int, _ bool) tea.Cmd {
		w, h = pw, ph
		// Not a nil command: tea.Batch drops nils and the caller checks for one.
		return func() tea.Msg { return nil }
	}
	t.Cleanup(func() { reconnectBrowserCmd = restore })
	return func() (int, int) { return w, h }
}

// wideDeadModel is deadModel on a window wide enough for the tree column and content area to differ.
func wideDeadModel(t *testing.T, shells int, browser bool) (*model, *session, *sshx.Client) {
	t.Helper()
	m, s, cli := deadModel(t, shells, browser)
	m.width, m.height = 200, 40
	m.relayout()
	if bw, _ := m.browserSize(); bw == m.paneW {
		t.Fatalf("the tree column is not on screen (browser width %d = pane width %d), so nothing here is being tested", bw, m.paneW)
	}
	return m, s, cli
}

// Regression: a reconnect built the browser at the content area's width, not the tree column's.
func TestReconnectBuildsTheBrowserAtTheColumnWidth(t *testing.T) {
	m, _, _ := wideDeadModel(t, 0, true)
	size := watchBrowserSize(t)
	wantW, wantH := m.browserSize()

	m.markDead("web", "")
	if cmd := m.reconnect(m.hosts[0]); cmd == nil {
		t.Fatal("the reconnect of a browser-only session issued nothing")
	}

	if gotW, gotH := size(); gotW != wantW || gotH != wantH {
		t.Fatalf("browser built at %dx%d, want the tree column's %dx%d — the content area is %d wide",
			gotW, gotH, wantW, wantH, m.paneW)
	}
}

// The same for the browser put back once the new connection has landed.
func TestReconnectLandingBuildsTheBrowserAtTheColumnWidth(t *testing.T) {
	m, _, _ := wideDeadModel(t, 1, false)
	size := watchBrowserSize(t)
	wantW, wantH := m.browserSize()

	m.pending["web"] = reconnectPlan{browser: true, browserDir: "/srv/www"}
	if cmd := m.applyPlan("web"); cmd == nil {
		t.Fatal("the landing put nothing back, so no browser was opened")
	}

	if gotW, gotH := size(); gotW != wantW || gotH != wantH {
		t.Fatalf("restored browser built at %dx%d, want the tree column's %dx%d — the content area is %d wide",
			gotW, gotH, wantW, wantH, m.paneW)
	}
}
