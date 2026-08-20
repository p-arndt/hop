package tui

import (
	"math"
	"strconv"
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

// A window too short for the whole card gets a card that ends inside it: the box has a
// bottom edge on screen, which is what makes the hint under it readable at all.
func TestHelpFitsAShortWindow(t *testing.T) {
	for _, h := range []int{10, 18, 24, 40} {
		m, _ := statusModel(t, 120, h)
		m.mode = modeBrowser
		m.help = true

		if got := strings.Count(m.renderHelp(), "\n") + 1; got > h {
			t.Fatalf("height %d: card is %d lines tall", h, got)
		}
	}
}

// And what the short window cut off is reachable rather than gone: this is the whole
// point of the card, so the lines it could not show must be a scroll away.
func TestHelpScrollsToWhatDoesNotFit(t *testing.T) {
	m, _ := statusModel(t, 120, 20)
	m.mode = modeBrowser
	m.help = true

	top := m.renderHelp()
	if !strings.Contains(top, "scroll") {
		t.Fatalf("a card that does not fit says nothing about scrolling:\n%s", top)
	}

	m.handleHelpKey(key(t, "end"))
	bottom := m.renderHelp()
	if m.helpScroll == 0 {
		t.Fatalf("the card did not scroll:\n%s", bottom)
	}

	seen := map[string]bool{}
	for _, line := range strings.Split(top, "\n") {
		seen[line] = true
	}
	fresh := 0
	for _, line := range strings.Split(bottom, "\n") {
		if !seen[line] {
			fresh++
		}
	}
	if fresh == 0 {
		t.Fatalf("scrolling to the end shows nothing the top did not:\n%s", bottom)
	}
}

// fitHelp windows the body rather than cutting it: every line is on some page, in order,
// and the last page ends on the last line.
func TestHelpBodyWindows(t *testing.T) {
	body := make([]string, 60)
	for i := range body {
		body[i] = "line " + strconv.Itoa(i)
	}

	m := helpModel()
	m.height = 10 + helpChrome // ten lines of body to a page

	if shown, more := m.fitHelp(strings.Join(body, "\n")); !more || shown != strings.Join(body[:10], "\n") {
		t.Fatalf("first page is %q (more=%v)", shown, more)
	}

	m.helpScroll = 7
	if shown, _ := m.fitHelp(strings.Join(body, "\n")); shown != strings.Join(body[7:17], "\n") {
		t.Fatalf("page from line 7 is %q", shown)
	}

	m.helpScroll = math.MaxInt32
	shown, _ := m.fitHelp(strings.Join(body, "\n"))
	if want := strings.Join(body[50:], "\n"); shown != want {
		t.Fatalf("last page is %q, want %q", shown, want)
	}

	// A window with room for all of it scrolls nowhere and says so.
	m.height = 100 + helpChrome
	if shown, more := m.fitHelp(strings.Join(body, "\n")); more || shown != strings.Join(body, "\n") {
		t.Fatalf("a body that fits was cut (more=%v)", more)
	}
}

// The scroll is clamped to what is actually hidden: it cannot run off either end, and a
// window that grew back to holding the whole card is not left scrolled past it.
func TestHelpScrollStaysOnTheCard(t *testing.T) {
	m, _ := statusModel(t, 120, 20)
	m.mode = modeShell
	m.help = true

	for range 5 {
		m.handleHelpKey(key(t, "up"))
	}
	m.renderHelp()
	if m.helpScroll != 0 {
		t.Fatalf("scrolled up past the top: %d", m.helpScroll)
	}

	m.handleHelpKey(key(t, "end"))
	m.renderHelp()
	end := m.helpScroll
	m.handleHelpKey(key(t, "pgdown"))
	if m.renderHelp(); m.helpScroll != end {
		t.Fatalf("scrolled down past the bottom: %d, want %d", m.helpScroll, end)
	}

	m.height = 200
	if card := m.renderHelp(); m.helpScroll != 0 || !strings.Contains(card, "KEYS") {
		t.Fatalf("a window that holds the whole card is still scrolled: %d\n%s", m.helpScroll, card)
	}
}
