package store

import "testing"

// The unpinned section is ordered by visits desc, so the host reached most often is the
// one under the cursor when the list opens.
func TestHostsOrderByVisitsDesc(t *testing.T) {
	s := newStore(t)
	for alias, visits := range map[string]int{"rare": 1, "often": 9, "sometimes": 4} {
		if _, err := s.Upsert(Host{Alias: alias, HostName: alias + ".test", Visits: visits}); err != nil {
			t.Fatalf("Upsert %s: %v", alias, err)
		}
	}
	if got, want := aliases(t, s), []string{"often", "sometimes", "rare"}; !equal(got, want) {
		t.Fatalf("order = %v, want %v", got, want)
	}
}

// Equal visit counts are broken by the most recent connection: two hosts used the same
// number of times are not interchangeable, the one used today is the likelier target.
func TestHostsBreakVisitTiesByLastConnect(t *testing.T) {
	s := newStore(t)
	for alias, last := range map[string]int64{"stale": 1_000, "fresh": 9_000, "older": 5_000} {
		if _, err := s.Upsert(Host{Alias: alias, HostName: alias + ".test", Visits: 3, LastConnect: last}); err != nil {
			t.Fatalf("Upsert %s: %v", alias, err)
		}
	}
	if got, want := aliases(t, s), []string{"fresh", "older", "stale"}; !equal(got, want) {
		t.Fatalf("order = %v, want %v", got, want)
	}
}

// A never-connected host sorts below every visited one — including one visited a single
// time — rather than riding at the top on a zero timestamp.
func TestHostsPutNeverVisitedLast(t *testing.T) {
	s := newStore(t)
	if _, err := s.Upsert(Host{Alias: "new", HostName: "new.test"}); err != nil {
		t.Fatalf("Upsert new: %v", err)
	}
	if _, err := s.Upsert(Host{Alias: "used", HostName: "used.test"}); err != nil {
		t.Fatalf("Upsert used: %v", err)
	}
	if err := s.Touch("used"); err != nil {
		t.Fatalf("Touch: %v", err)
	}
	if got, want := aliases(t, s), []string{"used", "new"}; !equal(got, want) {
		t.Fatalf("order = %v, want %v", got, want)
	}
}

// Touch is what moves a host up the list: enough connections and yesterday's third
// choice leads it. It counts every call, not every distinct day.
func TestTouchLiftsAHostPastTheOthers(t *testing.T) {
	s := newStore(t)
	seedFrecency(t, s) // a:30, b:20, c:10 — in that order

	for range 21 {
		if err := s.Touch("c"); err != nil {
			t.Fatalf("Touch: %v", err)
		}
	}
	if got, want := aliases(t, s), []string{"c", "a", "b"}; !equal(got, want) {
		t.Fatalf("order after touching c = %v, want %v", got, want)
	}

	h := findHost(t, s, "c")
	if h.Visits != 31 {
		t.Fatalf("visits = %d, want 31", h.Visits)
	}
	if h.LastConnect == 0 {
		t.Fatal("Touch left last_connect unset")
	}
}

// Editing a host must not cost it its place: Upsert rewrites the connection details and
// leaves the frecency Touch accumulated alone, even when the caller passes zeroes.
func TestUpsertKeepsFrecencyOnEdit(t *testing.T) {
	s := newStore(t)
	seedFrecency(t, s)
	if err := s.Touch("a"); err != nil {
		t.Fatalf("Touch: %v", err)
	}
	before := findHost(t, s, "a")

	if _, err := s.Upsert(Host{Alias: "a", HostName: "a-renamed.test", User: "deploy"}); err != nil {
		t.Fatalf("Upsert: %v", err)
	}
	after := findHost(t, s, "a")
	if after.HostName != "a-renamed.test" || after.User != "deploy" {
		t.Fatalf("edit did not land: %+v", after)
	}
	if after.Visits != before.Visits || after.LastConnect != before.LastConnect {
		t.Fatalf("frecency reset: visits %d->%d, last_connect %d->%d",
			before.Visits, after.Visits, before.LastConnect, after.LastConnect)
	}
}

// Touching an alias no host carries is not an error — a session can outlive the row it
// was started from, and there is nothing for the caller to do about it.
func TestTouchUnknownAliasIsANoOp(t *testing.T) {
	s := newStore(t)
	seedFrecency(t, s)
	if err := s.Touch("nobody"); err != nil {
		t.Fatalf("Touch unknown: %v", err)
	}
	if got := aliases(t, s); len(got) != 3 {
		t.Fatalf("hosts = %v, want the three seeded", got)
	}
}
