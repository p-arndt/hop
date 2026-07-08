package store

import (
	"path/filepath"
	"testing"
)

// newStore opens a throwaway database for one test.
func newStore(t *testing.T) *Store {
	t.Helper()
	st, err := OpenAt(filepath.Join(t.TempDir(), "hop.db"))
	if err != nil {
		t.Fatalf("OpenAt: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	return st
}

// withHost opens a store holding a single host named alias.
func withHost(t *testing.T, alias string) *Store {
	t.Helper()
	st := newStore(t)
	if _, err := st.Upsert(Host{Alias: alias, HostName: alias + ".example"}); err != nil {
		t.Fatalf("Upsert: %v", err)
	}
	return st
}

// paths projects the Path of each Dir, for comparing against an expected order.
func paths(dirs []Dir) []string {
	out := make([]string, len(dirs))
	for i, d := range dirs {
		out[i] = d.Path
	}
	return out
}

func TestTouchDirCountsVisits(t *testing.T) {
	st := withHost(t, "web1")

	for i := 0; i < 3; i++ {
		if err := st.TouchDir("web1", "/srv/app"); err != nil {
			t.Fatalf("TouchDir: %v", err)
		}
	}

	dirs, err := st.Dirs("web1", 0)
	if err != nil {
		t.Fatalf("Dirs: %v", err)
	}
	if len(dirs) != 1 {
		t.Fatalf("got %d dirs, want 1 (repeat visits must not duplicate the row)", len(dirs))
	}
	if dirs[0].Visits != 3 {
		t.Fatalf("visits = %d, want 3", dirs[0].Visits)
	}
	if dirs[0].LastVisit == 0 {
		t.Fatal("last_visit not stamped")
	}
}

// Dirs ranks by visit count first, so the directory you keep returning to leads
// even when another was seen more recently.
func TestDirsRankedByVisits(t *testing.T) {
	st := withHost(t, "web1")

	st.TouchDir("web1", "/var/log") // once
	st.TouchDir("web1", "/srv/app") // twice, and later
	st.TouchDir("web1", "/srv/app")

	dirs, err := st.Dirs("web1", 0)
	if err != nil {
		t.Fatalf("Dirs: %v", err)
	}
	want := []string{"/srv/app", "/var/log"}
	if got := paths(dirs); !equal(got, want) {
		t.Fatalf("order = %v, want %v", got, want)
	}
}

func TestDirsLimit(t *testing.T) {
	st := withHost(t, "web1")
	for _, p := range []string{"/a", "/b", "/c"} {
		st.TouchDir("web1", p)
	}

	dirs, err := st.Dirs("web1", 2)
	if err != nil {
		t.Fatalf("Dirs: %v", err)
	}
	if len(dirs) != 2 {
		t.Fatalf("got %d dirs, want 2", len(dirs))
	}
}

// Directories are scoped to their host: two hosts may share a path without
// seeing each other's history.
func TestDirsScopedToHost(t *testing.T) {
	st := withHost(t, "web1")
	if _, err := st.Upsert(Host{Alias: "db1"}); err != nil {
		t.Fatalf("Upsert: %v", err)
	}

	st.TouchDir("web1", "/srv/app")
	st.TouchDir("db1", "/var/lib/postgres")

	web, _ := st.Dirs("web1", 0)
	if got := paths(web); !equal(got, []string{"/srv/app"}) {
		t.Fatalf("web1 dirs = %v, want [/srv/app]", got)
	}
	db, _ := st.Dirs("db1", 0)
	if got := paths(db); !equal(got, []string{"/var/lib/postgres"}) {
		t.Fatalf("db1 dirs = %v, want [/var/lib/postgres]", got)
	}
}

// Touching a directory on an unknown host is a no-op, not an error: the caller
// need not prove the host still exists.
func TestTouchDirUnknownHostIsInert(t *testing.T) {
	st := newStore(t)

	if err := st.TouchDir("ghost", "/tmp"); err != nil {
		t.Fatalf("TouchDir on unknown host: %v", err)
	}
	dirs, err := st.Dirs("ghost", 0)
	if err != nil {
		t.Fatalf("Dirs: %v", err)
	}
	if len(dirs) != 0 {
		t.Fatalf("got %d dirs, want 0", len(dirs))
	}
}

func TestForgetDir(t *testing.T) {
	st := withHost(t, "web1")
	st.TouchDir("web1", "/a")
	st.TouchDir("web1", "/b")

	if err := st.ForgetDir("web1", "/a"); err != nil {
		t.Fatalf("ForgetDir: %v", err)
	}

	dirs, _ := st.Dirs("web1", 0)
	if got := paths(dirs); !equal(got, []string{"/b"}) {
		t.Fatalf("dirs = %v, want [/b]", got)
	}
}

// Deleting a host takes its directories with it, so a later host reusing the
// same row id cannot inherit them.
func TestDeleteHostDropsItsDirs(t *testing.T) {
	st := withHost(t, "web1")
	st.TouchDir("web1", "/srv/app")

	if err := st.Delete("web1"); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	var n int
	if err := st.db.QueryRow(`SELECT count(*) FROM dirs`).Scan(&n); err != nil {
		t.Fatalf("count dirs: %v", err)
	}
	if n != 0 {
		t.Fatalf("%d orphaned dir rows survived the host", n)
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
