// Package pathx holds the small path manipulations several of hop's packages share.
package pathx

import (
	"os"
	"path/filepath"
	"strings"
)

// ExpandHome resolves a leading "~" against the user's home directory. Only as a whole
// element ("~user/x" is a shell's business); both separators are accepted for Windows.
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
