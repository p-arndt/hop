// Package config holds hop's user settings: a small JSON file next to the hosts
// database, edited from the in-app settings popover or by hand.
//
// Loading never fails hard. A missing file is the normal first-run case, and a
// corrupt one is not worth refusing to start over — either way hop falls back to
// defaults and carries on, so a stray keystroke in an editor cannot lock you out
// of your own hosts.
package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// Config is the full set of user settings. Every field has a meaningful zero
// value, which is what makes a partial (or absent) file safe to load.
type Config struct {
	// Editor is the command run on the *remote* host to edit a file, flags and
	// all ("nvim", "vim -R"). Empty means: prefer the remote $EDITOR, and probe
	// the remote PATH when it is unset.
	Editor string `json:"editor"`

	// DownloadDir is where the browser's "d" puts files. Empty means <home>/Downloads.
	DownloadDir string `json:"downloadDir"`

	// Accent is the 256-color code for hop's highlight color. Empty means 212.
	Accent string `json:"accent"`

	// OpenWith is the local command the browser's "o" opens a file with, flags and
	// all ("code"). Empty means the desktop default (start / open / xdg-open).
	OpenWith string `json:"openWith"`
}

// DefaultAccent is hop's pink.
const DefaultAccent = "212"

// Default returns the settings hop uses when nothing has been configured.
func Default() Config {
	return Config{
		DownloadDir: defaultDownloadDir(),
		Accent:      DefaultAccent,
	}
}

// defaultDownloadDir is <home>/Downloads, falling back to the home directory
// itself when there is no Downloads folder, and to "." when there is no home.
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

// Path is the config file's location: <UserConfigDir>/hop/config.json, alongside
// hop.db.
func Path() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("config: locate config dir: %w", err)
	}
	return filepath.Join(dir, "hop", "config.json"), nil
}

// Load reads the config file, filling anything absent from Default. It returns
// defaults (and no error) when the file does not exist or cannot be parsed — see
// the package comment.
func Load() Config {
	path, err := Path()
	if err != nil {
		return Default()
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return Default()
	}

	// Start from the defaults so a file that omits a key still gets a sane value
	// for it, rather than the type's zero.
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
	return c
}

// Save writes the config file, creating its directory. The write goes to a temp
// file that is then renamed over the target, so an interrupted save cannot leave
// a half-written file behind.
func (c Config) Save() error {
	path, err := Path()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("config: create config dir: %w", err)
	}

	data, err := json.MarshalIndent(c.normalized(), "", "  ")
	if err != nil {
		return fmt.Errorf("config: encode: %w", err)
	}
	data = append(data, '\n')

	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("config: write: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("config: replace: %w", err)
	}
	return nil
}
