package config

import (
	"os"
	"path/filepath"
	"testing"
)

// isolate points os.UserConfigDir at a throwaway directory and returns where the config
// file will land, so the tests never touch the real one. Each platform reads a different
// variable, so all three are redirected and the directory comes from Path().
func isolate(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("AppData", dir)
	t.Setenv("XDG_CONFIG_HOME", dir)
	t.Setenv("HOME", dir)

	path, err := Path()
	if err != nil {
		t.Fatalf("Path: %v", err)
	}
	return filepath.Dir(path)
}

// A round trip through the file must return exactly what was saved.
func TestSaveLoadRoundTrip(t *testing.T) {
	isolate(t)

	want := Config{
		Editor:      "nvim -R",
		DownloadDir: `C:\tmp\dl`,
		Accent:      "99",
		OpenWith:    "code -n",
		Guidance:    GuidanceGuided,
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
	path := filepath.Join(dir, "config.json")
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
	path := filepath.Join(dir, "config.json")
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

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	for _, e := range entries {
		if e.Name() != "config.json" {
			t.Fatalf("Save left %q behind, want only config.json", e.Name())
		}
	}
}

// Mouse is the one field whose default is not its zero value, which is safe because Load
// starts from Default() and unmarshals over it: a file that omits the key comes back with
// the mouse on, and only one that says otherwise switches it off.
func TestLoadMouseDefaultsOn(t *testing.T) {
	dir := isolate(t)
	path := filepath.Join(dir, "config.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	if err := os.WriteFile(path, []byte(`{"editor":"nano"}`), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if !Load().Mouse {
		t.Fatal("a config with no mouse key loaded with the mouse off, want on")
	}

	if err := os.WriteFile(path, []byte(`{"mouse":false}`), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if Load().Mouse {
		t.Fatal(`a config saying "mouse": false loaded with the mouse on`)
	}
}

// The remote clipboard is the other field of that kind: a config omitting the key must
// not come back with it off, nor must a file that switched it off be ignored.
func TestLoadClipboardDefaultsOn(t *testing.T) {
	dir := isolate(t)
	path := filepath.Join(dir, "config.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	if err := os.WriteFile(path, []byte(`{"editor":"nano"}`), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if !Load().Clipboard {
		t.Fatal("a config with no clipboard key loaded with it off, want on")
	}

	if err := os.WriteFile(path, []byte(`{"clipboard":false}`), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if Load().Clipboard {
		t.Fatal(`a config saying "clipboard": false loaded with it on`)
	}
}

// The guidance profile is the one field whose value is checked rather than merely
// carried: an unknown word is the middle profile, not a broken screen. And a file that
// predates the setting entirely leaves its owner on hybrid rather than on the zero value.
func TestGuidanceIsNormalized(t *testing.T) {
	isolate(t)

	for _, stored := range []string{"", "loud", "GUIDED"} {
		cfg := Default()
		cfg.Guidance = stored
		if err := cfg.Save(); err != nil {
			t.Fatalf("Save %q: %v", stored, err)
		}
		if got := Load().Guidance; got != GuidanceHybrid {
			t.Fatalf("guidance %q loaded as %q, want %q", stored, got, GuidanceHybrid)
		}
	}

	for _, stored := range []string{GuidanceKeys, GuidanceHybrid, GuidanceGuided} {
		cfg := Default()
		cfg.Guidance = stored
		if err := cfg.Save(); err != nil {
			t.Fatalf("Save %q: %v", stored, err)
		}
		if got := Load().Guidance; got != stored {
			t.Fatalf("guidance %q loaded as %q", stored, got)
		}
	}
}

// Exists is what tells a first run from a later one, so it must answer for the file
// itself and not for the settings in it.
func TestExists(t *testing.T) {
	isolate(t)

	if Exists() {
		t.Fatal("Exists() is true before anything has been saved")
	}
	if err := Default().Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if !Exists() {
		t.Fatal("Exists() is false after a save")
	}
}
