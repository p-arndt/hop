// Package update wires hop into github.com/p-arndt/selfupdate: the `self-update`
// and `check-update` subcommands, the passive "a newer version is available"
// notice, and the TUI's startup check.
//
// The release pipeline (.github/workflows/release.yml) already publishes exactly
// the layout the library expects by default — one hop_<version>_<goos>_<goarch>
// archive per platform plus hop_<version>_checksums.txt — so the only override
// here is the upgrade hint, which has to name hop's `self-update` subcommand
// instead of the library's default `hop update`.
//
// Everything else (https-only downloads, SHA-256 verification, the atomic swap,
// the Windows rename dance, the daily on-disk cache) lives in the library.
package update

import (
	"context"
	"errors"
	"io"
	"net/http"
	"sync"

	"github.com/p-arndt/selfupdate"
	"github.com/p-arndt/selfupdate/version"
)

// UpdateTimeout bounds the whole check-download-verify-install cycle,
// generously: the user explicitly asked for it.
const UpdateTimeout = selfupdate.DefaultUpdateTimeout

// New returns an Updater for hop's releases. It takes a Config so production can
// pass the zero value while tests pass the three seams — APIBase, StatePath and
// ExecutablePath — that run the whole flow offline. The fields hop's release
// layout depends on are set here and are not overridable.
func New(cfg selfupdate.Config) (*selfupdate.Updater, error) {
	cfg.Owner = "p-arndt"
	cfg.Repo = "hop"
	// The derived default would be `hop update`; hop's subcommand is `self-update`.
	cfg.UpdateCmd = "hop self-update"
	if cfg.HTTP == nil {
		// One client for both paths. The notice bounds itself with its own 1.5s
		// context, so this generous timeout only ever applies to a real download.
		// A non-nil client still gets the library's https-only redirect policy.
		cfg.HTTP = &http.Client{Timeout: UpdateTimeout}
	}
	return selfupdate.New(cfg)
}

// Default is the process-wide Updater, built once on first use. New only fails on
// a missing Owner or Repo, both constants above, so a nil here is unreachable —
// the callers below still treat it as "updates unavailable" rather than panicking
// on a code path a user never asked for.
var Default = sync.OnceValue(func() *selfupdate.Updater {
	up, err := New(selfupdate.Config{})
	if err != nil {
		return nil
	}
	return up
})

// CleanupLeftovers removes the "<exe>.old" file left by a prior Windows
// self-update. Call it once at startup; it never errors.
func CleanupLeftovers() { selfupdate.CleanupLeftovers() }

// NotifyIfAvailable prints a one-line hint to w when the cached check says a
// newer release exists, then refreshes a stale cache for the next run. Write to
// stderr, never stdout, so `hop list` stays pipeable.
func NotifyIfAvailable(w io.Writer, current string) {
	if up := Default(); up != nil {
		up.NotifyIfAvailable(w, current)
	}
}

// Refresh returns the latest version when it is newer than current, and ""
// otherwise, refreshing a stale cache first. It blocks for up to 1.5s, so the TUI
// calls it off the UI thread.
func Refresh(current string) string {
	if up := Default(); up != nil {
		return up.Refresh(current)
	}
	return ""
}

// SelfUpdate checks for a newer release and, unless checkOnly, installs it over
// the running binary. It reports an error on dev and source builds, which have no
// release to compare against.
func SelfUpdate(ctx context.Context, current string, checkOnly bool) (*selfupdate.Result, error) {
	up := Default()
	if up == nil {
		return nil, errors.New("updater unavailable")
	}
	return up.SelfUpdate(ctx, current, checkOnly)
}

// IsNewer reports whether latest is a strictly newer version than current.
func IsNewer(latest, current string) bool { return version.IsNewer(latest, current) }
