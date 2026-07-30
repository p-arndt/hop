// Package clipboard writes text to the local system clipboard.
//
// It exists for one caller: a remote program that yanked something and asked, over
// OSC 52, for it to land on the clipboard of the machine you are sitting at (see
// internal/terminal/clipboard.go). Every platform has its own way of being asked
// that, and none of them is a file — hence a package with three implementations
// behind one function.
//
// Writing is all there is here on purpose. Pasting *into* a pane needs no
// clipboard access at all: the terminal hop runs in has already read the
// clipboard by the time hop hears about a paste, and hands over the text with it.
package clipboard

import "errors"

// ErrUnavailable is returned when the platform offers no way to reach the
// clipboard — a Linux box with none of the helpers installed, most commonly, or
// one with no display server to hold a clipboard at all. It is a normal
// condition, not a failure: the caller drops the copy and carries on.
var ErrUnavailable = errors.New("clipboard: no clipboard available")

// Write puts text on the system clipboard, replacing what was there.
func Write(text string) error { return write(text) }
