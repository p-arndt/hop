// Package config holds hop's user settings: a small JSON file in hop's config directory.
package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// Config is the full set of user settings.
type Config struct {
	// Empty prefers the remote $EDITOR, and probes the remote PATH when that is unset.
	Editor string `json:"editor"`

	// Empty means <home>/Downloads.
	DownloadDir string `json:"downloadDir"`

	// 256-color code for hop's highlight color; empty means DefaultAccent.
	Accent string `json:"accent"`

	// Local command a file is opened with; empty means the desktop default.
	OpenWith string `json:"openWith"`

	VimKeys bool `json:"vimKeys"`

	// Defaults to on: the one field whose zero value is not its default, safe only because
	// Load starts from Default() and unmarshals over it.
	Mouse bool `json:"mouse"`

	// One of the Guidance* constants; empty or unknown means GuidanceHybrid.
	Guidance string `json:"guidance"`

	CursorBlink bool `json:"cursorBlink"`

	// OSC 52 clipboard writes from the remote. Defaults to on, like Mouse.
	Clipboard bool `json:"clipboard"`
}

// The guidance profiles, stored as words so a hand-edited config says what it means.
const (
	GuidanceKeys   = "keys"
	GuidanceHybrid = "hybrid"
	GuidanceGuided = "guided"
)

// DefaultAccent is hop's pink.
const DefaultAccent = "212"

// Default returns the settings hop uses when nothing has been configured.
func Default() Config {
	return Config{
		DownloadDir: defaultDownloadDir(),
		Accent:      DefaultAccent,
		Guidance:    GuidanceHybrid,
		Mouse:       true,
		Clipboard:   true,
	}
}

// defaultDownloadDir is <home>/Downloads, falling back to the home directory, then ".".
func defaultDownloadDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return "."
	}
	dl := filepath.Join(home, "Downloads")
	if _, err := os.Stat(dl); err != nil {
		return home
	}
	return dl
}

// Path is the config file's location, in hop's own config directory.
func Path() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("config: locate config dir: %w", err)
	}
	return filepath.Join(dir, "hop", "config.json"), nil
}

// Exists reports whether a config file has been written yet — how hop tells a first run from
// a later one, so a newly added setting does not re-open a question for an existing user.
func Exists() bool {
	path, err := Path()
	if err != nil {
		return false
	}
	_, err = os.Stat(path)
	return err == nil
}

// Load reads the config file, filling anything absent from Default, and returns defaults
// when the file does not exist or cannot be parsed.
func Load() Config {
	path, err := Path()
	if err != nil {
		return Default()
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return Default()
	}

	// Start from the defaults, so a file that omits a key does not get the type's zero.
	cfg := Default()
	if err := json.Unmarshal(data, &cfg); err != nil {
		return Default()
	}
	return cfg.normalized()
}

// normalized fills in blanks that must not stay blank.
func (c Config) normalized() Config {
	if c.DownloadDir == "" {
		c.DownloadDir = defaultDownloadDir()
	}
	if c.Accent == "" {
		c.Accent = DefaultAccent
	}
	// An unknown profile (a typo, or a file written by a newer hop) falls back rather than errors.
	switch c.Guidance {
	case GuidanceKeys, GuidanceHybrid, GuidanceGuided:
	default:
		c.Guidance = GuidanceHybrid
	}
	return c
}

// Save writes the config file, creating its directory. The write goes to a temp file
// renamed over the target, so an interrupted save leaves nothing half-written.
func (c Config) Save() error {
	path, err := Path()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("config: create config dir: %w", err)
	}

	// The file is shared with internal/store, so settings merge in rather than replace it.
	merged, err := mergeIntoFile(path, c.normalized())
	if err != nil {
		return err
	}

	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, merged, 0o644); err != nil {
		return fmt.Errorf("config: write: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("config: replace: %w", err)
	}
	return nil
}

// mergeIntoFile encodes value's keys over the JSON object already at path, keeping the rest.
func mergeIntoFile(path string, value any) ([]byte, error) {
	doc := map[string]json.RawMessage{}
	if existing, err := os.ReadFile(path); err == nil {
		// A corrupt file is overwritten: Load treats it as absent, so refusing to save would
		// leave the user unable to fix it from the UI.
		_ = json.Unmarshal(existing, &doc)
	}

	encoded, err := json.Marshal(value)
	if err != nil {
		return nil, fmt.Errorf("config: encode: %w", err)
	}
	var own map[string]json.RawMessage
	if err := json.Unmarshal(encoded, &own); err != nil {
		return nil, fmt.Errorf("config: encode: %w", err)
	}
	for k, v := range own {
		doc[k] = v
	}

	out, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("config: encode: %w", err)
	}
	return append(out, '\n'), nil
}
