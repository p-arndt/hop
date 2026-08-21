package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"hop/internal/store"
)

// pinModel is a navigation-mode model over three hosts in frecency order a, b, c.
func pinModel(t *testing.T) *model {
	t.Helper()
	return hostMgmtModel(t,
		store.Host{Alias: "a", HostName: "a.test", Visits: 30},
		store.Host{Alias: "b", HostName: "b.test", Visits: 20},
		store.Host{Alias: "c", HostName: "c.test", Visits: 10},
	)
}

func listOrder(m *model) []string {
	out := make([]string, 0, len(m.filtered))
	for _, idx := range m.filtered {
		out = append(out, m.hosts[idx].Alias)
	}
	return out
}

// rowKinds is what each drawn row is: a heading by name, or the alias of a host.
func rowKinds(m *model) []string {
	out := make([]string, 0, len(m.rows))
	for _, r := range m.rows {
		if r.heading != "" {
			out = append(out, "#"+r.heading)
			continue
		}
		out = append(out, m.hosts[m.filtered[r.fi]].Alias)
	}
	return out
}

// 'p' pins the host above the frecency order and splits the list into sections; 'p' again undoes it.
func TestPinKeyTogglesAndSections(t *testing.T) {
	m := pinModel(t)
	m.cursor = 2 // "c", the least-visited host

	m.handleKey(key(t, "p"))

	if got, want := listOrder(m), []string{"c", "a", "b"}; strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("order after pinning c = %v, want %v", got, want)
	}
	if got, want := rowKinds(m), []string{"#PINNED", "c", "#HOSTS", "a", "b"}; strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("rows = %v, want %v", got, want)
	}
	if !m.hasSections() {
		t.Fatal("hasSections is false with a host pinned")
	}
	if h, _ := m.selectedHost(); h.Alias != "c" {
		t.Fatalf("cursor left %q after pinning c", h.Alias)
	}

	m.handleKey(key(t, "p"))

	if got, want := listOrder(m), []string{"a", "b", "c"}; strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("order after unpinning = %v, want frecency back: %v", got, want)
	}
	if m.hasSections() {
		t.Fatal("hasSections is true with nothing pinned")
	}
	if got, want := rowKinds(m), []string{"a", "b", "c"}; strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("rows = %v, want no headings: %v", got, want)
	}
}

func TestPinSurvivesReload(t *testing.T) {
	m := pinModel(t)
	m.cursor = 2
	m.handleKey(key(t, "p"))

	m.reloadHosts()

	if h, ok := m.selectedHost(); !ok || h.Alias != "c" || !h.Pinned {
		t.Fatalf("selected host after a reload = %+v, want a pinned c", h)
	}
}

// shift+k / shift+j move a pinned host inside its section, and stop at its ends.
func TestPinReorderKeys(t *testing.T) {
	m := pinModel(t)
	for _, alias := range []string{"a", "b", "c"} {
		if err := m.st.SetPinned(alias, true); err != nil {
			t.Fatalf("SetPinned %s: %v", alias, err)
		}
	}
	m.reloadHosts()
	m.cursor = 2 // "c", the last of the three pins

	m.handleKey(key(t, "K"))
	if got, want := listOrder(m), []string{"a", "c", "b"}; strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("order after shift+k = %v, want %v", got, want)
	}
	if h, _ := m.selectedHost(); h.Alias != "c" {
		t.Fatalf("the cursor did not travel with the host: it is on %q", h.Alias)
	}

	m.handleKey(key(t, "K"))
	m.handleKey(key(t, "K")) // already at the top: a no-op, not a wrap
	if got, want := listOrder(m), []string{"c", "a", "b"}; strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("order at the top of the section = %v, want %v", got, want)
	}

	m.handleKey(key(t, "J"))
	if got, want := listOrder(m), []string{"a", "c", "b"}; strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("order after shift+j = %v, want %v", got, want)
	}
}

// On an unpinned host the reorder keys report why rather than doing nothing.
func TestPinReorderOnUnpinnedHostSaysSo(t *testing.T) {
	m := pinModel(t)
	m.cursor = 0

	m.handleKey(key(t, "J"))

	if got, want := listOrder(m), []string{"a", "b", "c"}; strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("order = %v, want it unchanged: %v", got, want)
	}
	if m.status == "" || !strings.Contains(m.status, "not pinned") {
		t.Fatalf("status = %q, want it to explain that the host is not pinned", m.status)
	}
}

// A filter narrows the sections rather than dissolving them; empty ones lose their heading.
func TestFilterKeepsSections(t *testing.T) {
	m := hostMgmtModel(t,
		store.Host{Alias: "alpha", HostName: "a.test", Visits: 30},
		store.Host{Alias: "beta", HostName: "b.test", Visits: 20},
		store.Host{Alias: "gamma", HostName: "g.test", Visits: 10},
	)
	if err := m.st.SetPinned("gamma", true); err != nil {
		t.Fatalf("SetPinned: %v", err)
	}
	m.reloadHosts()

	// "a" hits every alias; the pinned one comes first whatever the score.
	m.filter = "a"
	m.applyFilter()
	if got := listOrder(m); got[0] != "gamma" {
		t.Fatalf("filtered order = %v, want the pinned host first", got)
	}
	if got, want := rowKinds(m), []string{"#PINNED", "gamma", "#HOSTS"}; strings.Join(got[:3], ",") != strings.Join(want, ",") {
		t.Fatalf("rows = %v, want both headings", got)
	}

	m.filter = "bet"
	m.applyFilter()
	if got, want := rowKinds(m), []string{"#HOSTS", "beta"}; strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("rows = %v, want only the HOSTS section: %v", got, want)
	}

	m.filter = "gam"
	m.applyFilter()
	if got, want := rowKinds(m), []string{"#PINNED", "gamma"}; strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("rows = %v, want only the PINNED section: %v", got, want)
	}
}

// The section headings carry counts and replace the sidebar's own title.
func TestRenderListDrawsSections(t *testing.T) {
	m := pinModel(t)
	m.cursor = 2
	m.handleKey(key(t, "p"))
	m.recomputeLayout()

	out := m.renderList(m.listWidth(), m.bodyHeight())

	if !strings.Contains(out, "PINNED") {
		t.Fatalf("the PINNED heading is not in the sidebar:\n%s", out)
	}
	if n := strings.Count(out, "HOSTS"); n != 1 {
		t.Fatalf("HOSTS appears %d times, want exactly the section heading:\n%s", n, out)
	}
	pinned := strings.Index(out, "PINNED")
	if hosts := strings.Index(out, "HOSTS"); hosts < pinned {
		t.Fatalf("HOSTS is drawn above PINNED:\n%s", out)
	}
	if !strings.Contains(ansi.Strip(out), "PINNED  1") {
		t.Fatalf("the PINNED heading does not carry its count:\n%s", out)
	}
}

// With sections on, a click lands on the host under the pointer; a heading selects nothing.
func TestClickWithSections(t *testing.T) {
	m := pinModel(t)
	m.cursor = 2
	m.handleKey(key(t, "p"))
	m.recomputeLayout()

	// Rows: header (0), border (1), then #PINNED, c, #HOSTS, a, b.
	if _, ok := m.listRowAt(2); ok {
		t.Fatal("a click on the PINNED heading selected a host")
	}
	if i, ok := m.listRowAt(3); !ok || m.hosts[m.filtered[i]].Alias != "c" {
		t.Fatalf("the row under the PINNED heading is not the pinned host (ok=%v)", ok)
	}
	if _, ok := m.listRowAt(4); ok {
		t.Fatal("a click on the HOSTS heading selected a host")
	}
	if i, ok := m.listRowAt(6); !ok || m.hosts[m.filtered[i]].Alias != "b" {
		t.Fatalf("the last row is not the last host (ok=%v)", ok)
	}
}

// A filter keeps the PINNED section in the user's order, which is what shift+j/k move in.
func TestFilterKeepsPinOrder(t *testing.T) {
	m := hostMgmtModel(t,
		store.Host{Alias: "prod", HostName: "p.test", Visits: 30},
		store.Host{Alias: "dev", HostName: "d.test", Visits: 20},
	)
	for _, alias := range []string{"prod", "dev"} {
		if err := m.st.SetPinned(alias, true); err != nil {
			t.Fatalf("SetPinned %s: %v", alias, err)
		}
	}
	m.reloadHosts()

	// "d" scores "dev" above "prod", but prod was pinned first.
	m.filter = "d"
	m.applyFilter()

	if got, want := listOrder(m), []string{"prod", "dev"}; strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("filtered pinned order = %v, want the pin order %v", got, want)
	}

	m.cursor = 1
	m.handleKey(key(t, "K"))
	if got, want := listOrder(m), []string{"dev", "prod"}; strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("order after shift+k under a filter = %v, want %v", got, want)
	}
}

// Paging is measured in drawn rows, headings included, so it never steps over a host.
func TestPageStepsBySectionRows(t *testing.T) {
	seed := make([]store.Host, 0, 12)
	for i := range 12 {
		seed = append(seed, store.Host{
			Alias:    string(rune('a' + i)),
			HostName: "h.test",
			Visits:   100 - i,
		})
	}
	m := hostMgmtModel(t, seed...)
	if err := m.st.SetPinned("a", true); err != nil {
		t.Fatalf("SetPinned: %v", err)
	}
	m.reloadHosts()
	m.height = 12 // a short window, so the list actually pages
	m.recomputeLayout()

	rows := m.listRows()
	m.cursor = 0
	from := m.cursorRow()
	m.handleKey(key(t, "pgdown"))

	switch moved := m.cursorRow() - from; {
	case moved <= 0:
		t.Fatalf("pgdn moved the cursor %d rows", moved)
	case moved > rows:
		t.Fatalf("pgdn advanced %d rows on a %d-row screen — the headings were paged over", moved, rows)
	}
	if m.rows[m.cursorRow()].heading != "" {
		t.Fatal("pgdn parked the cursor on a section heading")
	}

	m.handleKey(key(t, "pgup"))
	if m.cursor != 0 {
		t.Fatalf("pgup from one page down = %d, want back at the top", m.cursor)
	}
}
