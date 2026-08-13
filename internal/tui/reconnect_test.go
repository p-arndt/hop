package tui

import (
	"errors"
	"strings"
	"testing"

	"hop/internal/filebrowser"
	"hop/internal/sftpx"
	"hop/internal/sshx"
	"hop/internal/store"
)

// fakeSFTP is the slice of an SFTP connection the browser uses, over a directory
// that is not there: enough to build a real Browser (and to close one) without a
// server behind it.
type fakeSFTP struct{ dir string }

func (f fakeSFTP) Home() (string, error)             { return f.dir, nil }
func (fakeSFTP) List(string) ([]sftpx.Entry, error)  { return nil, nil }
func (fakeSFTP) Download(_, _ string) (int64, error) { return 0, nil }
func (fakeSFTP) Close() error                        { return nil }

// fakeBrowser builds a Browser standing in dir, over the fake above.
func fakeBrowser(t *testing.T, dir string) *filebrowser.Browser {
	t.Helper()
	br, err := filebrowser.New(fakeSFTP{dir: dir}, dir,
		filebrowser.Options{DownloadDir: t.TempDir()}, 40, 12)
	if err != nil {
		t.Fatalf("build browser: %v", err)
	}
	return br
}

// deadModel builds a model on host "web" holding n shells and, optionally, an SFTP
// browser on one connection, with the pane focused. cli is the connection the
// session is riding on, which the loss message has to name.
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
	// The host lives in the store as well as in the model: a landing shell reloads
	// the list from it, and a reconnect's plan is applied against the host it finds
	// there.
	host := store.Host{Alias: "web", HostName: "web.example.com", User: "deploy"}
	st := testStore(t)
	if _, err := st.Add(host); err != nil {
		t.Fatalf("add host: %v", err)
	}
	m := &model{
		st:         st,
		hosts:      []store.Host{host},
		filtered:   []int{0},
		highlights: map[int][]int{},
		sessions:   map[string]*session{"web": s},
		connecting: map[string]bool{},
		pending:    map[string]reconnectPlan{},
		notify:     make(chan struct{}, 1),
		active:     "web",
		mode:       paneModeIf(n > 0, modeShell),
		width:      100,
		height:     30,
		paneW:      60,
		paneH:      20,
		ready:      true,
	}
	m.recomputeLayout()
	t.Cleanup(func() {
		if s, live := m.sessions["web"]; live {
			s.close()
		}
	})
	return m, s, cli
}

// A dropped connection marks the session dead and leaves everything on screen: the
// shells it was holding, the browser, and the host in the session list. Tearing it
// down would take the reconnect offer — and the last screen the host drew — with it.
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

// Every channel on a dropped connection ends at once, so the shells report exits
// too. They are not exits: dropping their tabs would close the session behind the
// user's back, so a dead session ignores them.
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

// An editor's exit on a dropped connection is the same story: the channel was cut,
// nobody typed ":q".
func TestEditorExitAfterADropKeepsTheSession(t *testing.T) {
	m, s, cli := deadModel(t, 1, true)
	s.editors = append(s.editors, &editorTab{id: 1, name: "nginx.conf", path: "/etc/nginx.conf", pane: fakePane()})
	m.Update(sessionLostMsg{alias: "web", client: cli})

	m.Update(editorExitedMsg{alias: "web", id: 1})

	if len(s.editors) != 1 {
		t.Fatalf("editors = %d, want the tab kept on a dead session", len(s.editors))
	}
}

// The loss message names the connection that died, not just the host: it also fires
// for every close hop makes itself, and by then the alias may hold a *new*
// connection. Marking that one dead would kill a session that is perfectly alive.
func TestLossOfAReplacedConnectionIsIgnored(t *testing.T) {
	m, s, _ := deadModel(t, 1, false)

	m.Update(sessionLostMsg{alias: "web", client: &sshx.Client{}, err: errors.New("closed")})

	if s.dead {
		t.Fatal("a loss reported for a connection the session no longer holds marked it dead")
	}
}

// A dead pane forwards nothing to the far end — there is nothing there — and it does
// not quietly swallow keys either: what is left is reconnect, leave, and drop.
func TestDeadPaneKeyboard(t *testing.T) {
	m, _, cli := deadModel(t, 1, false)
	m.Update(sessionLostMsg{alias: "web", client: cli})

	// An ordinary key does nothing at all: no reconnect, no exit from the pane.
	if _, cmd := m.handleKey(key(t, "j")); cmd != nil {
		t.Fatal("a plain key on a dead pane ran a command")
	}
	if m.connecting["web"] || !m.focused() {
		t.Fatal("a plain key on a dead pane reconnected or left the pane")
	}

	// ctrl+o backs out to the list, leaving the pane on screen as the host's last
	// known state.
	m.handleKey(key(t, "ctrl+o"))
	if m.focused() || m.active != "web" {
		t.Fatalf("focused = %v, active = %q; want the list focused with the pane still shown",
			m.focused(), m.active)
	}
}

// 'r' on a dead pane reconnects: the dead session goes, a dial starts, and what was
// open on it is parked as the plan the landing will put back.
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

// Someone who was in the SFTP browser when the link dropped comes back to it: the
// browser is what the reconnect dials first, and the first thing to land is what
// takes the keyboard.
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

// The landing of a reconnect puts the rest back: the shell tabs it had and the
// browser, over the one new connection. Editor tabs are not reopened, and the status
// says so rather than leaving them quietly missing.
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

// A shell a reconnect is putting back lands quietly: the reconnect has already
// decided where the keyboard goes, so a restored tab must not steal it.
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

// A reconnect that fails for good must not leave its plan lying about, or the next
// ordinary connect to that host would restore tabs nobody asked for.
func TestFailedReconnectDropsThePlan(t *testing.T) {
	m, _, cli := deadModel(t, 2, false)
	m.Update(sessionLostMsg{alias: "web", client: cli})
	m.handleKey(key(t, "r"))

	m.Update(connectedMsg{alias: "web", err: errors.New("dial tcp: connection refused")})

	if _, still := m.pending["web"]; still {
		t.Fatal("a failed reconnect left its plan pending")
	}
}

// 'r' in the host list reconnects the dropped session under the cursor — a drop you
// notice by the red dot in the sidebar is as likely as one you notice in the pane.
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

// ...and on a host that is not dropped it says so rather than dialing something.
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

// The screen says the connection is gone in the three places you might be looking:
// the pane's banner, the header's breadcrumb, and the footer's keys.
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
	// The host list and the details card both mark the host, so the state is visible
	// with the pane left behind as much as in it.
	m.mode, m.active = modeList, ""
	if !strings.Contains(m.renderDetails(m.paneW), "connection lost") {
		t.Fatal("the details card does not say the host's session dropped")
	}
}

// Dropping a dead session is what 'd' means on it: the pane goes, and the host is
// idle again rather than sitting there red forever.
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

// Every way of asking for a shell on a dropped session means "get me back on this
// host": there is no shell to focus, only one to re-earn.
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
