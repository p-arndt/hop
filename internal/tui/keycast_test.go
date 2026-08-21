//go:build hopdemo

package tui

import (
	"strings"
	"testing"
)

// A typed run reads as one pill; named chords stay separate events.
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

// The trail composites into existing rows and never shifts hop's layout.
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
