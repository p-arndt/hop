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
	{
		label: "Mouse",
		kind:  fieldToggle,
		desc:  "Wheel, click, and drag-to-copy in the panes. ctrl+g lends the pointer back to your terminal.",
		get:   func(c config.Config) string { return onOff(c.Mouse) },
		set:   func(c *config.Config, v string) { c.Mouse = v == on },
	},
	{
		label: "Remote clipboard",
		kind:  fieldToggle,
		desc:  "A yank on the remote host (OSC 52) lands on your clipboard. Off: the host cannot write it.",
		get:   func(c config.Config) string { return onOff(c.Clipboard) },
		set:   func(c *config.Config, v string) { c.Clipboard = v == on },
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
		return m, m.adjust(f, -1)

	case "right", "l":
		return m, m.adjust(f, 1)

	case "enter", "i":
		if f.kind == fieldToggle {
			// There is nothing to type on a switch, so the key that would open the
			// buffer flips it instead — the obvious thing to happen when you press
			// enter on something that has two states.
			return m, m.adjust(f, 1)
		}
		m.settings.editing = true
		m.settings.buf = f.get(m.cfg)

	case "r":
		// Reset this field to its default — the way back out of a bad value.
		return m, m.commit(f, f.get(config.Default()))
	}
	return m, nil
}

// adjust walks a field's value by delta: the next colour along the palette, or the
// other side of a switch. A field you can only type into has nothing to walk, so
// ←/→ leave it alone.
//
// It applies as it goes, with nothing to confirm — a colour is judged by seeing it
// and a binding by living with it for a keystroke, and the same key walks back.
func (m *model) adjust(f settingsField, delta int) tea.Cmd {
	switch f.kind {
	case fieldColor:
		return m.commit(f, nextSwatch(f.swatches, f.get(m.cfg), delta))
	case fieldToggle:
		return m.commit(f, onOff(f.get(m.cfg) != on))
	}
	return nil
}

// commit writes a value to the config, applies it to everything already running and
// saves it. Every way of changing a setting — typing one, walking one, resetting one
// — ends here, so none of them can be the one that forgets to persist.
func (m *model) commit(f settingsField, v string) tea.Cmd {
	f.set(&m.cfg, v)
	cmd := m.applySettings()
	m.saveSettings()
	return cmd
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
//
// The mouse is the one setting hop cannot apply by itself — reporting is switched on
// in the *user's* terminal, which only Bubble Tea can address — so it comes back as
// a command for the caller to return.
func (m *model) applySettings() tea.Cmd {
	setAccent(m.cfg.Accent)

	opts := m.browserOptions()
	for _, s := range m.sessions {
		if s.browser != nil {
			s.browser.SetOptions(opts)
		}
	}
	m.applyClipboard()
	return m.applyMouse()
}

// applyMouse brings the terminal's mouse reporting in line with the setting, and
// returns nothing when it already is: mouseOn is what hop last asked the terminal
// for, so a settings edit that did not touch this field sends no sequence.
func (m *model) applyMouse() tea.Cmd {
	if m.cfg.Mouse == m.mouseOn {
		return nil
	}
	// A pointer hop is no longer reading cannot finish the drag it was in the middle
	// of, and a highlight left over one is a lie about what is on the clipboard.
	m.clearSelection()
	m.mouseOn = m.cfg.Mouse
	if m.mouseOn {
		// Cell motion, not all motion: drag is reported (a remote program's visual
		// select needs it) while a pointer merely crossing the window is not, which
		// would be a stream of events nothing here reads.
		return tea.EnableMouseCellMotion
	}
	return tea.DisableMouse
}

// toggleMouse hands the pointer between hop and the terminal, live — the ctrl+g
// binding. It moves the same setting the card edits and does not save it: the
// gesture is "let go of the mouse for a moment", and what hop opens with next time
// is a decision made in the settings card, not by a passing keystroke. (A save made
// afterwards does keep it, because the card shows the state this left behind, and a
// card that saved something other than what it shows would be worse.)
func (m *model) toggleMouse() tea.Cmd {
	m.cfg.Mouse = !m.cfg.Mouse
	cmd := m.applyMouse()
	if m.cfg.Mouse {
		m.setStatus(statusOK, "mouse on — hop has the pointer")
	} else {
		m.setStatus(statusOK, "mouse off — your terminal has the pointer (%s to take it back)", toggleMouseKey)
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

// settingsChrome is what the card costs before any field is drawn: its border and
// the row of padding inside it top and bottom, the title and the blank under it,
// then the rule, a blank, and the hint line.
const settingsChrome = 2 + 2 + 2 + 1 + 1 + 1

// settingsMinFields is the fewest fields the card shows before it stops shrinking:
// the selected one, and one on either side of it to say there are others. It is
// what keeps the floor below fixed as fields are added — the card scrolls instead
// of growing past the window (see settingsWindow).
const settingsMinFields = 3

// settingsFullH is how tall the card stands with a blank line between its fields —
// the height a window has to have before it can afford that air. settingsPackedH is
// how tall it stands with every field but no air between them. settingsMinH is the
// smallest it gets, showing settingsMinFields of them: a window shorter than this
// has its bottom rows cut off by the overlay. See renderSettings.
func settingsFullH() int   { return settingsChrome + settingsDescH + 3*len(settingsFields) }
func settingsPackedH() int { return settingsChrome + settingsDescH + 2*len(settingsFields) }
func settingsMinH() int    { return settingsChrome + settingsDescH + 2*settingsMinFields }

// settingsWindow is the run of fields the card has room to draw, as a first index
// and a count, and it always contains the cursor.
//
// A window tall enough for all of them gets all of them, which is every ordinary
// window: the scrolling below is for the short ones. There the fields that fit are
// centred on the cursor, so moving through the list walks it past a window that
// keeps what is selected in the middle — a scrollbar's behaviour without a
// scrollbar, which there is no row to spare for.
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

	// The fields are spaced apart where there is room and packed where there is not.
	// What has to survive a short window is the shape of the card — a field, the
	// selected one's explanation, and the key hints at its foot — so the air between
	// rows is what gives way first, rather than the bottom of the card being cut off.
	//
	// After the air, it is the number of fields on screen that gives way: below
	// settingsPackedH the list scrolls inside the card (see settingsWindow) instead
	// of the card growing past the window. settingsMinH is where that stops, and it
	// is where the test pins it — below it the overlay drops the bottom lines, hints
	// included, and the honest answer is a taller window.
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
