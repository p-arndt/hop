// Package config holds hop's user settings: a small JSON file next to the hosts database,
// edited from the settings popover or by hand.
//
// Loading never fails hard. A missing file is the normal first-run case and a corrupt one
// falls back to defaults, so a stray keystroke in an editor cannot lock you out of your
// own hosts.
package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// Config is the full set of user settings. Every field has a meaningful zero value, which
// is what makes a partial or absent file safe to load.
type Config struct {
	// Editor is the command run on the remote host to edit a file, flags and all. Empty
	// prefers the remote $EDITOR, and probes the remote PATH when that is unset.
	Editor string `json:"editor"`

	// DownloadDir is where the browser's "d" puts files. Empty means <home>/Downloads.
	DownloadDir string `json:"downloadDir"`

	// Accent is the 256-color code for hop's highlight color. Empty means 212.
	Accent string `json:"accent"`

	// OpenWith is the local command the browser's "o" opens a file with, flags and
	// all ("code"). Empty means the desktop default (start / open / xdg-open).
	OpenWith string `json:"openWith"`

	// VimKeys turns on the vim motions in the host list and the file browser. False, the
	// default, leaves those letters unbound: hop asks for vim rather than assuming it.
	VimKeys bool `json:"vimKeys"`

	// Mouse turns on mouse reporting: the wheel and clicks everywhere, and the pointer
	// forwarded to a remote program that asked for it. It defaults to on — the one field
	// whose zero value is not its default, which is safe because Load starts from
	// Default() and unmarshals over it.
	//
	// While hop reports the mouse it does the selecting itself: a drag over a pane
	// highlights and copies (see internal/tui/selection.go). ctrl+g hands the pointer back
	// for a selection that spans hop's own furniture; this setting hands it back for good.
	Mouse bool `json:"mouse"`

	// Guidance is how much of hop's keyboard hop puts on screen without being asked.
	// It changes what is *shown*, never what a key does: every binding works in every
	// profile, so nothing learnt in one is unlearnt by switching.
	//
	//   keys    the short legend and nothing else — for a keyboard already in the hand
	//   hybrid  the legend, the extras a wide window fits, and the host's actions
	//   guided  all of that, plus every action the host has, spelled out with its key
	//
	// Empty or unknown means Hybrid. See internal/tui/actions.go for what reads it.
	Guidance string `json:"guidance"`

	// CursorBlink lets hop blink the cursor in a pane, when the remote program has not
	// asked for a steady one (DECSCUSR). It is off by default: the shape and the hidden
	// state are the remote's to decide and hop always honours them, but the blink is a
	// clock hop has to run itself — a repaint twice a second — so it is asked for rather
	// than assumed. See internal/terminal/cursor.go.
	CursorBlink bool `json:"cursorBlink"`

	// Clipboard lets a program on a remote host put text on your clipboard over OSC 52 —
	// a yank in a remote vim, or tmux's set-clipboard. Like Mouse it defaults to on, and
	// for the same reason that is safe.
	//
	// The channel is one-way and hop keeps it that way: a remote asking to read the
	// clipboard is never answered (see internal/terminal/clipboard.go). But anything on
	// the far end can write it, not only what you started.
	Clipboard bool `json:"clipboard"`
}

// The guidance profiles. Stored as words rather than a number so a hand-edited config
// says what it means.
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

// defaultDownloadDir is <home>/Downloads, falling back to the home directory when there
// is no Downloads folder and to "." when there is no home.
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

// Path is the config file's location, alongside hop.db.
func Path() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("config: locate config dir: %w", err)
	}
	return filepath.Join(dir, "hop", "config.json"), nil
}

// Exists reports whether a config file has been written yet. It is how hop tells a first
// run from a later one: an existing file means these settings were once decided, even if
// the key being asked about is not in it, so a new setting must not re-open a question
// for someone who has been using hop for months.
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

	// Start from the defaults, so a file that omits a key gets a sane value for it rather
	// than the type's zero.
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
	// A profile hop does not know — a typo, or a file written by a newer hop — is the
	// middle one rather than an error: this setting decides how much is on screen, and
	// there is no answer to it worth refusing to start over.
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
