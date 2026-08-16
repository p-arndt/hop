package keys

import "time"

// doubleEscWindow is how long after an esc a second esc counts as "leave the pane" rather
// than two escapes bound for the remote shell. It lives here rather than in the caller
// because it is a property of the binding — see Binding.Window.
const doubleEscWindow = 400 * time.Millisecond

// now is the clock a Reader reads. A variable so a test can drive a chord's window
// without sleeping through it.
var now = time.Now

// Reader resolves keystrokes against a Map, holding the one piece of state a keyboard
// has: a sequence waiting for its second key.
//
// hop used to hold three of those, in three shapes — the browser's "gg" as a bool in the
// keymap, the leader as an alias string on the model, the double-esc as a timestamp — and
// each had to be armed, spent and disarmed by hand at every call site that could break
// it. A sequence is one mechanism, so it is one type.
//
// The leader is not a sequence here: it is a layer (Leader), because a chord that stays
// open and changes what the footer says is a mode rather than a pending keystroke.
type Reader struct {
	// pending is the first key of a half-typed sequence, "" when none is.
	pending string
	// layer is the layer that key was read in. A sequence does not survive leaving it:
	// an esc in a pane and an esc in the browser are not two halves of anything.
	layer Layer
	// deadline is when the pending sequence expires, zero when it waits for ever.
	deadline time.Time
}

// Result is what one keystroke came to.
type Result struct {
	// Action is what to do, None when the key means nothing in this layer.
	Action Action
	// Pending reports that the key armed a sequence and the *rest* of its meaning is not
	// decided yet. What the caller does with the key meanwhile is the caller's: the
	// browser swallows a first esc, a shell pane forwards it, because the remote program
	// is owed it.
	//
	// Action and Pending are not exclusive. A prefix key can have a meaning of its own —
	// esc in the host list backs out of the host *and* arms the double-esc that quits —
	// and a caller that only looked at one of the two fields would lose one of them.
	Pending bool
}

// Read resolves key in layer l and advances whatever sequence was in flight.
//
// A pending sequence is completed by its second key, and broken by any other — "g" then
// "j" is a plain "j", not a lost keystroke. A sequence that has outlived its window is
// broken the same way, so an esc typed a minute ago cannot pair with this one.
func (r *Reader) Read(m Map, l Layer, key string, vim bool) Result {
	key = Normalize(key)
	pending := r.pending
	expired := !r.deadline.IsZero() && now().After(r.deadline)
	r.pending, r.deadline = "", time.Time{}

	if pending != "" && r.layer == l && !expired {
		if a := m.Action(l, pending+" "+key, vim); a != None {
			return Result{Action: a}
		}
	}

	solo := m.Action(l, key, vim)
	if m.pending(l, key, vim) {
		r.pending, r.layer = key, l
		if w := m.window(l, key); w > 0 {
			r.deadline = now().Add(w)
		}
		return Result{Action: solo, Pending: true}
	}
	return Result{Action: solo}
}

// Reset drops a half-typed sequence. Leaving a mode calls it: what was typed in the mode
// you left is not the first half of what you type in the one you arrived in.
func (r *Reader) Reset() {
	r.pending, r.deadline = "", time.Time{}
}

// Pending reports whether a sequence is waiting for its second key.
func (r *Reader) Pending() bool { return r.pending != "" }

// window is how long a sequence starting with key waits in this layer. The shortest wins
// when two sequences share a first key: the tighter deadline is the one that was chosen
// because the key means something else on its own.
func (m Map) window(l Layer, key string) time.Duration {
	m = m.resolved()
	key = Normalize(key)
	var out time.Duration
	for _, b := range m.bindings {
		if b.Layer != l || b.Window <= 0 {
			continue
		}
		for _, k := range b.Keys {
			if head, _, isSeq := cutSeq(k); isSeq && head == key {
				if out == 0 || b.Window < out {
					out = b.Window
				}
			}
		}
	}
	return out
}
