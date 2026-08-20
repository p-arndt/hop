package tui

import (
	"fmt"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/lipgloss"

	"hop/internal/config"
	"hop/internal/store"
	"hop/internal/terminal"
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

// ---- characterization ----

// footerState is one row of the characterization set: a name for the arm it pins down,
// and the model that lands on it.
type footerState struct {
	name  string
	build func() *model
}

// footerStates is one state per arm of the legend, plus the edge conditions that live
// inside an arm: an importer on its first run, a shell with a second tab, a list cursor
// standing on a dropped session.
//
// It is deliberately exhaustive rather than representative. The legend is a first-match
// walk over ordered arms, so an arm that stops matching does not fail loudly — it falls
// through to the next one and quietly shows the legend for a mode you are not in.
func footerStates(t *testing.T) []footerState {
	t.Helper()
	base := func() *model { m, _ := statusModel(t, 120, 34); m.active, m.mode = "", modeList; return m }
	shell := func() (*model, *session) { m, s := statusModel(t, 120, 34); m.mode = modeShell; return m, s }

	return []footerState{
		{"card/auth", func() *model { m := base(); m.auth.open = true; return m }},
		{"card/guidance", func() *model { m := base(); m.guidance.open = true; return m }},
		{"card/help", func() *model { m := base(); m.help = true; return m }},
		{"card/hostkey", func() *model { m := base(); m.hostKey.open = true; return m }},
		{"card/confirm", func() *model { m := base(); m.confirm.open = true; return m }},
		{"card/palette", func() *model { m := base(); m.palette.open = true; return m }},
		{"card/menu", func() *model { m := base(); m.menu.open = true; return m }},
		{"card/hostform", func() *model { m := base(); m.hostForm.open = true; return m }},
		{"card/importer", func() *model { m := base(); m.importer.open = true; return m }},
		{"card/importer first run", func() *model {
			m := base()
			m.importer.open, m.importer.first = true, true
			return m
		}},
		{"card/tunnels editing", func() *model {
			m := base()
			m.tunnels.open, m.tunnels.editing = true, true
			return m
		}},
		{"card/tunnels list", func() *model { m := base(); m.tunnels.open = true; return m }},
		{"card/settings editing", func() *model {
			m := base()
			m.settings.open, m.settings.editing = true, true
			return m
		}},
		{"card/settings list", func() *model { m := base(); m.settings.open = true; return m }},
		// A card outranks every pane mode, and so does the leader: both are set up from a
		// shell, so it is the arm order rather than the mode that decides the answer.
		{"card/over a shell", func() *model { m, _ := shell(); m.help = true; return m }},
		{"leader/armed", func() *model {
			m, _ := shell()
			m.chords.leaderAlias = "web1"
			return m
		}},
		{"leader/armed with a cwd", func() *model {
			m, s := shell()
			p, _ := cwdPane(t, "/srv/app")
			s.shells = []*shellTab{{id: 1, pane: p}}
			m.chords.leaderAlias = "web1"
			return m
		}},

		{"mode/dead pane", func() *model { m, s := shell(); s.dead = true; return m }},
		{"mode/editor", func() *model {
			m, s := statusModel(t, 120, 34)
			m.mode = modeEditor
			s.editors = []*editorTab{{id: 1, name: "a.conf", path: "/etc/a.conf", pane: fakePane()}}
			return m
		}},
		{"mode/browser", func() *model {
			m, s := statusModel(t, 120, 34)
			m.mode, s.browser = modeBrowser, fakeBrowser(t, "/srv")
			return m
		}},
		{"mode/scrollback", func() *model { m, _ := statusModel(t, 120, 34); m.mode = modeScrollback; return m }},
		{"mode/shell", func() *model { m, _ := shell(); return m }},
		{"mode/shell with two tabs", func() *model {
			m, s := shell()
			s.shells = append(s.shells, &shellTab{id: 2, pane: fakePane()})
			return m
		}},
		{"mode/shell with a cwd", func() *model {
			m, s := shell()
			p, _ := cwdPane(t, "/srv/app")
			s.shells = []*shellTab{{id: 1, pane: p}}
			return m
		}},
		{"mode/shell with scrollback behind it", func() *model {
			m, s := shell()
			s.shells = []*shellTab{{id: 1, pane: scrolledPane(t)}}
			return m
		}},
		{"mode/filtering", func() *model { m := base(); m.filtering = true; return m }},
		{"mode/list", func() *model { return base() }},
		{"mode/list on a pinned host", func() *model {
			m := base()
			m.hosts[0].Pinned = true
			m.applyFilter()
			return m
		}},
		{"mode/list on a dropped session", func() *model {
			m := base()
			h, _ := m.selectedHost()
			m.sessions[h.Alias] = &session{dead: true}
			return m
		}},
		{"mode/empty list", func() *model {
			m := base()
			m.hosts, m.filtered, m.sessions = nil, nil, map[string]*session{}
			return m
		}},

		// The two things layered over whichever arm matched: the collapsed sidebar's hint,
		// prepended to the core, and the guidance profile's trim.
		{"layer/sidebar collapsed in a shell", func() *model { m, _ := shell(); m.sidebarHidden = true; return m }},
		{"layer/sidebar collapsed in the list", func() *model { m := base(); m.sidebarHidden = true; return m }},
		{"layer/guidance keys in a shell", func() *model {
			m, _ := shell()
			m.cfg.Guidance = config.GuidanceKeys
			return m
		}},
		{"layer/guidance guided in a shell", func() *model {
			m, _ := shell()
			m.cfg.Guidance = config.GuidanceGuided
			return m
		}},
		{"layer/guidance guided in the browser", func() *model {
			m, s := statusModel(t, 120, 34)
			m.mode, s.browser = modeBrowser, fakeBrowser(t, "/srv")
			m.cfg.Guidance = config.GuidanceGuided
			return m
		}},
		{"layer/guidance guided in the list", func() *model {
			m := base()
			m.cfg.Guidance = config.GuidanceGuided
			return m
		}},
	}
}

// scrolledPane is a pane with history behind it: enough lines have been printed for rows
// to have left the top of the screen. The scrollback hint is offered only over one of
// these, so a plain fakePane cannot stand in for it.
func scrolledPane(t *testing.T) *terminal.Pane {
	t.Helper()
	p, w := cwdPane(t, "/srv/app")
	for i := 0; i < 40; i++ {
		io.WriteString(w, "line\r\n")
	}
	deadline := time.Now().Add(3 * time.Second)
	for p.ScrollbackLen() == 0 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if p.ScrollbackLen() == 0 {
		t.Fatal("the pane never took anything into scrollback")
	}
	return p
}

// footerShape is one state's whole legend, flattened. The three lists are kept apart so a
// hint that moves between core and extra shows up as a difference rather than cancelling
// itself out — which of the two a hint is in decides whether a narrow window keeps it.
func footerShape(m *model) string {
	core, extra, help := m.footerHints()
	return "core: " + strings.Join(core, " | ") +
		"\nextra: " + strings.Join(extra, " | ") +
		"\nhelp: " + help
}

// TestFooterHintsDump prints the shape of every state, for regenerating the golden set
// below by hand. Skipped unless asked for: it asserts nothing.
func TestFooterHintsDump(t *testing.T) {
	if os.Getenv("HOP_FOOTER_DUMP") == "" {
		t.Skip("set HOP_FOOTER_DUMP=1 to regenerate the golden set")
	}
	for _, st := range footerStates(t) {
		fmt.Printf("\t%q: %q,\n", st.name, footerShape(st.build()))
	}
}

// footerGolden is the legend every state above produced before the arms became a table.
// It was captured from the switch it replaced, which is the only thing that makes it
// worth anything: the refactor is proven identical rather than assumed identical, and a
// hint that moves, changes wording or changes list fails here rather than on a user's
// screen.
//
// Regenerate with HOP_FOOTER_DUMP=1 go test ./internal/tui -run TestFooterHintsDump -v,
// and only after deciding on purpose that the legend should read differently.
var footerGolden = map[string]string{
	"card/auth":                            "core:  enter  submit |  esc  cancel |  ctrl+u  clear\nextra: \nhelp: ",
	"card/guidance":                        "core:  ↑↓  pick |  enter  start hopping\nextra: \nhelp: ",
	"card/help":                            "core:  esc  close\nextra: \nhelp: ",
	"card/hostkey":                         "core:  y  trust |  n  cancel\nextra: \nhelp: ",
	"card/confirm":                         "core:  y  delete |  n  cancel\nextra: \nhelp: ",
	"card/palette":                         "core:  type  search |  enter  run |  esc  close\nextra: \nhelp: ",
	"card/menu":                            "core:  ↑↓  move |  enter  run |  esc  close\nextra: \nhelp: ",
	"card/hostform":                        "core:  tab  next |  enter  save |  esc  cancel |  ctrl+u  clear\nextra: \nhelp: ",
	"card/importer":                        "core:  enter  import |  esc  cancel |  ctrl+u  clear\nextra: \nhelp: ",
	"card/importer first run":              "core:  enter  import |  esc  skip |  ctrl+u  clear\nextra: \nhelp: ",
	"card/tunnels editing":                 "core:  tab  next |  enter  save |  esc  back |  ctrl+u  clear\nextra: \nhelp: ",
	"card/tunnels list":                    "core:  enter  start / stop |  a  add |  e  edit |  esc  close\nextra: \nhelp: ",
	"card/settings editing":                "core:  enter  save |  esc  cancel |  ctrl+u  clear\nextra: \nhelp: ",
	"card/settings list":                   "core:  enter  edit |  r  reset |  esc  close\nextra: \nhelp: ",
	"card/over a shell":                    "core:  esc  close\nextra: \nhelp: ",
	"leader/armed":                         "core: leader |  o  out |  1-9  tab |  0  new shell |  ctrl+k  actions |  ?  keys | any other key cancels\nextra: \nhelp: ",
	"leader/armed with a cwd":              "core: leader |  o  out |  1-9  tab |  0  new shell |  c  vs code here |  ctrl+k  actions |  ?  keys | any other key cancels\nextra: \nhelp: ",
	"mode/dead pane":                       "core:  r  reconnect |  d  drop session |  ctrl+o  back\nextra: \nhelp:  ?  keys",
	"mode/editor":                          "core:  ctrl+o o  browser |  :q  close |  shift+→  tab\nextra:  alt+t  tree |  ctrl+o 1-9  jump |  ctrl+b  hide hosts\nhelp:  ctrl+o ?  keys",
	"mode/browser":                         "core:  ctrl+o  back |  enter  edit |  d  download\nextra:  tab  focus file |  \\  open beside |  space  mark |  t  target |  c  copy there |  v  move there |  ctrl+k  actions |  ←  up |  a  mark all |  u  upload |  o  open local |  x  delete |  shift+r  rename |  m  mkdir |  s  sort |  r  refresh |  ctrl+t  tree |  ctrl+b  hide hosts\nhelp:  ?  keys",
	"mode/scrollback":                      "core:  esc  back to live |  ↑↓  scroll |  home/end  top/live\nextra:  pgup/pgdn  page\nhelp:  ?  keys",
	"mode/shell":                           "core:  ctrl+o o  back |  ctrl+o  leader\nextra:  esc esc  back |  ctrl+b  hide hosts\nhelp:  ctrl+o ?  keys",
	"mode/shell with two tabs":             "core:  ctrl+o o  back |  ctrl+o  leader |  shift+→  shell\nextra:  ctrl+o 1-9  jump |  esc esc  back |  ctrl+b  hide hosts\nhelp:  ctrl+o ?  keys",
	"mode/shell with a cwd":                "core:  ctrl+o o  back |  ctrl+o  leader\nextra:  ctrl+o c  vs code here |  esc esc  back |  ctrl+b  hide hosts\nhelp:  ctrl+o ?  keys",
	"mode/shell with scrollback behind it": "core:  ctrl+o o  back |  ctrl+o  leader\nextra:  ctrl+o c  vs code here |  shift+↑  scrollback |  esc esc  back |  ctrl+b  hide hosts\nhelp:  ctrl+o ?  keys",
	"mode/filtering":                       "core:  type  filter |  enter  apply |  esc  clear\nextra:  ↑↓  move\nhelp: ",
	"mode/list":                            "core:  enter  connect |  space  actions |  /  filter\nextra:  ctrl+k  search actions |  ↑↓  move |  f  sftp |  a  add |  e  edit |  x  delete |  p  pin |  t  tunnels |  i  import |  ,  settings |  esc esc  quit\nhelp:  ?  keys",
	"mode/list on a pinned host":           "core:  enter  connect |  space  actions |  /  filter\nextra:  ctrl+k  search actions |  ↑↓  move |  f  sftp |  a  add |  e  edit |  x  delete |  p  pin |  t  tunnels |  i  import |  ,  settings |  esc esc  quit |  shift+kshift+j  reorder\nhelp:  ?  keys",
	"mode/list on a dropped session":       "core:  r  reconnect |  enter  connect |  f  sftp\nextra:  d  drop session |  ctrl+k  search actions |  ↑↓  move |  f  sftp |  a  add |  e  edit |  x  delete |  p  pin |  t  tunnels |  i  import |  ,  settings |  esc esc  quit\nhelp:  ?  keys",
	"mode/empty list":                      "core:  a  add host |  i  import\nextra:  ctrl+k  search actions |  ,  settings |  esc esc  quit\nhelp:  ?  keys",
	"layer/sidebar collapsed in a shell":   "core:  ctrl+b  show hosts |  ctrl+o o  back |  ctrl+o  leader\nextra:  esc esc  back |  ctrl+b  show hosts\nhelp:  ctrl+o ?  keys",
	"layer/sidebar collapsed in the list":  "core:  ctrl+b  show hosts |  enter  connect |  space  actions |  /  filter\nextra:  ctrl+k  search actions |  ↑↓  move |  f  sftp |  a  add |  e  edit |  x  delete |  p  pin |  t  tunnels |  i  import |  ,  settings |  esc esc  quit\nhelp:  ?  keys",
	"layer/guidance keys in a shell":       "core:  ctrl+o o  back |  ctrl+o  leader\nextra: \nhelp:  ctrl+o ?  keys",
	"layer/guidance guided in a shell":     "core:  ctrl+o o  back |  ctrl+o  leader |  ctrl+o ctrl+k  actions\nextra:  esc esc  back |  ctrl+b  hide hosts\nhelp:  ctrl+o ?  keys",
	"layer/guidance guided in the browser": "core:  ctrl+o  back |  enter  edit |  d  download |  ctrl+k  actions\nextra:  tab  focus file |  \\  open beside |  space  mark |  t  target |  c  copy there |  v  move there |  ←  up |  a  mark all |  u  upload |  o  open local |  x  delete |  shift+r  rename |  m  mkdir |  s  sort |  r  refresh |  ctrl+t  tree |  ctrl+b  hide hosts\nhelp:  ?  keys",
	"layer/guidance guided in the list":    "core:  enter  connect |  space  actions |  /  filter |  ctrl+k  search actions\nextra:  ↑↓  move |  f  sftp |  a  add |  e  edit |  x  delete |  p  pin |  t  tunnels |  i  import |  ,  settings |  esc esc  quit\nhelp:  ?  keys",
}

// The characterization: every state the legend has an arm for still reads exactly as it
// did. Nothing here says what the legend *should* say — that is the other tests' job.
// This one says only that it has not moved.
func TestFooterHintsAreUnchanged(t *testing.T) {
	states := footerStates(t)
	if len(states) != len(footerGolden) {
		t.Fatalf("%d states against %d golden entries: a state was added or dropped without its golden",
			len(states), len(footerGolden))
	}
	for _, st := range states {
		t.Run(st.name, func(t *testing.T) {
			want, ok := footerGolden[st.name]
			if !ok {
				t.Fatalf("no golden legend for %q", st.name)
			}
			if got := footerShape(st.build()); got != want {
				t.Fatalf("the %s legend changed:\n got:\n%s\nwant:\n%s", st.name, got, want)
			}
		})
	}
}
