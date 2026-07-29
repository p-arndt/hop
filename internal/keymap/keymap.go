// Package keymap is hop's motion keyboard, in one place.
//
// Two things in hop scroll — the host list and the file browser — and a key means
// the same thing in both. Rather than each spelling that keyboard out, they resolve
// keys through this package and act on the Motion it hands back: what a key *means*
// is decided here, and what it *does* is decided by the thing that scrolls, which is
// the only part that knows how tall it is or what is under the cursor.
//
// The two views bind different amounts of that keyboard (see Scope), but never
// different meanings: a key the list does bind does in the list what it does in the
// browser.
//
// That split is what makes the "Vim keys" setting a single fact rather than a flag
// threaded through three switch statements. It is also where a new motion key goes:
// one row in the table below, and both views have it.
package keymap

// Motion is what a key means to a view that scrolls. It is deliberately about the
// view and not about the content — In and Out are "descend" and "back out", which
// the host list reads as connect/disconnect-the-view and the browser reads as
// enter/leave a directory.
type Motion int

// Scope is which view is asking. The two do not hold quite the same keyboard: the
// browser walks directories that run to hundreds of entries, so it keeps the whole
// vim motion set, while the host list — which does not scroll, every host being on
// screen — keeps only the step keys. There, gg/G/H/M/L all land on rows a couple of
// j's away, and ctrl+f/b duplicate pgup/pgdn; binding them bought consistency at
// the price of holding nine keys hostage in the one view that has commands to spare.
type Scope int

const (
	// Full is the file browser: every motion in the table.
	Full Scope = iota
	// List is the host list: step, page and in/out, nothing else.
	List
)

const (
	// None is "not a motion key". The caller is free to give the key its own
	// meaning — with the vim keys off, that is exactly what the letters become.
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

// chordG is the first half of "gg": a motion only in pairs, so it is not in the
// table — Reader holds the half-typed one.
const chordG = "g"

// binding is what one key means, whether the "Vim keys" setting owns it, and
// whether the host list binds it as well as the browser (see Scope).
type binding struct {
	motion Motion
	vim    bool
	list   bool
}

// bindings is the whole motion keyboard. The vim column is the setting: those keys
// are plain letters (and the ctrl chords vim uses for paging), and hop holding them
// silently is a surprise to anyone who did not ask for vim — "h" and "l" alone step
// out of and into a host. So they are asked for.
//
// pgup/pgdn are pointedly not vim. They mean what ctrl+f/ctrl+b mean, but they are
// nobody's editor bindings, so turning the vim keys off must not cost you a way to
// page — and it does not.
//
// ctrl+b is missing on purpose, which is why ctrl+f has no partner here: hop binds
// it in every mode as the sidebar toggle (see tui.toggleSidebarKey), and a key that
// paged a directory in one view and moved the furniture in another would be worse
// than a key that does one thing. Paging back is pgup.
//
// The list column is the Scope split: the keys the host list binds too. It keeps
// the ones you reach for without thinking — step, page, in and out — and leaves the
// jumps and the ctrl chords to the browser.
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

// Vim reports whether key is one the "Vim keys" setting owns. It is the question a
// mode with its own keymap asks — the settings popover has no motions to resolve,
// but it still has to honour the setting, or it would be a card that answers to
// hjkl while holding the switch that says it does not.
func Vim(key string) bool {
	return key == chordG || bindings[key].vim
}

// Reader resolves keys to motions. It holds the only state a keymap has: a "gg"
// waiting for its second g.
//
// A view owns one and reads every key through it, so the chord is per-view — a
// lone "g" in the host list is not completed by a "g" typed later in the browser.
type Reader struct {
	pendingG bool
}

// Motion resolves key, or returns None when the key is not a motion — or is one the
// vim setting has switched off, which is the same answer on purpose: an unbound key
// and a key hop has decided not to bind are indistinguishable to the caller, and a
// letter it does not recognise is a letter it can use for something else.
//
// vim is passed on every key rather than held, so the setting has exactly one home
// (the config) and turning it off in the popover is live on the very next keystroke,
// with nothing to keep in sync.
//
// A lone "g" arms the "gg" chord and reads as None, which every caller already
// treats as "nothing to do" — no view binds a bare "g" to anything else. Any other
// key disarms it, so "g" "j" is a plain "j". In Scope List there is no chord to arm:
// "gg" is not bound there, so a "g" is simply not a motion.
//
// sc is passed per key for the same reason vim is: it is a fact about the caller,
// not state worth keeping, and a Reader belongs to exactly one view anyway.
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
