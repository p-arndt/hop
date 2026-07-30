package config

import (
	"os"
	"path/filepath"
	"testing"
)

// isolate points os.UserConfigDir at a throwaway directory and returns the
// directory the config file will actually land in, so the tests never touch the
// real one. Each platform reads a different variable — %AppData% on Windows,
// $XDG_CONFIG_HOME on Linux, $HOME/Library/Application Support on macOS (which
// ignores XDG entirely) — so all three are redirected and the resulting
// directory is derived from Path() rather than assumed.
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

// Mouse is the one field whose default is not its zero value, which is only safe
// because Load starts from Default() and unmarshals over it: a config written before
// hop had the setting — or any file that simply omits the key — comes back with the
// mouse on, and only a file that says otherwise switches it off.
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

// The remote clipboard is the other field of that kind, and the one where getting
// it backwards matters most: a config that omits the key must not come back with a
// remote host silently unable to hand you what you yanked — nor, the other way, must
// a file that switched it off be ignored.
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
