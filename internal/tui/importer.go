package tui

import (
	"os"
	"path/filepath"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/lipgloss"

	"hop/internal/pathx"
)

// importUI is the SSH-config import card's state.
type importUI struct {
	open  bool
	path  string
	first bool
}

// openImport shows the card, pre-filled with ~/.ssh/config.
func (m *model) openImport(first bool) {
	m.importer = importUI{open: true, path: defaultSSHConfigPath(), first: first}
	m.status = ""
}

// closeImport hides the card, importing nothing.
func (m *model) closeImport() {
	m.importer = importUI{}
}

// defaultSSHConfigPath returns ~/.ssh/config, or "" if the home directory is unknown.
func defaultSSHConfigPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".ssh", "config")
}

// haveSSHConfig reports whether there is a default OpenSSH config to import.
func haveSSHConfig() bool {
	p := defaultSSHConfigPath()
	if p == "" {
		return false
	}
	fi, err := os.Stat(p)
	return err == nil && !fi.IsDir()
}

// handleImportKey routes a key while the card is up; it swallows every key.
func (m *model) handleImportKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
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
		if msg.Text != "" {
			m.importer.path += msg.Text
		}
	}
	return m, nil
}

// submitImport merges the config into the store; a failure leaves the card up so the path can be corrected.
func (m *model) submitImport() {
	path := strings.TrimSpace(m.importer.path)
	if path == "" {
		m.setStatus(statusErr, "path can't be empty")
		return
	}

	n, err := m.st.ImportSSHConfig(pathx.ExpandHome(path))
	if err != nil {
		m.setStatus(statusErr, "import: %v", err)
		return
	}

	m.reloadHosts()
	m.closeImport()
	if n == 0 {
		// The file parsed but held nothing usable: only wildcards, or no Host stanzas.
		m.setStatus(statusWarn, "no hosts found in %s", path)
		return
	}
	m.setStatus(statusOK, "imported %d %s from %s", n, plural(n, "host", "hosts"), path)
}

// Card geometry.
const (
	importMaxW   = 56 // content width, borders and padding excluded
	importFloorW = 20
)

// importInnerW is the width available inside the card's border and padding.
func (m *model) importInnerW() int {
	room := max(m.width-2*cardPadX-2, importFloorW)
	return clamp(importMaxW, importFloorW, room)
}

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

func (m *model) renderImportPath(w int) string {
	const indent = "    "
	vw := w - lipgloss.Width(indent)
	text := truncate(m.importer.path, vw-3) + accentText.Render("▏")
	return indent + inputStyle.Width(vw).Render(text)
}

// wrapDim word-wraps prose to width w and dims each line.
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
