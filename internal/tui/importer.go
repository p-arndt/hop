package tui

import (
	"os"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// importUI is the SSH-config import card's own state. It holds the one thing the
// import needs — which file to read — plus whether it came up on its own, because
// a card hop opened for you says something different from one you asked for: the
// first is an offer on an empty host list, the second is a re-import.
type importUI struct {
	open bool
	path string
	// first is true when the card was opened automatically on a first run (no
	// hosts yet) rather than by pressing "i".
	first bool
}

// openImport shows the card, pre-filled with the path to import from. It starts on
// the default ~/.ssh/config rather than empty: the overwhelmingly common answer is
// already known, so the whole interaction can be a single enter.
func (m *model) openImport(first bool) {
	m.importer = importUI{open: true, path: defaultSSHConfigPath(), first: first}
	m.status = ""
}

// closeImport hides the card, importing nothing.
func (m *model) closeImport() {
	m.importer = importUI{}
}

// defaultSSHConfigPath is where OpenSSH keeps its per-user config. A home
// directory hop cannot locate yields "", which leaves the field blank for the
// user to fill in rather than making the card useless.
func defaultSSHConfigPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".ssh", "config")
}

// haveSSHConfig reports whether there is a default OpenSSH config to import. It is
// what decides whether a first run opens the card at all: offering an import with
// no file to read would put a dead end in front of a brand-new user.
func haveSSHConfig() bool {
	p := defaultSSHConfigPath()
	if p == "" {
		return false
	}
	fi, err := os.Stat(p)
	return err == nil && !fi.IsDir()
}

// expandHome resolves a leading "~" against the home directory, so a path typed
// the way it is spoken ("~/.ssh/config") opens the file it names. Anything else is
// returned untouched — including a bare "~user/…", which hop does not resolve.
func expandHome(path string) string {
	if path != "~" && !strings.HasPrefix(path, "~/") && !strings.HasPrefix(path, `~\`) {
		return path
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return path
	}
	if path == "~" {
		return home
	}
	return filepath.Join(home, path[2:])
}

// handleImportKey routes a key while the card is up. The path field always has the
// keyboard — there is only one field, and a form is a thing you type into — and
// like every modal here it swallows everything, so a stray key cannot reach the
// list behind it.
func (m *model) handleImportKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.closeImport()

	case "enter":
		m.submitImport()

	case "backspace":
		if b := m.importer.path; b != "" {
			r := []rune(b)
			m.importer.path = string(r[:len(r)-1])
		}

	case "ctrl+u":
		m.importer.path = ""

	default:
		if len(msg.Runes) > 0 {
			m.importer.path += string(msg.Runes)
		}
	}
	return m, nil
}

// submitImport reads the config and merges what it finds into the store. A failure
// leaves the card up so the path can be corrected rather than retyped from
// scratch; a success closes it and reloads the list, so the hosts are there behind
// the card as it goes.
//
// Import is an upsert per host (see store.ImportSSHConfig), so running it twice —
// or after editing a host by hand — refreshes what the config knows and leaves
// hop's own hosts alone. That is what makes this safe to offer as a re-import and
// not just a first-run step.
func (m *model) submitImport() {
	path := strings.TrimSpace(m.importer.path)
	if path == "" {
		m.setStatus(statusErr, "path can't be empty")
		return
	}

	n, err := m.st.ImportSSHConfig(expandHome(path))
	if err != nil {
		m.setStatus(statusErr, "import: %v", err)
		return
	}

	m.reloadHosts()
	m.closeImport()
	if n == 0 {
		// The file parsed but held nothing hop can use — every entry a wildcard, or
		// no Host stanzas at all. Saying so beats a cheerful "imported 0 hosts".
		m.setStatus(statusWarn, "no hosts found in %s", path)
		return
	}
	m.setStatus(statusOK, "imported %d %s from %s", n, plural(n, "host", "hosts"), path)
}

// Card geometry. The import card holds one field and a line of explanation, so it
// is as wide as a path is long rather than as wide as the form.
const (
	importMaxW   = 56 // content width, borders and padding excluded
	importFloorW = 20
)

// importInnerW is the width available to a rendered line: the box minus its border
// and padding, held to the window so the card never spills past the screen.
func (m *model) importInnerW() int {
	room := max(m.width-2*cardPadX-2, importFloorW)
	return clamp(importMaxW, importFloorW, room)
}

// renderImport draws the card: a title, a line saying what is about to happen, the
// path as a filled text input, and the two keys that end it. On a first run it also
// says that skipping costs nothing — an empty host list is a state you can leave by
// adding a host by hand.
func (m *model) renderImport() string {
	w := m.importInnerW()
	var b strings.Builder

	b.WriteString(truncate(titleStyle.Render("IMPORT SSH CONFIG"), w))
	b.WriteString("\n\n")

	blurb := "Read hosts from an OpenSSH config. Existing hosts are updated, not replaced."
	if m.importer.first {
		blurb = "Welcome to hop. Import the hosts you already have in your OpenSSH config."
	}
	b.WriteString(wrapDim(blurb, w))
	b.WriteString("\n\n")

	b.WriteString(truncate(settingsLabelSel.Render("Config file"), w))
	b.WriteString("\n")
	b.WriteString(m.renderImportPath(w))
	b.WriteString("\n\n")

	b.WriteString(rule(w))
	b.WriteString("\n")
	cancel := keyHint("esc", "cancel")
	if m.importer.first {
		cancel = keyHint("esc", "skip")
	}
	b.WriteString(truncate(keyHint("enter", "import")+"  "+cancel+"  "+keyHint("ctrl+u", "clear"), w))

	return cardBox.Width(w + 2*cardPadX).Render(b.String())
}

// renderImportPath draws the path row as a full-width text input with a caret at
// the end — the same shape the host form's focused field has, so the two read as
// one family.
func (m *model) renderImportPath(w int) string {
	const indent = "    "
	vw := w - lipgloss.Width(indent)
	text := truncate(m.importer.path, vw-3) + accentText.Render("▏")
	return indent + inputStyle.Width(vw).Render(text)
}

// wrapDim lays out prose inside the card: words to a line of width w, each line
// dimmed and cut to the card so nothing spills past its border.
func wrapDim(s string, w int) string {
	var lines []string
	line := ""
	for _, word := range strings.Fields(s) {
		switch {
		case line == "":
			line = word
		case lipgloss.Width(line+" "+word) <= w:
			line += " " + word
		default:
			lines = append(lines, line)
			line = word
		}
	}
	if line != "" {
		lines = append(lines, line)
	}
	for i, l := range lines {
		lines[i] = dimStyle.Render(truncate(l, w))
	}
	return strings.Join(lines, "\n")
}
