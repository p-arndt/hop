//go:build !darwin

package filebrowser

// quarantine is a no-op away from macOS: Windows zone identifiers and Linux
// desktops have no equivalent that the default handler consults, so the
// extension guard in executableName carries the whole load there.
func quarantine(string) error { return nil }
