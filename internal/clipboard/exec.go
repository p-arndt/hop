//go:build !windows

package clipboard

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// pipeTo runs a clipboard helper with text on its standard input. The timeout matters:
// xclip holds the selection, and a helper waiting on a dead display server never exits.
func pipeTo(text, name string, args ...string) error {
	if _, err := exec.LookPath(name); err != nil {
		return fmt.Errorf("%w: %s not found", ErrUnavailable, name)
	}

	ctx, cancel := context.WithTimeout(context.Background(), helperTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Stdin = strings.NewReader(text)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("clipboard: %s: %w", name, err)
	}
	return nil
}

// helperTimeout bounds a clipboard helper's run.
const helperTimeout = 5 * time.Second
