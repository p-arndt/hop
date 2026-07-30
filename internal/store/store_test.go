package store

import (
	"database/sql"
	"path/filepath"
	"testing"
)

// newStore opens a fresh, isolated store backed by a temp-dir database.
func newStore(t *testing.T) *Store {
	t.Helper()
	s, err := OpenAt(filepath.Join(t.TempDir(), "hop.db"))
	if err != nil {
		t.Fatalf("OpenAt: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

// findHost returns the host with the given alias from Hosts(), or nil if absent.
func findHost(t *testing.T, s *Store, alias string) *Host {
	t.Helper()
	hosts, err := s.Hosts()
	if err != nil {
		t.Fatalf("Hosts: %v", err)
	}
	for i := range hosts {
		if hosts[i].Alias == alias {
			return &hosts[i]
		}
	}
	return nil
}

func TestUpsertAndHosts(t *testing.T) {
	s := newStore(t)

	if _, err := s.Upsert(Host{
		Alias:    "web",
		HostName: "web.example.com",
		User:     "deploy",
		Tags:     []string{"prod", "eu"},
	}); err != nil {
		t.Fatalf("Upsert: %v", err)
	}

	h := findHost(t, s, "web")
	if h == nil {
		t.Fatalf("host %q not found after upsert", "web")
	}
	if h.HostName != "web.example.com" {
		t.Fatalf("HostName = %q, want %q", h.HostName, "web.example.com")
	}
	if h.User != "deploy" {
		t.Fatalf("User = %q, want %q", h.User, "deploy")
	}
	if h.Port != 22 {
		t.Fatalf("Port = %d, want 22 (default when 0)", h.Port)
	}
	if len(h.Tags) != 2 || h.Tags[0] != "prod" || h.Tags[1] != "eu" {
		t.Fatalf("Tags = %v, want [prod eu]", h.Tags)
	}
}

func TestUpsertUpdatesExisting(t *testing.T) {
	s := newStore(t)

	if _, err := s.Upsert(Host{Alias: "db", HostName: "old.example.com"}); err != nil {
		t.Fatalf("first Upsert: %v", err)
	}
	if err := s.Touch("db"); err != nil {
		t.Fatalf("Touch: %v", err)
	}
	if _, err := s.Upsert(Host{Alias: "db", HostName: "new.example.com"}); err != nil {
		t.Fatalf("second Upsert: %v", err)
	}

	hosts, err := s.Hosts()
	if err != nil {
		t.Fatalf("Hosts: %v", err)
	}
	if len(hosts) != 1 {
		t.Fatalf("len(hosts) = %d, want 1", len(hosts))
	}
	h := hosts[0]
	if h.HostName != "new.example.com" {
		t.Fatalf("HostName = %q, want %q", h.HostName, "new.example.com")
	}
	if h.Visits != 1 {
		t.Fatalf("Visits = %d, want 1 (Upsert must not reset visits on conflict)", h.Visits)
	}
}

func TestDelete(t *testing.T) {
	s := newStore(t)

	if _, err := s.Upsert(Host{Alias: "temp", HostName: "temp.example.com"}); err != nil {
		t.Fatalf("Upsert: %v", err)
	}
	if err := s.Delete("temp"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if h := findHost(t, s, "temp"); h != nil {
		t.Fatalf("host %q still present after Delete", "temp")
	}
}

func TestAddInsertsNewHost(t *testing.T) {
	s := newStore(t)

	if _, err := s.Add(Host{Alias: "web", HostName: "web.example.com", User: "deploy"}); err != nil {
		t.Fatalf("Add: %v", err)
	}
	h := findHost(t, s, "web")
	if h == nil {
		t.Fatalf("host %q not found after Add", "web")
	}
	if h.Port != 22 {
		t.Fatalf("Port = %d, want 22 (default when 0)", h.Port)
	}
}

// Add must refuse a taken alias and, crucially, leave the existing row untouched —
// this is the guarantee the in-memory duplicate check alone could not make.
func TestAddRefusesDuplicateWithoutOverwriting(t *testing.T) {
	s := newStore(t)

	if _, err := s.Add(Host{Alias: "web", HostName: "original.example.com", User: "root"}); err != nil {
		t.Fatalf("first Add: %v", err)
	}
	if _, err := s.Add(Host{Alias: "web", HostName: "impostor.example.com", User: "eve"}); err == nil {
		t.Fatalf("Add onto existing alias: got nil error, want non-nil")
	}

	h := findHost(t, s, "web")
	if h == nil {
		t.Fatalf("host %q missing after refused Add", "web")
	}
	if h.HostName != "original.example.com" || h.User != "root" {
		t.Fatalf("existing host overwritten by refused Add: %+v", h)
	}
}

func TestRename(t *testing.T) {
	s := newStore(t)

	if _, err := s.Upsert(Host{Alias: "a", HostName: "a.example.com"}); err != nil {
		t.Fatalf("Upsert: %v", err)
	}
	if err := s.Touch("a"); err != nil {
		t.Fatalf("Touch: %v", err)
	}
	if err := s.Rename("a", "b"); err != nil {
		t.Fatalf("Rename: %v", err)
	}

	if h := findHost(t, s, "a"); h != nil {
		t.Fatalf("host %q still present after Rename", "a")
	}
	h := findHost(t, s, "b")
	if h == nil {
		t.Fatalf("host %q not found after Rename", "b")
	}
	if h.Visits != 1 {
		t.Fatalf("Visits = %d, want 1 (Rename must preserve visits)", h.Visits)
	}
	if h.HostName != "a.example.com" {
		t.Fatalf("HostName = %q, want %q", h.HostName, "a.example.com")
	}
}

func TestRenameNoop(t *testing.T) {
	s := newStore(t)

	if _, err := s.Upsert(Host{Alias: "a", HostName: "a.example.com"}); err != nil {
		t.Fatalf("Upsert: %v", err)
	}
	if err := s.Rename("a", "a"); err != nil {
		t.Fatalf("Rename no-op returned error: %v", err)
	}
	if h := findHost(t, s, "a"); h == nil {
		t.Fatalf("host %q missing after no-op Rename", "a")
	}
}

func TestRenameConflict(t *testing.T) {
	s := newStore(t)

	if _, err := s.Upsert(Host{Alias: "a", HostName: "a.example.com"}); err != nil {
		t.Fatalf("Upsert a: %v", err)
	}
	if _, err := s.Upsert(Host{Alias: "b", HostName: "b.example.com"}); err != nil {
		t.Fatalf("Upsert b: %v", err)
	}
	if err := s.Rename("a", "b"); err == nil {
		t.Fatalf("Rename onto existing alias: got nil error, want non-nil")
	}

	if h := findHost(t, s, "a"); h == nil {
		t.Fatalf("host %q missing after failed Rename", "a")
	}
	if h := findHost(t, s, "b"); h == nil || h.HostName != "b.example.com" {
		t.Fatalf("host %q altered or missing after failed Rename: %+v", "b", h)
	}
}

func TestRenameMissing(t *testing.T) {
	s := newStore(t)

	if err := s.Rename("ghost", "x"); err == nil {
		t.Fatalf("Rename of nonexistent host: got nil error, want non-nil")
	}
}

// ---- pinning ----

// aliases is Hosts() reduced to the order the list would draw.
func aliases(t *testing.T, s *Store) []string {
	t.Helper()
	hosts, err := s.Hosts()
	if err != nil {
		t.Fatalf("Hosts: %v", err)
	}
	out := make([]string, len(hosts))
	for i, h := range hosts {
		out[i] = h.Alias
	}
	return out
}

// seedFrecency adds three hosts whose visit counts put them in a known order:
// a, b, c.
func seedFrecency(t *testing.T, s *Store) {
	t.Helper()
	for i, alias := range []string{"a", "b", "c"} {
		if _, err := s.Upsert(Host{Alias: alias, HostName: alias + ".test", Visits: 30 - i*10}); err != nil {
			t.Fatalf("Upsert %s: %v", alias, err)
		}
	}
}

// A pinned host sorts above every unpinned one, whatever the visit counts say,
// and unpinning puts it back where frecency had it.
func TestSetPinnedOrdersAboveFrecency(t *testing.T) {
	s := newStore(t)
	seedFrecency(t, s)

	if got := aliases(t, s); got[0] != "a" {
		t.Fatalf("unpinned order = %v, want the most-visited host first", got)
	}

	if err := s.SetPinned("c", true); err != nil {
		t.Fatalf("SetPinned: %v", err)
	}
	if got, want := aliases(t, s), []string{"c", "a", "b"}; !equal(got, want) {
		t.Fatalf("order after pinning c = %v, want %v", got, want)
	}
	if h := findHost(t, s, "c"); h == nil || !h.Pinned || h.PinOrder != 1 {
		t.Fatalf("pinned host = %+v, want Pinned with PinOrder 1", h)
	}

	if err := s.SetPinned("c", false); err != nil {
		t.Fatalf("SetPinned off: %v", err)
	}
	if got, want := aliases(t, s), []string{"a", "b", "c"}; !equal(got, want) {
		t.Fatalf("order after unpinning c = %v, want frecency back: %v", got, want)
	}
	if h := findHost(t, s, "c"); h == nil || h.Pinned || h.PinOrder != 0 {
		t.Fatalf("unpinned host = %+v, want not pinned and PinOrder 0", h)
	}
}

// A new pin goes to the end of the section, so pinning one host never reshuffles
// the order already set for the others.
func TestSetPinnedAppendsToSection(t *testing.T) {
	s := newStore(t)
	seedFrecency(t, s)

	for _, alias := range []string{"c", "b"} {
		if err := s.SetPinned(alias, true); err != nil {
			t.Fatalf("SetPinned %s: %v", alias, err)
		}
	}
	if got, want := aliases(t, s), []string{"c", "b", "a"}; !equal(got, want) {
		t.Fatalf("order = %v, want the second pin under the first: %v", got, want)
	}
}

// Pinning a host that is already pinned changes nothing — in particular it does
// not move it to the end of its own section.
func TestSetPinnedTwiceIsANoOp(t *testing.T) {
	s := newStore(t)
	seedFrecency(t, s)

	for _, alias := range []string{"c", "b"} {
		if err := s.SetPinned(alias, true); err != nil {
			t.Fatalf("SetPinned %s: %v", alias, err)
		}
	}
	if err := s.SetPinned("c", true); err != nil {
		t.Fatalf("SetPinned again: %v", err)
	}
	if got, want := aliases(t, s), []string{"c", "b", "a"}; !equal(got, want) {
		t.Fatalf("order = %v, want it unchanged: %v", got, want)
	}
}

func TestSetPinnedUnknownHost(t *testing.T) {
	s := newStore(t)
	if err := s.SetPinned("nope", true); err == nil {
		t.Fatal("SetPinned on a missing host returned no error")
	}
}

// MovePin walks a host through its section and stops at both ends.
func TestMovePin(t *testing.T) {
	s := newStore(t)
	seedFrecency(t, s)
	for _, alias := range []string{"a", "b", "c"} {
		if err := s.SetPinned(alias, true); err != nil {
			t.Fatalf("SetPinned %s: %v", alias, err)
		}
	}

	moved, err := s.MovePin("c", -1)
	if err != nil || !moved {
		t.Fatalf("MovePin up = %v, %v; want it to move", moved, err)
	}
	if got, want := aliases(t, s), []string{"a", "c", "b"}; !equal(got, want) {
		t.Fatalf("order after moving c up = %v, want %v", got, want)
	}

	if _, err := s.MovePin("c", -1); err != nil {
		t.Fatalf("MovePin: %v", err)
	}
	if got, want := aliases(t, s), []string{"c", "a", "b"}; !equal(got, want) {
		t.Fatalf("order after a second move up = %v, want %v", got, want)
	}

	moved, err = s.MovePin("c", -1)
	if err != nil {
		t.Fatalf("MovePin at the top: %v", err)
	}
	if moved {
		t.Fatal("MovePin reported a move at the top of the section")
	}

	if _, err := s.MovePin("c", 2); err != nil {
		t.Fatalf("MovePin down 2: %v", err)
	}
	if got, want := aliases(t, s), []string{"a", "b", "c"}; !equal(got, want) {
		t.Fatalf("order after moving c to the end = %v, want %v", got, want)
	}
	if moved, _ := s.MovePin("c", 1); moved {
		t.Fatal("MovePin reported a move at the end of the section")
	}
}

// An unpinned host has no place in the section to move within, so MovePin leaves
// the list alone rather than failing.
func TestMovePinUnpinnedIsANoOp(t *testing.T) {
	s := newStore(t)
	seedFrecency(t, s)
	if err := s.SetPinned("c", true); err != nil {
		t.Fatalf("SetPinned: %v", err)
	}

	moved, err := s.MovePin("a", -1)
	if err != nil {
		t.Fatalf("MovePin: %v", err)
	}
	if moved {
		t.Fatal("MovePin moved an unpinned host")
	}
	if got, want := aliases(t, s), []string{"c", "a", "b"}; !equal(got, want) {
		t.Fatalf("order = %v, want it unchanged: %v", got, want)
	}
}

// Deleting or unpinning out of the middle of the section must not leave a hole
// that later moves trip over: pin_order stays 1..n.
func TestPinOrderStaysDense(t *testing.T) {
	s := newStore(t)
	seedFrecency(t, s)
	for _, alias := range []string{"a", "b", "c"} {
		if err := s.SetPinned(alias, true); err != nil {
			t.Fatalf("SetPinned %s: %v", alias, err)
		}
	}
	if err := s.SetPinned("b", false); err != nil {
		t.Fatalf("SetPinned off: %v", err)
	}

	hosts, err := s.Hosts()
	if err != nil {
		t.Fatalf("Hosts: %v", err)
	}
	want := 1
	for _, h := range hosts {
		if !h.Pinned {
			continue
		}
		if h.PinOrder != want {
			t.Fatalf("%s has PinOrder %d, want %d — the section is not dense", h.Alias, h.PinOrder, want)
		}
		want++
	}

	// And a move still lands where it should with the hole closed.
	if _, err := s.MovePin("c", -1); err != nil {
		t.Fatalf("MovePin: %v", err)
	}
	if got, want := aliases(t, s), []string{"c", "a", "b"}; !equal(got, want) {
		t.Fatalf("order = %v, want %v", got, want)
	}
}

// A database written before the pin columns existed opens, migrates and reads
// back — CREATE TABLE IF NOT EXISTS is a no-op on it, so the ALTER pass is the
// only thing standing between an old install and a crash on every query.
func TestOpenMigratesOldDatabase(t *testing.T) {
	path := filepath.Join(t.TempDir(), "hop.db")

	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	if _, err := db.Exec(`
		CREATE TABLE hosts (
			id            INTEGER PRIMARY KEY,
			alias         TEXT UNIQUE NOT NULL,
			hostname      TEXT,
			user          TEXT,
			port          INTEGER DEFAULT 22,
			identity_file TEXT,
			tags          TEXT,
			grp           TEXT,
			visits        INTEGER DEFAULT 0,
			last_connect  INTEGER DEFAULT 0
		);
		INSERT INTO hosts (alias, hostname, user, identity_file, tags, grp)
		VALUES ('old', 'old.test', 'me', '', '', '');`); err != nil {
		t.Fatalf("seeding the old schema: %v", err)
	}
	db.Close()

	s, err := OpenAt(path)
	if err != nil {
		t.Fatalf("OpenAt on an old database: %v", err)
	}
	defer s.Close()

	h := findHost(t, s, "old")
	if h == nil {
		t.Fatal("the host from the old database is gone")
	}
	if h.Pinned {
		t.Fatalf("migrated host = %+v, want it unpinned", h)
	}
	if err := s.SetPinned("old", true); err != nil {
		t.Fatalf("SetPinned after migrating: %v", err)
	}
	if h := findHost(t, s, "old"); h == nil || !h.Pinned || h.PinOrder != 1 {
		t.Fatalf("host after pinning = %+v, want Pinned with PinOrder 1", h)
	}
}

func equal(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
