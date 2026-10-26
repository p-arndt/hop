package tui

import (
	"strings"
	"testing"

	"hop/internal/config"
	"hop/internal/keys"
)

func TestApplyKeybindings(t *testing.T) {
	m := &model{cfg: config.Config{Keybindings: map[string]string{
		"list.pin":     "z",
		"list.unknown": "x",
	}}}
	m.applyKeybindings()
	if got := m.binds.Action(keys.List, "z", false); got != keys.HostPin {
		t.Fatalf("z resolves to %q, want %q", got, keys.HostPin)
	}
	if got := m.binds.Action(keys.List, "p", false); got == keys.HostPin {
		t.Fatal("the previous binding still resolves to pin")
	}
	if m.statusKind != statusWarn || !strings.Contains(m.status, "list.unknown") {
		t.Fatalf("missing warning for unknown action: %q", m.status)
	}
}
