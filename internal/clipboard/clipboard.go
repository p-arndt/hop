// Package clipboard writes text to the local system clipboard, for OSC 52 requests from remote programs.
package clipboard

import "errors"

// ErrUnavailable means no clipboard is reachable; a normal condition, not a failure.
var ErrUnavailable = errors.New("clipboard: no clipboard available")

// Write puts text on the system clipboard, replacing what was there.
func Write(text string) error { return write(text) }
