package tui

import (
	"errors"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"hop/internal/filebrowser"
	"hop/internal/filebrowser/fbtest"
	"hop/internal/sftpx"
	"hop/internal/sshx"
	"hop/internal/store"
)

// startedBrowser records the directory a new browser was asked to start in.
type startedBrowser struct {
	dir   string
	calls int
}

// stubStartBrowser swaps in a browser opener that raises no SFTP and lands a fake browser.
func stubStartBrowser(t *testing.T) *startedBrowser {
	t.Helper()
	rec := &startedBrowser{}
	prev := startBrowserCmd
	startBrowserCmd = func(h store.Host, _ *sshx.Client, _ string, _ sshx.Prompter,
		_ filebrowser.Options, startDir string, _, _ int, restore bool) tea.Cmd {
		rec.dir, rec.calls = startDir, rec.calls+1
		br := fakeBrowser(t, startDir)
		return func() tea.Msg { return browserOpenedMsg{alias: h.Alias, browser: br, restore: restore} }
	}
	t.Cleanup(func() { startBrowserCmd = prev })
	return rec
}

// startedShell records the directory a new shell tab was asked to start in.
type startedShell struct {
	dir   string
	calls int
}

func stubExtraShell(t *testing.T) *startedShell {
	t.Helper()
	rec := &startedShell{}
	prev := extraShellCmd
	extraShellCmd = func(alias, startDir string, _ *sshx.Client, id, _, _ int, _ chan struct{}, restore bool) tea.Cmd {
		rec.dir, rec.calls = startDir, rec.calls+1
		return func() tea.Msg {
			return connectedMsg{alias: alias, tab: &shellTab{id: id, pane: fakePane()}, restore: restore}
		}
	}
	t.Cleanup(func() { extraShellCmd = prev })
	return rec
}

// shellOn builds a model focused on web's shell; dir is what that shell reported, "" for nothing.
func shellOn(t *testing.T, dir string, browser bool) (*model, *session) {
	t.Helper()
	m, s, _ := deadModel(t, 1, browser)
	if dir != "" {
		p, _ := cwdPane(t, dir)
		s.shells[0].pane = p
	}
	m.mode = modeShell
	return m, s
}

// leader types ctrl+o and then k inside the focused pane.
func leader(m *model, k rune) tea.Cmd {
	m.handleKey(ctrlO())
	_, cmd := m.handleKey(runeKey(k))
	return cmd
}

// run drives cmd's messages back through the model, as the Bubble Tea loop would.
func run(t *testing.T, m *model, cmd tea.Cmd) {
	t.Helper()
	if cmd == nil {
		t.Fatal("no command to run")
	}
	msg := cmd()
	if batch, ok := msg.(tea.BatchMsg); ok {
		for _, c := range batch {
			if c == nil {
				continue
			}
			// The spinner's tick would only schedule the next one.
			if msg := c(); msg != nil {
				if _, spinner := msg.(tickMsg); !spinner {
					m.Update(msg)
				}
			}
		}
		return
	}
	m.Update(msg)
}

func TestBrowseHereMovesTheOpenBrowserToTheShellsDirectory(t *testing.T) {
	m, s := shellOn(t, "/var/log/nginx", true)

	if cmd := leader(m, 'f'); cmd != nil {
		t.Fatal("an open browser was reopened; it should have been moved")
	}

	if got := s.browser.Path(); got != "/var/log/nginx" {
		t.Fatalf("browser is in %q, want the shell's /var/log/nginx", got)
	}
	if m.mode != modeBrowser || m.active != "web" {
		t.Fatalf("mode = %v on %q; want the keyboard in web's browser", m.mode, m.active)
	}
	if m.statusKind != statusOK || !strings.Contains(m.status, "/var/log/nginx") {
		t.Fatalf("status = %q (kind %v), want it to name the directory", m.status, m.statusKind)
	}
}

func TestBrowseHereOpensABrowserInTheShellsDirectory(t *testing.T) {
	rec := stubStartBrowser(t)
	m, s := shellOn(t, "/srv/app", false)

	run(t, m, leader(m, 'f'))

	if rec.calls != 1 || rec.dir != "/srv/app" {
		t.Fatalf("asked for %d browsers starting in %q, want one in /srv/app", rec.calls, rec.dir)
	}
	if s.browser == nil || s.browser.Path() != "/srv/app" {
		t.Fatal("the browser did not land in the shell's directory")
	}
	if m.mode != modeBrowser {
		t.Fatalf("mode = %v, want the browser to have the keyboard", m.mode)
	}
}

// Without a reported directory there is nowhere to go, but the chord still gets you a browser.
func TestBrowseHereWithoutACwdOpensTheDefaultAndSaysWhy(t *testing.T) {
	rec := stubStartBrowser(t)
	m, _ := shellOn(t, "", false)
	m.hosts[0].DefaultDir = "/srv/default"

	run(t, m, leader(m, 'f'))

	if rec.calls != 1 || rec.dir != "/srv/default" {
		t.Fatalf("asked for %d browsers starting in %q, want one in the default dir", rec.calls, rec.dir)
	}
	if m.mode != modeBrowser {
		t.Fatalf("mode = %v, want the browser to have the keyboard", m.mode)
	}
	if m.statusKind != statusWarn || !strings.Contains(m.status, "not reported its directory") {
		t.Fatalf("status = %q (kind %v); the landing hid why the browser is not where the shell is", m.status, m.statusKind)
	}
}

func TestBrowseHereWithoutACwdLeavesTheOpenBrowserWhereItIs(t *testing.T) {
	m, s := shellOn(t, "", true)

	leader(m, 'f')

	if got := s.browser.Path(); got != "/srv/www" {
		t.Fatalf("browser moved to %q with no directory to move to", got)
	}
	if m.mode != modeBrowser {
		t.Fatalf("mode = %v, want the browser to have the keyboard anyway", m.mode)
	}
	if m.statusKind != statusWarn || !strings.Contains(m.status, "not reported its directory") {
		t.Fatalf("status = %q (kind %v), want it to say why the browser did not move", m.status, m.statusKind)
	}
}

// listFails is a remote on which one directory cannot be listed.
type listFails struct {
	fbtest.Stub
	dir string
}

func (c listFails) List(dir string) ([]sftpx.Entry, error) {
	if dir == c.dir {
		return nil, errors.New("permission denied")
	}
	return c.Stub.List(dir)
}

func TestBrowseHereIntoAnUnlistableDirectorySaysSo(t *testing.T) {
	m, s := shellOn(t, "/root", true)
	br, err := filebrowser.New(listFails{Stub: fbtest.Stub{Dir: "/srv/www"}, dir: "/root"}, "web", "/srv/www",
		filebrowser.Options{DownloadDir: t.TempDir()}, 40, 12)
	if err != nil {
		t.Fatalf("build browser: %v", err)
	}
	s.browser = br

	leader(m, 'f')

	if got := s.browser.Path(); got != "/srv/www" {
		t.Fatalf("browser is in %q, want it left where it was", got)
	}
	if m.statusKind != statusErr || !strings.Contains(m.status, "permission denied") {
		t.Fatalf("status = %q (kind %v), want the listing error", m.status, m.statusKind)
	}
}

// browsingOn builds a model with the keyboard in web's browser over /srv, holding app/ and notes.txt.
func browsingOn(t *testing.T) (*model, *session) {
	t.Helper()
	m, s, _ := deadModel(t, 1, false)
	s.browser = fakeBrowserWith(t, "/srv",
		sftpx.Entry{Name: "app", IsDir: true}, sftpx.Entry{Name: "notes.txt"})
	m.mode = modeBrowser
	return m, s
}

func TestShellHereStartsInTheDirectoryUnderTheCursor(t *testing.T) {
	rec := stubExtraShell(t)
	m, s := browsingOn(t)

	_, cmd := m.handleKey(key(t, "S"))
	run(t, m, cmd)

	if rec.calls != 1 || rec.dir != "/srv/app" {
		t.Fatalf("asked for %d shells starting in %q, want one in /srv/app", rec.calls, rec.dir)
	}
	if len(s.shells) != 2 || s.activeSh != 1 {
		t.Fatalf("shells = %d, active = %d; want a second tab, shown", len(s.shells), s.activeSh)
	}
	if m.mode != modeShell {
		t.Fatalf("mode = %v, want the new shell to have the keyboard", m.mode)
	}
}

func TestShellHereOnAFileStartsInItsDirectory(t *testing.T) {
	rec := stubExtraShell(t)
	m, s := browsingOn(t)
	s.browser.Select(1) // notes.txt

	m.handleKey(key(t, "S"))

	if rec.calls != 1 || rec.dir != "/srv" {
		t.Fatalf("asked for %d shells starting in %q, want one in /srv", rec.calls, rec.dir)
	}
}
