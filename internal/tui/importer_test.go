package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
)

// writeSSHConfig drops an OpenSSH config in a temp dir and returns its path.
func writeSSHConfig(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write ssh config: %v", err)
	}
	return path
}

// setPath replaces whatever the card was pre-filled with, the way ctrl+u and
// typing would.
func setPath(m *model, path string) {
	m.importer.path = path
}

// "i" opens the card; pointing it at a config and pressing enter writes the hosts,
// closes the card and shows them in the list — no trip back to `hop import`.
func TestImportFlow(t *testing.T) {
	m := hostMgmtModel(t)

	m.handleKey(key(t, "i"))
	if !m.importer.open {
		t.Fatal("i did not open the import card")
	}
	if m.importer.first {
		t.Error("a card opened by hand should not claim to be a first run")
	}

	setPath(m, writeSSHConfig(t, "Host web\n  HostName web.example.com\n  User deploy\n  Port 2222\n\nHost db\n  HostName db.example.com\n"))
	m.handleKey(key(t, "enter"))

	if m.importer.open {
		t.Error("a successful import should close the card")
	}
	got := aliases(t, m)
	if len(got) != 2 {
		t.Fatalf("imported %d hosts, want 2: %v", len(got), got)
	}
	if h := got["web"]; h.HostName != "web.example.com" || h.User != "deploy" || h.Port != 2222 {
		t.Errorf("web imported as %+v", h)
	}
	// The list behind the card is reloaded, so the hosts are there the moment it goes.
	if len(m.hosts) != 2 {
		t.Errorf("model has %d hosts after import, want 2", len(m.hosts))
	}
	if m.statusKind != statusOK || !strings.Contains(m.status, "imported 2 hosts") {
		t.Errorf("status = %q (kind %v)", m.status, m.statusKind)
	}
}

// A path that does not exist keeps the card up with the typed path intact, so it
// can be corrected rather than retyped.
func TestImportBadPathKeepsCardOpen(t *testing.T) {
	m := hostMgmtModel(t)
	m.openImport(false)

	missing := filepath.Join(t.TempDir(), "nope", "config")
	setPath(m, missing)
	m.handleKey(key(t, "enter"))

	if !m.importer.open {
		t.Fatal("a failed import closed the card")
	}
	if m.importer.path != missing {
		t.Errorf("path = %q, want it left alone at %q", m.importer.path, missing)
	}
	if m.statusKind != statusErr {
		t.Errorf("status kind = %v, want statusErr (%q)", m.statusKind, m.status)
	}
	if len(aliases(t, m)) != 0 {
		t.Error("a failed import wrote hosts")
	}
}

// A config with nothing importable in it (only wildcard patterns) is reported as
// such rather than as a cheerful "imported 0 hosts".
func TestImportNoHostsWarns(t *testing.T) {
	m := hostMgmtModel(t)
	m.openImport(false)
	setPath(m, writeSSHConfig(t, "Host *\n  ServerAliveInterval 60\n"))
	m.handleKey(key(t, "enter"))

	if m.importer.open {
		t.Error("card stayed open after a clean parse")
	}
	if m.statusKind != statusWarn || !strings.Contains(m.status, "no hosts found") {
		t.Errorf("status = %q (kind %v), want a warning", m.status, m.statusKind)
	}
}

// An empty path is refused before anything touches the store.
func TestImportEmptyPathRefused(t *testing.T) {
	m := hostMgmtModel(t)
	m.openImport(false)
	m.handleKey(key(t, "ctrl+u"))
	m.handleKey(key(t, "enter"))

	if !m.importer.open || m.statusKind != statusErr {
		t.Errorf("empty path: open=%v status=%q kind=%v", m.importer.open, m.status, m.statusKind)
	}
}

// esc dismisses the card without importing anything — the "skip" a first run needs.
func TestImportEscSkips(t *testing.T) {
	m := hostMgmtModel(t)
	m.openImport(true)
	m.handleKey(key(t, "esc"))

	if m.importer.open {
		t.Error("esc left the card open")
	}
	if len(aliases(t, m)) != 0 {
		t.Error("esc imported hosts")
	}
}

// The card owns the keyboard while it is up: keys that mean something to the list
// behind it are text, not commands.
func TestImportCardIsModal(t *testing.T) {
	m := hostMgmtModel(t)
	m.openImport(false)
	m.handleKey(key(t, "ctrl+u"))
	typeRunes(t, m, "x/a")

	if m.importer.path != "x/a" {
		t.Errorf("path = %q, want the typed text", m.importer.path)
	}
	if m.confirm.open || m.hostForm.open || m.filtering {
		t.Error("keys leaked through the import card to the list")
	}
}

// Re-importing over hosts that are already there updates them instead of failing
// on the taken alias — which is what makes "i" a sync, not just a first-run step.
func TestImportUpdatesExistingHosts(t *testing.T) {
	m := hostMgmtModel(t)
	m.openImport(false)
	cfg := writeSSHConfig(t, "Host web\n  HostName old.example.com\n")
	setPath(m, cfg)
	m.handleKey(key(t, "enter"))

	if err := os.WriteFile(cfg, []byte("Host web\n  HostName new.example.com\n"), 0o600); err != nil {
		t.Fatalf("rewrite config: %v", err)
	}
	m.handleKey(key(t, "i"))
	setPath(m, cfg)
	m.handleKey(key(t, "enter"))

	got := aliases(t, m)
	if len(got) != 1 {
		t.Fatalf("re-import left %d hosts, want 1", len(got))
	}
	if got["web"].HostName != "new.example.com" {
		t.Errorf("hostname = %q, want the re-imported one", got["web"].HostName)
	}
}

// backspace edits the path a rune at a time, so a pre-filled default can be
// adjusted rather than cleared.
func TestImportBackspaceEditsPath(t *testing.T) {
	m := hostMgmtModel(t)
	m.openImport(false)
	setPath(m, "/etc/sshd")
	m.handleKey(key(t, "backspace"))
	m.handleKey(key(t, "backspace"))
	typeRunes(t, m, "h_config")

	if m.importer.path != "/etc/ssh_config" {
		t.Errorf("path = %q", m.importer.path)
	}
}

// expandHome resolves a leading ~, and leaves everything else alone.
func TestExpandHome(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("no home directory in this environment")
	}
	cases := []struct{ in, want string }{
		{"~/.ssh/config", filepath.Join(home, ".ssh", "config")},
		{"~", home},
		{"/etc/ssh/ssh_config", "/etc/ssh/ssh_config"},
		{"relative/config", "relative/config"},
		{"~user/.ssh/config", "~user/.ssh/config"},
	}
	for _, c := range cases {
		if got := expandHome(c.in); got != c.want {
			t.Errorf("expandHome(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// A path typed with a ~ imports the file it names.
func TestImportExpandsHomePath(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	if err := os.WriteFile(filepath.Join(home, "hosts.conf"), []byte("Host box\n  HostName box.example.com\n"), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	m := hostMgmtModel(t)
	m.openImport(false)
	setPath(m, "~/hosts.conf")
	m.handleKey(key(t, "enter"))

	if _, ok := aliases(t, m)["box"]; !ok {
		t.Errorf("~ path did not import (status %q)", m.status)
	}
}

// The card is pre-filled with the default config path, so the common case is one
// keystroke.
func TestOpenImportPrefillsDefaultPath(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	m := hostMgmtModel(t)
	m.openImport(true)
	if want := filepath.Join(home, ".ssh", "config"); m.importer.path != want {
		t.Errorf("path = %q, want %q", m.importer.path, want)
	}
	if !m.importer.first {
		t.Error("first-run flag not carried onto the card")
	}
}

// haveSSHConfig is what decides whether a first run offers the import at all: a
// real file yes, a missing one (or a directory) no.
func TestHaveSSHConfig(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	if haveSSHConfig() {
		t.Error("offered an import with no config file")
	}

	sshDir := filepath.Join(home, ".ssh")
	if err := os.MkdirAll(filepath.Join(sshDir, "config"), 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if haveSSHConfig() {
		t.Error("a directory named config counted as a config file")
	}

	if err := os.RemoveAll(filepath.Join(sshDir, "config")); err != nil {
		t.Fatalf("rm: %v", err)
	}
	if err := os.WriteFile(filepath.Join(sshDir, "config"), []byte("Host a\n"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	if !haveSSHConfig() {
		t.Error("a real config file was not offered")
	}
}

// The card renders inside the window it is centered on, and says which mode it is
// in: a first run offers to skip, a re-import to cancel.
func TestRenderImportCard(t *testing.T) {
	m := hostMgmtModel(t)
	m.openImport(true)
	card := m.renderImport()
	if !strings.Contains(card, "IMPORT SSH CONFIG") {
		t.Error("card has no title")
	}
	if !strings.Contains(card, "skip") {
		t.Error("first-run card does not offer to skip")
	}
	for _, line := range strings.Split(card, "\n") {
		if w := lipgloss.Width(line); w > m.width {
			t.Fatalf("card line %d wide in a %d-wide window: %q", w, m.width, line)
		}
	}

	m.openImport(false)
	if !strings.Contains(m.renderImport(), "cancel") {
		t.Error("re-import card does not offer to cancel")
	}
}
