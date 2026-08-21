package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// isolate points os.UserConfigDir at a throwaway directory and returns the config path.
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

// First run: no file at all, so defaults and no error.
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

// Load unmarshals over Default(), so an omitted key keeps the non-zero default.
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

// An unknown or missing guidance value falls back to hybrid rather than the zero value.
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

// Exists answers for the file itself, not for the settings in it.
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

// The file is shared with internal/store, so Save must merge rather than replace.
func TestSavePreservesForeignKeys(t *testing.T) {
	isolate(t)
	path, err := Path()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}

	existing := `{"accent":"99","hosts":{"version":1,"nextId":7,"entries":{"web":{"id":3,"pinned":true}}}}`
	if err := os.WriteFile(path, []byte(existing), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg := Default()
	cfg.Accent = "212"
	if err := cfg.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatalf("the saved file does not parse: %v\n%s", err, data)
	}

	hosts, ok := doc["hosts"].(map[string]any)
	if !ok {
		t.Fatalf("the host metadata was dropped: %s", data)
	}
	entries, ok := hosts["entries"].(map[string]any)
	if !ok || entries["web"] == nil {
		t.Fatalf("the host entries were dropped: %s", data)
	}
	if nextID, _ := hosts["nextId"].(float64); nextID != 7 {
		t.Fatalf("nextId = %v, want 7: %s", hosts["nextId"], data)
	}

	if got, _ := doc["accent"].(string); got != "212" {
		t.Fatalf("accent = %v, want 212", doc["accent"])
	}
	if Load().Accent != "212" {
		t.Fatalf("Load().Accent = %q, want 212", Load().Accent)
	}
}

func TestSaveOverwritesCorruptFile(t *testing.T) {
	isolate(t)
	path, err := Path()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("{not json at all"), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg := Default()
	cfg.Editor = "vim"
	if err := cfg.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if got := Load().Editor; got != "vim" {
		t.Fatalf("Editor = %q, want vim", got)
	}
}
