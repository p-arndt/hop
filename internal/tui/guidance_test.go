package tui

import (
	"strings"
	"testing"

	"hop/internal/config"
)

// isolateConfig points os.UserConfigDir at a throwaway directory, so a test that saves
// cannot rewrite the developer's own config.
func isolateConfig(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("AppData", dir)
	t.Setenv("XDG_CONFIG_HOME", dir)
	t.Setenv("HOME", dir)
}

// guidanceModel is a model with the first-run question up, on an isolated config.
func guidanceModel(t *testing.T) *model {
	t.Helper()
	isolateConfig(t)

	m := newMouseModel(3)
	m.cfg = config.Default()
	m.openGuidance()
	return m
}

// The question opens on the profile in force and answers with the one under the cursor,
// and the answer is on disk — the file existing is what stops it being asked again.
func TestGuidanceAnswerPersists(t *testing.T) {
	m := guidanceModel(t)

	if got := guidanceChoices[m.guidance.cursor].value; got != config.GuidanceHybrid {
		t.Fatalf("opened on %q, want the default %q", got, config.GuidanceHybrid)
	}
	if config.Exists() {
		t.Fatal("a config file exists before the question was answered")
	}

	m.handleKey(key(t, "down"))
	m.handleKey(key(t, "enter"))

	if m.guidance.open {
		t.Fatal("the question stayed up after enter")
	}
	if m.cfg.Guidance != config.GuidanceGuided {
		t.Fatalf("answered %q, want %q", m.cfg.Guidance, config.GuidanceGuided)
	}
	if got := config.Load().Guidance; got != config.GuidanceGuided {
		t.Fatalf("saved %q, want %q", got, config.GuidanceGuided)
	}
}

// esc is an answer too: the middle profile, saved — otherwise the question comes back on
// every start until it is answered the way hop wanted.
func TestGuidanceEscapeStillAnswers(t *testing.T) {
	m := guidanceModel(t)

	m.handleKey(key(t, "esc"))

	if m.guidance.open {
		t.Fatal("esc left the question up")
	}
	if !config.Exists() {
		t.Fatal("esc did not write a config, so the question would be asked again")
	}
	if got := config.Load().Guidance; got != config.GuidanceHybrid {
		t.Fatalf("esc saved %q, want %q", got, config.GuidanceHybrid)
	}
}

// It is modal, and above every other card: a keystroke must not reach the list behind it.
func TestGuidanceSwallowsKeys(t *testing.T) {
	m := guidanceModel(t)
	m.cursor = 1

	m.handleKey(key(t, "x")) // would arm a delete in the list

	if m.confirm.open {
		t.Fatal("a key reached the list under the question")
	}
	if !m.guidance.open {
		t.Fatal("an unrelated key answered the question")
	}
}

// The profile decides how much is on screen and nothing else. The clearest statement of
// that is the details card: the same keys, spelled out or not.
func TestProfileChangesWhatIsShownNotWhatWorks(t *testing.T) {
	m := newMouseModel(3)

	cases := []struct {
		guidance string
		actions  bool // the ACTIONS grid is drawn
		global   bool // hop's own keys are in it
	}{
		{config.GuidanceKeys, false, false},
		{config.GuidanceHybrid, true, false},
		{config.GuidanceGuided, true, true},
	}
	for _, c := range cases {
		m.cfg.Guidance = c.guidance
		card := m.renderDetails(m.paneW)

		if got := strings.Contains(card, "ACTIONS"); got != c.actions {
			t.Fatalf("%s: ACTIONS grid = %v, want %v:\n%s", c.guidance, got, c.actions, card)
		}
		if got := strings.Contains(card, "import an ssh config"); got != c.global {
			t.Fatalf("%s: hop's own keys = %v, want %v:\n%s", c.guidance, got, c.global, card)
		}
		// The host's keys are named in every profile, whether or not the card lists
		// them: the footer's core is not the profile's to cut.
		core, _, _ := m.footerHints()
		if !strings.Contains(strings.Join(core, " "), "connect") {
			t.Fatalf("%s: the footer core lost its keys: %v", c.guidance, core)
		}

		// And the keys themselves are untouched — the quietest profile still adds a host.
		m.handleKey(key(t, "a"))
		if !m.hostForm.open {
			t.Fatalf("%s: 'a' did not open the host form", c.guidance)
		}
		m.closeHostForm()
	}
}

// keys drops the extras a wide window would have room for; guided lifts the way to the
// action list into the core, where truncation cannot reach it.
func TestProfileShapesTheFooter(t *testing.T) {
	m := newMouseModel(3)

	m.cfg.Guidance = config.GuidanceKeys
	if _, extra, _ := m.footerHints(); len(extra) != 0 {
		t.Fatalf("keys profile offers extras: %v", extra)
	}

	m.cfg.Guidance = config.GuidanceHybrid
	hybridCore, hybridExtra, _ := m.footerHints()
	if len(hybridExtra) == 0 {
		t.Fatal("hybrid profile offers no extras")
	}

	m.cfg.Guidance = config.GuidanceGuided
	guidedCore, guidedExtra, _ := m.footerHints()
	if len(guidedCore) != len(hybridCore)+1 {
		t.Fatalf("guided core %v, want one more than hybrid's %v", guidedCore, hybridCore)
	}
	// Moved, not duplicated.
	if len(guidedExtra) != len(hybridExtra)-1 {
		t.Fatalf("guided extras %v, want one fewer than hybrid's %v", guidedExtra, hybridExtra)
	}
	joined := strings.Join(guidedCore, " ")
	if strings.Count(joined+" "+strings.Join(guidedExtra, " "), "search actions") != 1 {
		t.Fatalf("the palette hint is on the row twice: %v %v", guidedCore, guidedExtra)
	}
}
