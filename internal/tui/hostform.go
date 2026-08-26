package tui

import (
	"fmt"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/lipgloss"

	"hop/internal/store"
)

// hostFormField is one editable row of the add/edit card.
type hostFormField struct {
	label       string
	placeholder string
}

// hostFormFields are the rows of the card, in tab order. An array, so its length is a
// constant the buffer can be sized from.
var hostFormFields = [...]hostFormField{
	{"Alias", "required"},
	{"User", "none"},
	{"Hostname", "none"},
	{"Port", "22"},
	{"Identity file", "none"},
	{"Tags", "none"},
	{"Group", "none"},
	{"Default dir", "home"},
	{"Proxy jump", "none"},
	{"Proxy command", "none"},
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
	hfProxyJump
	hfProxyCommand
)

// hostFormUI is the add/edit card's own state; orig is the alias to rename from.
type hostFormUI struct {
	open   bool
	edit   bool
	orig   string
	cursor int
	buf    [len(hostFormFields)]string

	// Held so an Upsert hands the host's history back rather than zeroing it.
	visits      int
	lastConnect int64
}

// openHostFormAdd shows a blank card in add mode.
func (m *model) openHostFormAdd() {
	m.hostForm = hostFormUI{open: true}
	m.status = ""
}

// openHostFormEdit shows a card pre-filled from h, carrying its history through.
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
	f.buf[hfProxyJump] = h.ProxyJump
	f.buf[hfProxyCommand] = h.ProxyCommand

	m.hostForm = f
	m.status = ""
}

// closeHostForm hides the card, abandoning whatever was typed.
func (m *model) closeHostForm() {
	m.hostForm = hostFormUI{}
}

// handleHostFormKey routes a key while the card is up, swallowing everything.
func (m *model) handleHostFormKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
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
		// All text goes to the focused field, so vim motions are never gated here.
		if msg.Text != "" {
			m.hostForm.buf[m.hostForm.cursor] += msg.Text
		}
	}
	return m, nil
}

// moveHostForm walks the focus by delta, wrapping around the field list.
func (m *model) moveHostForm(delta int) {
	n := len(hostFormFields)
	m.hostForm.cursor = ((m.hostForm.cursor+delta)%n + n) % n
}

// submitHostForm validates the buffers and writes the host, leaving the card open on error.
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
		ProxyJump:    strings.TrimSpace(f.buf[hfProxyJump]),
		ProxyCommand: strings.TrimSpace(f.buf[hfProxyCommand]),
		Visits:       f.visits,
		LastConnect:  f.lastConnect,
	}

	verb := "saved"
	if f.edit {
		verb = "updated"

		// Rename itself rejects a taken alias, so it is the guard rather than the in-memory list.
		if alias != f.orig {
			if err := m.st.Rename(f.orig, alias); err != nil {
				m.setStatus(statusErr, "%v", err)
				return
			}
		}
		// Upsert's ON CONFLICT leaves visits and last_connect untouched, so an edit keeps history.
		if _, err := m.st.Upsert(host); err != nil {
			m.setStatus(statusErr, "save host: %v", err)
			return
		}
	} else {
		// Add is an INSERT that fails on a taken alias, so it cannot overwrite an unlisted host.
		host.Visits = 0
		host.LastConnect = 0
		if _, err := m.st.Add(host); err != nil {
			m.setStatus(statusErr, "%v", err)
			return
		}
	}

	// Land the cursor on the host just saved: reloadHosts alone would not find a new alias.
	m.reloadHostsSelecting(alias)
	m.closeHostForm()
	m.setStatus(statusOK, "%s %s", verb, alias)
}

// splitFormTags splits the tags field on commas; mirrors store.splitTags.
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
	hostFormMaxW   = 48 // content width, borders and padding excluded
	hostFormFloorW = 20
)

// hostFormChrome is what the card costs before any field is drawn.
const hostFormChrome = 2 + 2 + 2 + 1 + 1

// hostFormMinFields is the fewest fields the card shows: the selected one and its neighbours.
const hostFormMinFields = 3

// Card heights: with air between fields, packed, and at hostFormMinFields.
func hostFormFullH() int   { return hostFormChrome + 3*len(hostFormFields) }
func hostFormPackedH() int { return hostFormChrome + 2*len(hostFormFields) }
func hostFormMinH() int    { return hostFormChrome + 2*hostFormMinFields }

// hostFormWindow is the run of fields the card has room to draw, always containing the cursor.
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

// hostFormInnerW is the width available to a rendered row, held to the window.
func (m *model) hostFormInnerW() int {
	room := max(m.width-2*cardPadX-2, hostFormFloorW)
	return clamp(hostFormMaxW, hostFormFloorW, room)
}

// renderHostForm draws the add/edit card: a stack of labelled fields, the focused one lit.
func (m *model) renderHostForm() string {
	w := m.hostFormInnerW()
	var b strings.Builder

	title := "NEW HOST"
	if m.hostForm.edit {
		title = "EDIT HOST"
	}
	b.WriteString(titleStyle.Render(title))
	b.WriteString("\n\n")

	// Air gives way before the field count, so a short terminal never hides the focused field.
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

// renderHostFormValue draws one field's value, or a dim placeholder when blank.
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
