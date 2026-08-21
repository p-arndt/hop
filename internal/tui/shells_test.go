package tui

import (
	"path/filepath"
	"testing"

	"hop/internal/filebrowser"
	"hop/internal/sshx"
	"hop/internal/store"
)

// testStore opens an empty store on a throwaway database.
func testStore(t *testing.T) *store.Store {
	t.Helper()
	st, err := store.OpenAt(filepath.Join(t.TempDir(), "hop.config"), "")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	return st
}

// shellModel builds a model focused on host "web", already holding n shells on a
// connection. The panes are fakes (see editor_test.go): the tab bookkeeping never
// reads or writes them.
func shellModel(t *testing.T, n int) (*model, *session) {
	t.Helper()
	s := &session{client: &sshx.Client{}}
	for i := 0; i < n; i++ {
		s.shells = append(s.shells, &shellTab{id: i + 1, pane: fakePane()})
	}
	m := &model{hosts: []store.Host{{Alias: "web"}}, filtered: []int{0}, sessions: map[string]*session{"web": s}, connecting: map[string]bool{}, notify: make(chan struct{}, 1), layout: layout{paneW: 40, paneH: 12, height: 20}, focus: focus{active: "web", mode: paneModeIf(n > 0, modeShell)}}
	t.Cleanup(s.closeShells)
	return m, s
}

// alt+←/→ cycle the shells open on the host and wrap around at both ends.
func TestShellTabCycling(t *testing.T) {
	m, s := shellModel(t, 3)

	for _, want := range []int{1, 2, 0} { // right wraps 2 -> 0
		m.handleKey(altKey("right"))
		if s.activeSh != want {
			t.Fatalf("after alt+right: activeSh = %d, want %d", s.activeSh, want)
		}
	}
	for _, want := range []int{2, 1, 0} { // left wraps 0 -> 2
		m.handleKey(altKey("left"))
		if s.activeSh != want {
			t.Fatalf("after alt+left: activeSh = %d, want %d", s.activeSh, want)
		}
	}
	if !m.focused() {
		t.Fatal("switching shells left the pane")
	}
}

// alt+1…9 jump straight to a shell; a number with no shell behind it does nothing.
func TestShellTabJump(t *testing.T) {
	m, s := shellModel(t, 3)

	m.handleKey(altKey("3"))
	if s.activeSh != 2 {
		t.Fatalf("alt+3: activeSh = %d, want 2", s.activeSh)
	}
	m.handleKey(altKey("9")) // no ninth shell
	if s.activeSh != 2 {
		t.Fatalf("alt+9 with 3 shells moved to %d, want to stay on 2", s.activeSh)
	}
}

// 'S' opens another shell on a host that already has one — over the connection
// hop is already holding, so it never dials again.
func TestNewShellReusesTheConnection(t *testing.T) {
	m, s := shellModel(t, 1)
	m.mode = modeList

	_, cmd := m.handleKey(key(t, "S"))
	if cmd == nil {
		t.Fatal("S did not start a second shell")
	}
	if !m.connecting["web"] {
		t.Fatal("S did not mark the host as connecting")
	}
	if len(s.shells) != 1 {
		t.Fatalf("shells = %d before the command lands, want the original one", len(s.shells))
	}

	// A second S while the first is still in flight must not dial a second
	// connection behind the one already coming.
	if _, cmd := m.handleKey(key(t, "S")); cmd != nil {
		t.Fatal("S during an in-flight connect started another one")
	}
}

// 'enter' on a host with a live shell focuses it rather than opening another —
// only S does that.
func TestEnterFocusesExistingShell(t *testing.T) {
	m, s := shellModel(t, 2)
	m.mode = modeList
	s.activeSh = 1

	_, cmd := m.handleKey(key(t, "enter"))
	if cmd != nil {
		t.Fatal("enter opened a new shell on a host that already has one")
	}
	if !m.focused() || s.activeSh != 1 || len(s.shells) != 2 {
		t.Fatalf("focused = %v, activeSh = %d, shells = %d; want the second shell focused",
			m.focused(), s.activeSh, len(s.shells))
	}
}

// The connected shell lands as a new tab beside the ones already open, and the
// host stays on one connection.
func TestConnectedMsgAppendsShell(t *testing.T) {
	m, s := shellModel(t, 1)
	m.st = testStore(t) // a landing shell bumps the host's visit count
	tab := &shellTab{id: 7, pane: fakePane()}

	m.Update(connectedMsg{alias: "web", tab: tab})

	if len(s.shells) != 2 || s.activeSh != 1 {
		t.Fatalf("shells = %d, activeSh = %d; want the new shell appended and focused",
			len(s.shells), s.activeSh)
	}
	if m.connecting["web"] {
		t.Fatal("the host is still marked connecting after its shell landed")
	}
	if !m.focused() || m.active != "web" {
		t.Fatal("a new shell did not take the pane")
	}
}

// Exiting a shell closes its tab. The others keep running.
func TestShellExitDropsTab(t *testing.T) {
	m, s := shellModel(t, 3)
	s.activeSh = 2

	m.Update(shellExitedMsg{alias: "web", id: 3})

	if len(s.shells) != 2 {
		t.Fatalf("shells = %d, want the other two still open", len(s.shells))
	}
	if s.activeSh != 1 {
		t.Fatalf("activeSh = %d, want 1 after the tab above it closed", s.activeSh)
	}
	if !m.focused() {
		t.Fatal("left the pane while shells were still open")
	}
}

// The last shell exiting with nothing else open on the connection ends the
// session: the host goes back to idle rather than lingering as a dead pane.
func TestLastShellExitEndsSession(t *testing.T) {
	m, _ := shellModel(t, 1)

	m.Update(shellExitedMsg{alias: "web", id: 1})

	if _, live := m.sessions["web"]; live {
		t.Fatal("the session outlived its last shell with nothing else open on it")
	}
	if m.focused() || m.active != "" {
		t.Fatalf("focused = %v, active = %q; want the pane handed back to the host list",
			m.focused(), m.active)
	}
}

// ...but a browser open on the same connection keeps the session alive: exiting
// the shell drops back to it, and the SFTP view survives.
func TestLastShellExitKeepsBrowser(t *testing.T) {
	m, s := shellModel(t, 1)
	s.browser = &filebrowser.Browser{}

	m.Update(shellExitedMsg{alias: "web", id: 1})

	if _, live := m.sessions["web"]; !live {
		t.Fatal("exiting the shell tore down a session whose browser was still open")
	}
	if m.focused() || !m.browsing() {
		t.Fatalf("focused = %v, browsing = %v; want the browser back", m.focused(), m.browsing())
	}
}

// ← is the shell's, always. It moves the readline cursor, it is what alt+b/alt+f
// and the vim/htop arrows are built on, and hop taking it — even at what hop
// believes is a bare prompt — breaks editing on every server you connect to.
// Leaving a pane is ctrl+o or a double esc.
func TestLeftAlwaysGoesToTheShell(t *testing.T) {
	for _, tc := range []struct {
		name   string
		before []string
	}{
		{"bare prompt", nil},
		{"half-typed line", []string{"l", "s"}},
		{"after enter", []string{"l", "s", "enter"}},
		{"after ctrl+c", []string{"l", "s", "ctrl+c"}},
		{"after ctrl+u", []string{"l", "s", "ctrl+u"}},
		{"after backspacing the line away", []string{"l", "s", "backspace", "backspace"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m, _ := shellModel(t, 1)
			for _, k := range tc.before {
				m.handleKey(key(t, k))
			}

			m.handleKey(key(t, "left"))

			if !m.focused() {
				t.Fatalf("left at a %s left the pane, want it forwarded to the shell", tc.name)
			}
		})
	}
}

// The tab strip only costs a row once there is a second shell to switch to.
func TestShellSizeMakesRoomForTheStrip(t *testing.T) {
	m, _ := shellModel(t, 1)

	if _, h := m.shellSize(1); h != m.paneH {
		t.Fatalf("one shell gets height %d, want the whole pane (%d)", h, m.paneH)
	}
	if _, h := m.shellSize(2); h != m.paneH-1 {
		t.Fatalf("two shells get height %d, want one row less than the pane (%d)", h, m.paneH)
	}
}
