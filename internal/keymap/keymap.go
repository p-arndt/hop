// Package keymap names the keys hop binds conditionally, so that the packages
// binding them cannot drift apart on what they are.
package keymap

// Vim reports whether key is one of hop's vim motions: the bindings the "Vim keys"
// setting turns on, in the host list and the file browser alike.
//
// They are opt-in because every one of them is a plain letter that hop would
// otherwise be quietly holding — h and l alone step out of and into a host, and a
// user who never asked for vim has no way to know that before it happens. With the
// setting off, these keys are not bound at all: nothing moves, and the letters are
// free for what the mode says they do.
//
// pgup/pgdn are deliberately not here. They mean the same thing as ctrl+f/ctrl+b
// but are not vim, so they stay bound either way — turning vim keys off must not
// cost you a way to page.
func Vim(key string) bool {
	switch key {
	case "h", "j", "k", "l",
		"g", "G", "H", "M", "L",
		"ctrl+d", "ctrl+u", "ctrl+f", "ctrl+b":
		return true
	}
	return false
}
