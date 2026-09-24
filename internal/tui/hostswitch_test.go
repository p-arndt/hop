package tui

import (
	"slices"
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
	for _, it := range m.hostSwitch.items {
		out = append(out, it.t.alias)
	}
	return out
}

// switchHosts is the host rows alone, in the switcher's order.
func switchHosts(m *model) []string {
	var out []string
	for _, it := range m.hostSwitch.items {
		if it.t.kind == targetHost {
			out = append(out, it.t.alias)
		}
	}
	return out
}

// With no query go to is a tree: every open host with what is open on it indented under it,
// then the hosts with nothing open, under a heading.
func TestLeaderSpaceOpensGoToAsATreeOfWhatIsOpen(t *testing.T) {
	m := newMouseModel(3)
	m.sessions["hc"] = &session{shells: []*shellTab{{id: 1, pane: fakePane()}}}
	m.focusShell("hc")

	press(t, m, "ctrl+o", "space")

	if !m.hostSwitch.open {
		t.Fatal("ctrl+o space did not open the switcher")
	}
	items := m.hostSwitch.items
	want := []switchItem{
		{t: target{alias: "hc"}, tree: true},
		{t: target{alias: "hc", kind: targetShell, id: 1}, tree: true, num: 1},
		{heading: "not connected"},
		{t: target{alias: "ha"}},
		{t: target{alias: "hb"}},
	}
	if len(items) != len(want) {
		t.Fatalf("go to has %d rows, want %d: %+v", len(items), len(want), items)
	}
	for i, w := range want {
		got := items[i]
		if got.t != w.t || got.heading != w.heading || got.tree != w.tree || got.num != w.num {
			t.Fatalf("row %d = %+v, want %+v", i, got, w)
		}
	}
	if items[m.hostSwitch.cursor].heading != "" {
		t.Fatal("the cursor starts on a heading")
	}
}

// A query flattens the tree into one ranked list: what is open, most recently used first,
// then every host, the connected ones first.
func TestGoToFlattensWhileTyping(t *testing.T) {
	m := newMouseModel(3)
	m.sessions["hc"] = &session{shells: []*shellTab{{id: 1, pane: fakePane()}}}
	m.focusShell("hc")

	press(t, m, "ctrl+o", "space", "h")

	if first := m.hostSwitch.items[0].t; first.alias != "hc" {
		t.Fatalf("first row = %+v, want hc's", first)
	}
	for _, it := range m.hostSwitch.items {
		if it.heading != "" || it.tree {
			t.Fatalf("a typed query still draws the tree: %+v", it)
		}
	}
	if got := switchHosts(m); len(got) != 3 || got[0] != "hc" {
		t.Fatalf("hosts = %v, want the connected host first", got)
	}
}

// Only the chord: a bare space in a shell is the remote's.
func TestSpaceInAShellDoesNotOpenTheHostSwitcher(t *testing.T) {
	m := switchModel(t)

	press(t, m, "space")

	if m.hostSwitch.open {
		t.Fatal("a bare space opened the switcher instead of reaching the shell")
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

	for _, want := range []string{"GO TO", "ha", "hb", "hc", "$ shell 1", "here", "go"} {
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

	if got := switchAliases(m); len(got) == 0 || slices.ContainsFunc(got, func(a string) bool { return a != "hb" }) {
		t.Fatalf("pasting hb matched %v", got)
	}
}

func TestPOpensTheHostSwitcherFromTheBrowser(t *testing.T) {
	m := browserOnly(t, &session{})

	press(t, m, "p")

	if !m.hostSwitch.open {
		t.Fatal("p in the browser did not open the switcher")
	}
}

func TestLeaderSpaceOpensTheHostSwitcherFromAnEditor(t *testing.T) {
	m, _ := editorModel(t, "app.conf")

	press(t, m, "ctrl+o", "space")

	if !m.hostSwitch.open {
		t.Fatal("ctrl+o space did not open the switcher in an editor tab")
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

// A last place that has closed falls back to the one used before it.
func TestLeaderTabFallsBackWhenTheLastPlaceIsGone(t *testing.T) {
	m := switchModel(t)
	s := m.sessions["ha"]
	s.editors = []*editorTab{{id: 3, name: "a.conf", path: "/etc/a.conf", pane: fakePane()}}
	m.mode = modeEditor
	press(t, m)
	m.focusShell("hb")
	press(t, m)
	s.dropEditor(3)

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
	if as := m.contextActions(); !has(as, "go to anything open, or a host") || has(as, "back to the last host") {
		t.Fatalf("with no last host: %v", labels(as))
	}

	press(t, m, "ctrl+o", "space", "h", "b", "enter")
	if as := m.contextActions(); !has(as, "back to the last host") {
		t.Fatalf("with a last host: %v", labels(as))
	}
}
