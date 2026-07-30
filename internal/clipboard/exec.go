//go:build !windows

package clipboard

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// pipeTo runs a clipboard helper with text on its standard input.
//
// The timeout is the point of doing this by hand rather than with exec.Command
// alone. A clipboard helper is a well-behaved program that exits as soon as it has
// read its input — except when it is not: xclip forks and holds the selection
// until someone asks for it (which is how X clipboards work), and a helper waiting
// on a display server that is not answering waits forever. This runs on a
// goroutine hop does not join, so a hung one would simply accumulate.
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

// helperTimeout bounds a clipboard helper's run. Generous, because this is not a
// latency-sensitive path — nothing waits on the copy — and a slow desktop should
// still get the text.
const helperTimeout = 5 * time.Second
