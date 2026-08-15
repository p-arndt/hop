//go:build !windows && !darwin

package clipboard

import "os"

// The helpers, in the order they are tried. There is no one clipboard tool on
// Linux and the BSDs: which one works depends on the display server, so the list
// is walked until one of them is both installed and willing.
//
//   - wl-copy first, and only under Wayland: it is the native tool there, and it
//     is checked against WAYLAND_DISPLAY because a machine can have both installed
//     while only one of the two servers is actually running.
//   - xclip and xsel are the X tools, in the order they are usually installed.
//     They also serve XWayland, which is why they are tried unconditionally.
//   - clip.exe is the WSL case: a Linux hop talking to a Windows clipboard. It is
//     last because on a real Linux desktop it is not there at all.
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
