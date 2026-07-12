package tui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"hop/internal/config"
	"hop/internal/filebrowser"
)

// swatch is one colour on the accent picker: a name to say what it is, and the
// code hop actually stores.
type swatch struct {
	name string
	code string
}

// accentSwatches is the palette the accent field cycles through. A 256-colour code
// tells you nothing on its own, so the field shows the colours themselves and you
// pick one by looking at it; typing a code stays possible, but is no longer the
// only way in.
var accentSwatches = []swatch{
	{"pink", config.DefaultAccent}, // 212 — hop's own
	{"magenta", "205"},
	{"red", "203"},
	{"orange", "215"},
	{"yellow", "221"},
	{"green", "42"},
	{"teal", "44"},
	{"cyan", "51"},
	{"blue", "75"},
	{"indigo", "105"},
	{"purple", "141"},
	{"gray", "247"},
}

// settingsField is one editable row of the popover. Each field is described by
// how to read and write it, so the list below is the single place a new setting
// has to be added.
type settingsField struct {
	label string
	// placeholder stands in for an empty value: it says what hop does when the
	// field is left blank, which for most of them is "work it out".
	placeholder string
	// desc is the one-line explanation shown under the selected field. It is why
	// the popover can stay this sparse — the detail appears only where you are.
	desc string
	// swatches, when set, makes this a colour field: ←/→ walk the palette and the
	// row renders as swatches rather than as text. Typing a value still works, for
	// a code or a #hex that is not in the palette.
	swatches []swatch
	get      func(config.Config) string
	set      func(*config.Config, string)
}

var settingsFields = []settingsField{
	{
		label:       "Editor",
		placeholder: "auto-detect",
		desc:        "Command run on the remote host by enter. Blank: $EDITOR, else nvim/vim/vi/nano.",
		get:         func(c config.Config) string { return c.Editor },
		set:         func(c *config.Config, v string) { c.Editor = v },
	},
	{
		label:       "Download dir",
		placeholder: "~/Downloads",
		desc:        "Where the browser's d puts a file. Created on the next download if missing.",
		get:         func(c config.Config) string { return c.DownloadDir },
		set:         func(c *config.Config, v string) { c.DownloadDir = v },
	},
	{
		label:       "Accent color",
		placeholder: config.DefaultAccent,
		desc:        "←/→ to pick a color — it applies as you go. enter to type a 256-code or #hex.",
		swatches:    accentSwatches,
		get:         func(c config.Config) string { return c.Accent },
		set:         func(c *config.Config, v string) { c.Accent = v },
	},
	{
		label:       "Open with",
		placeholder: "OS default app",
		desc:        "Local command the browser's o opens a file with, e.g. code -n.",
		get:         func(c config.Config) string { return c.OpenWith },
		set:         func(c *config.Config, v string) { c.OpenWith = v },
	},
}

// settingsUI is the popover's own state. The values it edits live in model.cfg;
// this is only the cursor and the in-progress text.
type settingsUI struct {
	open    bool
	cursor  int
	editing bool
	buf     string
}

// openSettings shows the popover, always from the top.
func (m *model) openSettings() {
	m.settings = settingsUI{open: true}
	m.status = ""
}

// closeSettings hides the popover, abandoning any half-typed value.
func (m *model) closeSettings() {
	m.settings = settingsUI{}
}

// handleSettingsKey routes a key while the popover is up. It swallows everything:
// a modal that let keys through to the list behind it would be a trap.
func (m *model) handleSettingsKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	f := settingsFields[m.settings.cursor]
	key := msg.String()

	// Text-entry mode: the field has the keyboard.
	if m.settings.editing {
		switch key {
		case "enter":
			f.set(&m.cfg, strings.TrimSpace(m.settings.buf))
			m.settings.editing = false
			m.applySettings()
			m.saveSettings()
		case "esc":
			// Abandon the edit, keep the popover open.
			m.settings.editing = false
		case "backspace":
			if m.settings.buf != "" {
				r := []rune(m.settings.buf)
				m.settings.buf = string(r[:len(r)-1])
			}
		case "ctrl+u":
			m.settings.buf = ""
		default:
			if len(msg.Runes) > 0 {
				m.settings.buf += string(msg.Runes)
			}
		}
		return m, nil
	}

	switch key {
	case "esc", "q", ",", "ctrl+o":
		m.closeSettings()

	case "up", "k":
		m.settings.cursor--
		m.clampSettings()

	case "down", "j", "tab":
		m.settings.cursor++
		m.clampSettings()

	case "left", "h":
		m.cycleSwatch(f, -1)

	case "right", "l":
		m.cycleSwatch(f, 1)

	case "enter", "i":
		m.settings.editing = true
		m.settings.buf = f.get(m.cfg)

	case "r":
		// Reset this field to its default — the way back out of a bad value.
		f.set(&m.cfg, f.get(config.Default()))
		m.applySettings()
		m.saveSettings()
	}
	return m, nil
}

// cycleSwatch walks a colour field's palette by delta, wrapping around, and
// applies the colour as it goes: you judge a colour by seeing it, so there is
// nothing to confirm. A value not in the palette (a typed code) starts the walk
// from the beginning. On a text field this does nothing.
func (m *model) cycleSwatch(f settingsField, delta int) {
	n := len(f.swatches)
	if n == 0 {
		return
	}

	i := 0
	for j, s := range f.swatches {
		if s.code == f.get(m.cfg) {
			i = j
			break
		}
	}

	i = ((i+delta)%n + n) % n
	f.set(&m.cfg, f.swatches[i].code)
	m.applySettings()
	m.saveSettings()
}

// clampSettings wraps the cursor around the field list.
func (m *model) clampSettings() {
	n := len(settingsFields)
	m.settings.cursor = ((m.settings.cursor % n) + n) % n
}

// applySettings pushes the current config into everything already running, so a
// change takes effect on the spot rather than on the next launch: the palette is
// restyled, and every live browser picks up the new directories.
func (m *model) applySettings() {
	setAccent(m.cfg.Accent)
	filebrowser.SetAccent(m.cfg.Accent)

	opts := m.browserOptions()
	for _, s := range m.sessions {
		if s.browser != nil {
			s.browser.SetOptions(opts)
		}
	}
}

// browserOptions is the slice of the config the file browser cares about.
func (m *model) browserOptions() filebrowser.Options {
	return filebrowser.Options{
		DownloadDir: m.cfg.DownloadDir,
		OpenWith:    m.cfg.OpenWith,
	}
}

// saveSettings persists the config, reporting a failure rather than silently
// keeping a change that will not survive a restart.
func (m *model) saveSettings() {
	if err := m.cfg.Save(); err != nil {
		m.status = "settings: " + err.Error()
		return
	}
	m.status = "settings saved"
}

// Popover geometry.
const (
	settingsMaxW = 64 // content width, borders and padding excluded
	settingsMinW = 34
	settingsPadX = 3 // horizontal padding inside the border
	// settingsDescH is the height reserved for the selected field's explanation.
	// It is fixed, and the text wraps into it rather than being cut, so the card
	// keeps one shape: nothing below it jumps as the cursor moves.
	settingsDescH = 2
)

// settingsInnerW is the width available to a rendered row: the box minus its
// border and padding. Every line is held to it, because a modal that wraps spills
// outside its own frame.
func (m *model) settingsInnerW() int {
	w := settingsMaxW
	if roomy := m.width - 12; w > roomy {
		w = roomy
	}
	if w < settingsMinW {
		w = settingsMinW
	}
	return w
}

// renderSettings draws the popover: a card of stacked fields, each a quiet label
// over its value, with the selected one lit and explained at the foot of the card.
//
// The value sits *under* its label rather than beside it, which is what buys a
// long path or a command with flags the full width of the card instead of a
// column's worth — these values are mostly paths.
func (m *model) renderSettings() string {
	w := m.settingsInnerW()
	var b strings.Builder

	b.WriteString(titleStyle.Render("SETTINGS"))
	b.WriteString("\n\n")

	for i, f := range settingsFields {
		selected := i == m.settings.cursor

		bar, label := "  ", settingsLabel.Render(f.label)
		if selected {
			bar, label = selBar+" ", settingsLabelSel.Render(f.label)
		}
		b.WriteString(truncate(bar+label, w))
		b.WriteString("\n")
		b.WriteString(m.renderSettingsValue(f, selected, w))
		b.WriteString("\n\n")
	}

	b.WriteString(faint.Render(strings.Repeat("─", w)))
	b.WriteString("\n")

	// The selected field explains itself, in a fixed-height block.
	for _, line := range wrapExactly(settingsFields[m.settings.cursor].desc, w, settingsDescH) {
		b.WriteString(faint.Render(line))
		b.WriteString("\n")
	}
	b.WriteString("\n")

	switch {
	case m.settings.editing:
		b.WriteString(settingsHint("enter", "save", "esc", "cancel", "ctrl+u", "clear"))
	case len(settingsFields[m.settings.cursor].swatches) > 0:
		b.WriteString(settingsHint("↑↓", "move", "←→", "color", "enter", "custom", "esc", "close"))
	default:
		b.WriteString(settingsHint("↑↓", "move", "enter", "edit", "r", "reset", "esc", "close"))
	}

	return settingsBox.Width(w + 2*settingsPadX).Render(b.String())
}

// renderSettingsValue draws a field's value as a full-width row: the live text
// with a caret while it is being edited, the stored value when it has one, and a
// dim placeholder naming what hop does instead when it has none.
//
// The selected row is filled rather than merely coloured, so it reads as a field
// you are standing in — which is the thing "enter" is about to open.
func (m *model) renderSettingsValue(f settingsField, selected bool, w int) string {
	const indent = "    "
	vw := w - lipgloss.Width(indent)

	if selected && m.settings.editing {
		text := truncate(m.settings.buf, vw-3) + accentText.Render("▏")
		return indent + inputStyle.Width(vw).Render(text)
	}
	if len(f.swatches) > 0 {
		return indent + m.renderSwatches(f, selected, vw)
	}

	value, style := f.get(m.cfg), settingsValue
	if value == "" {
		value, style = f.placeholder, settingsPlaceholder
	}
	if selected {
		style = settingsValueSel.Width(vw)
	}
	return indent + style.Render(truncate(value, vw-2))
}

// renderSwatches draws a colour field: the palette as a row of colour blocks, the
// chosen one bracketed, and its name spelled out after them. The colours are the
// control — a row of codes would be the same information in a form nobody can read.
//
// A value that is not in the palette (a typed code or #hex) still gets a block of
// its own at the end, so what is in force is always on screen.
func (m *model) renderSwatches(f settingsField, selected bool, w int) string {
	current := f.get(m.cfg)
	if current == "" {
		current = f.placeholder
	}

	block := func(code string) string {
		return lipgloss.NewStyle().Background(lipgloss.Color(code)).Render("  ")
	}

	var b strings.Builder
	name := ""
	for _, s := range f.swatches {
		if s.code == current {
			name = s.name
			// Brackets, not a highlight: on a row of colours, colour is not
			// available as a way of marking one of them.
			b.WriteString("[" + block(s.code) + "]")
			continue
		}
		b.WriteString(" " + block(s.code) + " ")
	}

	if name == "" {
		// Not one of ours — a typed code, or a hand-edited config. Show it anyway,
		// so what is in force is always on screen.
		b.WriteString("[" + block(current) + "]")
		name = current
	}

	label := dimStyle.Render(name)
	if selected {
		label = accentText.Render(name)
	}
	return truncate(b.String()+" "+label, w)
}

// settingsHint renders the key legend as alternating keycap/label pairs.
func settingsHint(pairs ...string) string {
	var parts []string
	for i := 0; i+1 < len(pairs); i += 2 {
		parts = append(parts, kc(pairs[i])+" "+dimStyle.Render(pairs[i+1]))
	}
	return strings.Join(parts, "  ")
}

// wrapExactly word-wraps s to w cells and returns exactly n lines, padding with
// blanks or dropping the overflow. Callers use it to reserve a fixed block of
// space, so the layout around it cannot shift.
func wrapExactly(s string, w, n int) []string {
	wrapped := lipgloss.NewStyle().Width(w).Render(s)
	lines := strings.Split(wrapped, "\n")
	for len(lines) < n {
		lines = append(lines, "")
	}
	return lines[:n]
}
