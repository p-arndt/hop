// Package keymap is hop's motion keyboard, in one place.
//
// Two things in hop scroll — the host list and the file browser — and a key means the
// same thing in both. They resolve keys here and act on the Motion handed back: what a
// key means is decided here, what it does is decided by the thing that scrolls.
//
// The two views bind different amounts of that keyboard (see Scope), but never different
// meanings. That split is what makes the "Vim keys" setting a single fact, and a new
// motion key is one row in the table below.
package keymap

// Motion is what a key means to a view that scrolls, about the view rather than the
// content: In and Out are "descend" and "back out", which the list reads as connecting
// and the browser as entering a directory.
type Motion int

// Scope is which view is asking. The browser walks directories of hundreds of entries
// and keeps the whole vim motion set; the host list does not scroll, so gg/G/H/M/L would
// all land a couple of j's away and ctrl+f/b would duplicate pgup/pgdn — nine keys held
// hostage in the one view that has commands to spare.
type Scope int

const (
	// Full is the file browser: every motion in the table.
	Full Scope = iota
	// List is the host list: step, page and in/out, nothing else.
	List
)

const (
	// None is "not a motion key": the caller is free to give it its own meaning, which
	// is what the letters become with the vim keys off.
	None Motion = iota

	Up
	Down
	Top       // the first entry, wherever the view is scrolled to
	Bottom    // the last entry
	HalfUp    // half a screen
	HalfDown  //
	PageUp    // a full screen
	PageDown  //
	ScreenTop // the first entry *in view*, which in a list that fits is Top
	ScreenMid //
	ScreenBot //
	In        // descend into the thing under the cursor
	Out       // back out of it
)

// chordG is the first half of "gg": a motion only in pairs, so it is not in the table.
const chordG = "g"

// binding is what one key means, whether the "Vim keys" setting owns it, and whether the
// host list binds it as well as the browser (see Scope).
type binding struct {
	motion Motion
	vim    bool
	list   bool
}

// bindings is the whole motion keyboard.
//
// The vim column is the setting: those keys are plain letters, and hop holding them
// silently would surprise anyone who did not ask for vim. pgup/pgdn are pointedly not
// vim, so turning the setting off never costs you a way to page.
//
// ctrl+b is missing on purpose, which is why ctrl+f has no partner: hop binds it in every
// mode as the sidebar toggle. Paging back is pgup.
//
// The list column is the Scope split: step, page, in and out, leaving the jumps and the
// ctrl chords to the browser.
var bindings = map[string]binding{
	"up":     {Up, false, true},
	"k":      {Up, true, true},
	"down":   {Down, false, true},
	"j":      {Down, true, true},
	"pgup":   {PageUp, false, true},
	"pgdown": {PageDown, false, true},
	"ctrl+f": {PageDown, true, false},
	"ctrl+u": {HalfUp, true, false},
	"ctrl+d": {HalfDown, true, false},
	"G":      {Bottom, true, false},
	"H":      {ScreenTop, true, false},
	"M":      {ScreenMid, true, false},
	"L":      {ScreenBot, true, false},
	"enter":  {In, false, true},
	"right":  {In, false, true},
	"l":      {In, true, true},
	"left":   {Out, false, true},
	"h":      {Out, true, true},
}

// Vim reports whether key is one the "Vim keys" setting owns — the question a mode with
// its own keymap asks. The settings popover has no motions to resolve but still has to
// honour the setting, being the card that holds the switch.
func Vim(key string) bool {
	return key == chordG || bindings[key].vim
}

// Reader resolves keys to motions, holding the only state a keymap has: a "gg" waiting
// for its second g. A view owns one, so the chord is per-view.
type Reader struct {
	pendingG bool
}

// Motion resolves key, returning None when it is not a motion — or is one the vim setting
// has switched off, which is the same answer on purpose: a letter the caller does not get
// back is a letter it can use for something else.
//
// vim and sc are passed per key rather than held, so the setting has exactly one home and
// turning it off in the popover is live on the next keystroke.
//
// A lone "g" arms the "gg" chord and reads as None; any other key disarms it, so "g" "j"
// is a plain "j". Scope List binds no "gg", so there a "g" is simply not a motion.
func (r *Reader) Motion(sc Scope, key string, vim bool) Motion {
	armed := r.pendingG
	r.pendingG = false

	if !vim && Vim(key) {
		return None
	}
	if key == chordG {
		if sc == List {
			return None
		}
		if armed {
			return Top
		}
		r.pendingG = true
		return None
	}
	b := bindings[key]
	if sc == List && !b.list {
		return None
	}
	return b.motion
}
