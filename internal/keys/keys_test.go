package keys

import (
	"strings"
	"testing"
)

// Table invariants: an id names one thing, and a key means one thing per layer.
func TestDefaultsAreConsistent(t *testing.T) {
	m := Defaults()

	seen := map[string]Action{}
	for _, b := range m.bindings {
		if b.Action == None {
			t.Fatalf("binding %+v has no action", b)
		}
		if b.Label == "" {
			t.Fatalf("%s has no label; the help card and the palette render from it", b.Action)
		}
		if len(b.Keys) == 0 {
			t.Fatalf("%s ships unbound", b.Action)
		}
		if !strings.Contains(string(b.Action), ".") {
			t.Fatalf("action %q is not namespaced; config.json rebinds by this id", b.Action)
		}
		for _, k := range b.Keys {
			if k != Normalize(k) {
				t.Fatalf("%s binds %q, which is not its normalized spelling", b.Action, k)
			}
			id := mapKey(b.Layer, k)
			if other, dup := seen[id]; dup && other != b.Action {
				t.Fatalf("%q means both %s and %s in the %s layer", k, other, b.Action, b.Layer)
			}
			seen[id] = b.Action
		}
	}
}

func TestActionIsPerLayer(t *testing.T) {
	m := Defaults()
	cases := []struct {
		layer Layer
		key   string
		want  Action
	}{
		{List, "d", HostDrop},
		{Browser, "d", BrowserDownload},
		{List, "f", HostBrowser},
		{Browser, "u", BrowserUpload},
		{Global, "ctrl+b", Sidebar},
		{Leader, "o", LeaderOut},
		{Pane, "ctrl+o", LeaderKey},
		{DeadPane, "r", DeadReconnect},
		// Not bound in this layer: a pane is owed the key.
		{Pane, "d", None},
		{List, "ctrl+b", None}, // Global's, and asked for as Global
		{Browser, "T", None},
	}
	for _, c := range cases {
		if got := m.Action(c.layer, c.key, true); got != c.want {
			t.Errorf("Action(%s, %q) = %q, want %q", c.layer, c.key, got, c.want)
		}
	}
}

// With vim off the plain letters resolve to nothing, and the non-vim keys keep working.
func TestVimKeysAreOwnedBySetting(t *testing.T) {
	m := Defaults()

	if got := m.Action(Browser, "j", true); got != Down {
		t.Fatalf("j with vim on = %q, want %q", got, Down)
	}
	if got := m.Action(Browser, "j", false); got != None {
		t.Fatalf("j with vim off = %q, want none", got)
	}
	if got := m.Action(Browser, "down", false); got != Down {
		t.Fatalf("down with vim off = %q, want %q", got, Down)
	}
	// A vim letter is also not half a sequence when the setting is off.
	if m.pending(Browser, "g", false) {
		t.Fatal("g arms the gg chord with vim off")
	}
}

// Layer lists only keys that work: nothing unbound, no vim letters when the setting is off.
func TestLayerListsWhatWorks(t *testing.T) {
	m := Defaults()

	withVim := m.Layer(Browser, true)
	withoutVim := m.Layer(Browser, false)
	if len(withoutVim) >= len(withVim) {
		t.Fatalf("browser layer: %d rows with vim off, %d with it on; want fewer",
			len(withoutVim), len(withVim))
	}
	for _, b := range withoutVim {
		if b.Vim {
			t.Fatalf("%s is a vim binding but is listed with the setting off", b.Action)
		}
	}

	m, errs := New(map[string]string{string(BrowserUpload): ""})
	if len(errs) != 0 {
		t.Fatalf("unbinding: %v", errs)
	}
	for _, b := range m.Layer(Browser, true) {
		if b.Action == BrowserUpload {
			t.Fatal("an unbound action is still listed")
		}
	}
}

func TestOverrideMovesABinding(t *testing.T) {
	m, errs := New(map[string]string{string(BrowserDownload): "y"})
	if len(errs) != 0 {
		t.Fatalf("New: %v", errs)
	}
	if got := m.Action(Browser, "y", true); got != BrowserDownload {
		t.Fatalf("y = %q, want %q", got, BrowserDownload)
	}
	if got := m.Action(Browser, "d", true); got != None {
		t.Fatalf("d after the move = %q, want nothing", got)
	}
	if got := m.Action(Browser, "u", true); got != BrowserUpload {
		t.Fatalf("u = %q, want it untouched", got)
	}
	if got := m.Keycap(BrowserDownload); got != "y" {
		t.Fatalf("keycap = %q, want the key the user chose", got)
	}
}

// A rebound key is drawn as itself, not with the default's symbol.
func TestOverrideDropsTheSymbol(t *testing.T) {
	if got := Defaults().Keycap(PaneNextTab); got != "shift+→" {
		t.Fatalf("default keycap = %q, want the symbol", got)
	}
	m, _ := New(map[string]string{string(PaneNextTab): "ctrl+n"})
	if got := m.Keycap(PaneNextTab); got != "ctrl+n" {
		t.Fatalf("keycap after rebinding = %q, want ctrl+n", got)
	}
}

// A bad override row keeps its default and reports why, rather than failing the load.
func TestBadOverridesAreRefusedNotFatal(t *testing.T) {
	m, errs := New(map[string]string{
		"list.no-such-action":   "z",
		string(BrowserDownload): "u", // already the upload key
		string(BrowserRefresh):  "y", // fine, and must survive the two above
	})
	if len(errs) != 2 {
		t.Fatalf("errors = %v, want one for the unknown id and one for the clash", errs)
	}
	if got := m.Action(Browser, "d", true); got != BrowserDownload {
		t.Fatalf("download = %q, want it left at its default", got)
	}
	if got := m.Action(Browser, "u", true); got != BrowserUpload {
		t.Fatalf("u = %q, want the upload it already was", got)
	}
	if got := m.Action(Browser, "y", true); got != BrowserRefresh {
		t.Fatalf("y = %q, want the good override applied anyway", got)
	}
}

// Unbinding one way out of a pane is allowed; unbinding both restores the defaults.
func TestEscapeHatchSurvives(t *testing.T) {
	m, errs := New(map[string]string{string(LeaderKey): ""})
	if len(errs) != 0 {
		t.Fatalf("dropping one way out: %v", errs)
	}
	if got := m.Action(Pane, "ctrl+o", true); got != None {
		t.Fatalf("leader = %q, want it unbound", got)
	}

	m, errs = New(map[string]string{string(LeaderKey): "", string(PaneLeave): ""})
	if len(errs) == 0 {
		t.Fatal("dropping both ways out was accepted")
	}
	if got := m.Action(Pane, "ctrl+o", true); got != LeaderKey {
		t.Fatalf("leader = %q, want the defaults back", got)
	}
}

// Bubble Tea's " " for the space bar collides with the sequence separator.
func TestSpaceIsNormalized(t *testing.T) {
	m := Defaults()
	if got := m.Action(List, " ", true); got != Menu {
		t.Fatalf("space = %q, want %q", got, Menu)
	}
	if got := m.Keycap(Menu); got != "space" {
		t.Fatalf("keycap = %q, want it spelled out", got)
	}
	m, errs := New(map[string]string{string(HostAdd): " "})
	if len(errs) == 0 {
		t.Fatal("space was accepted for a second action in the list layer")
	}
	if got := m.Action(List, "space", true); got != Menu {
		t.Fatalf("space after the refused override = %q, want %q", got, Menu)
	}
}
