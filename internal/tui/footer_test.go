package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"

	"hop/internal/store"
)

// footerModes puts m into each mode the footer has a legend for, so a rule about the
// legend can be asserted against all of them at once rather than one test per mode.
func footerModes(t *testing.T) map[string]func() *model {
	t.Helper()
	return map[string]func() *model{
		"list": func() *model { m, _ := statusModel(t, 120, 34); m.active, m.mode = "", modeList; return m },
		"empty list": func() *model {
			m, _ := statusModel(t, 120, 34)
			m.active, m.mode = "", modeList
			m.hosts, m.filtered = nil, nil
			return m
		},
		"filter": func() *model {
			m, _ := statusModel(t, 120, 34)
			m.active, m.mode = "", modeList
			m.filtering = true
			return m
		},
		"shell":      func() *model { m, _ := statusModel(t, 120, 34); m.mode = modeShell; return m },
		"scrollback": func() *model { m, _ := statusModel(t, 120, 34); m.mode = modeScrollback; return m },
		"browser": func() *model {
			m, s := statusModel(t, 120, 34)
			m.mode, s.browser = modeBrowser, fakeBrowser(t, "/srv")
			return m
		},
		"editor": func() *model {
			m, s := statusModel(t, 120, 34)
			m.mode = modeEditor
			s.editors = []*editorTab{{id: 1, name: "a.conf", path: "/etc/a.conf", pane: fakePane()}}
			return m
		},
		"dead": func() *model {
			m, s := statusModel(t, 120, 34)
			m.mode, s.dead = modeShell, true
			return m
		},
	}
}

// The rule the whole trim rests on: no mode's legend runs past four keys. The keyboard no
// longer fits a row, so the row stops pretending to be the keyboard and points at the
// card instead.
func TestFooterKeepsToFourKeys(t *testing.T) {
	for name, build := range footerModes(t) {
		t.Run(name, func(t *testing.T) {
			m := build()
			core, _, help := m.footerHints()
			n := len(core)
			if help != "" {
				n++
			}
			if n > 4 {
				t.Fatalf("the %s legend names %d keys, want at most 4:\n%s", name, n, m.renderFooter())
			}
			if n == 0 {
				t.Fatalf("the %s legend is empty", name)
			}
		})
	}
}

// The one hint that makes every other hint optional. It is in every mode's legend, and it
// is reachable by the key the legend names — in a shell that cannot be a bare "?", which
// is text the remote is owed, so there it is the leader's.
func TestFooterAlwaysOffersTheCard(t *testing.T) {
	for name, build := range footerModes(t) {
		t.Run(name, func(t *testing.T) {
			m := build()
			if m.filtering {
				// Every printable key is part of the filter, "?" included, so the legend
				// has no card key to honestly offer.
				if _, _, help := m.footerHints(); help != "" {
					t.Fatalf("the filter legend offers a card key that would be typed into the filter: %q", help)
				}
				return
			}
			if !strings.Contains(m.renderFooter(), "keys") {
				t.Fatalf("the %s legend does not offer the help card:\n%s", name, m.renderFooter())
			}
			// The chord belongs only where a bare "?" would be typed at a remote.
			wantChord := (m.editing() || m.mode == modeShell) && !m.activeDead()
			if got := strings.Contains(m.renderFooter(), "ctrl+o ?"); got != wantChord {
				t.Fatalf("the %s legend offers the chord = %v, want %v — a bare ? is text in a pane that forwards:\n%s",
					name, got, wantChord, m.renderFooter())
			}
		})
	}
}

// And the key works from every one of them: a legend naming a key that does nothing is
// worse than no legend.
func TestTheCardOpensFromEveryMode(t *testing.T) {
	for name, build := range footerModes(t) {
		t.Run(name, func(t *testing.T) {
			m := build()
			if m.filtering {
				return // no key to press: "?" is filter text here
			}
			if (m.editing() || m.mode == modeShell) && !m.activeDead() {
				// Through the leader, since the bare key belongs to the remote.
				m.handleKey(key(t, "ctrl+o"))
			}
			m.handleKey(key(t, "?"))
			if !m.help {
				t.Fatalf("? did not open the card from %s", name)
			}
		})
	}
}

// A shell that has not asked for the keyboard back must not be typed into by accident: in
// a live shell a bare "?" is a question mark, not a card.
func TestBareQuestionMarkStaysTextInAShell(t *testing.T) {
	m, _ := statusModel(t, 120, 34)
	m.mode = modeShell
	m.handleKey(key(t, "?"))
	if m.help {
		t.Fatal("a bare ? in a live shell opened the card instead of reaching the remote")
	}
}

// Whatever is in a legend, it fits the window — the classic 80 columns included, which is
// the width the trim exists for.
func TestFooterFitsTheWindow(t *testing.T) {
	for _, w := range []int{200, 120, 80, 60, 40} {
		for name, build := range footerModes(t) {
			m := build()
			m.width = w
			m.recomputeLayout()
			got := m.renderFooter()
			if n := strings.Count(got, "\n"); n != 0 {
				t.Fatalf("the %s legend at width %d is %d rows, want 1:\n%s", name, w, n+1, got)
			}
			if gw := lipgloss.Width(got); gw > w {
				t.Fatalf("the %s legend at width %d rendered %d wide:\n%s", name, w, gw, got)
			}
		}
	}
}

// The list's legend is about the host under the cursor, so a dropped session there is
// worth a slot — and an empty list, where there is nothing to connect to, spends its slots
// on the two keys that make a list at all.
func TestListFooterFollowsTheCursor(t *testing.T) {
	m, _ := statusModel(t, 120, 34)
	m.active, m.mode = "", modeList

	if got := m.renderFooter(); strings.Contains(got, "reconnect") {
		t.Fatalf("the list legend offers reconnect with no dropped session:\n%s", got)
	}

	h, _ := m.selectedHost()
	m.sessions[h.Alias] = &session{dead: true}
	if got := m.renderFooter(); !strings.Contains(got, "reconnect") {
		t.Fatalf("the list legend does not offer reconnect on a dropped session:\n%s", got)
	}

	m.hosts, m.filtered, m.sessions = []store.Host{}, nil, map[string]*session{}
	got := m.renderFooter()
	if !strings.Contains(got, "add host") || !strings.Contains(got, "import") {
		t.Fatalf("the empty list's legend does not offer the two keys that fill it:\n%s", got)
	}
	if strings.Contains(got, "connect") {
		t.Fatalf("the empty list's legend offers connecting to nothing:\n%s", got)
	}
}

// Collapsed, the way back to the hosts outranks the mode's own keys: with the sidebar gone
// nothing else on screen says it is still there.
func TestSidebarHintLeadsWhileCollapsed(t *testing.T) {
	m, _ := statusModel(t, 120, 34)
	m.mode, m.sidebarHidden = modeShell, true
	got := m.renderFooter()
	if !strings.Contains(got, "show hosts") {
		t.Fatalf("a collapsed sidebar is not offered back in the legend:\n%s", got)
	}
	if i, j := strings.Index(got, "show hosts"), strings.Index(got, "back"); i > j {
		t.Fatalf("the way back to the hosts is not first while collapsed:\n%s", got)
	}
}

// A wide terminal is not made to look like a narrow one: the room a window has goes to
// keys, in priority order, and a window with no room shows only the core.
func TestFooterSpendsTheRoomAWindowHas(t *testing.T) {
	for name, build := range footerModes(t) {
		t.Run(name, func(t *testing.T) {
			narrow, wide := build(), build()
			narrow.width, wide.width = 60, 220
			narrow.recomputeLayout()
			wide.recomputeLayout()

			core, extra, _ := wide.footerHints()
			if len(extra) == 0 {
				return // nothing this mode could add; the core is the whole of it
			}
			if !strings.Contains(wide.renderFooter(), stripHint(extra[0])) {
				t.Fatalf("a 220-column %s legend does not spend its room on %q:\n%s",
					name, extra[0], wide.renderFooter())
			}
			// The narrow one is allowed fewer keys, never more — but never fewer than
			// the way out, which is the first thing each mode's list names.
			if !strings.Contains(narrow.renderFooter(), stripHint(core[0])) {
				t.Fatalf("the 60-column %s legend dropped its way out (%q):\n%s",
					name, core[0], narrow.renderFooter())
			}
			// And what it does show, it shows whole: a legend ending mid-word names no
			// key, so a hint that does not fit is dropped rather than cut.
			for _, w := range strings.Fields(narrow.renderFooter()) {
				if strings.HasSuffix(w, "…") {
					t.Fatalf("the 60-column %s legend cut %q in half:\n%s", name, w, narrow.renderFooter())
				}
			}
			if len(narrow.renderFooter()) > len(wide.renderFooter()) {
				t.Fatalf("the 60-column %s legend is longer than the 220-column one", name)
			}
		})
	}
}

// stripHint is a hint's label, which is the part that survives styling in a test binary
// with colour off and is unique enough to look for.
func stripHint(hint string) string {
	parts := strings.Fields(hint)
	if len(parts) == 0 {
		return hint
	}
	return parts[len(parts)-1]
}

// esc esc leaves hop from the host list, the same "one level out" the panes bind — the
// list being the last level. One esc must not: it drops the host you were reading about,
// and a stray esc is not a quit.
func TestDoubleEscQuitsFromTheList(t *testing.T) {
	m, _ := statusModel(t, 120, 34)
	m.active, m.mode = "web1", modeList

	if _, cmd := m.handleKey(key(t, "esc")); cmd != nil {
		t.Fatal("a single esc in the list quit hop")
	}
	if m.active != "" {
		t.Fatalf("the first esc did not drop the selected host: active = %q", m.active)
	}

	if _, cmd := m.handleKey(key(t, "esc")); cmd == nil {
		t.Fatal("esc esc in the list did not quit hop")
	}
}

// Outside the window the pair is not a pair: two escs far enough apart are two firsts.
func TestSlowDoubleEscDoesNotQuit(t *testing.T) {
	m, _ := statusModel(t, 120, 34)
	m.active, m.mode = "web1", modeList

	m.handleKey(key(t, "esc"))
	m.reader.Reset() // as if the user paused past the chord's window
	if _, cmd := m.handleKey(key(t, "esc")); cmd != nil {
		t.Fatal("two escs outside the window quit hop")
	}
}
