package tui

import (
	"strings"
	"testing"

	"hop/internal/store"
)

// A host imported from an untrusted SSH config can smuggle terminal escapes in its
// fields. The row and the details card strip control characters, keeping the text.
func TestRenderStripsControlSequencesFromHostFields(t *testing.T) {
	evil := store.Host{
		Alias:    "web\x1b]0;pwned\x07one",
		HostName: "host\x1b[2Jname",
		User:     "de\x1bploy",
	}

	m := &model{
		hosts:      []store.Host{evil},
		sessions:   map[string]*session{},
		connecting: map[string]bool{},
		highlights: map[int][]int{},
		width:      120,
		height:     34,
		ready:      true,
	}
	m.applyFilter()
	m.recomputeLayout()

	row := m.renderRow(evil, nil, false, 60)
	card := m.renderDetails(60)

	for _, tc := range []struct {
		name, out string
	}{
		{"renderRow", row},
		{"renderDetails", card},
	} {
		if strings.Contains(tc.out, "\x1b]") {
			t.Errorf("%s leaked an OSC escape:\n%q", tc.name, tc.out)
		}
		if strings.Contains(tc.out, "\x1b]0;pwned\x07") {
			t.Errorf("%s leaked the injected title sequence:\n%q", tc.name, tc.out)
		}
		if strings.Contains(tc.out, "\x1b[2J") {
			t.Errorf("%s leaked the injected clear-screen sequence:\n%q", tc.name, tc.out)
		}
		// The printable remainder still shows: the defense strips the escape bytes, it
		// does not blank the field.
		if !strings.Contains(tc.out, "web") || !strings.Contains(tc.out, "one") {
			t.Errorf("%s dropped the printable alias remainder:\n%q", tc.name, tc.out)
		}
		if !strings.Contains(tc.out, "host") || !strings.Contains(tc.out, "name") {
			t.Errorf("%s dropped the printable hostname remainder:\n%q", tc.name, tc.out)
		}
		if strings.ContainsRune(tc.out, '\x07') {
			t.Errorf("%s leaked a BEL byte:\n%q", tc.name, tc.out)
		}
	}
}

// highlight strips control characters on the path where the fuzzy filter recorded hits
// too: the matched runes stay underlined and the hit offsets do not shift.
func TestHighlightStripsControlCharsButKeepsMatches(t *testing.T) {
	// "we" + ESC + "b1": the 'w' and 'e' are matched; the ESC sits at byte 2.
	alias := "we\x1bb1"
	hits := []int{0, 1}

	out := highlight(alias, hits, aliasStyle, matchStyle)

	// A raw ESC from the alias must not survive; lipgloss's own escapes are always
	// followed by '[' and a color code, so assert on the injected byte's context.
	if strings.Contains(out, "\x1bb1") {
		t.Errorf("highlight leaked the raw control char:\n%q", out)
	}
	// The matched characters carry the hit style's escapes and the rest the base style;
	// both printable remainders must be present.
	if !strings.Contains(out, "w") || !strings.Contains(out, "e") {
		t.Errorf("highlight dropped a matched character:\n%q", out)
	}
	if !strings.Contains(out, "b1") {
		t.Errorf("highlight dropped the printable remainder:\n%q", out)
	}
}
