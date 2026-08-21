package filebrowser

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

// A scratch copy handed to `open` must carry com.apple.quarantine so Gatekeeper sees it.
func TestQuarantineSetsXattr(t *testing.T) {
	p := filepath.Join(t.TempDir(), "remote.bin")
	if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := quarantine(p); err != nil {
		t.Fatalf("quarantine(%q) = %v", p, err)
	}

	buf := make([]byte, 128)
	n, err := unix.Getxattr(p, "com.apple.quarantine", buf)
	if err != nil {
		t.Fatalf("com.apple.quarantine not set: %v", err)
	}
	val := string(buf[:n])
	if !strings.HasPrefix(val, "0083;") || !strings.Contains(val, ";hop;") {
		t.Fatalf("quarantine value = %q, want flags 0083 and agent hop", val)
	}
}
