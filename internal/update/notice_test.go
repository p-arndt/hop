package update

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// seedState writes a cache file into an isolated config dir and points the
// process at it. Each platform reads a different variable for UserConfigDir —
// %AppData% on Windows, $XDG_CONFIG_HOME on Linux, $HOME/Library/Application
// Support on macOS (which ignores XDG entirely) — so all three are redirected
// and the location is derived from statePath rather than assumed.
func seedState(t *testing.T, st state) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("AppData", dir)
	t.Setenv("XDG_CONFIG_HOME", dir)
	t.Setenv("HOME", dir)
	t.Setenv("USERPROFILE", dir)

	path, err := statePath()
	if err != nil {
		t.Fatalf("statePath: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	data, _ := json.Marshal(st)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestNotifyPrintsWhenNewerCached(t *testing.T) {
	// Fresh check so no network refresh is attempted.
	seedState(t, state{LastCheck: time.Now(), Latest: "0.4.0"})
	t.Setenv("HOP_NO_UPDATE_CHECK", "")

	var buf bytes.Buffer
	NotifyIfAvailable(&buf, "0.2.1")
	if !strings.Contains(buf.String(), "0.4.0") {
		t.Errorf("expected a hint mentioning 0.4.0, got %q", buf.String())
	}
	if !strings.Contains(buf.String(), "hop self-update") {
		t.Errorf("the hint should name the command that upgrades, got %q", buf.String())
	}
}

func TestNotifySilentWhenCurrent(t *testing.T) {
	seedState(t, state{LastCheck: time.Now(), Latest: "0.2.1"})
	var buf bytes.Buffer
	NotifyIfAvailable(&buf, "0.2.1")
	if buf.Len() != 0 {
		t.Errorf("expected no output when on latest, got %q", buf.String())
	}
}

func TestNotifyRespectsOptOut(t *testing.T) {
	seedState(t, state{LastCheck: time.Now(), Latest: "0.4.0"})
	t.Setenv("HOP_NO_UPDATE_CHECK", "1")
	var buf bytes.Buffer
	NotifyIfAvailable(&buf, "0.2.1")
	if buf.Len() != 0 {
		t.Errorf("opt-out should silence the notice, got %q", buf.String())
	}
}

func TestNotifySilentForDevBuild(t *testing.T) {
	seedState(t, state{LastCheck: time.Now(), Latest: "0.4.0"})
	var buf bytes.Buffer
	NotifyIfAvailable(&buf, "dev")
	if buf.Len() != 0 {
		t.Errorf("dev builds should never see the notice, got %q", buf.String())
	}
}

// Refresh is what the TUI footer asks. With a fresh cache it must answer from
// disk — the startup path can't afford a network round trip per launch.
func TestRefreshUsesFreshCacheWithoutNetwork(t *testing.T) {
	seedState(t, state{LastCheck: time.Now(), Latest: "0.4.0"})
	t.Setenv("HOP_NO_UPDATE_CHECK", "")

	if got := Refresh("0.2.1"); got != "0.4.0" {
		t.Errorf("Refresh = %q, want 0.4.0", got)
	}
	if got := Refresh("0.4.0"); got != "" {
		t.Errorf("Refresh on the latest version = %q, want \"\"", got)
	}
	if got := Refresh("dev"); got != "" {
		t.Errorf("Refresh on a dev build = %q, want \"\"", got)
	}
	t.Setenv("HOP_NO_UPDATE_CHECK", "1")
	if got := Refresh("0.2.1"); got != "" {
		t.Errorf("opt-out should silence Refresh, got %q", got)
	}
}

// A stale cache is refreshed from the network, and the new version is written
// back so the next launch answers from disk again.
func TestRefreshRenewsStaleCache(t *testing.T) {
	seedState(t, state{LastCheck: time.Now().Add(-48 * time.Hour), Latest: "0.3.0"})
	t.Setenv("HOP_NO_UPDATE_CHECK", "")

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(Release{Tag: "v0.5.0"})
	}))
	defer srv.Close()

	st := refresh(&Client{HTTP: srv.Client(), APIBase: srv.URL, Owner: "p-arndt", Repo: "hop"}, loadState())
	if st.Latest != "0.5.0" {
		t.Fatalf("refreshed latest = %q, want 0.5.0", st.Latest)
	}
	if got := loadState().Latest; got != "0.5.0" {
		t.Errorf("cache not written back: %q", got)
	}
}

// An unreachable GitHub must not make every launch retry: the check window is
// claimed before the fetch, so a failure still counts as "checked today".
func TestRefreshClaimsWindowOnFailure(t *testing.T) {
	seedState(t, state{LastCheck: time.Now().Add(-48 * time.Hour), Latest: "0.3.0"})

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "nope", http.StatusInternalServerError)
	}))
	defer srv.Close()

	refresh(&Client{HTTP: srv.Client(), APIBase: srv.URL, Owner: "p-arndt", Repo: "hop"}, loadState())
	st := loadState()
	if time.Since(st.LastCheck) > time.Minute {
		t.Error("a failed check should still claim the window")
	}
	if st.Latest != "0.3.0" {
		t.Errorf("a failed check must not clobber the cached version: %q", st.Latest)
	}
}
