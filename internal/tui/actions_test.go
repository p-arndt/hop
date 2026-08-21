package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"hop/internal/keys"
	"hop/internal/store"
)

// labels is what the user would read in the menu or the palette, in order.
func labels(as []action) []string {
	out := make([]string, len(as))
	for i, a := range as {
		out[i] = a.label
	}
	return out
}

func has(as []action, label string) bool {
	for _, a := range as {
		if a.label == label {
			return true
		}
	}
	return false
}

// The registry only offers what the state allows: an idle host can be connected to, a
// live one focused, a dropped one reconnected — and each excludes the others. This is
// the rule the menu and the palette both rest on, so it is tested against the model
// rather than through a keystroke.
func TestHostActionAvailability(t *testing.T) {
	m := newNavModel(2)
	m.sessions = map[string]*session{}

	idle := m.availableHostActions()
	if !has(idle, "connect") || has(idle, "focus its shell") || has(idle, "disconnect everything on it") {
		t.Fatalf("idle host: %v", labels(idle))
	}
	if !has(idle, "pin it to the top") || has(idle, "unpin it") {
		t.Fatalf("unpinned host: %v", labels(idle))
	}

	m.sessions["h0"] = &session{}
	live := m.availableHostActions()
	if has(live, "connect") || !has(live, "focus its shell") || !has(live, "disconnect everything on it") {
		t.Fatalf("live host: %v", labels(live))
	}
	if has(live, "reconnect and reopen") {
		t.Fatalf("live host offers reconnect: %v", labels(live))
	}

	m.sessions["h0"] = &session{dead: true}
	dead := m.availableHostActions()
	if !has(dead, "reconnect and reopen") || has(dead, "focus its shell") {
		t.Fatalf("dropped host: %v", labels(dead))
	}

	m.hosts[0].Pinned = true
	m.applyFilter()
	pinned := m.availableHostActions()
	if !has(pinned, "unpin it") || has(pinned, "pin it to the top") {
		t.Fatalf("pinned host: %v", labels(pinned))
	}
}

// A cursor standing on nothing offers nothing to do to a host, and the menu it would
// open does not open.
func TestMenuNeedsAHost(t *testing.T) {
	m := newNavModel(0)
	if as := m.availableHostActions(); len(as) != 0 {
		t.Fatalf("empty list offers %v", labels(as))
	}
	m.handleKey(key(t, "space"))
	if m.menu.open {
		t.Fatal("menu opened with no host under the cursor")
	}
}

// space opens the menu on the host under the cursor, and enter on a row does what that
// row's key does — here the edit form, which is the host form pre-filled.
func TestMenuRunsTheSelectedAction(t *testing.T) {
	m := newNavModel(3)
	m.cursor = 1

	m.handleKey(key(t, "space"))
	if !m.menu.open {
		t.Fatal("space did not open the menu")
	}
	if m.menu.alias != "h1" {
		t.Fatalf("menu opened on %q, want h1", m.menu.alias)
	}

	// Down to "edit this host", whichever row the availability rules put it on.
	want := -1
	for i, a := range m.menu.items {
		if a.label == "edit this host" {
			want = i
		}
	}
	if want < 0 {
		t.Fatalf("no edit row: %v", labels(m.menu.items))
	}
	for range want {
		m.handleKey(key(t, "down"))
	}
	m.handleKey(key(t, "enter"))

	if m.menu.open {
		t.Fatal("menu stayed open after running an action")
	}
	if !m.hostForm.open {
		t.Fatal("enter on 'edit this host' did not open the host form")
	}
	if !m.hostForm.edit || m.hostForm.orig != "h1" {
		t.Fatalf("form opened on %q (edit=%v), want an edit of h1", m.hostForm.orig, m.hostForm.edit)
	}
}

// esc closes the menu without deciding anything, and the keys it swallowed never reach
// the list underneath — "x" inside the menu must not arm a delete.
func TestMenuSwallowsKeys(t *testing.T) {
	m := newNavModel(3)
	m.handleKey(key(t, "space"))
	m.handleKey(key(t, "x"))
	if m.confirm.open {
		t.Fatal("a key inside the menu reached the list")
	}
	m.handleKey(key(t, "esc"))
	if m.menu.open {
		t.Fatal("esc did not close the menu")
	}
}

// The palette opens on everything, narrows as you type, and runs the row you land on.
func TestPaletteFiltersAndRuns(t *testing.T) {
	m := newNavModel(3)

	m.handleKey(key(t, "ctrl+k"))
	if !m.palette.open {
		t.Fatal("ctrl+k did not open the palette")
	}
	if len(m.palette.items) != len(m.contextActions()) {
		t.Fatalf("palette opened on %d items, want all %d", len(m.palette.items), len(m.contextActions()))
	}

	for _, r := range "add" {
		m.handleKey(key(t, string(r)))
	}
	if len(m.palette.items) == 0 || m.palette.items[0].label != "add a new host" {
		t.Fatalf("query %q matched %v", m.palette.query, labels(m.palette.items))
	}

	m.handleKey(key(t, "enter"))
	if m.palette.open {
		t.Fatal("palette stayed open after running an action")
	}
	if !m.hostForm.open || m.hostForm.edit {
		t.Fatalf("enter did not open the add form (open=%v edit=%v)", m.hostForm.open, m.hostForm.edit)
	}
}

// The key is matched as well as the label: someone who half-remembers the chord finds
// the row by typing it.
func TestPaletteMatchesTheKey(t *testing.T) {
	m := newNavModel(1)
	m.openPalette()
	m.palette.query = "ctrl+b"
	m.filterPalette()

	if !has(m.palette.items, "hide / show the sidebar") {
		t.Fatalf("ctrl+b matched %v", labels(m.palette.items))
	}
}

// A query nothing matches leaves an empty list, and enter on it does nothing rather
// than running whatever was selected before.
func TestPaletteEmptyQueryIsInert(t *testing.T) {
	m := newNavModel(1)
	m.openPalette()
	for _, r := range "zzzz" {
		m.handleKey(key(t, string(r)))
	}
	if len(m.palette.items) != 0 {
		t.Fatalf("nonsense query matched %v", labels(m.palette.items))
	}
	m.handleKey(key(t, "enter"))
	if m.hostForm.open || m.confirm.open || m.settings.open {
		t.Fatal("enter on an empty palette ran something")
	}
}

// Both are drawn as rows of "what it does … the key that does it": the key on every row
// is the whole point, since it is what makes the palette unnecessary next time.
func TestActionRowsCarryTheirKey(t *testing.T) {
	m := newNavModel(2)
	m.width, m.height = 100, 24
	m.hosts[0] = store.Host{Alias: "web-01", HostName: "example.test"}
	m.applyFilter()

	m.openHostMenu()
	menu, _, _ := m.menuAt()
	if !strings.Contains(menu, "web-01") {
		t.Fatalf("menu does not name its host:\n%s", menu)
	}
	for _, want := range []string{"sftp file browser", "delete this host"} {
		if !strings.Contains(menu, want) {
			t.Fatalf("menu is missing %q:\n%s", want, menu)
		}
	}

	// The palette shows a window onto its matches, so the global actions are below the
	// host's until a query narrows to them.
	m.closeMenu()
	m.openPalette()
	m.palette.query = "add"
	m.filterPalette()
	if pal := m.renderPalette(); !strings.Contains(pal, "add a new host") {
		t.Fatalf("palette is missing a global action:\n%s", pal)
	}
}

// A right-click stands the cursor on the host it landed on and opens its menu — the one
// gesture, not two. Row 3 is the first host (see newMouseModel).
func TestRightClickOpensTheMenu(t *testing.T) {
	m := newMouseModel(4)

	m.handleMouse(tea.MouseMsg{X: 4, Y: 5, Button: tea.MouseButtonRight, Action: tea.MouseActionPress})
	if !m.menu.open {
		t.Fatal("right-click did not open the menu")
	}
	if m.cursor != 2 {
		t.Fatalf("cursor at %d, want the row that was clicked (2)", m.cursor)
	}
	if m.menu.alias != m.hosts[2].Alias {
		t.Fatalf("menu opened on %q, want %q", m.menu.alias, m.hosts[2].Alias)
	}

	// While it is up the pointer belongs to it: a click on another row must not move the
	// cursor out from under the question.
	m.handleMouse(click(4, 3))
	if m.cursor != 2 || !m.menu.open {
		t.Fatalf("a click reached the list under the menu (cursor=%d open=%v)", m.cursor, m.menu.open)
	}
}

// The menu is drawn under the row it belongs to, and clear of the screen's bottom.
func TestMenuIsAnchoredToItsRow(t *testing.T) {
	m := newMouseModel(4)
	m.cursor = 1
	m.openHostMenu()

	card, x, y := m.menuAt()
	if x != 2 {
		t.Fatalf("menu at column %d, want 2", x)
	}
	// Under its host, since a 20-row window has the room below it.
	if want := m.cursorScreenRow() + 1; y != want {
		t.Fatalf("menu at row %d, want %d (under its host)", y, want)
	}
	if row := m.cursorScreenRow(); y <= row {
		t.Fatalf("menu at row %d covers the host row %d", y, row)
	}
	_ = card

	// A host near the bottom of a short window gets a shorter menu rather than one
	// running off the screen — and it still stands clear of the row it belongs to.
	m.closeMenu()
	m.cursor = 3
	m.height = 16
	m.openHostMenu()
	card, _, y = m.menuAt()
	if y+lipgloss.Height(card) > m.menuBottom() {
		t.Fatalf("menu covers the status bar: row %d + %d lines > %d", y, lipgloss.Height(card), m.menuBottom())
	}
	if row := m.cursorScreenRow(); y <= row && y+lipgloss.Height(card) > row {
		t.Fatalf("menu at row %d hides the host row %d it belongs to", y, row)
	}
}

// Each mode's palette offers that mode's keyboard. Anything else would be a list of keys
// that do nothing where you are standing.
func TestContextActionsFollowTheMode(t *testing.T) {
	m := newNavModel(2)
	m.sessions = map[string]*session{}

	if as := m.contextActions(); !has(as, "connect") || !has(as, "add a new host") {
		t.Fatalf("list mode: %v", labels(as))
	}

	m.mode = modeBrowser
	browser := m.contextActions()
	if !has(browser, "download the file") || has(browser, "connect") {
		t.Fatalf("browser mode: %v", labels(browser))
	}

	m.mode = modeEditor
	editor := m.contextActions()
	if !has(editor, "back to the file browser") || has(editor, "download the file") {
		t.Fatalf("editor mode: %v", labels(editor))
	}

	m.mode = modeShell
	shell := m.contextActions()
	if !has(shell, "another shell on this host") || has(shell, "connect") {
		t.Fatalf("shell mode: %v", labels(shell))
	}
	// In a pane hop's keyboard is behind the leader, and the palette says so.
	for _, a := range shell {
		if a.label == "another shell on this host" && a.keycap() != "ctrl+o 0" {
			t.Fatalf("the leader is missing from %q: %q", a.label, a.keycap())
		}
	}
}

// A row picked in a pane runs the action itself rather than replaying its keystrokes,
// and the leader it was offered behind is not left half-open by the running.
func TestChordActionsRunWithoutReplay(t *testing.T) {
	m := newNavModel(2)
	m.sessions = map[string]*session{"h0": {}}
	m.active, m.mode = "h0", modeShell

	// The palette from inside a pane is itself behind the leader.
	m.handleKey(key(t, "ctrl+o"))
	m.handleKey(key(t, "ctrl+k"))
	if !m.palette.open {
		t.Fatal("ctrl+o ctrl+k did not open the palette in a pane")
	}

	for _, r := range "all the keys" {
		m.handleKey(key(t, string(r)))
	}
	m.handleKey(key(t, "enter"))

	if !m.help {
		t.Fatal("running 'all the keys' from a pane did not open the card")
	}
	if m.leaderArmed() {
		t.Fatal("the leader was left armed")
	}
}

// The card, the palette and the footer are hand-written lists, not generated from the key
// registry, so a key added to internal/keys reaches the user only if someone remembers all
// three. Marking, the target and copy/move were bound and working while appearing in none
// of them — a key that is never shown is a key that does not exist. This walks the layers
// that have such lists and insists every action is reachable from the card or the palette.
//
// The editor layer joined the walk when the split gained a way out: the split key was
// reachable from the browser for a release before anything on screen said how to undo it,
// which is the same failure one layer over.
func TestEveryActionIsDiscoverable(t *testing.T) {
	// The motions are left out on purpose, for the reason browserSpecs states: nobody
	// opens a menu to move the cursor down. The card lists them as a group of their own.
	motions := map[keys.Action]bool{
		keys.Up: true, keys.Down: true, keys.Top: true, keys.Bottom: true,
		keys.HalfUp: true, keys.HalfDown: true, keys.PageUp: true, keys.PageDown: true,
		keys.ScreenTop: true, keys.ScreenMid: true, keys.ScreenBot: true,
		keys.BrowserUp: true,
	}

	// The editor's two ways out are written into the card by hand rather than resolved
	// from the registry — they are drawn as the leader chord and as the remote editor's
	// own ":q", which is what the hand actually does — so the tables cannot name them.
	editorExempt := map[keys.Action]bool{
		keys.LeaderKey: true, keys.EditorLeave: true,
	}

	for _, tc := range []struct {
		layer  keys.Layer
		specs  []spec
		card   []keys.Action
		exempt map[keys.Action]bool
	}{
		{keys.Browser, browserSpecs, browserHelpActions, motions},
		{keys.Editor, editorSpecs, editorHelpActions, editorExempt},
	} {
		t.Run(tc.layer.String(), func(t *testing.T) {
			inPalette := map[keys.Action]bool{}
			for _, sp := range tc.specs {
				inPalette[sp.id] = true
			}

			inCard := map[keys.Action]bool{}
			for _, a := range tc.card {
				inCard[a] = true
			}

			for _, b := range keys.Defaults().Layer(tc.layer, true) {
				if tc.exempt[b.Action] || inPalette[b.Action] || inCard[b.Action] {
					continue
				}
				t.Errorf("%s is bound in the %s layer but appears in neither the card nor the palette",
					b.Action, tc.layer)
			}
		})
	}
}
