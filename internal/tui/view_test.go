package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"hop/internal/store"
)

// viewModel builds a laid-out model with a handful of hosts, ready to render.
func viewModel(w, h int) *model {
	m := &model{hosts: []store.Host{
		{Alias: "web1", HostName: "web1.example.com", User: "deploy", Port: 22, Visits: 3},
		{Alias: "raspberrypi", HostName: "pi", User: "parndt", IdentityFile: "~/.ssh/id_ed25519"},
		{Alias: "db-primary", HostName: "10.0.0.9", User: "root", Port: 2222, Group: "prod"},
	}, sessions: map[string]*session{}, connecting: map[string]bool{}, highlights: map[int][]int{}, layout: layout{width: w, height: h, ready: true}}
	m.applyFilter()
	m.recomputeLayout()
	return m
}

// The screen is exactly the window in every mode and at every size.
func TestViewFitsTheWindow(t *testing.T) {
	sizes := []struct{ w, h int }{
		{200, 40}, // wide enough for all three columns, and for a split beside them
		{120, 34}, // a comfortable terminal
		{80, 24},  // the classic one
		{60, 16},  // narrow: the sidebar is at its floor
		{40, 10},  // absurd, and still not allowed to spill
	}
	modes := map[string]func(m *model){
		"details":  func(m *model) { m.cursor = 1 },
		"filter":   func(m *model) { m.filtering = true; m.filter = "pi"; m.applyFilter() },
		"help":     func(m *model) { m.help = true },
		"settings": func(m *model) { m.openSettings() },
		"tunnel manager": func(m *model) {
			m.hosts[0].Forwards = []store.Forward{{ID: 1, Kind: store.ForwardLocal, BindHost: "127.0.0.1", BindPort: 15432, TargetHost: "db.internal", TargetPort: 5432}}
			m.openTunnels(m.hosts[0])
		},
		"tunnel editor": func(m *model) {
			m.openTunnels(m.hosts[0])
			m.handleKey(key(t, "a"))
		},
		"connecting": func(m *model) {
			m.connecting["raspberrypi"] = true
			m.setStatus(statusErr, "connect web1 failed: dial tcp: connection refused")
		},
		"dropped": func(m *model) {
			m.sessions["web1"] = &session{dead: true, lostWhy: "ssh: unexpected packet in response to channel open"}
			m.active, m.mode = "web1", modeShell
		},
		"no hosts":          func(m *model) { m.hosts = nil; m.applyFilter() },
		"sidebar collapsed": func(m *model) { m.toggleSidebar() },
		"sftp column": func(m *model) {
			m.sessions["web1"] = &session{browser: fakeBrowser(t, "/srv")}
			m.active, m.mode = "web1", modeBrowser
			m.relayout()
		},
		"split editors": func(m *model) {
			s := &session{browser: fakeBrowser(t, "/srv"), editors: []*editorTab{
				{id: 1, name: "a.conf", path: "/etc/a.conf", pane: fakePane()},
				{id: 2, name: "b.conf", path: "/etc/b.conf", pane: fakePane()},
			}}
			t.Cleanup(s.closeEditors)
			s.openSplit()
			s.splitEd = 1
			m.sessions["web1"] = s
			m.active, m.mode = "web1", modeEditor
			m.relayout()
		},
	}

	for name, setup := range modes {
		for _, sz := range sizes {
			t.Run(name, func(t *testing.T) {
				m := viewModel(sz.w, sz.h)
				setup(m)

				lines := strings.Split(m.View(), "\n")
				if len(lines) != sz.h {
					t.Fatalf("%dx%d: view is %d lines, want %d", sz.w, sz.h, len(lines), sz.h)
				}
				for i, ln := range lines {
					if got := lipgloss.Width(ln); got > sz.w {
						t.Fatalf("%dx%d: line %d is %d cells wide, want at most %d:\n%q",
							sz.w, sz.h, i, got, sz.w, ln)
					}
				}
			})
		}
	}
}

// lipgloss would wrap an over-wide scrollback line and grow the screen past the window.
func TestPaneContentWiderThanTheBoxDoesNotGrowTheScreen(t *testing.T) {
	m := viewModel(100, 20)
	m.active = "web1"
	m.mode = modeShell

	// A pane laid out for a wider window, as a resize leaves behind in scrollback.
	wide := strings.Repeat("x", m.paneW*2-1)
	pane := fakePaneWith(t, m.paneW*2, m.paneH, wide+"\r\n"+wide, wide)
	m.sessions["web1"] = &session{shells: []*shellTab{{id: 1, pane: pane}}}

	lines := strings.Split(m.View(), "\n")
	if len(lines) != 20 {
		t.Fatalf("view is %d lines, want 20 — the over-wide rows wrapped and grew the pane", len(lines))
	}
	for i, ln := range lines {
		if got := lipgloss.Width(ln); got > 100 {
			t.Fatalf("line %d is %d cells wide, want at most 100", i, got)
		}
	}
}

func TestPanesFillTheWidth(t *testing.T) {
	m := viewModel(120, 34)
	m.cursor = 0

	body := strings.Split(m.renderList(m.frame.list.w, m.frame.list.h), "\n")[0] +
		strings.Split(m.renderRight(m.frame.content.h), "\n")[0]

	if got := lipgloss.Width(body); got != 120 {
		t.Fatalf("the two panes are %d cells wide, want the full window (120)", got)
	}
}

func TestThreeColumnsFillTheWidth(t *testing.T) {
	m, _ := columnModel(t, 200, 34)

	body := strings.Split(m.renderList(m.frame.list.w, m.frame.list.h), "\n")[0] +
		strings.Split(m.renderTree(m.frame.tree), "\n")[0] +
		strings.Split(m.renderRight(m.frame.content.h), "\n")[0]

	if got := lipgloss.Width(body); got != 200 {
		t.Fatalf("the three columns are %d cells wide, want the full window (200)", got)
	}
}

func TestFilterRecordsMatchedCharacters(t *testing.T) {
	m := viewModel(80, 24)
	m.filter = "rpi"
	m.applyFilter()

	if len(m.filtered) == 0 {
		t.Fatal("no host matched \"rpi\", want raspberrypi")
	}
	idx := m.filtered[0]
	if alias := m.hosts[idx].Alias; alias != "raspberrypi" {
		t.Fatalf("first match is %q, want raspberrypi", alias)
	}

	hits := m.highlights[idx]
	if len(hits) != 3 {
		t.Fatalf("highlights = %v, want one offset per matched character", hits)
	}
	alias := m.hosts[idx].Alias
	for _, at := range hits {
		if at < 0 || at >= len(alias) {
			t.Fatalf("offset %d is outside %q", at, alias)
		}
		if !strings.ContainsRune("rpi", rune(alias[at])) {
			t.Fatalf("offset %d is %q, which is not in the filter", at, alias[at])
		}
	}
}

func TestClearingTheFilterClearsHighlights(t *testing.T) {
	m := viewModel(80, 24)
	m.filter = "pi"
	m.applyFilter()
	if len(m.highlights) == 0 {
		t.Fatal("a filter with matches recorded no highlights")
	}

	m.filter = ""
	m.applyFilter()
	if len(m.highlights) != 0 {
		t.Fatalf("highlights = %v after clearing the filter, want none", m.highlights)
	}
}

func TestStatusExpiryOnlyRetiresItsOwnMessage(t *testing.T) {
	m := viewModel(80, 24)
	m.setStatus(statusOK, "connected to web1")
	stale := m.statusGen

	m.setStatus(statusErr, "connect db-primary failed")
	m.Update(statusExpiredMsg{gen: stale})
	if m.status == "" {
		t.Fatal("the first status's timer cleared the message that replaced it")
	}

	m.Update(statusExpiredMsg{gen: m.statusGen})
	if m.status != "" {
		t.Fatalf("status = %q, want the message to have been retired", m.status)
	}
}

func TestReportingAStatusArmsItsExpiry(t *testing.T) {
	m := viewModel(80, 24)
	m.cursor = 0

	if _, cmd := m.Update(statusExpiredMsg{gen: 0}); cmd != nil {
		t.Fatal("a message that reports nothing armed an expiry")
	}

	_, cmd := m.Update(key(t, "s")) // no live session: reports "no live session for web1"
	if m.status == "" {
		t.Fatal("s on an unconnected host reported nothing")
	}
	if cmd == nil {
		t.Fatal("a status went up with nothing armed to take it down")
	}
}

func TestSpinnerStopsWhenNothingIsConnecting(t *testing.T) {
	m := viewModel(80, 24)
	m.ticking = true

	_, cmd := m.Update(tickMsg{})
	if cmd != nil {
		t.Fatal("the spinner kept ticking with no connect in flight")
	}
	if m.ticking {
		t.Fatal("ticking still set after the clock stopped; a later connect would not restart it")
	}

	m.connecting["web1"] = true
	if _, cmd := m.Update(tickMsg{}); cmd == nil {
		t.Fatal("the spinner stopped while a connect was still in flight")
	}
}

func TestNewShellFromAFocusedPane(t *testing.T) {
	m := viewModel(120, 34)
	m.notify = make(chan struct{}, 1)
	m.sessions["web1"] = &session{}
	m.active, m.mode = "web1", modeShell

	if foot := m.renderFooter(); !strings.Contains(foot, "leader") {
		t.Fatalf("the focused pane's footer does not name the leader:\n%s", foot)
	}
	m.handleKey(tea.KeyMsg{Type: tea.KeyCtrlO})
	if foot := m.renderFooter(); !strings.Contains(foot, "new shell") {
		t.Fatalf("the open leader's footer does not name the new-shell key:\n%s", foot)
	}
	m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'z'}}) // close it again

	altZero := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("0"), Alt: true}
	_, cmd := m.Update(altZero)
	if cmd == nil {
		t.Fatal("alt+0 in a focused pane started no shell")
	}
	if !m.connecting["web1"] {
		t.Fatal("alt+0 did not open the shell on the host the pane is on")
	}
}

// Two connects in flight must share one clock, or the spinner runs at double speed.
func TestOneSpinnerClockAtATime(t *testing.T) {
	m := viewModel(80, 24)
	m.notify = make(chan struct{}, 1)

	if cmd := m.openShell(m.hosts[0], false); cmd == nil {
		t.Fatal("the first connect started no clock")
	}
	if !m.ticking {
		t.Fatal("the first connect did not mark the clock as running")
	}

	m.ticking = true
	before := m.ticking
	m.openShell(m.hosts[2], false)
	if m.ticking != before {
		t.Fatal("the second connect disturbed the running clock")
	}
}
