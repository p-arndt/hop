package filebrowser

import (
	"fmt"
	"time"

	"golang.org/x/sys/unix"
)

// quarantine sets com.apple.quarantine on p, the attribute macOS stamps on
// files that arrived from an untrusted source. LaunchServices then treats an
// `open` of p the way it treats a browser download — Gatekeeper prompts before
// anything executable runs — instead of launching it silently. hop writes its
// SFTP copies with plain os.Create, which does not pick the attribute up on
// its own.
//
// The value is the documented "flags;hex mtime;agent name;UUID" quartet; the
// flags 0083 (kLSQuarantineTypeOtherDownload | download + user-approval bits)
// match what browsers write, and the UUID field may be empty.
func quarantine(p string) error {
	val := fmt.Sprintf("0083;%08x;hop;", time.Now().Unix())
	if err := unix.Setxattr(p, "com.apple.quarantine", []byte(val), 0); err != nil {
		return fmt.Errorf("set com.apple.quarantine: %w", err)
	}
	return nil
}
