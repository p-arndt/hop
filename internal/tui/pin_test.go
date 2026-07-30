package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"hop/internal/store"
)

// pinModel is a navigation-mode model over three hosts in a known frecency order:
// a, b, c.
func pinModel(t *testing.T) *model {
	t.Helper()
	return hostMgmtModel(t,
		store.Host{Alias: "a", HostName: "a.test", Visits: 30},
		store.Host{Alias: "b", HostName: "b.test", Visits: 20},
		store.Host{Alias: "c", HostName: "c.test", Visits: 10},
	)
}

// listOrder is the aliases in the order the sidebar would draw them.
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

// 'p' pins the host under the cursor, which lifts it above the frecency order and
// gives the list its PINNED / HOSTS sections. A second 'p' puts it back.
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
	// The cursor follows the host it was on, wherever the new order put it.
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

// The pin outlives the model: it is a column in the database, not a field in a
// list that gets rebuilt on every reload.
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

// Reordering only means something inside the pinned section; on a host that is not
// pinned the keys say so rather than silently doing nothing.
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

// A filter narrows the sections, it does not dissolve them: a pinned match still
// sorts above an unpinned one, and a section with nothing left in it loses its
// heading rather than drawing an empty block.
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

	// "a" hits every alias; the pinned one comes first whatever the match score.
	m.filter = "a"
	m.applyFilter()
	if got := listOrder(m); got[0] != "gamma" {
		t.Fatalf("filtered order = %v, want the pinned host first", got)
	}
	if got, want := rowKinds(m), []string{"#PINNED", "gamma", "#HOSTS"}; strings.Join(got[:3], ",") != strings.Join(want, ",") {
		t.Fatalf("rows = %v, want both headings", got)
	}

	// A filter only the unpinned hosts match drops the PINNED heading entirely.
	m.filter = "bet"
	m.applyFilter()
	if got, want := rowKinds(m), []string{"#HOSTS", "beta"}; strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("rows = %v, want only the HOSTS section: %v", got, want)
	}

	// And one only the pinned host matches drops the HOSTS heading.
	m.filter = "gam"
	m.applyFilter()
	if got, want := rowKinds(m), []string{"#PINNED", "gamma"}; strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("rows = %v, want only the PINNED section: %v", got, want)
	}
}

// The sections are drawn — with their counts — and they replace the sidebar's own
// HOSTS title rather than sitting under a second copy of the word.
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

// The mouse maps rows through the same bookkeeping the renderer uses, so with the
// sections on a click still lands on the host under the pointer — and a click on a
// heading selects nothing.
func TestClickWithSections(t *testing.T) {
	m := pinModel(t)
	m.cursor = 2
	m.handleKey(key(t, "p"))
	m.recomputeLayout()

	// Rows: the screen header (0), the sidebar border (1), then #PINNED, c,
	// #HOSTS, a, b — the title row is gone, the sections having taken it.
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

// A filter must not reshuffle the PINNED section: it is drawn in the order the
// user arranged, whatever the fuzzy matcher thinks of the aliases, because that is
// the order shift+j/k move in. Drawing it by match score would leave the reorder
// keys moving a host somewhere other than where it appears to be.
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

	// And the reorder keys agree with what is drawn: shift+k on the second row
	// moves it above the first, rather than asking the store to move a host that
	// was already at the top.
	m.cursor = 1
	m.handleKey(key(t, "K"))
	if got, want := listOrder(m), []string{"dev", "prod"}; strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("order after shift+k under a filter = %v, want %v", got, want)
	}
}

// Paging is measured in drawn rows, headings included — the screen is. A page must
// therefore never step over a host that was not on screen.
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

	// A page is a screen of rows: paging by a count of *hosts* would step over the
	// headings as well and land a host or two past what the screen ever showed.
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
