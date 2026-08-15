package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
)

// statusModel is viewModel's hosts with a session on web1, ready to be put into whichever
// mode a test is about.
func statusModel(t *testing.T, w, h int) (*model, *session) {
	t.Helper()
	m := viewModel(w, h)
	s := &session{shells: []*shellTab{{id: 1, pane: fakePane()}}}
	t.Cleanup(s.closeShells)
	m.sessions["web1"] = s
	m.active = "web1"
	return m, s
}

// The bar says where you are, in every mode: the host, what you are doing on it, and the
// thing you are doing it to. This is the whole point of the row — a footer that no longer
// spells out the keys is only affordable because this line says what they would act on.
func TestStatusSaysWhereYouAre(t *testing.T) {
	cases := []struct {
		name  string
		setup func(m *model, s *session)
		want  []string
	}{
		{
			// The list: the host under the cursor, since it is what every key here acts on.
			"list", func(m *model, s *session) { m.active, m.mode = "", modeList; m.cursor = 1 },
			[]string{"hosts", "raspberrypi"},
		},
		{
			// A shell that has reported no cwd says so rather than showing a stale one.
			"shell without a cwd", func(m *model, s *session) { m.mode = modeShell },
			[]string{"web1", "shell"},
		},
		{
			"scrollback", func(m *model, s *session) { m.mode = modeScrollback },
			[]string{"web1", "scrollback"},
		},
		{
			"browser", func(m *model, s *session) { m.mode = modeBrowser; s.browser = fakeBrowser(t, "/srv/www") },
			[]string{"web1", "sftp", "/srv/www"},
		},
		{
			"editor", func(m *model, s *session) {
				m.mode = modeEditor
				s.editors = []*editorTab{{id: 1, name: "nginx.conf", path: "/etc/nginx/nginx.conf", pane: fakePane()}}
			},
			[]string{"web1", "edit", "/etc/nginx/nginx.conf"},
		},
		{
			// A dropped session says so here as well as on its pane: the pane scrolls, the
			// bar does not.
			"dead", func(m *model, s *session) { m.mode = modeShell; s.dead = true },
			[]string{"web1", "disconnected"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m, s := statusModel(t, 160, 34)
			tc.setup(m, s)
			got := m.renderStatus()
			for _, want := range tc.want {
				if !strings.Contains(got, want) {
					t.Fatalf("status bar does not say %q:\n%s", want, got)
				}
			}
		})
	}
}

// The shell's crumb is the directory the remote is standing in, and it moves with it:
// that is what makes the bar an answer to "where am I" rather than "which host am I on".
func TestStatusFollowsTheRemoteDirectory(t *testing.T) {
	m, _ := vscodeModel(t, 1, "/srv/app/releases/current")
	m.mode, m.width = modeShell, 160

	if got := m.renderStatus(); !strings.Contains(got, "/srv/app/releases/current") {
		t.Fatalf("status bar does not carry the shell's cwd:\n%s", got)
	}
}

// The right-hand end is what you would have typed at ssh. It is the thing the alias hides,
// and the reason two aliases on one box can be told apart.
func TestStatusNamesTheTarget(t *testing.T) {
	m, _ := statusModel(t, 160, 34)

	// In the list it is the host under the cursor. web1 is on the default port, which
	// carries no information and so is left off.
	m.active, m.mode, m.cursor = "", modeList, 0
	if got := m.renderStatus(); !strings.Contains(got, "deploy@web1.example.com") {
		t.Fatalf("status bar does not name the selected host's target:\n%s", got)
	}
	if got := m.renderStatus(); strings.Contains(got, ":22") {
		t.Fatalf("status bar spells out the default port:\n%s", got)
	}

	// A port that is not 22 is the whole reason to show one.
	m.cursor = 2
	if got := m.renderStatus(); !strings.Contains(got, "root@10.0.0.9:2222") {
		t.Fatalf("status bar does not spell out a non-default port:\n%s", got)
	}

	// In a pane it is the host the pane is on, whatever the cursor has wandered to.
	m.active, m.mode, m.cursor = "web1", modeShell, 2
	if got := m.renderStatus(); !strings.Contains(got, "web1.example.com") {
		t.Fatalf("status bar in a pane names the cursor's host, not the pane's:\n%s", got)
	}
}

// Which tab of several is up belongs on this row, beside the place — it is part of where
// you are, and it is what the footer no longer has room to say.
func TestStatusCountsTabs(t *testing.T) {
	m, s := statusModel(t, 160, 34)
	s.shells = append(s.shells, &shellTab{id: 2, pane: fakePane()})
	s.activeSh, m.mode = 1, modeShell

	if got := m.renderStatus(); !strings.Contains(got, "shell 2/2") {
		t.Fatalf("status bar does not say which shell is up:\n%s", got)
	}

	s.editors = []*editorTab{
		{id: 1, name: "a.conf", path: "/etc/a.conf", pane: fakePane()},
		{id: 2, name: "b.conf", path: "/etc/b.conf", pane: fakePane()},
	}
	s.activeEd, m.mode = 1, modeEditor
	if got := m.renderStatus(); !strings.Contains(got, "file 2/2") {
		t.Fatalf("status bar does not say which file is up:\n%s", got)
	}
}

// Whatever it is asked to hold, the bar is exactly one row of exactly the window's width.
// It sits between the panes and the footer, so a bar that wrapped would push the footer
// off the bottom of the screen.
func TestStatusFitsOneRow(t *testing.T) {
	long := "/srv/www/example.com/releases/20240115T101500Z/vendor/bundle/ruby/3.2.0/gems"
	for _, w := range []int{200, 120, 80, 60, 40, 20} {
		m, s := statusModel(t, w, 24)
		m.mode = modeBrowser
		s.browser = fakeBrowser(t, long)

		got := m.renderStatus()
		if n := strings.Count(got, "\n"); n != 0 {
			t.Fatalf("status bar at width %d is %d rows, want 1:\n%s", w, n+1, got)
		}
		if gw := lipgloss.Width(got); gw != w {
			t.Fatalf("status bar at width %d rendered %d wide", w, gw)
		}
	}
}

// A path too long for the row is cut at its front, not its end: the deepest directory is
// the one that says where you are, and the mount point it hangs off rarely does.
func TestStatusElidesAPathFromTheLeft(t *testing.T) {
	m, s := statusModel(t, 60, 24)
	m.mode = modeBrowser
	s.browser = fakeBrowser(t, "/srv/www/example.com/releases/current/public/assets")

	got := m.renderStatus()
	if !strings.Contains(got, "assets") {
		t.Fatalf("the elided path lost its tail, which is the part that says where you are:\n%s", got)
	}
	if !strings.Contains(got, "…") {
		t.Fatalf("a path too long for the row was cut without saying so:\n%s", got)
	}
}
