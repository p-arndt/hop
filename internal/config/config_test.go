package config

import (
	"os"
	"path/filepath"
	"testing"
)

// isolate points os.UserConfigDir at a throwaway directory, so the tests never
// touch the real config file. It covers both the Windows (%AppData%) and the
// Unix (XDG_CONFIG_HOME) lookups.
func isolate(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("AppData", dir)
	t.Setenv("XDG_CONFIG_HOME", dir)
	return dir
}

// A round trip through the file must return exactly what was saved.
func TestSaveLoadRoundTrip(t *testing.T) {
	isolate(t)

	want := Config{
		Editor:      "nvim -R",
		DownloadDir: `C:\tmp\dl`,
		Accent:      "99",
		OpenWith:    "code -n",
	}
	if err := want.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}

	if got := Load(); got != want {
		t.Fatalf("Load() = %+v, want %+v", got, want)
	}
}

// First run: no file at all. Defaults, and no error surfaced to the caller.
func TestLoadMissingFileYieldsDefaults(t *testing.T) {
	isolate(t)

	got := Load()
	if got.Accent != DefaultAccent {
		t.Fatalf("Accent = %q, want the default %q", got.Accent, DefaultAccent)
	}
	if got.DownloadDir == "" {
		t.Fatal("DownloadDir is empty; a missing file must still yield a usable one")
	}
	if got.Editor != "" || got.OpenWith != "" {
		t.Fatalf("Editor = %q, OpenWith = %q; both should default to auto (empty)", got.Editor, got.OpenWith)
	}
}

// A corrupt file must not keep hop from starting: it loads as defaults. Losing a
// setting is recoverable; being locked out of your hosts is not.
func TestLoadCorruptFileYieldsDefaults(t *testing.T) {
	dir := isolate(t)
	path := filepath.Join(dir, "hop", "config.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte("{ this is not json"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	if got := Load(); got.Accent != DefaultAccent {
		t.Fatalf("Load() = %+v, want defaults", got)
	}
}

// A file that sets only one key keeps sane values for the rest, rather than
// inheriting Go's zero values (an empty accent would render as no colour at all).
func TestLoadPartialFileFillsDefaults(t *testing.T) {
	dir := isolate(t)
	path := filepath.Join(dir, "hop", "config.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte(`{"editor":"nano"}`), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	got := Load()
	if got.Editor != "nano" {
		t.Fatalf("Editor = %q, want nano", got.Editor)
	}
	if got.Accent != DefaultAccent {
		t.Fatalf("Accent = %q, want the default %q", got.Accent, DefaultAccent)
	}
	if got.DownloadDir == "" {
		t.Fatal("DownloadDir is empty; an omitted key must fall back to the default")
	}
}

// Save must not leave its temp file lying next to the config.
func TestSaveLeavesNoTempFile(t *testing.T) {
	dir := isolate(t)

	if err := Default().Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}

	entries, err := os.ReadDir(filepath.Join(dir, "hop"))
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	for _, e := range entries {
		if e.Name() != "config.json" {
			t.Fatalf("Save left %q behind, want only config.json", e.Name())
		}
	}
}
