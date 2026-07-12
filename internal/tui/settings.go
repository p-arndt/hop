package tui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"hop/internal/config"
	"hop/internal/filebrowser"
	"hop/internal/keymap"
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

// fieldKind is how a setting is edited, and so how its row is drawn. It is the one
// thing the popover switches on: adding a kind means teaching adjust how to walk it
// and renderSettingsValue how to draw it, and nothing else.
type fieldKind int

const (
	// fieldText is a value you type: enter opens a buffer on it.
	fieldText fieldKind = iota
	// fieldColor is a value you type *or* walk: ←/→ step through the palette, which
	// is how a colour is really chosen, and enter still opens the buffer for a
	// 256-code or a #hex that is not on it.
	fieldColor
	// fieldToggle is a switch: ←/→ or enter flips it, and there is nothing to type,
	// because neither of the two values is worth spelling out.
	fieldToggle
)

// settingsField is one editable row of the popover. Each field is described by
// how to read and write it, so the list below is the single place a new setting
// has to be added.
type settingsField struct {
	label string
	kind  fieldKind
	// placeholder stands in for an empty value: it says what hop does when the
	// field is left blank, which for most of them is "work it out".
	placeholder string
	// desc is the one-line explanation shown under the selected field. It is why
	// the popover can stay this sparse — the detail appears only where you are.
	desc string
	// swatches is the palette a fieldColor walks. Unused by the other kinds.
	swatches []swatch
	// get and set are string-valued for every kind, so that typing, resetting and
	// persisting are one code path rather than one per type. A kind whose config
	// field is not a string (a switch is a bool) converts here, at the boundary,
	// and nowhere else.
	get func(config.Config) string
	set func(*config.Config, string)
}

// on and off are a switch's two values, as the string get/set deal in. The config
// stores a real bool — this is only how it crosses the table.
const (
	on  = "on"
	off = "off"
)

// onOff renders a bool as the value a switch reads and writes.
func onOff(b bool) string {
	if b {
		return on
	}
	return off
}

var settingsFields = []settingsField{
	{
		label:       "Editor",
		kind:        fieldText,
		placeholder: "auto-detect",
		desc:        "Command run on the remote host by enter. Blank: $EDITOR, else nvim/vim/vi/nano.",
		get:         func(c config.Config) string { return c.Editor },
		set:         func(c *config.Config, v string) { c.Editor = v },
	},
	{
		label:       "Download dir",
		kind:        fieldText,
		placeholder: "~/Downloads",
		desc:        "Where the browser's d puts a file. Created on the next download if missing.",
		get:         func(c config.Config) string { return c.DownloadDir },
		set:         func(c *config.Config, v string) { c.DownloadDir = v },
	},
	{
		label:       "Accent color",
		kind:        fieldColor,
		placeholder: config.DefaultAccent,
		desc:        "←/→ to pick a color — it applies as you go. enter to type a 256-code or #hex.",
		swatches:    accentSwatches,
		get:         func(c config.Config) string { return c.Accent },
		set:         func(c *config.Config, v string) { c.Accent = v },
	},
	{
		label:       "Open with",
		kind:        fieldText,
		placeholder: "OS default app",
		desc:        "Local command the browser's o opens a file with, e.g. code -n.",
		get:         func(c config.Config) string { return c.OpenWith },
		set:         func(c *config.Config, v string) { c.OpenWith = v },
	},
	{
		label: "Vim keys",
		kind:  fieldToggle,
		desc:  "hjkl, gg/G, H/M/L and ctrl+d/u/f/b in the list and the browser. Off: arrows, enter, esc.",
		get:   func(c config.Config) string { return onOff(c.VimKeys) },
		set:   func(c *config.Config, v string) { c.VimKeys = v == on },
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
//
// The vim motions are gated here exactly as they are everywhere else: a card that
// still answered to hjkl while claiming the vim keys are off would be lying about
// the setting it is holding. Turning them off from this very card cannot strand you
// — ↑↓ and ←→ and enter drive every row and are never gated, which is what the hint
// line at the foot of the card has been naming all along.
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

	// Below text entry, not above it: while a field has the keyboard, "h" is a
	// letter of the value being typed, and dropping it there would make the vim
	// setting decide what you are allowed to name your editor.
	if !m.cfg.VimKeys && keymap.Vim(key) {
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
		m.adjust(f, -1)

	case "right", "l":
		m.adjust(f, 1)

	case "enter", "i":
		if f.kind == fieldToggle {
			// There is nothing to type on a switch, so the key that would open the
			// buffer flips it instead — the obvious thing to happen when you press
			// enter on something that has two states.
			m.adjust(f, 1)
			return m, nil
		}
		m.settings.editing = true
		m.settings.buf = f.get(m.cfg)

	case "r":
		// Reset this field to its default — the way back out of a bad value.
		m.commit(f, f.get(config.Default()))
	}
	return m, nil
}

// adjust walks a field's value by delta: the next colour along the palette, or the
// other side of a switch. A field you can only type into has nothing to walk, so
// ←/→ leave it alone.
//
// It applies as it goes, with nothing to confirm — a colour is judged by seeing it
// and a binding by living with it for a keystroke, and the same key walks back.
func (m *model) adjust(f settingsField, delta int) {
	switch f.kind {
	case fieldColor:
		m.commit(f, nextSwatch(f.swatches, f.get(m.cfg), delta))
	case fieldToggle:
		m.commit(f, onOff(f.get(m.cfg) != on))
	}
}

// commit writes a value to the config, applies it to everything already running and
// saves it. Every way of changing a setting — typing one, walking one, resetting one
// — ends here, so none of them can be the one that forgets to persist.
func (m *model) commit(f settingsField, v string) {
	f.set(&m.cfg, v)
	m.applySettings()
	m.saveSettings()
}

// nextSwatch is the colour delta steps along the palette from current, wrapping at
// both ends. A colour that is not on the palette (a typed code) starts the walk from
// the beginning, since there is nowhere on it to walk from.
func nextSwatch(palette []swatch, current string, delta int) string {
	n := len(palette)
	if n == 0 {
		return current
	}

	i := 0
	for j, s := range palette {
		if s.code == current {
			i = j
			break
		}
	}

	i = ((i+delta)%n + n) % n
	return palette[i].code
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
		VimKeys:     m.cfg.VimKeys,
	}
}

// saveSettings persists the config, reporting a failure rather than silently
// keeping a change that will not survive a restart.
func (m *model) saveSettings() {
	if err := m.cfg.Save(); err != nil {
		m.setStatus(statusErr, "settings: %v", err)
		return
	}
	m.setStatus(statusOK, "settings saved")
}

// Popover geometry.
const (
	settingsMaxW = 64 // content width, borders and padding excluded
	// settingsFloorW is the narrowest the card gets before it stops shrinking and
	// starts truncating: a window narrower than this has bigger problems.
	settingsFloorW = 20
	// settingsDescH is the height reserved for the selected field's explanation.
	// It is fixed, and the text wraps into it rather than being cut, so the card
	// keeps one shape: nothing below it jumps as the cursor moves.
	settingsDescH = 2
)

// settingsInnerW is the width available to a rendered row: the box minus its
// border and padding. Every line is held to it, because a modal that wraps spills
// outside its own frame — and the box itself is held to the window, because a
// card wider than the screen is worse than a cramped one.
func (m *model) settingsInnerW() int {
	room := max(m.width-2*cardPadX-2, settingsFloorW)
	return clamp(settingsMaxW, settingsFloorW, room)
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

	b.WriteString(rule(w))
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
	default:
		switch settingsFields[m.settings.cursor].kind {
		case fieldToggle:
			b.WriteString(settingsHint("↑↓", "move", "←→ enter", "toggle", "r", "reset", "esc", "close"))
		case fieldColor:
			b.WriteString(settingsHint("↑↓", "move", "←→", "color", "enter", "custom", "esc", "close"))
		default:
			b.WriteString(settingsHint("↑↓", "move", "enter", "edit", "r", "reset", "esc", "close"))
		}
	}

	return cardBox.Width(w + 2*cardPadX).Render(b.String())
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
	switch f.kind {
	case fieldToggle:
		return indent + m.renderToggle(f, selected, vw)
	case fieldColor:
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

// renderToggle draws an on/off field as both of its states with the live one
// bracketed — the swatch strip's "brackets, not a highlight" mark, so the two kinds
// of picker read the same way. Showing the state you are not in is what says the row
// is a switch at all, and which way ←/→ will throw it.
func (m *model) renderToggle(f settingsField, selected bool, w int) string {
	live := settingsValue
	if selected {
		live = accentText
	}

	state := func(label string, current bool) string {
		if !current {
			return dimStyle.Render(" " + label + " ")
		}
		return live.Render("[" + label + "]")
	}

	isOn := f.get(m.cfg) == on
	return truncate(state(on, isOn)+"  "+state(off, !isOn), w)
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
		parts = append(parts, keyHint(pairs[i], pairs[i+1]))
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
