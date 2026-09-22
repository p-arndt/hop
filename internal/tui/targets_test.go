package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"hop/internal/sshx"
)

// placesModel is switchModel with more open on ha: a browser at /srv and two editor tabs.
func placesModel(t *testing.T) (*model, *session) {
	t.Helper()
	m := switchModel(t)
	s := m.sessions["ha"]
	s.client = &sshx.Client{}
	s.browser = fakeBrowser(t, "/srv")
	s.editors = []*editorTab{
		{id: 11, name: "a.conf", path: "/etc/a.conf", pane: fakePane()},
		{id: 12, name: "nginx.conf", path: "/etc/nginx/nginx.conf", pane: fakePane()},
	}
	t.Cleanup(s.closeEditors)
	return m, s
}

// pick opens the switcher and lands on the row for want, as a user arrowing to it would.
func pick(t *testing.T, m *model, want target) {
	t.Helper()
	if m.mode == modeBrowser {
		press(t, m, "p")
	} else {
		press(t, m, "ctrl+o", "space")
	}
	for i, it := range m.hostSwitch.items {
		if it.t == want {
			m.hostSwitch.cursor = i
			press(t, m, "enter")
			return
		}
	}
	t.Fatalf("no row for %+v among %v", want, switchAliases(m))
}

func TestSwitcherListsEverythingOpenOnEveryHost(t *testing.T) {
	m, _ := placesModel(t)
	press(t, m, "ctrl+o", "space")

	card := ansi.Strip(m.modalCard())

	for _, want := range []string{"$ shell 1", "▤ /srv", "✎ /etc/a.conf", "✎ /etc/nginx/nginx.conf"} {
		if !strings.Contains(card, want) {
			t.Fatalf("the card is missing %q:\n%s", want, card)
		}
	}
}

func TestSwitcherEnterWithNoQueryGoesBackToThePreviousPlace(t *testing.T) {
	m, s := placesModel(t)
	s.focusTab(1)
	m.mode = modeEditor
	press(t, m)
	pick(t, m, target{alias: "hb", kind: targetShell, id: 2})

	press(t, m, "ctrl+o", "space", "enter")

	if m.active != "ha" || m.mode != modeEditor || s.editor().id != 12 {
		t.Fatalf("active = %q, mode = %v; want ha's nginx.conf back", m.active, m.mode)
	}

	press(t, m, "ctrl+o", "space", "enter")
	if m.active != "hb" || m.mode != modeShell {
		t.Fatalf("active = %q, mode = %v; a second go should return to hb's shell", m.active, m.mode)
	}
}

func TestSwitcherLandsOnTheChosenEditorTab(t *testing.T) {
	m, s := placesModel(t)
	m.focusShell("hb")
	press(t, m)

	pick(t, m, target{alias: "ha", kind: targetEditor, id: 12})

	if m.active != "ha" || m.mode != modeEditor || s.editor() == nil || s.editor().id != 12 {
		t.Fatalf("active = %q, mode = %v; want ha's second editor tab", m.active, m.mode)
	}
}

func TestSwitcherLandsOnTheChosenShellTab(t *testing.T) {
	m, s := placesModel(t)
	s.shells = append(s.shells, &shellTab{id: 7, pane: fakePane()})
	m.focusShell("hb")
	press(t, m)

	pick(t, m, target{alias: "ha", kind: targetShell, id: 7})

	if m.active != "ha" || m.mode != modeShell || s.shell().id != 7 {
		t.Fatalf("active = %q, mode = %v, shell %d; want ha's second shell", m.active, m.mode, s.shell().id)
	}
}

func TestSwitcherLandsInTheBrowser(t *testing.T) {
	m, _ := placesModel(t)

	pick(t, m, target{alias: "ha", kind: targetBrowser})

	if m.active != "ha" || m.mode != modeBrowser {
		t.Fatalf("active = %q, mode = %v; want ha's browser", m.active, m.mode)
	}
}

func TestSwitcherLandsInTheTerminalPanel(t *testing.T) {
	m, s := openDrawer(t)
	m.focusFiles()
	press(t, m)

	pick(t, m, target{alias: "web", kind: targetDrawer})

	if m.mode != modeDrawer || !s.drawerOpen {
		t.Fatalf("mode = %v, open = %v; want the keyboard in the panel", m.mode, s.drawerOpen)
	}
}

func TestSwitcherFiltersOnPaths(t *testing.T) {
	m, _ := placesModel(t)
	press(t, m, "ctrl+o", "space", "n", "g", "i", "n", "x")

	if len(m.hostSwitch.items) == 0 || m.hostSwitch.items[0].t != (target{alias: "ha", kind: targetEditor, id: 12}) {
		t.Fatalf("nginx matched %+v first, want the nginx.conf tab", m.hostSwitch.items)
	}
}

func TestSwitcherHostRowLandsOnTheHostsLastPlace(t *testing.T) {
	m, _ := placesModel(t)
	m.mode = modeBrowser
	press(t, m)
	m.focusShell("hb")
	press(t, m)

	pick(t, m, target{alias: "ha"})

	if m.active != "ha" || m.mode != modeBrowser {
		t.Fatalf("active = %q, mode = %v; want ha's browser, where it was left", m.active, m.mode)
	}
}

func TestSwitcherReconnectsADeadHost(t *testing.T) {
	m := switchModel(t)
	m.sessions["hb"].dead = true
	press(t, m, "ctrl+o", "space")
	for _, it := range m.hostSwitch.items {
		if it.t.alias == "hb" && it.t.kind != targetHost {
			t.Fatalf("offered %+v on a dead session, where nothing can be landed on", it.t)
		}
	}
	press(t, m, "h", "b")

	_, cmd := m.Update(key(t, "enter"))

	if cmd == nil || !m.connecting["hb"] {
		t.Fatal("enter on a dead host did not reconnect it")
	}
}

func TestSwitcherTunnelRowOpensTheTunnelManager(t *testing.T) {
	m := switchModel(t)
	m.sessions["hb"].tunnels = map[int64]*sshx.Tunnel{1: {}}

	pick(t, m, target{alias: "hb", kind: targetTunnels})

	if !m.tunnels.open || m.tunnels.alias != "hb" {
		t.Fatal("the tunnel row did not open hb's tunnel manager")
	}
}

func TestTargetLabelsAreStripped(t *testing.T) {
	m, s := placesModel(t)
	s.editors[0].path = "/etc/\x1b[31mevil"

	label := m.targetLabel(target{alias: "ha", kind: targetEditor, id: 11})

	if strings.ContainsRune(label, '\x1b') {
		t.Fatalf("label %q carries a control character", label)
	}
}

// ---- last place ----

func TestHostListEnterLandsWhereTheHostWasLeft(t *testing.T) {
	m, s := placesModel(t)
	s.focusTab(1)
	m.mode = modeEditor
	press(t, m)
	m.leaveAll()
	press(t, m)

	press(t, m, "enter")

	if m.active != "ha" || m.mode != modeEditor || s.editor().id != 12 || len(s.shells) != 1 {
		t.Fatalf("active = %q, mode = %v, shells = %d; want ha's nginx.conf and no new shell", m.active, m.mode, len(s.shells))
	}
}

func TestLeaderTabLandsOnTheExactTabLeft(t *testing.T) {
	m, s := placesModel(t)
	s.shells = append(s.shells, &shellTab{id: 7, pane: fakePane()})
	s.activeSh = 1
	press(t, m)
	m.focusShell("hb")
	press(t, m)
	s.activeSh = 0

	press(t, m, "ctrl+o", "tab")

	if m.active != "ha" || s.shell().id != 7 {
		t.Fatalf("active = %q, shell %d; want the second shell, the one left", m.active, s.shell().id)
	}
}

// With nothing used yet the host falls back to its shell, then its browser, then an editor.
func TestLastPlaceFallsBackToShellBrowserEditor(t *testing.T) {
	m, s := placesModel(t)
	m.used = nil

	if lp, _ := m.lastPlace("ha"); lp.kind != targetShell {
		t.Fatalf("last place = %+v, want the shell", lp)
	}
	s.closeShells()
	if lp, _ := m.lastPlace("ha"); lp.kind != targetBrowser {
		t.Fatalf("last place = %+v, want the browser", lp)
	}
}
