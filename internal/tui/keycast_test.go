//go:build hopdemo

package tui

import (
	"strings"
	"testing"
)

// Typing a command has to read as one pill, not as one pill per letter, and named
// chords have to stay separate events.
func TestKeycastGroupsTypedRuns(t *testing.T) {
	m := viewModel(120, 34)

	for _, k := range []string{"d", "f", " ", "-", "h", "enter", "ctrl+o", "f"} {
		m.keycastRecord(k)
	}

	got := make([]string, 0, len(m.keycast))
	for _, ev := range m.keycast {
		got = append(got, ev.label)
	}
	want := []string{"df -h", "enter", "ctrl+o", "f"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("keycast = %v, want %v", got, want)
	}
}

// The trail is a window on the last few events, not a transcript.
func TestKeycastKeepsOnlyTheRecentEvents(t *testing.T) {
	m := viewModel(120, 34)

	for _, k := range []string{"enter", "ctrl+o", "f", "down", "up", "enter", "esc"} {
		m.keycastRecord(k)
	}
	if len(m.keycast) != keycastLen {
		t.Fatalf("kept %d events, want %d", len(m.keycast), keycastLen)
	}
	if last := m.keycast[len(m.keycast)-1].label; last != "esc" {
		t.Errorf("newest event is %q, want esc", last)
	}
}

// Drawing the trail must not change the shape of the screen: it is composited into
// the existing rows, so a recording never shifts hop's layout.
func TestKeycastDrawKeepsTheScreenShape(t *testing.T) {
	m := viewModel(120, 34)
	before := m.View()

	m.keycastRecord("ctrl+o")
	after := m.View()

	if bl, al := strings.Count(before, "\n"), strings.Count(after, "\n"); bl != al {
		t.Errorf("view went from %d to %d lines with the keycast up", bl+1, al+1)
	}
	if !strings.Contains(after, "ctrl+o") {
		t.Error("the keycast pill is not on the screen")
	}
}
