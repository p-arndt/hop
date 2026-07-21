package store

import (
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
