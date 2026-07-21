package tui

import (
	"strings"
	"testing"

	"hop/internal/store"
)

// A host imported from an untrusted SSH config can smuggle terminal escape
// sequences in its fields. They must never survive to the terminal: the row and
// the details card strip control characters, keeping only the printable text.
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
		// The printable remainder still has to show: the defense strips the escape
		// bytes (ESC, BEL), it does not blank the field — the now-inert payload is
		// left as ordinary text.
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

// highlight strips control characters even on the path where the fuzzy filter
// recorded hits: the matched printable runes stay underlined, the smuggled
// control char never reaches the terminal, and the hit offsets do not shift.
func TestHighlightStripsControlCharsButKeepsMatches(t *testing.T) {
	// "we" + ESC + "b1": the 'w' and 'e' are matched; the ESC sits at byte 2.
	alias := "we\x1bb1"
	hits := []int{0, 1}

	out := highlight(alias, hits, aliasStyle, matchStyle)

	// A raw ESC from the alias must not survive; only lipgloss's own styling
	// escapes may, and those are always followed by '[' and a color code rather
	// than a bare payload byte. Assert on the injected byte's context.
	if strings.Contains(out, "\x1bb1") {
		t.Errorf("highlight leaked the raw control char:\n%q", out)
	}
	// The matched characters are rendered through the hit style, so they carry
	// its escapes; the surrounding text carries the base style. Both printable
	// remainders must be present.
	if !strings.Contains(out, "w") || !strings.Contains(out, "e") {
		t.Errorf("highlight dropped a matched character:\n%q", out)
	}
	if !strings.Contains(out, "b1") {
		t.Errorf("highlight dropped the printable remainder:\n%q", out)
	}
}
