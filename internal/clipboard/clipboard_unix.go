//go:build !windows && !darwin

package clipboard

import "os"

// Tried in order; which one works depends on the display server. wl-copy is gated on
// WAYLAND_DISPLAY (both servers can be installed), and clip.exe is the WSL fallback.
var helpers = []struct {
	name    string
	args    []string
	wayland bool
}{
	{name: "wl-copy", wayland: true},
	{name: "xclip", args: []string{"-selection", "clipboard"}},
	{name: "xsel", args: []string{"--input", "--clipboard"}},
	{name: "clip.exe"},
}

func write(text string) error {
	wayland := os.Getenv("WAYLAND_DISPLAY") != ""

	var lastErr error = ErrUnavailable
	for _, h := range helpers {
		if h.wayland && !wayland {
			continue
		}
		err := pipeTo(text, h.name, h.args...)
		if err == nil {
			return nil
		}
		lastErr = err
	}
	return lastErr
}
