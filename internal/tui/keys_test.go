package tui

import (
	"fmt"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"hop/internal/store"
)

// key builds the tea.KeyMsg whose String() is name.
func key(t *testing.T, name string) tea.KeyMsg {
	t.Helper()
	switch name {
	case "ctrl+o":
		return tea.KeyMsg{Type: tea.KeyCtrlO}
	case "ctrl+d":
		return tea.KeyMsg{Type: tea.KeyCtrlD}
	case "ctrl+u":
		return tea.KeyMsg{Type: tea.KeyCtrlU}
	case "ctrl+f":
		return tea.KeyMsg{Type: tea.KeyCtrlF}
	case "ctrl+b":
		return tea.KeyMsg{Type: tea.KeyCtrlB}
	case "up":
		return tea.KeyMsg{Type: tea.KeyUp}
	case "down":
		return tea.KeyMsg{Type: tea.KeyDown}
	case "left":
		return tea.KeyMsg{Type: tea.KeyLeft}
	case "right":
		return tea.KeyMsg{Type: tea.KeyRight}
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}
	case "esc":
		return tea.KeyMsg{Type: tea.KeyEsc}
	default:
		if len([]rune(name)) != 1 {
			t.Fatalf("key: unsupported name %q", name)
		}
		return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(name)}
	}
}

// newNavModel builds a model in navigation mode with n hosts in the filtered
// list and a viewport where listRows() == 15.
func newNavModel(n int) *model {
	filtered := make([]int, n)
	for i := range filtered {
		filtered[i] = i
	}
	return &model{filtered: filtered, height: 20}
}

func TestNavVimMotions(t *testing.T) {
	cases := []struct {
		name string
		keys []string
		want int
	}{
		{"j moves down", []string{"j"}, 1},
		{"k clamps at top", []string{"k", "k"}, 0},
		{"j clamps at bottom", []string{"G", "j"}, 29},
		{"G jumps to last", []string{"G"}, 29},
		{"gg jumps to first", []string{"G", "g", "g"}, 0},
		{"lone g is inert", []string{"j", "j", "g"}, 2},
		{"g then other key cancels", []string{"G", "g", "j", "g"}, 29},
		{"H jumps to first", []string{"G", "H"}, 0},
		{"L jumps to last", []string{"L"}, 29},
		{"M jumps to middle", []string{"M"}, 15},
		{"ctrl+d half page", []string{"ctrl+d"}, 7},
		{"ctrl+u half page back", []string{"G", "ctrl+u"}, 22},
		{"ctrl+f full page", []string{"ctrl+f"}, 15},
		{"ctrl+b full page back", []string{"G", "ctrl+b"}, 14},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := newNavModel(30)
			for _, k := range tc.keys {
				m.handleKey(key(t, k))
			}
			if m.cursor != tc.want {
				t.Fatalf("cursor = %d, want %d", m.cursor, tc.want)
			}
		})
	}
}

// An empty host list must not drive the cursor negative.
func TestNavMotionsOnEmptyList(t *testing.T) {
	for _, k := range []string{"G", "L", "M", "j", "ctrl+d", "ctrl+f"} {
		t.Run(k, func(t *testing.T) {
			m := newNavModel(0)
			m.handleKey(key(t, k))
			if m.cursor != 0 {
				t.Fatalf("cursor = %d, want 0", m.cursor)
			}
		})
	}
}

// left/h/esc are the back key in navigation mode: they drop the details view.
func TestNavBackKeys(t *testing.T) {
	for _, k := range []string{"esc", "left", "h"} {
		t.Run(k, func(t *testing.T) {
			// Navigation mode showing a host's details: active is set, but
			// neither browsing nor focused (those modes swallow keys first).
			m := newNavModel(30)
			m.active = "web1"
			m.status = "connected to web1"

			m.handleKey(key(t, k))

			if m.active != "" {
				t.Fatalf("active = %q, want empty", m.active)
			}
			if m.status != "" {
				t.Fatalf("status = %q, want empty", m.status)
			}
		})
	}
}

// enter/right/l are the forward key: with no host under the cursor they must be
// inert rather than panic.
func TestNavForwardKeysOnEmptyList(t *testing.T) {
	for _, k := range []string{"enter", "right", "l"} {
		t.Run(k, func(t *testing.T) {
			m := newNavModel(0)
			m.connecting = map[string]bool{}
			if _, cmd := m.handleKey(key(t, k)); cmd != nil {
				t.Fatal("got a connect command for an empty list, want none")
			}
		})
	}
}

// newPaneModel builds a model focused on a terminal pane. The session map is
// empty, so key forwarding is a no-op and we can assert purely on mode changes.
func newPaneModel() *model {
	return &model{
		active:   "web1",
		focused:  true,
		sessions: map[string]*session{},
		height:   20,
	}
}

func TestPaneDoubleEscLeaves(t *testing.T) {
	m := newPaneModel()

	m.handleKey(key(t, "esc"))
	if !m.focused {
		t.Fatal("a single esc left the pane, want it forwarded to the shell")
	}
	if m.lastEsc.IsZero() {
		t.Fatal("first esc did not arm the double-esc window")
	}

	m.handleKey(key(t, "esc"))
	if m.focused {
		t.Fatal("double esc did not leave the pane")
	}
	if !m.lastEsc.IsZero() {
		t.Fatal("lastEsc not reset on leaving the pane")
	}
}

// Two escs further apart than the window are two independent escapes for the
// remote shell, not a "leave" chord.
func TestPaneSlowEscsStayInPane(t *testing.T) {
	m := newPaneModel()

	m.handleKey(key(t, "esc"))
	m.lastEsc = time.Now().Add(-2 * doubleEscWindow) // as if the user paused
	m.handleKey(key(t, "esc"))

	if !m.focused {
		t.Fatal("two slow escs left the pane, want both forwarded to the shell")
	}
	if m.lastEsc.IsZero() {
		t.Fatal("the second esc should re-arm the window")
	}
}

// An intervening key breaks the sequence: esc, j, esc is not a double-esc.
func TestPaneEscSequenceBrokenByOtherKey(t *testing.T) {
	m := newPaneModel()

	m.handleKey(key(t, "esc"))
	m.handleKey(key(t, "j"))
	if !m.lastEsc.IsZero() {
		t.Fatal("an intervening key did not clear the pending esc")
	}

	m.handleKey(key(t, "esc"))
	if !m.focused {
		t.Fatal("esc-j-esc left the pane, want it treated as two lone escs")
	}
}

// ctrl+o still leaves the pane, and clears any half-finished esc chord.
func TestPaneCtrlOLeaves(t *testing.T) {
	m := newPaneModel()

	m.handleKey(key(t, "esc"))
	m.handleKey(key(t, "ctrl+o"))

	if m.focused {
		t.Fatal("ctrl+o did not leave the pane")
	}
	if !m.lastEsc.IsZero() {
		t.Fatal("lastEsc not reset on leaving the pane")
	}
}

// The browser leaves on the same two chords the pane does: a fast double esc...
func TestBrowsingDoubleEscLeaves(t *testing.T) {
	m := newBrowseModel()

	m.handleKey(key(t, "esc"))
	if !m.browsing {
		t.Fatal("a lone esc left the browser, want it to only arm the window")
	}

	m.handleKey(key(t, "esc"))
	if m.browsing {
		t.Fatal("double esc did not leave the browser")
	}
	if !m.lastEsc.IsZero() {
		t.Fatal("lastEsc not reset on leaving the browser")
	}
}

// ...and any other key in between breaks the chord, as in the pane.
func TestBrowsingEscOtherEscIsNotAChord(t *testing.T) {
	m := newBrowseModel()

	m.handleKey(key(t, "esc"))
	m.handleKey(key(t, "j"))
	m.handleKey(key(t, "esc"))

	if !m.browsing {
		t.Fatal("esc-j-esc left the browser, want it treated as two lone escs")
	}
}

// A slow second esc is two lone escapes, not a chord.
func TestBrowsingSlowDoubleEscStays(t *testing.T) {
	m := newBrowseModel()

	m.handleKey(key(t, "esc"))
	m.lastEsc = time.Now().Add(-2 * doubleEscWindow)
	m.handleKey(key(t, "esc"))

	if !m.browsing {
		t.Fatal("a slow double esc left the browser, want the window to have expired")
	}
}

func TestBrowsingCtrlOLeaves(t *testing.T) {
	m := newBrowseModel()

	m.handleKey(key(t, "esc"))
	m.handleKey(key(t, "ctrl+o"))

	if m.browsing {
		t.Fatal("ctrl+o did not leave the browser")
	}
	if !m.lastEsc.IsZero() {
		t.Fatal("lastEsc not reset on leaving the browser")
	}
}

// newBrowseModel builds a model in browsing mode with no live session, so keys
// that would reach the browser are simply dropped.
func newBrowseModel() *model {
	return &model{active: "web1", browsing: true, sessions: map[string]*session{}, height: 20}
}

// ---- recent directories in the sidebar ----

// newDirModel builds a navigation-mode model with n hosts, where the host at
// index hostIdx has the given recent directories. The dir cache is primed
// directly so no store (and no database) is needed.
func newDirModel(n, hostIdx int, dirs ...string) *model {
	m := newNavModel(n)
	m.hosts = make([]store.Host, n)
	for i := range m.hosts {
		m.hosts[i] = store.Host{Alias: fmt.Sprintf("host%d", i)}
	}
	m.dirCursor = -1
	m.dirs = map[string][]store.Dir{}
	if hostIdx < n {
		ds := make([]store.Dir, len(dirs))
		for i, p := range dirs {
			ds[i] = store.Dir{Path: p, Visits: 1}
		}
		m.dirs[m.hosts[hostIdx].Alias] = ds
	}
	return m
}

// j walks off the host row into its directories, then on to the next host.
func TestDirCursorWalksThroughDirs(t *testing.T) {
	m := newDirModel(3, 0, "/a", "/b")

	if m.dirIdx() != -1 {
		t.Fatalf("dirIdx = %d, want -1 (host row selected)", m.dirIdx())
	}

	m.handleKey(key(t, "j"))
	if m.cursor != 0 || m.dirIdx() != 0 {
		t.Fatalf("after 1×j: cursor=%d dirIdx=%d, want 0/0", m.cursor, m.dirIdx())
	}
	m.handleKey(key(t, "j"))
	if m.cursor != 0 || m.dirIdx() != 1 {
		t.Fatalf("after 2×j: cursor=%d dirIdx=%d, want 0/1", m.cursor, m.dirIdx())
	}
	// Dirs exhausted: fall through to the next host's row.
	m.handleKey(key(t, "j"))
	if m.cursor != 1 || m.dirIdx() != -1 {
		t.Fatalf("after 3×j: cursor=%d dirIdx=%d, want 1/-1", m.cursor, m.dirIdx())
	}
}

// k off a host row lands on the previous host's last directory, not its header.
func TestDirCursorWalksBackUpIntoDirs(t *testing.T) {
	m := newDirModel(3, 0, "/a", "/b")
	m.cursor = 1

	m.handleKey(key(t, "k"))
	if m.cursor != 0 || m.dirIdx() != 1 {
		t.Fatalf("cursor=%d dirIdx=%d, want 0/1 (last dir of host0)", m.cursor, m.dirIdx())
	}
	m.handleKey(key(t, "k"))
	if m.dirIdx() != 0 {
		t.Fatalf("dirIdx = %d, want 0", m.dirIdx())
	}
	m.handleKey(key(t, "k"))
	if m.cursor != 0 || m.dirIdx() != -1 {
		t.Fatalf("cursor=%d dirIdx=%d, want 0/-1 (back on the host row)", m.cursor, m.dirIdx())
	}
}

// A stale dirCursor pointing past the end of the current host's dirs reads as
// "host row selected" rather than indexing out of range.
func TestDirIdxIgnoresOutOfRangeCursor(t *testing.T) {
	m := newDirModel(3, 0, "/a")
	m.dirCursor = 7

	if m.dirIdx() != -1 {
		t.Fatalf("dirIdx = %d, want -1", m.dirIdx())
	}
	// And j descends into the first directory, as it would from any host row,
	// rather than indexing out of range.
	m.handleKey(key(t, "j"))
	if m.cursor != 0 || m.dirIdx() != 0 {
		t.Fatalf("cursor=%d dirIdx=%d, want 0/0", m.cursor, m.dirIdx())
	}
}

// The jump motions always land on a host row, never mid-directory-list.
func TestJumpMotionsResetDirCursor(t *testing.T) {
	for _, k := range []string{"G", "H", "M", "L", "ctrl+d", "ctrl+u", "ctrl+f", "ctrl+b"} {
		t.Run(k, func(t *testing.T) {
			m := newDirModel(3, 0, "/a", "/b")
			m.dirCursor = 1

			m.handleKey(key(t, k))
			if m.dirIdx() != -1 {
				t.Fatalf("dirIdx = %d after %q, want -1", m.dirIdx(), k)
			}
		})
	}
}

// The back keys step out of the directory list before they drop the details
// view, so one press never does both.
func TestBackKeyLeavesDirListFirst(t *testing.T) {
	for _, k := range []string{"esc", "left", "h"} {
		t.Run(k, func(t *testing.T) {
			m := newDirModel(3, 0, "/a", "/b")
			m.dirCursor = 1
			m.active = "host0"

			m.handleKey(key(t, k))
			if m.dirIdx() != -1 {
				t.Fatalf("dirIdx = %d, want -1", m.dirIdx())
			}
			if m.active != "host0" {
				t.Fatalf("active = %q, want it untouched by the first back press", m.active)
			}

			// A second press now leaves the details view.
			m.handleKey(key(t, k))
			if m.active != "" {
				t.Fatalf("active = %q, want empty", m.active)
			}
		})
	}
}

// enter on a directory of a host with a live pane types a cd instead of
// reconnecting. With no pane the model parks the directory for the shell that
// the returned command will open.
func TestEnterOnDirDefersCD(t *testing.T) {
	for _, k := range []string{"enter", "right", "l"} {
		t.Run(k, func(t *testing.T) {
			m := newDirModel(3, 0, "/srv/app")
			m.dirCursor = 0
			m.sessions = map[string]*session{}
			m.connecting = map[string]bool{}

			_, cmd := m.handleKey(key(t, k))
			if cmd == nil {
				t.Fatal("no connect command issued")
			}
			if got := m.pendingCD["host0"]; got != "/srv/app" {
				t.Fatalf("pendingCD[host0] = %q, want %q", got, "/srv/app")
			}
			if !m.connecting["host0"] {
				t.Fatal("host0 not marked as connecting")
			}
		})
	}
}

// x only forgets directories; on a host row it explains itself rather than
// silently doing nothing (or, worse, deleting the host).
func TestForgetKeyOnHostRowIsInert(t *testing.T) {
	m := newDirModel(3, 0, "/a")

	m.handleKey(key(t, "x"))

	if m.status == "" {
		t.Fatal("x on a host row gave no feedback")
	}
	if got := m.dirs["host0"]; len(got) != 1 {
		t.Fatalf("x on a host row dropped a directory: %v", got)
	}
}

// Only the host under the cursor contributes directory rows.
func TestSidebarRowsExpandOnlyCursorHost(t *testing.T) {
	m := newDirModel(3, 1, "/a", "/b")
	m.cursor = 1

	rows := m.sidebarRows()
	if len(rows) != 5 {
		t.Fatalf("got %d rows, want 5 (3 hosts + 2 dirs)", len(rows))
	}
	for i, want := range []bool{false, false, true, true, false} {
		if isDir := rows[i].dir != nil; isDir != want {
			t.Fatalf("row %d: dir=%v, want %v", i, isDir, want)
		}
	}
	if !rows[3].last {
		t.Fatal("final directory not flagged as last (it draws the └ glyph)")
	}
	if rows[2].last {
		t.Fatal("first of two directories flagged as last")
	}
}

// A "g" typed into the filter is literal text, not the start of a "gg" motion.
func TestFilterSwallowsG(t *testing.T) {
	m := newNavModel(30)
	m.filtering = true
	m.handleKey(key(t, "g"))

	if m.filter != "g" {
		t.Fatalf("filter = %q, want %q", m.filter, "g")
	}
	if m.pendingG {
		t.Fatal("pendingG set while filtering, want false")
	}
}
