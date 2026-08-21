package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"

	"hop/internal/config"
)

// settingsModel opens the popover on an isolated config file.
func settingsModel(t *testing.T) *model {
	t.Helper()
	// os.UserConfigDir must not find the developer's real config.
	dir := t.TempDir()
	t.Setenv("AppData", dir)
	t.Setenv("XDG_CONFIG_HOME", dir)
	t.Setenv("HOME", dir)

	m := &model{sessions: map[string]*session{}, notify: make(chan struct{}, 1), cfg: config.Default(), settings: settingsUI{open: true}, layout: layout{width: 100, height: 30}}
	return m
}

// fieldIndex finds a field by label, so tests do not hard-code the list's order.
func fieldIndex(t *testing.T, label string) int {
	t.Helper()
	for i, f := range settingsFields {
		if f.label == label {
			return i
		}
	}
	t.Fatalf("no settings field labelled %q", label)
	return -1
}

func TestSettingsEditCommitsAndPersists(t *testing.T) {
	m := settingsModel(t)
	m.settings.cursor = fieldIndex(t, "Editor")

	m.handleKey(key(t, "enter")) // start editing
	if !m.settings.editing {
		t.Fatal("enter did not start editing the field")
	}
	for _, r := range "nano" {
		m.handleKey(key(t, string(r)))
	}
	m.handleKey(key(t, "enter")) // commit

	if m.settings.editing {
		t.Fatal("enter did not finish editing")
	}
	if m.cfg.Editor != "nano" {
		t.Fatalf("cfg.Editor = %q, want nano", m.cfg.Editor)
	}
	if got := config.Load().Editor; got != "nano" {
		t.Fatalf("saved config has Editor = %q, want nano", got)
	}
	if m.status != "settings saved" {
		t.Fatalf("status = %q, want the save to be reported", m.status)
	}
}

// esc while typing abandons the edit and leaves the stored value alone.
func TestSettingsEditCancel(t *testing.T) {
	m := settingsModel(t)
	m.cfg.Editor = "vim"
	m.settings.cursor = fieldIndex(t, "Editor")

	m.handleKey(key(t, "enter"))
	for _, r := range "xyz" {
		m.handleKey(key(t, string(r)))
	}
	m.handleKey(key(t, "esc"))

	if m.settings.editing {
		t.Fatal("esc did not leave text entry")
	}
	if m.cfg.Editor != "vim" {
		t.Fatalf("cfg.Editor = %q, want the abandoned edit to leave vim alone", m.cfg.Editor)
	}
	if !m.settings.open {
		t.Fatal("esc closed the popover; it should only have cancelled the edit")
	}
}

// "r" puts a field back to its default.
func TestSettingsResetField(t *testing.T) {
	m := settingsModel(t)
	m.cfg.Accent = "99"
	m.settings.cursor = fieldIndex(t, "Accent color")

	m.handleKey(key(t, "r"))

	if m.cfg.Accent != config.DefaultAccent {
		t.Fatalf("Accent = %q, want the default %q", m.cfg.Accent, config.DefaultAccent)
	}
}

// ←/→ walk the accent palette, wrapping, applying and persisting as they go.
func TestAccentSwatchCycling(t *testing.T) {
	m := settingsModel(t)
	m.settings.cursor = fieldIndex(t, "Accent color")

	if m.cfg.Accent != accentSwatches[0].code {
		t.Fatalf("Accent starts at %q, want the first swatch %q", m.cfg.Accent, accentSwatches[0].code)
	}

	m.handleKey(key(t, "right"))
	if want := accentSwatches[1].code; m.cfg.Accent != want {
		t.Fatalf("after right: Accent = %q, want %q", m.cfg.Accent, want)
	}
	if got := config.Load().Accent; got != accentSwatches[1].code {
		t.Fatalf("saved Accent = %q; picking a color must persist it", got)
	}

	m.handleKey(key(t, "left"))
	m.handleKey(key(t, "left"))
	if want := accentSwatches[len(accentSwatches)-1].code; m.cfg.Accent != want {
		t.Fatalf("after wrapping left: Accent = %q, want the last swatch %q", m.cfg.Accent, want)
	}
}

func TestAccentCustomValueSurvives(t *testing.T) {
	m := settingsModel(t)
	m.settings.cursor = fieldIndex(t, "Accent color")

	m.handleKey(key(t, "enter"))  // opens with the current value pre-filled…
	m.handleKey(key(t, "ctrl+u")) // …so clear it before typing a new one
	for _, r := range "99" {
		m.handleKey(key(t, string(r)))
	}
	m.handleKey(key(t, "enter"))

	if m.cfg.Accent != "99" {
		t.Fatalf("Accent = %q, want the typed 99", m.cfg.Accent)
	}
	// A value outside the palette gets its own swatch on the strip.
	strip := m.renderSwatches(settingsFields[m.settings.cursor], true, 60)
	if !strings.Contains(strip, "99") {
		t.Fatalf("swatch strip = %q, want the custom value shown", strip)
	}
}

// The vim keys field is a switch: it flips, applies to open browsers, persists, and takes no text.
func TestVimKeysToggle(t *testing.T) {
	m := settingsModel(t)
	m.settings.cursor = fieldIndex(t, "Vim keys")

	if m.cfg.VimKeys {
		t.Fatal("vim keys start on; they are meant to be opt-in")
	}

	m.handleKey(key(t, "enter"))
	if m.settings.editing {
		t.Fatal("enter opened a text field on a switch")
	}
	if !m.cfg.VimKeys {
		t.Fatal("enter did not turn the vim keys on")
	}
	if !config.Load().VimKeys {
		t.Fatal("turning the vim keys on did not persist")
	}
	if !m.browserOptions().VimKeys {
		t.Fatal("the browsers were not told the vim keys are on")
	}

	m.handleKey(key(t, "left"))
	if m.cfg.VimKeys {
		t.Fatal("left did not turn the vim keys back off")
	}
	m.handleKey(key(t, "right"))
	if !m.cfg.VimKeys {
		t.Fatal("right did not turn the vim keys back on")
	}

	m.handleKey(key(t, "r"))
	if m.cfg.VimKeys {
		t.Fatal("r did not reset the vim keys to off")
	}
}

func TestArrowsLeaveTextFieldsAlone(t *testing.T) {
	m := settingsModel(t)
	m.settings.cursor = fieldIndex(t, "Editor")
	m.cfg.Editor = "nvim"

	m.handleKey(key(t, "right"))
	m.handleKey(key(t, "left"))

	if m.cfg.Editor != "nvim" {
		t.Fatalf("Editor = %q; arrows must not touch a text field", m.cfg.Editor)
	}
}

func TestSettingsSwallowsKeys(t *testing.T) {
	m := settingsModel(t)
	m.filtered = []int{0, 1, 2}
	m.cursor = 1

	m.handleKey(key(t, "down")) // would move the host cursor if it leaked through

	if m.cursor != 1 {
		t.Fatalf("host cursor moved to %d; the popover must swallow the key", m.cursor)
	}
	if m.settings.cursor != 1 {
		t.Fatalf("settings cursor = %d, want down to have moved it to 1", m.settings.cursor)
	}
}

// The popover gates hjkl on the vim setting; the arrows and enter never are.
func TestSettingsHonoursVimSetting(t *testing.T) {
	m := settingsModel(t)

	m.handleKey(key(t, "j"))
	if m.settings.cursor != 0 {
		t.Fatalf("settings cursor = %d; with the vim keys off, j must not move it", m.settings.cursor)
	}

	m.cfg.VimKeys = true
	m.handleKey(key(t, "j"))
	if m.settings.cursor != 1 {
		t.Fatalf("settings cursor = %d; with the vim keys on, j must move it", m.settings.cursor)
	}

	m.cfg.VimKeys = false
	m.handleKey(key(t, "down"))
	if m.settings.cursor != 2 {
		t.Fatalf("settings cursor = %d, want the arrow to move it to 2 with vim off", m.settings.cursor)
	}
}

// While a field has the keyboard, "h" is a letter, not a motion the gate can veto.
func TestSettingsTypingIsNotGated(t *testing.T) {
	m := settingsModel(t)
	m.settings.cursor = fieldIndex(t, "Editor")

	m.handleKey(key(t, "enter")) // open the buffer
	for _, r := range "helix" {
		m.handleKey(key(t, string(r)))
	}
	m.handleKey(key(t, "enter")) // commit

	if m.cfg.Editor != "helix" {
		t.Fatalf("cfg.Editor = %q, want helix — the vim gate ate letters out of a typed value", m.cfg.Editor)
	}
}

// esc closes the popover; "," opens it again from the host list.
func TestSettingsOpenClose(t *testing.T) {
	m := settingsModel(t)

	m.handleKey(key(t, "esc"))
	if m.settings.open {
		t.Fatal("esc did not close the popover")
	}

	m.handleKey(key(t, ","))
	if !m.settings.open {
		t.Fatal(", did not reopen the popover from the host list")
	}
}

// The configured editor runs on the remote host with its flags and a quoted path.
func TestConfiguredEditorIsUsed(t *testing.T) {
	got := remoteEditorCmd("vim -R", "/tmp/my notes.md")
	want := `exec vim -R '/tmp/my notes.md'`
	if got != want {
		t.Fatalf("remoteEditorCmd = %q, want %q", got, want)
	}

	if auto := remoteEditorCmd("", "/etc/hosts"); !strings.Contains(auto, "${EDITOR:-") {
		t.Fatalf("remoteEditorCmd with no setting = %q, want the remote $EDITOR probe", auto)
	}
}

// The popover floats: the screen behind it keeps its size.
func TestOverlayKeepsBackgroundShape(t *testing.T) {
	bg := strings.Repeat("abcdefghij\n", 6)
	bg = strings.TrimSuffix(bg, "\n")
	fg := "XX\nXX"

	got := overlay(bg, fg, 4, 2)
	lines := strings.Split(got, "\n")

	if len(lines) != 6 {
		t.Fatalf("overlay produced %d lines, want the background's 6", len(lines))
	}
	for i, ln := range lines {
		if w := lipgloss.Width(ln); w != 10 {
			t.Fatalf("line %d is %d cells wide, want the background's 10: %q", i, w, ln)
		}
	}
	if lines[0] != "abcdefghij" {
		t.Fatalf("line above the box was touched: %q", lines[0])
	}
	if lines[2] != "abcdXXghij" {
		t.Fatalf("line 2 = %q, want the box spliced in at column 4", lines[2])
	}
	if lines[4] != "abcdefghij" {
		t.Fatalf("line below the box was touched: %q", lines[4])
	}
}

func TestOverlayClipsAtBottom(t *testing.T) {
	bg := "aaaa\nbbbb"
	got := overlay(bg, "XX\nXX\nXX", 1, 1)

	lines := strings.Split(got, "\n")
	if len(lines) != 2 {
		t.Fatalf("overlay grew the screen to %d lines, want 2", len(lines))
	}
	if lines[1] != "bXXb" {
		t.Fatalf("line 1 = %q, want the visible row of the box spliced in", lines[1])
	}
}

// The card fits every window from settingsMinH rows up, the standard 24 included.
func TestSettingsCardFitsTheWindow(t *testing.T) {
	if settingsMinH() > 24 {
		t.Fatalf("the packed card needs %d rows; it must fit a standard 24-row terminal", settingsMinH())
	}
	for h := settingsMinH(); h <= settingsFullH()+8; h++ {
		m := &model{cfg: config.Default(), layout: layout{height: h, width: 100}}
		m.openSettings()
		if got := lipgloss.Height(m.renderSettings()); got > h {
			t.Errorf("a %d-row window got a %d-line card", h, got)
		}
	}
}

// A window too short for every field shows a run of them, always holding the cursor's.
func TestSettingsCardScrollsToTheCursor(t *testing.T) {
	m := &model{cfg: config.Default(), layout: layout{height: settingsMinH(), width: 100}}
	m.openSettings()

	for i := range settingsFields {
		m.settings.cursor = i
		first, count := m.settingsWindow()

		if count >= len(settingsFields) {
			t.Fatalf("a %d-row window drew all %d fields", m.height, count)
		}
		if i < first || i >= first+count {
			t.Fatalf("field %d is outside the drawn window [%d, %d)", i, first, first+count)
		}
		if got := lipgloss.Height(m.renderSettings()); got > m.height {
			t.Fatalf("cursor on field %d: a %d-row window got a %d-line card", i, m.height, got)
		}
		if !strings.Contains(m.renderSettings(), settingsFields[i].label) {
			t.Fatalf("field %q is selected but not drawn", settingsFields[i].label)
		}
	}
}
