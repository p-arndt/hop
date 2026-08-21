//go:build !darwin

package filebrowser

// quarantine is a no-op away from macOS: no equivalent the default handler consults.
func quarantine(string) error { return nil }
