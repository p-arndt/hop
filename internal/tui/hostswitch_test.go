package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

// switchModel builds a model on hosts ha, hb, hc with a shell open on ha and hb, the
// keyboard in ha's shell.
func switchModel(t *testing.T) *model {
	t.Helper()
	m := newMouseModel(3)
	m.sessions["ha"] = &session{shells: []*shellTab{{id: 1, pane: fakePane()}}}
	m.sessions["hb"] = &session{shells: []*shellTab{{id: 2, pane: fakePane()}}}
	m.focusShell("ha")
	press(t, m) // lets noteHost see where the keyboard starts
	return m
}

// press sends each key through Update, as a running hop would.
func press(t *testing.T, m *model, names ...string) {
	t.Helper()
	if len(names) == 0 {
		m.Update(nil)
	}
	for _, n := range names {
		m.Update(key(t, n))
	}
}

func switchAliases(m *model) []string {
	var out []string
	for _, h := range m.hostSwitch.items {
		out = append(out, h.Alias)
	}
	return out
}

func TestLeaderSpaceOpensTheHostSwitcherWithSessionsFirst(t *testing.T) {
	m := newMouseModel(3)
	m.sessions["hc"] = &session{shells: []*shellTab{{id: 1, pane: fakePane()}}}
	m.focusShell("hc")

	press(t, m, "ctrl+o", "space")

	if !m.hostSwitch.open {
		t.Fatal("ctrl+o space did not open the host switcher")
	}
	if got := switchAliases(m); len(got) != 3 || got[0] != "hc" || got[1] != "ha" || got[2] != "hb" {
		t.Fatalf("items = %v, want the connected host first, then the list's order", got)
	}
}

// Only the chord: a bare space in a shell is the remote's.
func TestSpaceInAShellDoesNotOpenTheHostSwitcher(t *testing.T) {
	m := switchModel(t)

	press(t, m, "space")

	if m.hostSwitch.open {
		t.Fatal("a bare space opened the host switcher instead of reaching the shell")
	}
}

func TestHostSwitcherFiltersAsYouType(t *testing.T) {
	m := switchModel(t)
	press(t, m, "ctrl+o", "space", "h", "c")

	if got := switchAliases(m); len(got) != 1 || got[0] != "hc" {
		t.Fatalf("query %q matched %v, want only hc", m.hostSwitch.query, got)
	}

	press(t, m, "backspace", "backspace", "x", "y", "z")
	if len(m.hostSwitch.items) != 0 {
		t.Fatalf("nonsense matched %v", switchAliases(m))
	}
	press(t, m, "enter")
	if m.active != "ha" || m.mode != modeShell {
		t.Fatalf("enter on an empty switcher moved to %q in mode %v", m.active, m.mode)
	}
}

func TestHostSwitcherEnterFocusesThatHostsShell(t *testing.T) {
	m := switchModel(t)

	press(t, m, "ctrl+o", "space", "h", "b", "enter")

	if m.hostSwitch.open {
		t.Fatal("the switcher stayed open after hopping")
	}
	if m.active != "hb" || m.mode != modeShell {
		t.Fatalf("active = %q, mode = %v; want hb's shell", m.active, m.mode)
	}
}

func TestHostSwitcherConnectsAHostWithoutASession(t *testing.T) {
	m := switchModel(t)
	press(t, m, "ctrl+o", "space", "h", "c")

	_, cmd := m.Update(key(t, "enter"))

	if cmd == nil || !m.connecting["hc"] {
		t.Fatal("enter on an unconnected host did not start a connect")
	}
}

func TestHostSwitcherCardNamesEveryHost(t *testing.T) {
	m := switchModel(t)
	press(t, m, "ctrl+o", "space")

	card := ansi.Strip(m.modalCard())

	for _, want := range []string{"HOSTS", "ha", "hb", "hc", "hop"} {
		if !strings.Contains(card, want) {
			t.Fatalf("the card is missing %q:\n%s", want, card)
		}
	}
}

func TestHostSwitcherEscChangesNothing(t *testing.T) {
	m := switchModel(t)

	press(t, m, "ctrl+o", "space", "down", "esc")

	if m.hostSwitch.open {
		t.Fatal("esc did not close the switcher")
	}
	if m.active != "ha" || m.mode != modeShell {
		t.Fatalf("esc moved the keyboard to %q in mode %v", m.active, m.mode)
	}
}

func TestPasteIntoTheHostSwitcherSearches(t *testing.T) {
	m := switchModel(t)
	press(t, m, "ctrl+o", "space")

	m.Update(tea.PasteMsg{Content: "hb"})

	if got := switchAliases(m); len(got) != 1 || got[0] != "hb" {
		t.Fatalf("pasting hb matched %v", got)
	}
}

func TestPOpensTheHostSwitcherFromTheBrowser(t *testing.T) {
	m := browserOnly(t, &session{})

	press(t, m, "p")

	if !m.hostSwitch.open {
		t.Fatal("p in the browser did not open the host switcher")
	}
}

func TestLeaderSpaceOpensTheHostSwitcherFromAnEditor(t *testing.T) {
	m, _ := editorModel(t, "app.conf")

	press(t, m, "ctrl+o", "space")

	if !m.hostSwitch.open {
		t.Fatal("ctrl+o space did not open the host switcher in an editor tab")
	}
}

func TestLeaderTabGoesBackAndForthBetweenTheLastTwoHosts(t *testing.T) {
	m := switchModel(t)
	press(t, m, "ctrl+o", "space", "h", "b", "enter")

	press(t, m, "ctrl+o", "tab")
	if m.active != "ha" || m.mode != modeShell {
		t.Fatalf("active = %q, mode = %v; want back in ha's shell", m.active, m.mode)
	}

	press(t, m, "ctrl+o", "tab")
	if m.active != "hb" {
		t.Fatalf("active = %q; a second ctrl+o tab should return to hb", m.active)
	}
}

func TestLeaderTabLandsInTheModeTheLastHostWasShowing(t *testing.T) {
	m := switchModel(t)
	m.sessions["ha"].browser = fakeBrowser(t, "/srv")
	m.mode = modeBrowser
	press(t, m)

	press(t, m, "p", "h", "b", "enter")
	press(t, m, "ctrl+o", "tab")

	if m.active != "ha" || m.mode != modeBrowser {
		t.Fatalf("active = %q, mode = %v; want ha's browser", m.active, m.mode)
	}
}

// A mode the host no longer has falls back to the first it does: shell, browser, editor.
func TestLeaderTabFallsBackWhenTheLastModeIsGone(t *testing.T) {
	m := switchModel(t)
	m.sessions["ha"].editors = []*editorTab{{id: 3, name: "a.conf", path: "/etc/a.conf", pane: fakePane()}}
	m.focusShell("hb")
	press(t, m)
	m.last = hostView{alias: "ha", mode: modeBrowser}

	press(t, m, "ctrl+o", "tab")

	if m.active != "ha" || m.mode != modeShell {
		t.Fatalf("active = %q, mode = %v; want ha's shell", m.active, m.mode)
	}
}

func TestLeaderTabWithoutALastHostSaysSo(t *testing.T) {
	m := switchModel(t)

	press(t, m, "ctrl+o", "tab")

	if m.active != "ha" || m.statusKind != statusWarn || m.status == "" {
		t.Fatalf("active = %q, status = %q (%v); want a warning and no move", m.active, m.status, m.statusKind)
	}
}

func TestLeaderTabToAHostWhoseSessionIsGoneSaysSo(t *testing.T) {
	m := switchModel(t)
	press(t, m, "ctrl+o", "space", "h", "b", "enter")
	delete(m.sessions, "ha")

	press(t, m, "ctrl+o", "tab")

	if m.active != "hb" || m.statusKind != statusWarn || m.status == "" {
		t.Fatalf("active = %q, status = %q (%v); want a warning and no move", m.active, m.status, m.statusKind)
	}
}

// The palette offers the way back only while it would work.
func TestPaletteOffersTheHostChordsInAPane(t *testing.T) {
	m := switchModel(t)
	if as := m.contextActions(); !has(as, "hop to another host") || has(as, "back to the last host") {
		t.Fatalf("with no last host: %v", labels(as))
	}

	press(t, m, "ctrl+o", "space", "h", "b", "enter")
	if as := m.contextActions(); !has(as, "back to the last host") {
		t.Fatalf("with a last host: %v", labels(as))
	}
}
