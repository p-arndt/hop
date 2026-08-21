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

// footerModes builds one model per mode the footer has a legend for.
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

// No mode's legend runs past four keys; past that the row points at the card instead.
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

// Every mode's legend offers the card, behind the leader where a bare "?" is text.
func TestFooterAlwaysOffersTheCard(t *testing.T) {
	for name, build := range footerModes(t) {
		t.Run(name, func(t *testing.T) {
			m := build()
			if m.filtering {
				if _, _, help := m.footerHints(); help != "" {
					t.Fatalf("the filter legend offers a card key that would be typed into the filter: %q", help)
				}
				return
			}
			if !strings.Contains(m.renderFooter(), "keys") {
				t.Fatalf("the %s legend does not offer the help card:\n%s", name, m.renderFooter())
			}
			wantChord := (m.editing() || m.mode == modeShell) && !m.activeDead()
			if got := strings.Contains(m.renderFooter(), "ctrl+o ?"); got != wantChord {
				t.Fatalf("the %s legend offers the chord = %v, want %v — a bare ? is text in a pane that forwards:\n%s",
					name, got, wantChord, m.renderFooter())
			}
		})
	}
}

func TestTheCardOpensFromEveryMode(t *testing.T) {
	for name, build := range footerModes(t) {
		t.Run(name, func(t *testing.T) {
			m := build()
			if m.filtering {
				return // no key to press: "?" is filter text here
			}
			if (m.editing() || m.mode == modeShell) && !m.activeDead() {
				m.handleKey(key(t, "ctrl+o"))
			}
			m.handleKey(key(t, "?"))
			if !m.help {
				t.Fatalf("? did not open the card from %s", name)
			}
		})
	}
}

func TestBareQuestionMarkStaysTextInAShell(t *testing.T) {
	m, _ := statusModel(t, 120, 34)
	m.mode = modeShell
	m.handleKey(key(t, "?"))
	if m.help {
		t.Fatal("a bare ? in a live shell opened the card instead of reaching the remote")
	}
}

// Every legend is one row and fits the window, the classic 80 columns included.
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

// The list legend offers reconnect only on a dropped session, and add/import when empty.
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
			if !strings.Contains(narrow.renderFooter(), stripHint(core[0])) {
				t.Fatalf("the 60-column %s legend dropped its way out (%q):\n%s",
					name, core[0], narrow.renderFooter())
			}
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

// stripHint is a hint's label, the part that survives styling with colour off.
func stripHint(hint string) string {
	parts := strings.Fields(hint)
	if len(parts) == 0 {
		return hint
	}
	return parts[len(parts)-1]
}

// One esc only drops the selected host; the second quits.
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

// footerState names one arm of the legend and the model that lands on it.
type footerState struct {
	name  string
	build func() *model
}

// footerStates is one state per arm; exhaustive because the arms are a first-match walk.
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

// scrolledPane is a pane with rows already pushed into scrollback.
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

// footerShape flattens one state's legend, keeping core, extra and help apart.
func footerShape(m *model) string {
	core, extra, help := m.footerHints()
	return "core: " + strings.Join(core, " | ") +
		"\nextra: " + strings.Join(extra, " | ") +
		"\nhelp: " + help
}

// Prints every state's legend for regenerating the golden set; asserts nothing.
func TestFooterHintsDump(t *testing.T) {
	if os.Getenv("HOP_FOOTER_DUMP") == "" {
		t.Skip("set HOP_FOOTER_DUMP=1 to regenerate the golden set")
	}
	for _, st := range footerStates(t) {
		fmt.Printf("\t%q: %q,\n", st.name, footerShape(st.build()))
	}
}

// footerGolden is the legend each state produced before the arms became a table.
// Regenerate with HOP_FOOTER_DUMP=1 go test ./internal/tui -run TestFooterHintsDump -v.
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

// Characterization: every state's legend still reads exactly as it did.
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
