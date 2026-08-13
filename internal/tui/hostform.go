package tui

import (
	"fmt"
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"hop/internal/store"
)

// hostFormField is one editable row of the add/edit card. Unlike a settings field it has
// no get/set into a live config: the form edits its own buffer and only touches the
// store on submit.
type hostFormField struct {
	label string
	// placeholder says what the field means when nothing is typed: "required" for the
	// alias, and the default hop falls back to for the rest.
	placeholder string
}

// hostFormFields are the rows of the card, in tab order — the single place a field's
// position is decided. An array rather than a slice so its length is a constant the
// buffer below can be sized from.
var hostFormFields = [...]hostFormField{
	{"Alias", "required"},
	{"User", "none"},
	{"Hostname", "none"},
	{"Port", "22"},
	{"Identity file", "none"},
	{"Tags", "none"},
	{"Group", "none"},
	{"Default dir", "home"},
}

// Field indices into hostFormFields and hostFormUI.buf.
const (
	hfAlias = iota
	hfUser
	hfHostname
	hfPort
	hfIdentity
	hfTags
	hfGroup
	hfDefaultDir
)

// hostFormUI is the add/edit card's own state: one buffer per field, and in edit mode
// the identity of the host it stands in for — orig is the alias to rename from, and
// visits/lastConnect are carried through so saving does not reset the host's history.
type hostFormUI struct {
	open   bool
	edit   bool
	orig   string
	cursor int
	buf    [len(hostFormFields)]string

	// visits and lastConnect are the edited host's history, held so an Upsert hands them
	// back rather than zeroing them.
	visits      int
	lastConnect int64
}

// openHostFormAdd shows a blank card in add mode.
func (m *model) openHostFormAdd() {
	m.hostForm = hostFormUI{open: true}
	m.status = ""
}

// openHostFormEdit shows a card pre-filled from h, remembering h.Alias as the alias to
// rename from and carrying its history through so a save preserves it.
func (m *model) openHostFormEdit(h store.Host) {
	f := hostFormUI{
		open:        true,
		edit:        true,
		orig:        h.Alias,
		visits:      h.Visits,
		lastConnect: h.LastConnect,
	}
	f.buf[hfAlias] = h.Alias
	f.buf[hfUser] = h.User
	f.buf[hfHostname] = h.HostName
	if h.Port != 0 {
		f.buf[hfPort] = strconv.Itoa(h.Port)
	}
	f.buf[hfIdentity] = h.IdentityFile
	f.buf[hfTags] = strings.Join(h.Tags, ", ")
	f.buf[hfGroup] = h.Group
	f.buf[hfDefaultDir] = h.DefaultDir

	m.hostForm = f
	m.status = ""
}

// closeHostForm hides the card, abandoning whatever was typed.
func (m *model) closeHostForm() {
	m.hostForm = hostFormUI{}
}

// handleHostFormKey routes a key while the card is up. Unlike the settings popover there
// is no separate editing mode: the focused field always has the keyboard. It swallows
// everything, since a modal that let keys through would be a trap.
func (m *model) handleHostFormKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.closeHostForm()

	case "enter":
		m.submitHostForm()

	case "up", "shift+tab":
		m.moveHostForm(-1)

	case "down", "tab":
		m.moveHostForm(1)

	case "backspace":
		if b := m.hostForm.buf[m.hostForm.cursor]; b != "" {
			r := []rune(b)
			m.hostForm.buf[m.hostForm.cursor] = string(r[:len(r)-1])
		}

	case "ctrl+u":
		m.hostForm.buf[m.hostForm.cursor] = ""

	default:
		// Anything carrying text goes to the focused field — which is why the vim motions
		// are never gated here: "h" is a letter of the value.
		if len(msg.Runes) > 0 {
			m.hostForm.buf[m.hostForm.cursor] += string(msg.Runes)
		}
	}
	return m, nil
}

// moveHostForm walks the focus by delta, wrapping around the field list.
func (m *model) moveHostForm(delta int) {
	n := len(hostFormFields)
	m.hostForm.cursor = ((m.hostForm.cursor+delta)%n + n) % n
}

// submitHostForm validates the buffers and writes the host, leaving the card open on any
// error so a bad value can be fixed rather than lost. On success it reloads the list,
// parks the cursor on the saved host and closes the card. Nothing touches the store until
// every check has passed.
func (m *model) submitHostForm() {
	f := &m.hostForm

	alias := strings.TrimSpace(f.buf[hfAlias])
	if alias == "" {
		m.setStatus(statusErr, "alias can't be empty")
		return
	}

	port := 22
	if p := strings.TrimSpace(f.buf[hfPort]); p != "" {
		n, err := strconv.Atoi(p)
		if err != nil {
			m.setStatus(statusErr, "port must be a number")
			return
		}
		port = n
	}

	host := store.Host{
		Alias:        alias,
		User:         strings.TrimSpace(f.buf[hfUser]),
		HostName:     strings.TrimSpace(f.buf[hfHostname]),
		Port:         port,
		IdentityFile: strings.TrimSpace(f.buf[hfIdentity]),
		Tags:         splitFormTags(f.buf[hfTags]),
		Group:        strings.TrimSpace(f.buf[hfGroup]),
		DefaultDir:   strings.TrimSpace(f.buf[hfDefaultDir]),
		Visits:       f.visits,
		LastConnect:  f.lastConnect,
	}

	verb := "saved"
	if f.edit {
		verb = "updated"

		// A rename only has to clear the way when the alias actually changed. Rename
		// itself rejects a taken alias, so it is the guard rather than the in-memory list.
		if alias != f.orig {
			if err := m.st.Rename(f.orig, alias); err != nil {
				m.setStatus(statusErr, "%v", err)
				return
			}
		}
		// Upsert updates the row in place; its ON CONFLICT clause leaves visits and
		// last_connect untouched, so a plain edit keeps history.
		if _, err := m.st.Upsert(host); err != nil {
			m.setStatus(statusErr, "save host: %v", err)
			return
		}
	} else {
		// A new host starts with a clean history and goes in through Add — an INSERT that
		// fails on a taken alias — so it cannot overwrite a host the list did not know of.
		host.Visits = 0
		host.LastConnect = 0
		if _, err := m.st.Add(host); err != nil {
			m.setStatus(statusErr, "%v", err)
			return
		}
	}

	// Land the cursor on the host just saved: a new or renamed alias reloadHosts alone
	// would not find.
	m.reloadHostsSelecting(alias)
	m.closeHostForm()
	m.setStatus(statusOK, "%s %s", verb, alias)
}

// splitFormTags turns the tags field's text into the slice the store keeps: comma
// separated, trimmed, empties dropped. It mirrors store.splitTags, so tags typed here
// shape up like tags read back from an SSH config import.
func splitFormTags(s string) []string {
	parts := strings.Split(s, ",")
	out := parts[:0]
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// Card geometry.
const (
	hostFormMaxW = 48 // content width, borders and padding excluded
	// hostFormFloorW is the narrowest the card gets before it stops shrinking.
	hostFormFloorW = 20
)

// hostFormChrome is what the card costs before any field is drawn: border, padding,
// title and its blank, then the rule and the hint line.
const hostFormChrome = 2 + 2 + 2 + 1 + 1

// hostFormMinFields is the fewest fields the card shows: the selected one and one on
// either side. It keeps the floor below fixed as fields are added (see hostFormWindow).
const hostFormMinFields = 3

// hostFormFullH is how tall the card stands with a blank line between its fields,
// hostFormPackedH with every field and no air, hostFormMinH with hostFormMinFields of
// them — below which the overlay cuts the bottom rows off. See renderHostForm.
func hostFormFullH() int   { return hostFormChrome + 3*len(hostFormFields) }
func hostFormPackedH() int { return hostFormChrome + 2*len(hostFormFields) }
func hostFormMinH() int    { return hostFormChrome + 2*hostFormMinFields }

// hostFormWindow is the run of fields the card has room to draw, as a first index and a
// count, always containing the cursor. It is the settings popover's rule: all of them
// where there is room, otherwise what fits, centred on the cursor.
func (m *model) hostFormWindow() (first, count int) {
	n := len(hostFormFields)
	if m.height >= hostFormPackedH() {
		return 0, n
	}
	room := (m.height - hostFormChrome) / 2
	count = clamp(room, hostFormMinFields, n)
	first = clamp(m.hostForm.cursor-count/2, 0, n-count)
	return first, count
}

// hostFormInnerW is the width available to a rendered row: the box less its border and
// padding, held to the window so the card never spills past the screen.
func (m *model) hostFormInnerW() int {
	room := max(m.width-2*cardPadX-2, hostFormFloorW)
	return clamp(hostFormMaxW, hostFormFloorW, room)
}

// renderHostForm draws the add/edit card: a stack of fields, each a quiet label over its
// value, with the focused one lit and holding a caret — the settings popover's shape.
func (m *model) renderHostForm() string {
	w := m.hostFormInnerW()
	var b strings.Builder

	title := "NEW HOST"
	if m.hostForm.edit {
		title = "EDIT HOST"
	}
	b.WriteString(titleStyle.Render(title))
	b.WriteString("\n\n")

	// Air gives way first, then the number of fields on screen — the settings popover's
	// order of give, so a short terminal never hides the field you tabbed to.
	gap := "\n\n"
	if m.height < hostFormFullH() {
		gap = "\n"
	}

	first, count := m.hostFormWindow()
	for i := first; i < first+count; i++ {
		f := hostFormFields[i]
		selected := i == m.hostForm.cursor

		bar, label := "  ", settingsLabel.Render(f.label)
		if selected {
			bar, label = selBar+" ", settingsLabelSel.Render(f.label)
		}
		b.WriteString(truncate(bar+label, w))
		b.WriteString("\n")
		b.WriteString(m.renderHostFormValue(i, f, selected, w))
		b.WriteString(gap)
	}

	b.WriteString(rule(w))
	b.WriteString("\n")
	hints := keyHint("tab", "next") + "  " + keyHint("enter", "save") + "  " + keyHint("esc", "cancel")
	if count < len(hostFormFields) {
		// Only when the card is scrolling: a count showing all the fields would be noise.
		hints += "  " + faint.Render(fmt.Sprintf("%d/%d", m.hostForm.cursor+1, len(hostFormFields)))
	}
	b.WriteString(truncate(hints, w))

	return cardBox.Width(w + 2*cardPadX).Render(b.String())
}

// renderHostFormValue draws one field's value as a full-width row: the focused field is
// filled like a text input with a caret; the rest show their value, or a dim placeholder
// naming the default when blank.
func (m *model) renderHostFormValue(i int, f hostFormField, selected bool, w int) string {
	const indent = "    "
	vw := w - lipgloss.Width(indent)

	if selected {
		text := truncate(m.hostForm.buf[i], vw-3) + accentText.Render("▏")
		return indent + inputStyle.Width(vw).Render(text)
	}

	value, style := m.hostForm.buf[i], settingsValue
	if value == "" {
		value, style = f.placeholder, settingsPlaceholder
	}
	return indent + style.Render(truncate(value, vw-2))
}
