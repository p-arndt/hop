package tui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"hop/internal/config"
	"hop/internal/filebrowser"
	"hop/internal/keys"
)

// swatch is one colour on the accent picker: a name, and the code hop stores.
type swatch struct {
	name string
	code string
}

// accentSwatches is the palette the accent field cycles through. A 256-colour code
// tells you nothing on its own, so the field shows the colours themselves; typing a
// code stays possible.
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

// fieldKind is how a setting is edited, and so how its row is drawn. Adding a kind
// means teaching adjust how to walk it and renderSettingsValue how to draw it.
type fieldKind int

const (
	// fieldText is a value you type: enter opens a buffer on it.
	fieldText fieldKind = iota
	// fieldColor is a value you type or walk: ←/→ step through the palette, and enter
	// still opens the buffer for a 256-code or #hex that is not on it.
	fieldColor
	// fieldToggle is a switch: ←/→ or enter flips it, and there is nothing to type.
	fieldToggle
	// fieldChoice is one of a fixed few words: ←/→ walk them, enter takes the next, and
	// there is nothing to type — an unknown value would only be normalised away.
	fieldChoice
)

// settingsField is one editable row of the popover, described by how to read and write
// it — so the list below is the single place a new setting is added.
type settingsField struct {
	label string
	kind  fieldKind
	// placeholder says what hop does when the field is left blank.
	placeholder string
	// desc is the one-line explanation shown under the selected field.
	desc string
	// swatches is the palette a fieldColor walks. Unused by the other kinds.
	swatches []swatch
	// choices are the values a fieldChoice walks, in order, with what each one means.
	// Unused by the other kinds.
	choices []choice
	// get and set are string-valued for every kind, so typing, resetting and persisting
	// are one code path. A non-string config field converts here and nowhere else.
	get func(config.Config) string
	set func(*config.Config, string)
}

// on and off are a switch's two values in the strings get/set deal in. The config
// stores a real bool.
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

// choice is one value of a fieldChoice: the word stored, and the half-line saying what
// picking it does — which is the whole of what a profile is.
type choice struct {
	value string
	desc  string
}

// guidanceChoices are the three profiles, quietest first, so walking right is walking
// toward more help.
var guidanceChoices = []choice{
	{config.GuidanceKeys, "the short legend, nothing else"},
	{config.GuidanceHybrid, "the legend plus what a wide window fits, and the host's actions"},
	{config.GuidanceGuided, "all of it: every action a host has, spelled out with its key"},
}

var settingsFields = []settingsField{
	{
		label:   "Guidance",
		kind:    fieldChoice,
		desc:    "How much of the keyboard hop puts on screen. Every key works in all three.",
		choices: guidanceChoices,
		get:     func(c config.Config) string { return c.Guidance },
		set:     func(c *config.Config, v string) { c.Guidance = v },
	},
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
	{
		label: "Mouse",
		kind:  fieldToggle,
		desc:  "Wheel, click, and drag-to-copy in the panes. ctrl+g lends the pointer back to your terminal.",
		get:   func(c config.Config) string { return onOff(c.Mouse) },
		set:   func(c *config.Config, v string) { c.Mouse = v == on },
	},
	{
		label: "Cursor blink",
		kind:  fieldToggle,
		desc:  "Blink the cursor in a pane. Off: it stands still. Its shape and hiding are always the remote's.",
		get:   func(c config.Config) string { return onOff(c.CursorBlink) },
		set:   func(c *config.Config, v string) { c.CursorBlink = v == on },
	},
	{
		label: "Remote clipboard",
		kind:  fieldToggle,
		desc:  "A yank on the remote host (OSC 52) lands on your clipboard. Off: the host cannot write it.",
		get:   func(c config.Config) string { return onOff(c.Clipboard) },
		set:   func(c *config.Config, v string) { c.Clipboard = v == on },
	},
}

// settingsUI is the popover's own state: the cursor and the in-progress text. The
// values it edits live in model.cfg.
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

// handleSettingsKey routes a key while the popover is up, swallowing everything: a
// modal that let keys through would be a trap.
//
// The vim motions are gated here as everywhere else. Turning them off from this card
// cannot strand you: ↑↓, ←→ and enter drive every row and are never gated.
func (m *model) handleSettingsKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	f := settingsFields[m.settings.cursor]
	key := msg.String()

	// Text entry: the field has the keyboard.
	if m.settings.editing {
		switch key {
		case "enter":
			f.set(&m.cfg, strings.TrimSpace(m.settings.buf))
			m.settings.editing = false
			cmd := m.applySettings()
			m.saveSettings()
			return m, cmd
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

	// Below text entry, not above it: while a field has the keyboard, "h" is a letter of
	// the value being typed.
	if !m.cfg.VimKeys && m.binds.Vim(key) {
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
		return m, m.adjust(f, -1)

	case "right", "l":
		return m, m.adjust(f, 1)

	case "enter", "i":
		if f.kind == fieldToggle || f.kind == fieldChoice {
			// Nothing to type on a switch, so the key that would open the buffer flips it.
			return m, m.adjust(f, 1)
		}
		m.settings.editing = true
		m.settings.buf = f.get(m.cfg)

	case "r":
		// Reset this field to its default.
		return m, m.commit(f, f.get(config.Default()))
	}
	return m, nil
}

// adjust walks a field's value by delta: the next colour along the palette, or the other
// side of a switch. A type-only field has nothing to walk. It applies as it goes, with
// nothing to confirm — the same key walks back.
func (m *model) adjust(f settingsField, delta int) tea.Cmd {
	switch f.kind {
	case fieldColor:
		return m.commit(f, nextSwatch(f.swatches, f.get(m.cfg), delta))
	case fieldToggle:
		return m.commit(f, onOff(f.get(m.cfg) != on))
	case fieldChoice:
		return m.commit(f, nextChoice(f.choices, f.get(m.cfg), delta))
	}
	return nil
}

// commit writes a value to the config, applies it to everything running and saves it.
// Every way of changing a setting ends here, so none can forget to persist.
func (m *model) commit(f settingsField, v string) tea.Cmd {
	f.set(&m.cfg, v)
	cmd := m.applySettings()
	m.saveSettings()
	return cmd
}

// nextChoice is the value delta steps along a choice list from current, wrapping at both
// ends. A value not on the list starts the walk from the beginning.
func nextChoice(choices []choice, current string, delta int) string {
	n := len(choices)
	if n == 0 {
		return current
	}

	i := 0
	for j, c := range choices {
		if c.value == current {
			i = j
			break
		}
	}

	i = ((i+delta)%n + n) % n
	return choices[i].value
}

// nextSwatch is the colour delta steps along the palette from current, wrapping at both
// ends. A colour not on the palette starts the walk from the beginning.
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

// applySettings pushes the current config into everything already running: the palette
// is restyled and every live browser picks up the new directories.
//
// The mouse is the one setting hop cannot apply itself — reporting is switched on in
// the user's terminal, which only Bubble Tea can address — so it comes back as a
// command for the caller to return.
func (m *model) applySettings() tea.Cmd {
	setAccent(m.cfg.Accent)

	opts := m.browserOptions()
	for _, s := range m.sessions {
		if s.browser != nil {
			s.browser.SetOptions(opts)
		}
	}
	m.applyClipboard()
	return tea.Batch(m.applyMouse(), m.applyCursorBlink())
}

// applyMouse brings the terminal's mouse reporting in line with the setting. mouseOn is
// what hop last asked for, so an edit that did not touch this field sends no sequence.
func (m *model) applyMouse() tea.Cmd {
	if m.cfg.Mouse == m.mouseOn {
		return nil
	}
	// A pointer hop no longer reads cannot finish its drag, and the leftover highlight
	// would misstate what is on the clipboard.
	m.clearSelection()
	m.mouseOn = m.cfg.Mouse
	if m.mouseOn {
		// Cell motion, not all motion: drag is reported, since a remote program's visual
		// select needs it, while a pointer merely crossing the window is not.
		return tea.EnableMouseCellMotion
	}
	return tea.DisableMouse
}

// toggleMouse hands the pointer between hop and the terminal — the ctrl+g binding. It
// moves the same setting the card edits but does not save it: the gesture is "let go of
// the mouse for a moment". A later save does keep it, since the card shows this state.
func (m *model) toggleMouse() tea.Cmd {
	m.cfg.Mouse = !m.cfg.Mouse
	cmd := m.applyMouse()
	if m.cfg.Mouse {
		m.setStatus(statusOK, "mouse on — hop has the pointer")
	} else {
		m.setStatus(statusOK, "mouse off — your terminal has the pointer (%s to take it back)", m.binds.Keycap(keys.Mouse))
	}
	return cmd
}

// browserOptions is the slice of the config the file browser cares about.
func (m *model) browserOptions() filebrowser.Options {
	return filebrowser.Options{
		DownloadDir: m.cfg.DownloadDir,
		OpenWith:    m.cfg.OpenWith,
		VimKeys:     m.cfg.VimKeys,
	}
}

// saveSettings persists the config, reporting a failure rather than silently keeping a
// change that will not survive a restart.
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
	// settingsFloorW is the narrowest the card gets before it starts truncating.
	settingsFloorW = 20
	// settingsDescH is the fixed height reserved for the selected field's explanation,
	// so nothing below it jumps as the cursor moves.
	settingsDescH = 2
)

// settingsChrome is what the card costs before any field is drawn: border, padding,
// title and its blank, then the rule, a blank and the hint line.
const settingsChrome = 2 + 2 + 2 + 1 + 1 + 1

// settingsMinFields is the fewest fields the card shows: the selected one and one on
// either side. It keeps the floor below fixed as fields are added — the card scrolls
// rather than growing past the window (see settingsWindow).
const settingsMinFields = 3

// settingsFullH is how tall the card stands with a blank line between its fields,
// settingsPackedH with every field and no air, settingsMinH with settingsMinFields of
// them — below which the overlay cuts the bottom rows off. See renderSettings.
func settingsFullH() int   { return settingsChrome + settingsDescH + 3*len(settingsFields) }
func settingsPackedH() int { return settingsChrome + settingsDescH + 2*len(settingsFields) }
func settingsMinH() int    { return settingsChrome + settingsDescH + 2*settingsMinFields }

// settingsWindow is the run of fields the card has room to draw, as a first index and a
// count, always containing the cursor. A window tall enough gets all of them; a short
// one centres what fits on the cursor.
func (m *model) settingsWindow() (first, count int) {
	n := len(settingsFields)
	if m.height >= settingsPackedH() {
		return 0, n
	}
	room := (m.height - settingsChrome - settingsDescH) / 2
	count = clamp(room, settingsMinFields, n)
	first = clamp(m.settings.cursor-count/2, 0, n-count)
	return first, count
}

// settingsInnerW is the width available to a rendered row: the box less its border and
// padding. Every line is held to it, since a modal that wraps spills out of its frame,
// and the box itself is held to the window.
func (m *model) settingsInnerW() int {
	room := max(m.width-2*cardPadX-2, settingsFloorW)
	return clamp(settingsMaxW, settingsFloorW, room)
}

// renderSettings draws the popover: stacked fields, each a quiet label over its value,
// with the selected one lit and explained at the foot. The value sits under its label
// rather than beside it, which gives a long path the full width of the card.
func (m *model) renderSettings() string {
	w := m.settingsInnerW()
	var b strings.Builder

	b.WriteString(titleStyle.Render("SETTINGS"))
	b.WriteString("\n\n")

	// Air between the rows is what gives way first in a short window, so the shape of
	// the card survives. After that it is the number of fields on screen: below
	// settingsPackedH the list scrolls inside the card (see settingsWindow), down to
	// settingsMinH, below which the overlay drops the bottom lines.
	gap := "\n\n"
	if m.height < settingsFullH() {
		gap = "\n"
	}

	first, count := m.settingsWindow()
	for i := first; i < first+count; i++ {
		f := settingsFields[i]
		selected := i == m.settings.cursor

		bar, label := "  ", settingsLabel.Render(f.label)
		if selected {
			bar, label = selBar+" ", settingsLabelSel.Render(f.label)
		}
		b.WriteString(truncate(bar+label, w))
		b.WriteString("\n")
		b.WriteString(m.renderSettingsValue(f, selected, w))
		b.WriteString(gap)
	}

	b.WriteString(rule(w))
	b.WriteString("\n")

	// The selected field explains itself, in a fixed-height block.
	for _, line := range wrapExactly(m.settingsDesc(settingsFields[m.settings.cursor]), w, settingsDescH) {
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
		case fieldChoice:
			b.WriteString(settingsHint("↑↓", "move", "←→ enter", "profile", "r", "reset", "esc", "close"))
		default:
			b.WriteString(settingsHint("↑↓", "move", "enter", "edit", "r", "reset", "esc", "close"))
		}
	}

	return cardBox.Width(w + 2*cardPadX).Render(b.String())
}

// renderSettingsValue draws a field's value as a full-width row: the live text with a
// caret while it is being edited, the stored value, or a dim placeholder. The selected
// row is filled rather than coloured, so it reads as a field you are standing in.
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
	case fieldChoice:
		return indent + m.renderChoices(f, selected, vw)
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

// renderToggle draws an on/off field as both states with the live one bracketed, as the
// swatch strip marks its selection. Showing the state you are not in is what says the
// row is a switch.
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

// settingsDesc is the line under the card. A choice field says what the value it is
// standing on does rather than what the field is: the field's name already said that,
// and the difference between the three is the only thing worth the two lines.
func (m *model) settingsDesc(f settingsField) string {
	if f.kind != fieldChoice {
		return f.desc
	}
	current := f.get(m.cfg)
	for _, c := range f.choices {
		if c.value == current {
			return f.desc + " " + c.value + ": " + c.desc + "."
		}
	}
	return f.desc
}

// renderChoices draws a choice field as all of its values with the live one bracketed —
// the toggle's trick with more than two states. Showing the ones you are not on is what
// says the row is a dial rather than a word.
func (m *model) renderChoices(f settingsField, selected bool, w int) string {
	live := settingsValue
	if selected {
		live = accentText
	}

	current := f.get(m.cfg)
	parts := make([]string, 0, len(f.choices))
	for _, c := range f.choices {
		if c.value == current {
			parts = append(parts, live.Render("["+c.value+"]"))
			continue
		}
		parts = append(parts, dimStyle.Render(" "+c.value+" "))
	}
	return truncate(strings.Join(parts, " "), w)
}

// renderSwatches draws a colour field: the palette as a row of blocks, the chosen one
// bracketed, its name after them. A value not in the palette gets a block of its own at
// the end, so what is in force is always on screen.
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
			// Brackets, not a highlight: on a row of colours, colour cannot mark one.
			b.WriteString("[")
			b.WriteString(block(s.code))
			b.WriteString("]")
			continue
		}
		b.WriteString(" ")
		b.WriteString(block(s.code))
		b.WriteString(" ")
	}

	if name == "" {
		// A typed code or a hand-edited config; show it anyway.
		b.WriteString("[")
		b.WriteString(block(current))
		b.WriteString("]")
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

// wrapExactly word-wraps s to w cells and returns exactly n lines, padding or dropping
// the overflow, so the layout around it cannot shift.
func wrapExactly(s string, w, n int) []string {
	wrapped := lipgloss.NewStyle().Width(w).Render(s)
	lines := strings.Split(wrapped, "\n")
	for len(lines) < n {
		lines = append(lines, "")
	}
	return lines[:n]
}
