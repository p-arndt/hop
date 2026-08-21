package tui

import (
	"strings"
	"testing"

	"hop/internal/store"
)

func TestRenderStripsControlSequencesFromHostFields(t *testing.T) {
	evil := store.Host{
		Alias:    "web\x1b]0;pwned\x07one",
		HostName: "host\x1b[2Jname",
		User:     "de\x1bploy",
	}

	m := &model{hosts: []store.Host{evil}, sessions: map[string]*session{}, connecting: map[string]bool{}, highlights: map[int][]int{}, layout: layout{width: 120, height: 34, ready: true}}
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
		// The escape bytes are stripped; the field is not blanked.
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

// The hit offsets do not shift when control characters are stripped.
func TestHighlightStripsControlCharsButKeepsMatches(t *testing.T) {
	// "we" + ESC + "b1": the 'w' and 'e' are matched; the ESC sits at byte 2.
	alias := "we\x1bb1"
	hits := []int{0, 1}

	out := highlight(alias, hits, aliasStyle, matchStyle)

	// lipgloss escapes are always followed by '[', so assert on the injected byte's context.
	if strings.Contains(out, "\x1bb1") {
		t.Errorf("highlight leaked the raw control char:\n%q", out)
	}
	if !strings.Contains(out, "w") || !strings.Contains(out, "e") {
		t.Errorf("highlight dropped a matched character:\n%q", out)
	}
	if !strings.Contains(out, "b1") {
		t.Errorf("highlight dropped the printable remainder:\n%q", out)
	}
}
