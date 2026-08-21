package keys

import "time"

// doubleEscWindow is how long after an esc a second esc means "leave the pane".
const doubleEscWindow = 400 * time.Millisecond

// now is a variable so a test can drive a chord's window without sleeping through it.
var now = time.Now

// Reader resolves keystrokes against a Map, holding a sequence waiting for its second
// key. The leader is not a sequence here: it is a layer (Leader).
type Reader struct {
	pending string
	// layer is the layer that key was read in; a sequence does not survive leaving it.
	layer Layer
	// deadline is when the pending sequence expires, zero when it waits for ever.
	deadline time.Time
}

type Result struct {
	Action Action
	// Pending reports that the key armed a sequence. Not exclusive with Action: a prefix
	// key can also have a meaning of its own.
	Pending bool
}

// Read resolves key in layer l and advances whatever sequence was in flight: a pending
// sequence is broken by any key but its second, and by its window expiring.
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

// Reset drops a half-typed sequence; leaving a mode calls it.
func (r *Reader) Reset() {
	r.pending, r.deadline = "", time.Time{}
}

func (r *Reader) Pending() bool { return r.pending != "" }

// window is how long a sequence starting with key waits in this layer; the shortest wins
// when two sequences share a first key.
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
