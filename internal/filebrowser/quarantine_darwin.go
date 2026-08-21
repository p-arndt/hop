package filebrowser

import (
	"fmt"
	"time"

	"golang.org/x/sys/unix"
)

// quarantine stamps com.apple.quarantine on p so Gatekeeper prompts on `open`; os.Create does not.
// The value is the documented "flags;hex mtime;agent;UUID" quartet, flags 0083 as browsers write.
func quarantine(p string) error {
	val := fmt.Sprintf("0083;%08x;hop;", time.Now().Unix())
	if err := unix.Setxattr(p, "com.apple.quarantine", []byte(val), 0); err != nil {
		return fmt.Errorf("set com.apple.quarantine: %w", err)
	}
	return nil
}
