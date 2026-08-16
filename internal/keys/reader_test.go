package keys

import (
	"testing"
	"time"
)

// clock pins the Reader's clock at t and returns a function that advances it, so a
// chord's window can be driven without sleeping through it.
func clock(t *testing.T, start time.Time) func(time.Duration) {
	t.Helper()
	at := start
	old := now
	now = func() time.Time { return at }
	t.Cleanup(func() { now = old })
	return func(d time.Duration) { at = at.Add(d) }
}

// "gg" is two keystrokes that mean one thing. The first is swallowed while hop waits, and
// a key that is not the second half is read as itself rather than lost.
func TestReaderResolvesASequence(t *testing.T) {
	m := Defaults()
	var r Reader

	if got := r.Read(m, Browser, "g", true); got.Action != None || !got.Pending {
		t.Fatalf("first g = %+v, want a pending sequence", got)
	}
	if !r.Pending() {
		t.Fatal("Reader does not report the half-typed chord")
	}
	if got := r.Read(m, Browser, "g", true); got.Action != Top {
		t.Fatalf("second g = %+v, want %q", got, Top)
	}
	if r.Pending() {
		t.Fatal("the chord stayed armed after it fired")
	}

	// g then something else: the something else keeps its own meaning.
	r.Read(m, Browser, "g", true)
	if got := r.Read(m, Browser, "j", true); got.Action != Down {
		t.Fatalf("g then j = %+v, want a plain %q", got, Down)
	}
}

// The double-esc has a window because its first key means something on its own: an esc
// bound for a remote program must not turn into half a chord for the rest of the session.
func TestReaderSequenceWindow(t *testing.T) {
	m := Defaults()
	advance := clock(t, time.Date(2026, time.August, 16, 12, 0, 0, 0, time.UTC))
	var r Reader

	if got := r.Read(m, Pane, "esc", true); !got.Pending {
		t.Fatalf("first esc = %+v, want it pending", got)
	}
	advance(100 * time.Millisecond)
	if got := r.Read(m, Pane, "esc", true); got.Action != PaneLeave {
		t.Fatalf("second esc inside the window = %+v, want %q", got, PaneLeave)
	}

	r.Read(m, Pane, "esc", true)
	advance(doubleEscWindow + time.Millisecond)
	got := r.Read(m, Pane, "esc", true)
	if got.Action != None || !got.Pending {
		t.Fatalf("esc after the window = %+v, want a fresh pending esc", got)
	}
}

// A sequence does not survive leaving the layer it was started in: an esc typed at a
// remote shell and an esc typed in the browser are not two halves of anything.
func TestReaderSequenceIsPerLayer(t *testing.T) {
	m := Defaults()
	var r Reader

	r.Read(m, Pane, "esc", true)
	if got := r.Read(m, Browser, "esc", true); got.Action != None || !got.Pending {
		t.Fatalf("esc in another layer = %+v, want it to start over there", got)
	}

	r.Read(m, Browser, "g", true)
	r.Reset()
	if r.Pending() {
		t.Fatal("Reset left the chord armed")
	}
	if got := r.Read(m, Browser, "g", true); !got.Pending {
		t.Fatalf("after Reset = %+v, want the chord to start over", got)
	}
}

// A key with no meaning in this layer resolves to nothing, which is what lets a pane
// forward it to the remote program.
func TestReaderPassesThroughUnboundKeys(t *testing.T) {
	m := Defaults()
	var r Reader

	got := r.Read(m, Pane, "a", true)
	if got.Action != None || got.Pending {
		t.Fatalf("plain letter in a pane = %+v, want nothing", got)
	}
	if got := r.Read(m, Pane, "ctrl+o", true); got.Action != LeaderKey {
		t.Fatalf("leader = %+v, want %q", got, LeaderKey)
	}
}

// A prefix key can mean something on its own. In the host list esc backs out of the host
// and arms the double-esc that quits: a caller reading only one of the two fields would
// lose one of the two meanings.
func TestReaderPrefixKeepsItsSoloMeaning(t *testing.T) {
	m := Defaults()
	clock(t, time.Date(2026, time.August, 16, 12, 0, 0, 0, time.UTC))
	var r Reader

	got := r.Read(m, List, "esc", true)
	if got.Action != Back || !got.Pending {
		t.Fatalf("first esc in the list = %+v, want %q and a pending chord", got, Back)
	}
	if got := r.Read(m, List, "esc", true); got.Action != Quit {
		t.Fatalf("second esc = %+v, want %q", got, Quit)
	}
}
