// Package pathx holds the small path manipulations several of hop's packages need and
// none of them owns. It deliberately depends on nothing but the standard library, so the
// SSH layer, the file browser and the TUI can all reach it without a direction problem.
package pathx

import (
	"os"
	"path/filepath"
	"strings"
)

// ExpandHome resolves a leading "~" against the user's home directory.
//
// Only a leading one, and only as a whole path element: "~/x" and "~" expand, "~user/x"
// does not — that is a shell's business, and a file literally named "~foo" should stay
// itself. A home directory that cannot be found leaves the path alone rather than
// failing: the caller is about to use it and will report a better error than this can.
//
// Both separators are accepted, because a Windows user types the one their shell shows
// them and a config file may carry either.
func ExpandHome(p string) string {
	if p != "~" && !strings.HasPrefix(p, "~/") && !strings.HasPrefix(p, `~\`) {
		return p
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return p
	}
	if p == "~" {
		return home
	}
	return filepath.Join(home, filepath.FromSlash(p[2:]))
}
