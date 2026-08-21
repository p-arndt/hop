// Package update wires hop into github.com/p-arndt/selfupdate.
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

// UpdateTimeout bounds the whole check-download-verify-install cycle.
const UpdateTimeout = selfupdate.DefaultUpdateTimeout

// New returns an Updater for hop's releases; cfg carries only the test seams.
func New(cfg selfupdate.Config) (*selfupdate.Updater, error) {
	cfg.Owner = "p-arndt"
	cfg.Repo = "hop"
	// The library's derived default would be `hop update`.
	cfg.UpdateCmd = "hop self-update"
	if cfg.HTTP == nil {
		// The notice bounds itself with a 1.5s context, so this only applies to a real download.
		cfg.HTTP = &http.Client{Timeout: UpdateTimeout}
	}
	return selfupdate.New(cfg)
}

// Default is the process-wide Updater, built once on first use.
var Default = sync.OnceValue(func() *selfupdate.Updater {
	up, err := New(selfupdate.Config{})
	if err != nil {
		return nil
	}
	return up
})

// CleanupLeftovers removes the "<exe>.old" file left by a prior Windows self-update.
func CleanupLeftovers() { selfupdate.CleanupLeftovers() }

// NotifyIfAvailable prints a one-line hint to w when a newer release exists.
func NotifyIfAvailable(w io.Writer, current string) {
	if up := Default(); up != nil {
		up.NotifyIfAvailable(w, current)
	}
}

// Refresh returns the latest version when newer than current; it blocks up to 1.5s.
func Refresh(current string) string {
	if up := Default(); up != nil {
		return up.Refresh(current)
	}
	return ""
}

// SelfUpdate checks for a newer release and, unless checkOnly, installs it.
func SelfUpdate(ctx context.Context, current string, checkOnly bool) (*selfupdate.Result, error) {
	up := Default()
	if up == nil {
		return nil, errors.New("updater unavailable")
	}
	return up.SelfUpdate(ctx, current, checkOnly)
}

// IsNewer reports whether latest is a strictly newer version than current.
func IsNewer(latest, current string) bool { return version.IsNewer(latest, current) }
