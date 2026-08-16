package tui

import (
	"strings"
	"testing"

	"hop/internal/keys"
)

// helpModel is a model with hop's default keyboard and the vim keys on, which is the
// keyboard these tests are about: the card renders whatever the model's map says.
func helpModel() *model {
	m := &model{binds: keys.Defaults()}
	m.cfg.VimKeys = true
	return m
}

// The card opens on the mode you were in: its section is lifted to the top of the left
// column and marked. This is what the short footer leans on — the row names two or three
// keys and points here, which only helps if "here" starts where you are.
func TestHelpOpensOnTheModeYouAreIn(t *testing.T) {
	cases := []struct {
		mode paneMode
		want string
	}{
		{modeList, "LIST"},
		{modeShell, "SHELL"},
		{modeScrollback, "SHELL"}, // history has no keyboard of its own; it is the shell's
		{modeBrowser, "SFTP BROWSER"},
		{modeEditor, "EDITOR"},
	}

	for _, tc := range cases {
		t.Run(tc.want, func(t *testing.T) {
			left, _, lead := helpModel().helpFor(tc.mode)
			if lead != tc.want {
				t.Fatalf("card opened on %q, want %q", lead, tc.want)
			}
			if len(left) == 0 || left[0].title != tc.want {
				t.Fatalf("the %v section is not the first thing on the card: %v", tc.want, titles(left))
			}
		})
	}
}

// Lifting a section moves it, never copies it: a card listing SHELL twice would be a card
// you cannot trust to be a complete table.
func TestHelpKeepsEverySectionOnce(t *testing.T) {
	before, beforeRight, _ := helpModel().helpFor(modeList)
	all := append(titles(before), titles(beforeRight)...)

	for _, mode := range []paneMode{modeList, modeShell, modeScrollback, modeBrowser, modeEditor} {
		left, right, _ := helpModel().helpFor(mode)
		got := append(titles(left), titles(right)...)
		if len(got) != len(all) {
			t.Fatalf("mode %s: card has %d sections, want %d: %v", modeName(mode), len(got), len(all), got)
		}
		seen := map[string]int{}
		for _, title := range got {
			seen[title]++
			if seen[title] > 1 {
				t.Fatalf("mode %s: section %q appears twice: %v", modeName(mode), title, got)
			}
		}
		for _, title := range all {
			if seen[title] == 0 {
				t.Fatalf("mode %s: section %q went missing: %v", modeName(mode), title, got)
			}
		}
	}
}

// The card names the key that opened it, in the form that mode actually takes — the SHELL
// section the chord, the ones hop owns the plain key.
func TestHelpNamesItsOwnKey(t *testing.T) {
	left, right, _ := helpModel().helpFor(modeList)
	for _, sec := range append(left, right...) {
		want, ok := map[string]string{
			"LIST":         "?",
			"SHELL":        "ctrl+o ?",
			"SFTP BROWSER": "?",
			"EDITOR":       "ctrl+o ?",
		}[sec.title]
		if !ok {
			continue
		}
		found := false
		for _, k := range sec.keys {
			if k[0] == want {
				found = true
			}
		}
		if !found {
			t.Fatalf("the %s section does not name %q as its way to this card", sec.title, want)
		}
	}
}

// The marked section is visible as such on the rendered card, not merely first.
func TestHelpMarksWhereYouAre(t *testing.T) {
	m, _ := statusModel(t, 120, 34)
	m.mode = modeBrowser
	m.help = true

	card := m.renderHelp()
	if !strings.Contains(card, "you are here") {
		t.Fatalf("the card does not mark the section it opened on:\n%s", card)
	}
	if i, j := strings.Index(card, "SFTP BROWSER"), strings.Index(card, "LIST"); i > j {
		t.Fatalf("the browser section is not ahead of the list's on a card opened from the browser:\n%s", card)
	}
}

func titles(secs []helpSection) []string {
	out := make([]string, len(secs))
	for i, s := range secs {
		out[i] = s.title
	}
	return out
}
