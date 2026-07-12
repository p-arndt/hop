package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"

	"hop/internal/store"
)

// viewModel builds a laid-out model with a handful of hosts, ready to render.
func viewModel(w, h int) *model {
	m := &model{
		hosts: []store.Host{
			{Alias: "web1", HostName: "web1.example.com", User: "deploy", Port: 22, Visits: 3},
			{Alias: "raspberrypi", HostName: "pi", User: "parndt", IdentityFile: "~/.ssh/id_ed25519"},
			{Alias: "db-primary", HostName: "10.0.0.9", User: "root", Port: 2222, Group: "prod"},
		},
		sessions:   map[string]*session{},
		connecting: map[string]bool{},
		highlights: map[int][]int{},
		width:      w,
		height:     h,
		ready:      true,
	}
	m.applyFilter()
	m.recomputeLayout()
	return m
}

// Whatever the window and whatever is up, the screen is exactly the window: every
// line fits across it, and there are exactly as many lines as it is tall. A view
// that overruns either way corrupts the terminal rather than merely looking wrong.
func TestViewFitsTheWindow(t *testing.T) {
	sizes := []struct{ w, h int }{
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
		"connecting": func(m *model) {
			m.connecting["raspberrypi"] = true
			m.setStatus(statusErr, "connect web1 failed: dial tcp: connection refused")
		},
		"no hosts": func(m *model) { m.hosts = nil; m.applyFilter() },
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

// The two panes together fill the width: a gap down the right-hand side is the
// layout arithmetic being off, which is invisible until you look for it.
func TestPanesFillTheWidth(t *testing.T) {
	m := viewModel(120, 34)
	m.cursor = 0

	body := strings.Split(m.renderList(m.listWidth(), m.bodyHeight()), "\n")[0] +
		strings.Split(m.renderRight(m.bodyHeight()), "\n")[0]

	if got := lipgloss.Width(body); got != 120 {
		t.Fatalf("the two panes are %d cells wide, want the full window (120)", got)
	}
}

// The filter records which characters of an alias it matched, so the row can pick
// them out — a fuzzy hit that cannot explain itself looks like a bug.
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
	// Every recorded offset must be inside the alias and on a character the
	// filter actually asked for.
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

// Clearing the filter clears the highlights with it — a stale one would underline
// characters in a list nobody is filtering.
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

// A status retires itself, but only the one its timer was armed for: a message
// that has since been replaced must not take its successor down with it.
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

// Reporting something arms the timer that will take it back down. Without the
// command, a status would sit in the header until the next one displaced it.
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

// The spinner's clock runs only while something is dialing: an idle hop must not
// keep waking up to redraw a screen that is not changing.
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

// Two connects in flight must not run two clocks — the frame counter would
// advance at double speed, and the spinner with it.
func TestOneSpinnerClockAtATime(t *testing.T) {
	m := viewModel(80, 24)
	m.notify = make(chan struct{}, 1)

	if cmd := m.openShell(m.hosts[0], false); cmd == nil {
		t.Fatal("the first connect started no clock")
	}
	if !m.ticking {
		t.Fatal("the first connect did not mark the clock as running")
	}

	// A second host dialing while the first is still in flight rides the clock
	// that is already running.
	m.ticking = true
	before := m.ticking
	m.openShell(m.hosts[2], false)
	if m.ticking != before {
		t.Fatal("the second connect disturbed the running clock")
	}
}
