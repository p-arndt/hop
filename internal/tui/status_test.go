package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
)

// statusModel is viewModel's hosts with a session on web1.
func statusModel(t *testing.T, w, h int) (*model, *session) {
	t.Helper()
	m := viewModel(w, h)
	s := &session{shells: []*shellTab{{id: 1, pane: fakePane()}}}
	t.Cleanup(s.closeShells)
	m.sessions["web1"] = s
	m.active = "web1"
	return m, s
}

// The crumb names the host, the tab in front, and the thing that tab is at.
func TestTheCrumbSaysWhereYouAre(t *testing.T) {
	cases := []struct {
		name  string
		setup func(m *model, s *session)
		want  []string
	}{
		{
			"list", func(m *model, s *session) { m.active, m.mode = "", modeList; m.cursor = 1 },
			[]string{"hosts", "raspberrypi"},
		},
		{
			// A shell that has reported no cwd is named alone rather than with a stale path.
			"shell without a cwd", func(m *model, s *session) { m.mode = modeShell },
			[]string{"web1", "shell 1"},
		},
		{
			"scrollback", func(m *model, s *session) { m.mode = modeScrollback },
			[]string{"web1", "scrollback"},
		},
		{
			"browser", func(m *model, s *session) { m.mode = modeBrowser; s.browser = fakeBrowser(t, "/srv/www") },
			[]string{"web1", "files", "/srv/www"},
		},
		{
			"editor", func(m *model, s *session) {
				m.mode = modeEditor
				s.editors = []*editorTab{{id: 1, name: "nginx.conf", path: "/etc/nginx/nginx.conf", pane: fakePane()}}
			},
			[]string{"web1", "nginx.conf", "/etc/nginx"},
		},
		{
			"dead", func(m *model, s *session) { m.mode = modeShell; s.dead = true },
			[]string{"web1", "disconnected"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m, s := statusModel(t, 160, 34)
			tc.setup(m, s)
			got := m.renderFooter()
			for _, want := range tc.want {
				if !strings.Contains(got, want) {
					t.Fatalf("the footer does not say %q:\n%s", want, got)
				}
			}
		})
	}
}

func TestTheCrumbFollowsTheRemoteDirectory(t *testing.T) {
	m, _ := vscodeModel(t, 1, "/srv/app/releases/current")
	m.mode, m.width = modeShell, 160

	if got := m.renderFooter(); !strings.Contains(got, "/srv/app/releases/current") {
		t.Fatalf("the footer does not carry the shell's cwd:\n%s", got)
	}
}

// Which of several shells is up is in the crumb, numbered as the tab row numbers shells.
func TestTheCrumbNamesTheShell(t *testing.T) {
	m, s := statusModel(t, 160, 34)
	s.shells = append(s.shells, &shellTab{id: 2, pane: fakePane()})
	s.activeSh, m.mode = 1, modeShell

	if got := m.renderFooter(); !strings.Contains(got, "shell 2") {
		t.Fatalf("the footer does not say which shell is up:\n%s", got)
	}
}

// A transient status takes the crumb's place while it lasts, and the crumb comes back after.
func TestAStatusTakesTheCrumbsPlace(t *testing.T) {
	m, s := statusModel(t, 160, 34)
	m.mode = modeBrowser
	s.browser = fakeBrowser(t, "/srv/www")

	m.setStatus(statusOK, "downloaded app.conf")
	got := m.renderFooter()
	if !strings.Contains(got, "✓ downloaded app.conf") || strings.Contains(got, "/srv/www") {
		t.Fatalf("the footer = %q, want the status in the crumb's place", got)
	}

	m.clearStatus()
	if got := m.renderFooter(); !strings.Contains(got, "/srv/www") {
		t.Fatalf("the crumb did not come back after the status: %q", got)
	}
}

// Exactly one row of the window's width; a wrap would push the footer off screen.
func TestTheFooterFitsOneRow(t *testing.T) {
	long := "/srv/www/example.com/releases/20240115T101500Z/vendor/bundle/ruby/3.2.0/gems"
	for _, w := range []int{200, 120, 80, 60, 40, 20} {
		m, s := statusModel(t, w, 24)
		m.mode = modeBrowser
		s.browser = fakeBrowser(t, long)

		got := m.renderFooter()
		if n := strings.Count(got, "\n"); n != 0 {
			t.Fatalf("the footer at width %d is %d rows, want 1:\n%s", w, n+1, got)
		}
		if gw := lipgloss.Width(got); gw != w {
			t.Fatalf("the footer at width %d rendered %d wide", w, gw)
		}
	}
}

// Cut at the front: the deepest directory is the one that says where you are.
func TestTheCrumbElidesAPathFromTheLeft(t *testing.T) {
	m, s := statusModel(t, 100, 24)
	m.mode = modeBrowser
	s.browser = fakeBrowser(t, "/srv/www/example.com/releases/current/public/assets")

	got := m.renderFooter()
	if !strings.Contains(got, "assets") {
		t.Fatalf("the elided path lost its tail, which is the part that says where you are:\n%s", got)
	}
	if !strings.Contains(got, "…") {
		t.Fatalf("a path too long for the row was cut without saying so:\n%s", got)
	}
}

// A remote directory's name can carry an escape; the crumb must not hand it to the terminal.
func TestCrumbStripsControlBytesFromTheTail(t *testing.T) {
	m, s := statusModel(t, 160, 34)
	s.editors = []*editorTab{{id: 1, name: "a", path: "/tmp/\x1b]52;c;cHduZWQ=\x07/a", pane: fakePane()}}
	m.mode = modeEditor
	m.relayout()

	if foot := m.renderFooter(); strings.Contains(foot, "\x1b]52") || strings.Contains(foot, "\x07") {
		t.Fatalf("the footer passes a remote escape through: %q", foot)
	}
}
