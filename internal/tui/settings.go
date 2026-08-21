package tui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"hop/internal/config"
	"hop/internal/filebrowser"
	"hop/internal/keys"
)

type swatch struct {
	name string
	code string
}

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

// fieldKind is how a setting is edited, and so how its row is drawn.
type fieldKind int

const (
	fieldText fieldKind = iota
	fieldColor
	fieldToggle
	fieldChoice
)

// settingsField is one editable row of the popover.
type settingsField struct {
	label       string
	kind        fieldKind
	placeholder string
	desc        string
	swatches    []swatch
	choices     []choice
	// get and set are string-valued for every kind, so typing, resetting and persisting
	// are one code path.
	get func(config.Config) string
	set func(*config.Config, string)
}

const (
	on  = "on"
	off = "off"
)

func onOff(b bool) string {
	if b {
		return on
	}
	return off
}

// choice is one value of a fieldChoice: the word stored, and what picking it does.
type choice struct {
	value string
	desc  string
}

// guidanceChoices are the three profiles, quietest first, so walking right walks toward more help.
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

// settingsUI is the popover's own state; the values it edits live in model.cfg.
type settingsUI struct {
	open    bool
	cursor  int
	editing bool
	buf     string
}

func (m *model) openSettings() {
	m.settings = settingsUI{open: true}
	m.status = ""
}

func (m *model) closeSettings() {
	m.settings = settingsUI{}
}

// handleSettingsKey routes a key while the popover is up, swallowing everything.
func (m *model) handleSettingsKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	f := settingsFields[m.settings.cursor]
	key := msg.String()

	if m.settings.editing {
		switch key {
		case "enter":
			f.set(&m.cfg, strings.TrimSpace(m.settings.buf))
			m.settings.editing = false
			cmd := m.applySettings()
			m.saveSettings()
			return m, cmd
		case "esc":
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

	// Below text entry, not above it: while a field has the keyboard, "h" is a letter.
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
		return m, m.commit(f, f.get(config.Default()))
	}
	return m, nil
}

// adjust walks a field's value by delta, applying as it goes.
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

// commit is the single path every setting change takes, so none can forget to persist.
func (m *model) commit(f settingsField, v string) tea.Cmd {
	f.set(&m.cfg, v)
	cmd := m.applySettings()
	m.saveSettings()
	return cmd
}

// nextChoice is delta steps along the list from current, wrapping; an unknown value starts at 0.
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

// nextSwatch is delta steps along the palette from current, wrapping; an unknown colour starts at 0.
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

func (m *model) clampSettings() {
	n := len(settingsFields)
	m.settings.cursor = ((m.settings.cursor % n) + n) % n
}

// applySettings pushes the config into everything running; the mouse comes back as a command
// because only Bubble Tea can address the user's terminal.
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

// applyMouse syncs terminal mouse reporting; mouseOn is what hop last asked for, so an edit
// elsewhere sends no sequence.
func (m *model) applyMouse() tea.Cmd {
	if m.cfg.Mouse == m.mouseOn {
		return nil
	}
	// A pointer hop no longer reads cannot finish its drag, leaving a lying highlight.
	m.clearSelection()
	m.mouseOn = m.cfg.Mouse
	if m.mouseOn {
		// Cell motion, not all motion: drag is reported, a pointer merely crossing is not.
		return tea.EnableMouseCellMotion
	}
	return tea.DisableMouse
}

// toggleMouse (ctrl+g) moves the same setting the card edits but deliberately does not save it.
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

func (m *model) browserOptions() filebrowser.Options {
	return filebrowser.Options{
		DownloadDir: m.cfg.DownloadDir,
		OpenWith:    m.cfg.OpenWith,
		VimKeys:     m.cfg.VimKeys,
	}
}

func (m *model) saveSettings() {
	if err := m.cfg.Save(); err != nil {
		m.setStatus(statusErr, "settings: %v", err)
		return
	}
	m.setStatus(statusOK, "settings saved")
}

// Popover geometry.
const (
	settingsMaxW   = 64 // content width, borders and padding excluded
	settingsFloorW = 20 // narrowest before the card truncates
	settingsDescH  = 2  // fixed, so nothing below it jumps as the cursor moves
)

// settingsChrome is what the card costs before any field is drawn: border, padding, title
// and its blank, then the rule, a blank and the hint line.
const settingsChrome = 2 + 2 + 2 + 1 + 1 + 1

// settingsMinFields is the fewest fields the card shows; past that it scrolls (settingsWindow).
const settingsMinFields = 3

// Card heights: aired, packed, and at settingsMinFields — below that the overlay clips.
func settingsFullH() int   { return settingsChrome + settingsDescH + 3*len(settingsFields) }
func settingsPackedH() int { return settingsChrome + settingsDescH + 2*len(settingsFields) }
func settingsMinH() int    { return settingsChrome + settingsDescH + 2*settingsMinFields }

// settingsWindow is the run of fields the card has room to draw, always containing the cursor.
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

// settingsInnerW is the width every row is held to, since a modal that wraps spills its frame.
func (m *model) settingsInnerW() int {
	room := max(m.width-2*cardPadX-2, settingsFloorW)
	return clamp(settingsMaxW, settingsFloorW, room)
}

// renderSettings draws the popover: stacked fields, the selected one lit and explained.
func (m *model) renderSettings() string {
	w := m.settingsInnerW()
	var b strings.Builder

	b.WriteString(titleStyle.Render("SETTINGS"))
	b.WriteString("\n\n")

	// Air gives way first in a short window, then the field count (see settingsWindow).
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

// renderToggle draws both states with the live one bracketed, which is what says the row is a switch.
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

// settingsDesc is the line under the card; a choice field describes the value it stands on.
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

// renderChoices draws every value with the live one bracketed, which says the row is a dial.
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

// renderSwatches draws the palette as blocks; a value off it gets a block of its own, so what
// is in force is always on screen.
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

func settingsHint(pairs ...string) string {
	var parts []string
	for i := 0; i+1 < len(pairs); i += 2 {
		parts = append(parts, keyHint(pairs[i], pairs[i+1]))
	}
	return strings.Join(parts, "  ")
}

// wrapExactly word-wraps s to w cells and returns exactly n lines, so the layout cannot shift.
func wrapExactly(s string, w, n int) []string {
	wrapped := lipgloss.NewStyle().Width(w).Render(s)
	lines := strings.Split(wrapped, "\n")
	for len(lines) < n {
		lines = append(lines, "")
	}
	return lines[:n]
}
