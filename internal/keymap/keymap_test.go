package keymap

import "testing"

// The motion table is the whole point of the package: one key, one meaning, the same
// in every view that scrolls.
func TestMotions(t *testing.T) {
	cases := []struct {
		key  string
		want Motion
	}{
		{"up", Up},
		{"down", Down},
		{"k", Up},
		{"j", Down},
		{"pgup", PageUp},
		{"pgdown", PageDown},
		{"ctrl+b", PageUp},
		{"ctrl+f", PageDown},
		{"ctrl+u", HalfUp},
		{"ctrl+d", HalfDown},
		{"G", Bottom},
		{"H", ScreenTop},
		{"M", ScreenMid},
		{"L", ScreenBot},
		{"enter", In},
		{"right", In},
		{"l", In},
		{"left", Out},
		{"h", Out},

		// Not motions: the views bind these to their own commands, and the keymap
		// must leave them alone.
		{"q", None},
		{"d", None},
		{"o", None},
		{"r", None},
		{"/", None},
		{",", None},
		{"esc", None},
	}
	for _, tc := range cases {
		t.Run(tc.key, func(t *testing.T) {
			var r Reader
			if got := r.Motion(tc.key, true); got != tc.want {
				t.Fatalf("Motion(%q) = %v, want %v", tc.key, got, tc.want)
			}
		})
	}
}

// "gg" is a chord: a lone g is nothing, a second g is Top, and any key in between
// abandons it.
func TestChordGG(t *testing.T) {
	var r Reader

	if got := r.Motion("g", true); got != None {
		t.Fatalf("a lone g = %v, want None — it only arms the chord", got)
	}
	if got := r.Motion("g", true); got != Top {
		t.Fatalf("gg = %v, want Top", got)
	}
	// The chord is spent: a third g arms a fresh one rather than jumping again.
	if got := r.Motion("g", true); got != None {
		t.Fatalf("the g after a completed gg = %v, want None", got)
	}

	// g, then something else, then g: the middle key breaks it.
	r = Reader{}
	r.Motion("g", true)
	if got := r.Motion("j", true); got != Down {
		t.Fatalf("the key interrupting a gg = %v, want it to act as itself (Down)", got)
	}
	if got := r.Motion("g", true); got != None {
		t.Fatalf("g after an interrupted chord = %v, want None — the first g is gone", got)
	}
}

// With the vim keys off, every key the setting owns resolves to None: not to some
// other motion, and not to a chord left half-armed for later.
func TestVimKeysOff(t *testing.T) {
	for _, key := range []string{"h", "j", "k", "l", "g", "G", "H", "M", "L",
		"ctrl+d", "ctrl+u", "ctrl+f", "ctrl+b"} {
		t.Run(key, func(t *testing.T) {
			var r Reader
			if got := r.Motion(key, false); got != None {
				t.Fatalf("Motion(%q, vim=false) = %v, want None", key, got)
			}
			if !Vim(key) {
				t.Fatalf("Vim(%q) = false; the setting is meant to own it", key)
			}

			// Nothing was armed, so the setting coming back on cannot complete a
			// motion the user began without it.
			if got := r.Motion("g", true); got != None {
				t.Fatalf("after %q with vim off, the first g with vim on = %v, want None", key, got)
			}
		})
	}
}

// Turning the vim keys off must not cost a way to move: the keys that are nobody's
// editor bindings stay bound either way.
func TestPlainKeysSurviveVimOff(t *testing.T) {
	cases := map[string]Motion{
		"up":     Up,
		"down":   Down,
		"pgup":   PageUp,
		"pgdown": PageDown,
		"left":   Out,
		"right":  In,
		"enter":  In,
	}
	for key, want := range cases {
		t.Run(key, func(t *testing.T) {
			var r Reader
			if got := r.Motion(key, false); got != want {
				t.Fatalf("Motion(%q, vim=false) = %v, want %v", key, got, want)
			}
			if Vim(key) {
				t.Fatalf("Vim(%q) = true; the setting must not own it", key)
			}
		})
	}
}

// Each view reads through its own Reader, so a "g" left half-typed in the host list
// is not completed by a "g" typed later in the file browser.
func TestChordsAreNotShared(t *testing.T) {
	var list, browser Reader

	list.Motion("g", true) // the list is armed

	if got := browser.Motion("g", true); got != None {
		t.Fatalf("the browser's first g = %v, want None — it saw the list's half-typed gg", got)
	}
	if got := browser.Motion("g", true); got != Top {
		t.Fatalf("the browser's own gg = %v, want Top", got)
	}
}
