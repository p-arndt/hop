package keys

import (
	"testing"
	"time"
)

// clock pins the Reader's clock and returns a function that advances it.
func clock(t *testing.T, start time.Time) func(time.Duration) {
	t.Helper()
	at := start
	old := now
	now = func() time.Time { return at }
	t.Cleanup(func() { now = old })
	return func(d time.Duration) { at = at.Add(d) }
}

// The first keystroke is swallowed while hop waits; a non-matching second is read as itself.
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

// A pending sequence expires, so a solo key never stays half a chord.
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

// A prefix key reports its own action and arms the sequence in the same read.
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
