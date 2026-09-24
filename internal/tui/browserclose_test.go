package tui

import (
	"testing"
)

// browserOnly builds a model browsing host "ha", with s filled in by the caller.
func browserOnly(t *testing.T, s *session) *model {
	t.Helper()
	s.browser = fakeBrowser(t, "/srv")
	m := newMouseModel(3)
	m.active = "ha"
	m.mode = modeBrowser
	m.sessions["ha"] = s
	return m
}

func TestQClosesALoneBrowserAndItsConnection(t *testing.T) {
	m := browserOnly(t, &session{})

	m.handleBrowserKey(key(t, "q"))

	if _, live := m.sessions["ha"]; live {
		t.Fatal("the session outlived the only thing open on it")
	}
	if m.mode != modeList || m.active != "" {
		t.Fatalf("mode = %v, active = %q; want the host list", m.mode, m.active)
	}
}

func TestQClosingTheBrowserKeepsTheShells(t *testing.T) {
	s := &session{shells: []*shellTab{{id: 1, pane: fakePane()}}}
	m := browserOnly(t, s)

	m.handleBrowserKey(key(t, "q"))

	if s.browser != nil {
		t.Fatal("the browser is still open")
	}
	if m.sessions["ha"] != s || len(s.shells) != 1 {
		t.Fatal("closing the browser took the shell with it")
	}
	// The files tab went; the host's last place is what is left of it.
	if m.mode != modeShell || m.active != "ha" {
		t.Fatalf("mode = %v, active = %q; want ha's shell tab", m.mode, m.active)
	}
}

func TestQClosingTheBrowserHandsTheKeyboardToTheOpenFile(t *testing.T) {
	s := &session{editors: []*editorTab{{id: 1, name: "app.conf", path: "/srv/app.conf", pane: fakePane()}}}
	m := browserOnly(t, s)

	m.handleBrowserKey(key(t, "q"))

	if s.browser != nil || len(s.editors) != 1 {
		t.Fatal("want the browser gone and the editor tab kept")
	}
	if m.mode != modeEditor {
		t.Fatalf("mode = %v, want the editor", m.mode)
	}
}
