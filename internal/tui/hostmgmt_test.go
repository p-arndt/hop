package tui

import (
	"strconv"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"

	"hop/internal/config"
	"hop/internal/store"
)

// hostMgmtModel builds a navigation-mode model backed by a real temp-file store.
func hostMgmtModel(t *testing.T, seed ...store.Host) *model {
	t.Helper()
	st, err := store.OpenAt(t.TempDir()+"/hop.config", "")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { st.Close() })

	for _, h := range seed {
		if _, err := st.Upsert(h); err != nil {
			t.Fatalf("seed %q: %v", h.Alias, err)
		}
	}

	hosts, err := st.Hosts()
	if err != nil {
		t.Fatalf("load hosts: %v", err)
	}

	m := &model{st: st, hosts: hosts, sessions: map[string]*session{}, notify: make(chan struct{}, 1), cfg: config.Default(), layout: layout{width: 100, height: 30}}
	m.applyFilter()
	return m
}

// typeRunes feeds each rune of s to the model as its own keypress.
func typeRunes(t *testing.T, m *model, s string) {
	t.Helper()
	for _, r := range s {
		m.handleKey(key(t, string(r)))
	}
}

func aliases(t *testing.T, m *model) map[string]store.Host {
	t.Helper()
	hosts, err := m.st.Hosts()
	if err != nil {
		t.Fatalf("read hosts: %v", err)
	}
	out := map[string]store.Host{}
	for _, h := range hosts {
		out[h.Alias] = h
	}
	return out
}

// Adding writes the host, closes the card and parks the cursor on the new row.
func TestAddHostFlow(t *testing.T) {
	m := hostMgmtModel(t)

	m.handleKey(key(t, "a"))
	if !m.hostForm.open || m.hostForm.edit {
		t.Fatal("a did not open the add form")
	}

	typeRunes(t, m, "web-01")   // Alias (starts focused)
	m.handleKey(key(t, "down")) // → User
	typeRunes(t, m, "deploy")
	m.handleKey(key(t, "down")) // → Hostname
	typeRunes(t, m, "10.0.0.5")
	m.handleKey(key(t, "down")) // → Port
	typeRunes(t, m, "2222")
	m.handleKey(key(t, "enter")) // save

	if m.hostForm.open {
		t.Fatal("saving did not close the form")
	}
	got, ok := aliases(t, m)["web-01"]
	if !ok {
		t.Fatal("the host was not written to the store")
	}
	if got.User != "deploy" || got.HostName != "10.0.0.5" || got.Port != 2222 {
		t.Fatalf("stored host = %+v, want deploy@10.0.0.5:2222", got)
	}
	if h, ok := m.selectedHost(); !ok || h.Alias != "web-01" {
		t.Fatalf("cursor is not on the new host; selected = %+v", h)
	}
}

func TestAddHostDefaultsPort(t *testing.T) {
	m := hostMgmtModel(t)
	m.handleKey(key(t, "a"))
	typeRunes(t, m, "bare")
	m.handleKey(key(t, "enter"))

	if got := aliases(t, m)["bare"]; got.Port != 22 {
		t.Fatalf("port = %d, want the default 22", got.Port)
	}
}

func TestAddHostEmptyAliasKeepsFormOpen(t *testing.T) {
	m := hostMgmtModel(t)
	m.handleKey(key(t, "a"))
	m.handleKey(key(t, "enter"))

	if !m.hostForm.open {
		t.Fatal("an empty alias closed the form; it should have kept it open")
	}
	if m.statusKind != statusErr {
		t.Fatalf("status kind = %v, want an error about the empty alias", m.statusKind)
	}
}

func TestAddHostBadPortKeepsFormOpen(t *testing.T) {
	m := hostMgmtModel(t)
	m.handleKey(key(t, "a"))
	typeRunes(t, m, "web")
	m.hostForm.cursor = hfPort
	typeRunes(t, m, "abc")
	m.handleKey(key(t, "enter"))

	if !m.hostForm.open {
		t.Fatal("a bad port closed the form; it should have kept it open")
	}
	if _, ok := aliases(t, m)["web"]; ok {
		t.Fatal("a host with a bad port was written anyway")
	}
}

// A duplicate alias must not overwrite the existing host.
func TestAddHostDuplicateRefused(t *testing.T) {
	m := hostMgmtModel(t, store.Host{Alias: "web", HostName: "old", Port: 22})
	m.handleKey(key(t, "a"))
	typeRunes(t, m, "web")
	m.handleKey(key(t, "enter"))

	if !m.hostForm.open {
		t.Fatal("a duplicate alias closed the form")
	}
	if got := aliases(t, m)["web"]; got.HostName != "old" {
		t.Fatalf("the existing host was overwritten: hostname = %q", got.HostName)
	}
}

func TestEditHostRenamePreservesHistory(t *testing.T) {
	m := hostMgmtModel(t, store.Host{Alias: "web", HostName: "h", Port: 22})
	if err := m.st.Touch("web"); err != nil { // give it some history
		t.Fatalf("touch: %v", err)
	}
	m.reloadHosts()

	m.handleKey(key(t, "e"))
	if !m.hostForm.open || !m.hostForm.edit {
		t.Fatal("e did not open the edit form")
	}
	m.handleKey(key(t, "ctrl+u")) // clear the pre-filled alias
	typeRunes(t, m, "web2")
	m.handleKey(key(t, "enter"))

	all := aliases(t, m)
	if _, ok := all["web"]; ok {
		t.Fatal("the old alias survived the rename")
	}
	got, ok := all["web2"]
	if !ok {
		t.Fatal("the renamed host is missing")
	}
	if got.Visits != 1 {
		t.Fatalf("visits = %d, want the pre-rename history (1) preserved", got.Visits)
	}
}

func TestEditHostFieldPreservesVisits(t *testing.T) {
	m := hostMgmtModel(t, store.Host{Alias: "web", HostName: "old", Port: 22})
	if err := m.st.Touch("web"); err != nil {
		t.Fatalf("touch: %v", err)
	}
	m.reloadHosts()

	m.handleKey(key(t, "e"))
	m.hostForm.cursor = hfHostname
	m.handleKey(key(t, "ctrl+u"))
	typeRunes(t, m, "new")
	m.handleKey(key(t, "enter"))

	got := aliases(t, m)["web"]
	if got.HostName != "new" {
		t.Fatalf("hostname = %q, want the edit to have taken", got.HostName)
	}
	if got.Visits != 1 {
		t.Fatalf("visits = %d, want the history (1) preserved through an edit", got.Visits)
	}
}

func TestDeleteConfirmFlow(t *testing.T) {
	m := hostMgmtModel(t, store.Host{Alias: "web", HostName: "h", Port: 22})

	m.handleKey(key(t, "x"))
	if !m.confirm.open || m.confirm.alias != "web" {
		t.Fatalf("x did not arm the confirm for web; confirm = %+v", m.confirm)
	}

	m.handleKey(key(t, "y"))
	if m.confirm.open {
		t.Fatal("confirming did not close the card")
	}
	if _, ok := aliases(t, m)["web"]; ok {
		t.Fatal("the host was not deleted")
	}
	if len(m.hosts) != 0 {
		t.Fatalf("list still has %d hosts after delete", len(m.hosts))
	}
}

func TestDeleteCancel(t *testing.T) {
	m := hostMgmtModel(t, store.Host{Alias: "web", HostName: "h", Port: 22})

	m.handleKey(key(t, "x"))
	m.handleKey(key(t, "n"))
	if m.confirm.open {
		t.Fatal("n did not dismiss the confirm")
	}
	if _, ok := aliases(t, m)["web"]; !ok {
		t.Fatal("cancelling still deleted the host")
	}
}

func TestHostFormSwallowsKeys(t *testing.T) {
	m := hostMgmtModel(t,
		store.Host{Alias: "a", HostName: "h"},
		store.Host{Alias: "b", HostName: "h"},
	)
	m.cursor = 0

	m.handleKey(key(t, "a"))
	m.handleKey(key(t, "down"))

	if m.cursor != 0 {
		t.Fatalf("host cursor moved to %d; the form must swallow the key", m.cursor)
	}
	if m.hostForm.cursor != 1 {
		t.Fatalf("form cursor = %d, want down to have moved it to field 1", m.hostForm.cursor)
	}
}

func TestConfirmSwallowsKeys(t *testing.T) {
	m := hostMgmtModel(t, store.Host{Alias: "web", HostName: "h"})

	m.handleKey(key(t, "x"))
	m.handleKey(key(t, "down"))

	if !m.confirm.open {
		t.Fatal("an unrelated key dismissed the confirm")
	}
	if _, ok := aliases(t, m)["web"]; !ok {
		t.Fatal("an unrelated key deleted the host")
	}
}

func TestAddHostWithDefaultDir(t *testing.T) {
	m := hostMgmtModel(t)
	m.handleKey(key(t, "a"))
	typeRunes(t, m, "web")
	m.hostForm.cursor = hfDefaultDir
	typeRunes(t, m, "/srv/app")
	m.handleKey(key(t, "enter"))

	if got := aliases(t, m)["web"]; got.DefaultDir != "/srv/app" {
		t.Fatalf("stored DefaultDir = %q, want /srv/app", got.DefaultDir)
	}
}

// An edit pre-fills the default directory and can clear it again.
func TestEditHostDefaultDirRoundTrip(t *testing.T) {
	m := hostMgmtModel(t, store.Host{Alias: "web", HostName: "h", Port: 22, DefaultDir: "/srv/app"})

	m.handleKey(key(t, "e"))
	if got := m.hostForm.buf[hfDefaultDir]; got != "/srv/app" {
		t.Fatalf("the form pre-filled DefaultDir as %q, want /srv/app", got)
	}
	m.hostForm.cursor = hfDefaultDir
	m.handleKey(key(t, "ctrl+u"))
	typeRunes(t, m, "~/work")
	m.handleKey(key(t, "enter"))

	if got := aliases(t, m)["web"]; got.DefaultDir != "~/work" {
		t.Fatalf("DefaultDir = %q, want ~/work", got.DefaultDir)
	}

	m.handleKey(key(t, "e"))
	m.hostForm.cursor = hfDefaultDir
	m.handleKey(key(t, "ctrl+u"))
	m.handleKey(key(t, "enter"))

	if got := aliases(t, m)["web"]; got.DefaultDir != "" {
		t.Fatalf("DefaultDir = %q, want it cleared", got.DefaultDir)
	}
}

// A pending reconnect's browser directory outranks the host's default.
func TestBrowserStartDirPrefersTheDroppedSession(t *testing.T) {
	m := hostMgmtModel(t, store.Host{Alias: "web", HostName: "h", Port: 22, DefaultDir: "/srv/app"})
	h := m.hosts[0]

	if got := m.browserStartDir(h); got != "/srv/app" {
		t.Fatalf("browserStartDir with no plan = %q, want the host default /srv/app", got)
	}

	m.pending = map[string]reconnectPlan{"web": {browser: true, browserDir: "/var/log"}}
	if got := m.browserStartDir(h); got != "/var/log" {
		t.Fatalf("browserStartDir with a plan = %q, want the dropped session's /var/log", got)
	}
}

// The card fits the window by giving up air first, then scrolling fields.
func TestHostFormFitsTheWindow(t *testing.T) {
	if hostFormMinH() > 24 {
		t.Fatalf("the packed card needs %d rows; it must fit a standard 24-row terminal", hostFormMinH())
	}
	for h := hostFormMinH(); h <= hostFormFullH()+8; h++ {
		m := hostMgmtModel(t)
		m.height = h
		m.openHostFormAdd()
		for cursor := range hostFormFields {
			m.hostForm.cursor = cursor
			if got := lipgloss.Height(m.renderHostForm()); got > h {
				t.Fatalf("a %d-row window with the cursor on field %d got a %d-line card", h, cursor, got)
			}
		}
	}
}

// However short the window, the field the cursor is on stays drawn.
func TestHostFormWindowHoldsTheCursor(t *testing.T) {
	for h := 10; h <= hostFormFullH()+4; h++ {
		m := hostMgmtModel(t)
		m.height = h
		m.openHostFormAdd()
		for cursor := range hostFormFields {
			m.hostForm.cursor = cursor
			first, count := m.hostFormWindow()
			if cursor < first || cursor >= first+count {
				t.Fatalf("a %d-row window drew fields [%d,%d) with the cursor on %d",
					h, first, first+count, cursor)
			}
			if first < 0 || first+count > len(hostFormFields) {
				t.Fatalf("a %d-row window drew fields [%d,%d), outside the form", h, first, first+count)
			}
		}
	}
}

// The "n/8" counter appears exactly when the card shows fewer fields than it has.
func TestHostFormCounterOnlyWhenScrolled(t *testing.T) {
	for _, c := range []struct {
		height int
		want   bool
	}{
		{hostFormFullH(), false},
		{hostFormPackedH(), false},
		{hostFormPackedH() - 2, true},
		{hostFormMinH(), true},
	} {
		m := hostMgmtModel(t)
		m.height = c.height
		m.openHostFormAdd()
		got := strings.Contains(m.renderHostForm(), "/"+strconv.Itoa(len(hostFormFields)))
		if got != c.want {
			t.Errorf("a %d-row card shows the counter = %v, want %v", c.height, got, c.want)
		}
	}
}
