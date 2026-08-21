package store

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// newStore opens a fresh, isolated store backed by files in a temp dir.
func newStore(t *testing.T) *Store {
	t.Helper()
	s, err := OpenAt(filepath.Join(t.TempDir(), "hop.config"), "")
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

	id, err := s.Upsert(Host{Alias: "temp", HostName: "temp.example.com"})
	if err != nil {
		t.Fatalf("Upsert: %v", err)
	}
	if _, err := s.AddForward(id, Forward{Kind: ForwardLocal, BindPort: 8080, TargetHost: "localhost", TargetPort: 80}); err != nil {
		t.Fatalf("AddForward: %v", err)
	}
	if err := s.Delete("temp"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if h := findHost(t, s, "temp"); h != nil {
		t.Fatalf("host %q still present after Delete", "temp")
	}
	hosts, err := s.Hosts()
	if err != nil {
		t.Fatalf("Hosts: %v", err)
	}
	forwards := 0
	for _, h := range hosts {
		forwards += len(h.Forwards)
	}
	if forwards != 0 {
		t.Fatalf("deleted host left %d forwarding definitions behind", forwards)
	}
}

func TestForwardCRUDAndHostLoading(t *testing.T) {
	s := newStore(t)
	hostID, err := s.Add(Host{Alias: "web", HostName: "web.example.com"})
	if err != nil {
		t.Fatalf("Add host: %v", err)
	}

	local := Forward{Kind: ForwardLocal, BindHost: "127.0.0.1", BindPort: 8080, TargetHost: "app.internal", TargetPort: 80}
	id, err := s.AddForward(hostID, local)
	if err != nil {
		t.Fatalf("AddForward: %v", err)
	}
	h := findHost(t, s, "web")
	if h == nil || len(h.Forwards) != 1 {
		t.Fatalf("loaded host forwards = %+v, want one", h)
	}
	got := h.Forwards[0]
	if got.ID != id || got.HostID != hostID || got.Kind != ForwardLocal || got.TargetHost != "app.internal" {
		t.Fatalf("loaded forward = %+v", got)
	}

	got.Kind = ForwardRemote
	got.BindHost = ""
	got.BindPort = 9090
	got.TargetHost = "127.0.0.1"
	got.TargetPort = 3000
	if err := s.UpdateForward(got); err != nil {
		t.Fatalf("UpdateForward: %v", err)
	}
	h = findHost(t, s, "web")
	if len(h.Forwards) != 1 || h.Forwards[0].Kind != ForwardRemote || h.Forwards[0].BindPort != 9090 {
		t.Fatalf("updated forwards = %+v", h.Forwards)
	}

	if err := s.DeleteForward(hostID, id); err != nil {
		t.Fatalf("DeleteForward: %v", err)
	}
	if h = findHost(t, s, "web"); len(h.Forwards) != 0 {
		t.Fatalf("deleted forward still loaded: %+v", h.Forwards)
	}
}

func TestForwardValidationAndDuplicateListener(t *testing.T) {
	s := newStore(t)
	hostID, err := s.Add(Host{Alias: "web", HostName: "web.example.com"})
	if err != nil {
		t.Fatalf("Add host: %v", err)
	}

	invalid := []Forward{
		{Kind: "dynamic", BindPort: 8080, TargetHost: "x", TargetPort: 80},
		{Kind: ForwardLocal, BindPort: 0, TargetHost: "x", TargetPort: 80},
		{Kind: ForwardLocal, BindPort: 8080, TargetHost: "", TargetPort: 80},
		{Kind: ForwardLocal, BindPort: 8080, TargetHost: "x", TargetPort: 70000},
	}
	for _, f := range invalid {
		if _, err := s.AddForward(hostID, f); err == nil {
			t.Fatalf("AddForward(%+v) succeeded, want validation error", f)
		}
	}

	f := Forward{Kind: ForwardLocal, BindHost: "127.0.0.1", BindPort: 8080, TargetHost: "x", TargetPort: 80}
	if _, err := s.AddForward(hostID, f); err != nil {
		t.Fatalf("first AddForward: %v", err)
	}
	f.TargetPort = 81
	if _, err := s.AddForward(hostID, f); err == nil {
		t.Fatal("duplicate listener was accepted")
	}
}

func TestImportSSHConfigIncludesTCPForwards(t *testing.T) {
	s := newStore(t)
	path := filepath.Join(t.TempDir(), "config")
	config := `
Host web
  HostName web.example.com
  User deploy
  LocalForward 15432 db.internal:5432
  LocalForward [::1]:16379 cache.internal:6379
  RemoteForward 127.0.0.1:18080 127.0.0.1:3000
  DynamicForward 1080

Host *
  LocalForward 19090 metrics.internal:9090
`
	if err := os.WriteFile(path, []byte(config), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	if n, err := s.ImportSSHConfig(path); err != nil || n != 1 {
		t.Fatalf("ImportSSHConfig = %d, %v; want 1, nil", n, err)
	}

	h := findHost(t, s, "web")
	if h == nil {
		t.Fatal("imported host missing")
	}
	if len(h.Forwards) != 4 {
		t.Fatalf("imported forwards = %+v, want 4 TCP forwards", h.Forwards)
	}
	got := map[string]Forward{}
	for _, f := range h.Forwards {
		got[string(f.Kind)+":"+f.BindHost+":"+strconv.Itoa(f.BindPort)] = f
	}
	if f := got["local:127.0.0.1:15432"]; f.TargetHost != "db.internal" || f.TargetPort != 5432 {
		t.Fatalf("local forward = %+v", f)
	}
	if f := got["local:::1:16379"]; f.TargetHost != "cache.internal" || f.TargetPort != 6379 {
		t.Fatalf("IPv6 local forward = %+v", f)
	}
	if f := got["remote:127.0.0.1:18080"]; f.TargetPort != 3000 {
		t.Fatalf("remote forward = %+v", f)
	}
	if f := got["local:127.0.0.1:19090"]; f.TargetHost != "metrics.internal" {
		t.Fatalf("wildcard forward = %+v", f)
	}

	// Re-import updates a listener's target rather than duplicating it.
	config = strings.Replace(config, "db.internal:5432", "db-new.internal:6432", 1)
	if err := os.WriteFile(path, []byte(config), 0o600); err != nil {
		t.Fatalf("rewrite config: %v", err)
	}
	if _, err := s.ImportSSHConfig(path); err != nil {
		t.Fatalf("re-import: %v", err)
	}
	h = findHost(t, s, "web")
	if len(h.Forwards) != 4 {
		t.Fatalf("re-import duplicated forwards: %+v", h.Forwards)
	}
	for _, f := range h.Forwards {
		if f.Kind == ForwardLocal && f.BindPort == 15432 && (f.TargetHost != "db-new.internal" || f.TargetPort != 6432) {
			t.Fatalf("re-import did not update target: %+v", f)
		}
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

// seedFrecency adds three hosts whose visit counts put them in the order a, b, c.
func seedFrecency(t *testing.T, s *Store) {
	t.Helper()
	for i, alias := range []string{"a", "b", "c"} {
		if _, err := s.Upsert(Host{Alias: alias, HostName: alias + ".test", Visits: 30 - i*10}); err != nil {
			t.Fatalf("Upsert %s: %v", alias, err)
		}
	}
}

// Unpinning puts a host back where frecency had it.
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

// In particular, re-pinning does not move a host to the end of its own section.
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

// Removing from the middle of the section leaves no hole: pin_order stays 1..n.
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

	if _, err := s.MovePin("c", -1); err != nil {
		t.Fatalf("MovePin: %v", err)
	}
	if got, want := aliases(t, s), []string{"c", "a", "b"}; !equal(got, want) {
		t.Fatalf("order = %v, want %v", got, want)
	}
}

// A pre-pin-columns database migrates, with the missing columns read as zero values.
func TestOpenMigratesFirstSchema(t *testing.T) {
	path := copyFixture(t, "legacy-hop-v1.db")

	s, err := OpenAt(path, "")
	if err != nil {
		t.Fatalf("OpenAt on a first-schema database: %v", err)
	}
	defer s.Close()

	h := findHost(t, s, "old")
	if h == nil {
		t.Fatal("the host from the old database is gone")
	}
	if h.Pinned || h.PinOrder != 0 {
		t.Fatalf("migrated host = %+v, want it unpinned", h)
	}
	if h.DefaultDir != "" {
		t.Fatalf("migrated host = %+v, want an empty DefaultDir", h)
	}
	if h.HostName != "old.test" || h.User != "me" || h.Visits != 3 || h.LastConnect != 1690000000 {
		t.Fatalf("migrated host = %+v, want the old values preserved", h)
	}
	if len(h.Tags) != 1 || h.Tags[0] != "legacy" || h.Group != "team" {
		t.Fatalf("migrated host = %+v, want tags [legacy] and group team", h)
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

// Both write paths keep the default directory, and Upsert can change or clear it.
func TestDefaultDirRoundTrips(t *testing.T) {
	s := newStore(t)

	if _, err := s.Add(Host{Alias: "web", HostName: "web.test", DefaultDir: "/srv/app"}); err != nil {
		t.Fatalf("Add: %v", err)
	}
	if h := findHost(t, s, "web"); h == nil || h.DefaultDir != "/srv/app" {
		t.Fatalf("after Add, host = %+v, want DefaultDir /srv/app", h)
	}

	if _, err := s.Upsert(Host{Alias: "web", HostName: "web.test", DefaultDir: "~/work"}); err != nil {
		t.Fatalf("Upsert: %v", err)
	}
	if h := findHost(t, s, "web"); h == nil || h.DefaultDir != "~/work" {
		t.Fatalf("after Upsert, host = %+v, want DefaultDir ~/work", h)
	}

	if _, err := s.Upsert(Host{Alias: "web", HostName: "web.test"}); err != nil {
		t.Fatalf("clearing Upsert: %v", err)
	}
	if h := findHost(t, s, "web"); h == nil || h.DefaultDir != "" {
		t.Fatalf("after clearing, host = %+v, want an empty DefaultDir", h)
	}
}

func TestImportSSHConfigReadsProxyDirectives(t *testing.T) {
	s := newStore(t)
	path := filepath.Join(t.TempDir(), "config")
	config := `
Host ssm
  HostName i-0123456789abcdef0
  ProxyCommand aws ssm start-session --target %h --document-name AWS-StartSSHSession --parameters portNumber=%p

Host db01
  HostName db01.internal
  ProxyJump bastion.example.com

Host plain
  HostName plain.example.com
  ProxyCommand none
`
	if err := os.WriteFile(path, []byte(config), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	if n, err := s.ImportSSHConfig(path); err != nil || n != 3 {
		t.Fatalf("ImportSSHConfig = %d, %v; want 3, nil", n, err)
	}

	if got := findHost(t, s, "ssm").ProxyCommand; !strings.Contains(got, "aws ssm start-session") {
		t.Errorf("ssm ProxyCommand = %q, want the aws ssm line", got)
	}
	if got := findHost(t, s, "db01").ProxyJump; got != "bastion.example.com" {
		t.Errorf("db01 ProxyJump = %q, want %q", got, "bastion.example.com")
	}
	// "none" disables the directive in ssh; carrying it over would run a program called "none".
	if got := findHost(t, s, "plain").ProxyCommand; got != "" {
		t.Errorf("plain ProxyCommand = %q, want empty", got)
	}
}

func TestImportSSHConfigRefreshesProxyOnReimport(t *testing.T) {
	s := newStore(t)
	if _, err := s.Add(Host{Alias: "ssm", HostName: "old.example.com"}); err != nil {
		t.Fatalf("Add: %v", err)
	}

	path := filepath.Join(t.TempDir(), "config")
	config := `
Host ssm
  HostName i-0123456789abcdef0
  ProxyCommand aws ssm start-session --target %h
`
	if err := os.WriteFile(path, []byte(config), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	if _, err := s.ImportSSHConfig(path); err != nil {
		t.Fatalf("ImportSSHConfig: %v", err)
	}

	h := findHost(t, s, "ssm")
	if h.ProxyCommand != "aws ssm start-session --target %h" {
		t.Errorf("ProxyCommand = %q, want it filled in by the re-import", h.ProxyCommand)
	}
}

func TestUpsertRoundTripsProxyFields(t *testing.T) {
	s := newStore(t)
	if _, err := s.Upsert(Host{Alias: "db01", HostName: "db01.internal", ProxyJump: "jump@bastion:2222"}); err != nil {
		t.Fatalf("Upsert: %v", err)
	}
	if got := findHost(t, s, "db01").ProxyJump; got != "jump@bastion:2222" {
		t.Errorf("ProxyJump = %q, want %q", got, "jump@bastion:2222")
	}

	if _, err := s.Upsert(Host{Alias: "db01", HostName: "db01.internal"}); err != nil {
		t.Fatalf("Upsert: %v", err)
	}
	if got := findHost(t, s, "db01").ProxyJump; got != "" {
		t.Errorf("ProxyJump = %q after clearing, want empty", got)
	}
}

func TestHostByAlias(t *testing.T) {
	s := newStore(t)
	if _, err := s.Add(Host{Alias: "bastion", HostName: "b.example.com", User: "ops", Port: 2222, Tags: []string{"edge"}}); err != nil {
		t.Fatalf("Add: %v", err)
	}

	h, ok, err := s.HostByAlias("bastion")
	if err != nil || !ok {
		t.Fatalf("HostByAlias = %v, %v; want a host", ok, err)
	}
	if h.HostName != "b.example.com" || h.User != "ops" || h.Port != 2222 {
		t.Errorf("HostByAlias = %+v, want the stored values", h)
	}
	if len(h.Tags) != 1 || h.Tags[0] != "edge" {
		t.Errorf("Tags = %v, want [edge]", h.Tags)
	}

	// A miss is not an error: the jump resolver falls back to a bare hostname.
	if _, ok, err := s.HostByAlias("nope"); err != nil || ok {
		t.Errorf("HostByAlias(nope) = %v, %v; want false, nil", ok, err)
	}
}

// Add and Upsert share one INSERT, so a field reaching only one path must not pass unnoticed.
func TestAddRoundTripsAllFields(t *testing.T) {
	s := newStore(t)

	want := Host{
		Alias:        "bastioned",
		HostName:     "internal.example.com",
		User:         "deploy",
		Port:         2222,
		IdentityFile: "~/.ssh/id_ed25519",
		Tags:         []string{"prod", "eu"},
		Group:        "core",
		DefaultDir:   "/srv/app",
		ProxyCommand: "cloudflared access ssh --hostname %h",
		ProxyJump:    "jump.example.com",
	}
	if _, err := s.Add(want); err != nil {
		t.Fatalf("Add: %v", err)
	}

	got, ok, err := s.HostByAlias("bastioned")
	if err != nil || !ok {
		t.Fatalf("HostByAlias: %v, ok=%v", err, ok)
	}
	if got.HostName != want.HostName || got.User != want.User || got.Port != want.Port ||
		got.IdentityFile != want.IdentityFile || got.Group != want.Group ||
		got.DefaultDir != want.DefaultDir || got.ProxyCommand != want.ProxyCommand ||
		got.ProxyJump != want.ProxyJump || strings.Join(got.Tags, ",") != "prod,eu" {
		t.Fatalf("Add lost fields: %+v", got)
	}
}

// copyFixture copies a checked-in legacy database into a temp dir.
func copyFixture(t *testing.T, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	path := filepath.Join(t.TempDir(), "hop.db")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	return path
}

// The last SQLite release's schema migrates with every field, forward and pin intact.
func TestMigrateFullLegacyDatabase(t *testing.T) {
	path := copyFixture(t, "legacy-hop.db")

	s, err := OpenAt(path, "")
	if err != nil {
		t.Fatalf("OpenAt on a legacy database: %v", err)
	}
	defer s.Close()

	hosts, err := s.Hosts()
	if err != nil {
		t.Fatalf("Hosts: %v", err)
	}
	if len(hosts) != 5 {
		t.Fatalf("got %d hosts, want 5", len(hosts))
	}

	if hosts[0].Alias != "web-prod" || hosts[1].Alias != "db-prod" {
		t.Fatalf("order = %s, %s; want the pinned hosts first", hosts[0].Alias, hosts[1].Alias)
	}
	if !hosts[0].Pinned || hosts[0].PinOrder != 1 || !hosts[1].Pinned || hosts[1].PinOrder != 2 {
		t.Fatalf("pins = %+v / %+v", hosts[0], hosts[1])
	}

	web := findHost(t, s, "web-prod")
	if web.HostName != "10.1.0.5" || web.User != "deploy" || web.Port != 2222 {
		t.Fatalf("web-prod = %+v", web)
	}
	if web.IdentityFile != "~/.ssh/id_ed25519" || web.DefaultDir != "/srv/app" || web.ProxyJump != "bastion" {
		t.Fatalf("web-prod = %+v", web)
	}
	if web.Visits != 42 || web.LastConnect != 1700000000 {
		t.Fatalf("web-prod frecency = %d visits, %d", web.Visits, web.LastConnect)
	}
	if len(web.Tags) != 2 || web.Tags[0] != "prod" || web.Tags[1] != "web" || web.Group != "eu-west" {
		t.Fatalf("web-prod tags = %v, group = %q", web.Tags, web.Group)
	}
	if len(web.Forwards) != 1 || web.Forwards[0].BindPort != 8080 || web.Forwards[0].TargetPort != 80 {
		t.Fatalf("web-prod forwards = %+v", web.Forwards)
	}

	db := findHost(t, s, "db-prod")
	if len(db.Forwards) != 2 {
		t.Fatalf("db-prod forwards = %+v, want 2", db.Forwards)
	}
	var local, remote int
	for _, f := range db.Forwards {
		switch f.Kind {
		case ForwardLocal:
			local++
		case ForwardRemote:
			remote++
		}
	}
	if local != 1 || remote != 1 {
		t.Fatalf("db-prod forwards = %+v, want one of each kind", db.Forwards)
	}

	// A ProxyCommand far longer than one SQLite page.
	broker := findHost(t, s, "via-broker")
	if len(broker.ProxyCommand) < 9000 {
		t.Fatalf("via-broker ProxyCommand = %d chars, want the full long value", len(broker.ProxyCommand))
	}
	if !strings.Contains(broker.ProxyCommand, "aws ssm start-session") {
		t.Fatalf("via-broker ProxyCommand = %.60q...", broker.ProxyCommand)
	}
}

// The database moves aside under .bak rather than being deleted, and runs once.
func TestMigrateKeepsBackupAndRunsOnce(t *testing.T) {
	path := copyFixture(t, "legacy-hop.db")

	s, err := OpenAt(path, "")
	if err != nil {
		t.Fatalf("OpenAt: %v", err)
	}
	if _, err := s.Add(Host{Alias: "added-after", HostName: "new.test"}); err != nil {
		t.Fatalf("Add: %v", err)
	}
	s.Close()

	if _, err := os.Stat(path + ".bak"); err != nil {
		t.Fatalf("the original database was not kept: %v", err)
	}
	if isSQLiteFile(path) {
		t.Fatal("the hosts file is still a SQLite database")
	}

	// Reopening reads the config file, not the backup.
	again, err := OpenAt(path, "")
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer again.Close()
	hosts, err := again.Hosts()
	if err != nil {
		t.Fatalf("Hosts: %v", err)
	}
	if len(hosts) != 6 {
		t.Fatalf("got %d hosts after reopen, want 6", len(hosts))
	}
	if findHost(t, again, "added-after") == nil {
		t.Fatal("the host added after migrating is gone")
	}
}

// Hosts live where OpenSSH reads them, metadata in hop's config.json; both must survive.
func TestHostsAndMetadataSplitAcrossDirectories(t *testing.T) {
	sshDir, cfgDir := t.TempDir(), t.TempDir()
	hostsPath := filepath.Join(sshDir, "hop.config")
	metaPath := filepath.Join(cfgDir, "hop", "config.json")

	s, err := OpenAt(hostsPath, metaPath)
	if err != nil {
		t.Fatalf("OpenAt: %v", err)
	}
	if _, err := s.Add(Host{Alias: "web", HostName: "web.test", Tags: []string{"prod"}, Group: "eu"}); err != nil {
		t.Fatalf("Add: %v", err)
	}
	if err := s.SetPinned("web", true); err != nil {
		t.Fatalf("SetPinned: %v", err)
	}
	if err := s.Touch("web"); err != nil {
		t.Fatalf("Touch: %v", err)
	}
	s.Close()

	// Nothing hop-only leaked into the directory OpenSSH reads.
	entries, err := os.ReadDir(sshDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "hop.config" {
		t.Fatalf("~/.ssh got %v, want only hop.config", entries)
	}
	if _, err := os.Stat(metaPath); err != nil {
		t.Fatalf("the metadata was not written to its own directory: %v", err)
	}

	again, err := OpenAt(hostsPath, metaPath)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer again.Close()
	h := findHost(t, again, "web")
	if h == nil {
		t.Fatal("host gone after reopen")
	}
	if h.HostName != "web.test" {
		t.Fatalf("host = %+v, want the config-file half", h)
	}
	if !h.Pinned || h.PinOrder != 1 || h.Visits != 1 || h.Group != "eu" {
		t.Fatalf("host = %+v, want the sidecar half", h)
	}
	if len(h.Tags) != 1 || h.Tags[0] != "prod" {
		t.Fatalf("tags = %v", h.Tags)
	}
}

// A hand-added host shows up with zero metadata rather than none at all.
func TestHandWrittenHostIsPickedUp(t *testing.T) {
	dir := t.TempDir()
	hostsPath := filepath.Join(dir, "hop.config")

	s, err := OpenAt(hostsPath, "")
	if err != nil {
		t.Fatalf("OpenAt: %v", err)
	}
	if _, err := s.Add(Host{Alias: "known", HostName: "known.test"}); err != nil {
		t.Fatalf("Add: %v", err)
	}
	s.Close()

	existing, err := os.ReadFile(hostsPath)
	if err != nil {
		t.Fatal(err)
	}
	added := string(existing) + "\nHost byhand\n    HostName byhand.test\n    User someone\n"
	if err := os.WriteFile(hostsPath, []byte(added), 0o600); err != nil {
		t.Fatal(err)
	}

	again, err := OpenAt(hostsPath, "")
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer again.Close()
	h := findHost(t, again, "byhand")
	if h == nil {
		t.Fatal("the hand-written host was not picked up")
	}
	if h.HostName != "byhand.test" || h.User != "someone" || h.ID == 0 {
		t.Fatalf("host = %+v", h)
	}
	if findHost(t, again, "known") == nil {
		t.Fatal("the host hop wrote is gone")
	}
}

func TestSidecarIsPrunedOfDeletedHosts(t *testing.T) {
	dir := t.TempDir()
	hostsPath := filepath.Join(dir, "hop.config")
	metaPath := hostsPath + ".json"

	s, err := OpenAt(hostsPath, metaPath)
	if err != nil {
		t.Fatalf("OpenAt: %v", err)
	}
	if _, err := s.Add(Host{Alias: "gone", HostName: "gone.test", Tags: []string{"stale"}}); err != nil {
		t.Fatalf("Add: %v", err)
	}
	if err := s.Delete("gone"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	s.Close()

	data, err := os.ReadFile(metaPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "gone") {
		t.Fatalf("the metadata still carries the deleted host: %s", data)
	}
}

// config.json is shared with the settings, so writing a host must merge into it.
func TestPersistPreservesSettingsInSharedFile(t *testing.T) {
	dir := t.TempDir()
	hostsPath := filepath.Join(dir, "hop.config")
	metaPath := filepath.Join(dir, "config.json")

	settings := `{"editor":"nvim","accent":"99","vimKeys":true}`
	if err := os.WriteFile(metaPath, []byte(settings), 0o600); err != nil {
		t.Fatal(err)
	}

	s, err := OpenAt(hostsPath, metaPath)
	if err != nil {
		t.Fatalf("OpenAt: %v", err)
	}
	if _, err := s.Add(Host{Alias: "web", HostName: "web.test", Tags: []string{"prod"}}); err != nil {
		t.Fatalf("Add: %v", err)
	}
	if err := s.Touch("web"); err != nil {
		t.Fatalf("Touch: %v", err)
	}
	s.Close()

	data, err := os.ReadFile(metaPath)
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatalf("the saved file does not parse: %v\n%s", err, data)
	}
	if doc["editor"] != "nvim" || doc["accent"] != "99" || doc["vimKeys"] != true {
		t.Fatalf("the settings were dropped: %s", data)
	}
	if _, ok := doc["hosts"]; !ok {
		t.Fatalf("the host metadata was not written: %s", data)
	}
}

func TestLoadIgnoresConfigWithoutHostMetadata(t *testing.T) {
	dir := t.TempDir()
	metaPath := filepath.Join(dir, "config.json")
	if err := os.WriteFile(metaPath, []byte(`{"editor":"nvim"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	s, err := OpenAt(filepath.Join(dir, "hop.config"), metaPath)
	if err != nil {
		t.Fatalf("OpenAt: %v", err)
	}
	defer s.Close()
	hosts, err := s.Hosts()
	if err != nil {
		t.Fatalf("Hosts: %v", err)
	}
	if len(hosts) != 0 {
		t.Fatalf("got %d hosts, want none", len(hosts))
	}
}
