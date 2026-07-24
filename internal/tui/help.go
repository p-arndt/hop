package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// helpSection is one group of bindings in the help card: a mode you can be in,
// and what the keys do while you are in it.
type helpSection struct {
	title string
	keys  [][2]string
}

// The whole of hop's keyboard, grouped by the mode that owns it, and split into
// the two columns the card stands up in. It is the same set of bindings the
// footer shows a slice of — the footer says what is live right now, this says
// what exists.
//
// The two motion sections are built rather than declared, because what hop binds in
// them depends on the "Vim keys" setting. A reference card that lists keys you do
// not have is worse than no card: the card shows the keyboard you are actually
// holding, and when vim is off it says where to go and turn it on.

func helpLeft(vim bool) []helpSection {
	return []helpSection{
		{"LIST", append(motionKeys(vim), [][2]string{
			{"/", "filter the hosts"},
			{"a", "add a new host"},
			{"i", "import an ssh config"},
			{"q", "quit hop"},
		}...)},
		{"HOST", [][2]string{
			{"enter", "connect / focus its shell"},
			{"S", "another shell, same connection"},
			{"s", "focus the host's shell"},
			{"f", "sftp file browser"},
			{"o", "open in VS Code Remote"},
			{"e", "edit this host"},
			{"x", "delete this host"},
			{"d", "disconnect everything on it"},
			{",", "settings"},
		}},
	}
}

func helpRight(vim bool) []helpSection {
	open, up := [2]string{"enter", "open dir / edit file"}, [2]string{"←", "up a directory"}
	if vim {
		open, up = [2]string{"enter / l", "open dir / edit file"}, [2]string{"h / ←", "up a directory"}
	}

	return []helpSection{
		{"SHELL", [][2]string{
			{"←", "back to hop (at a bare prompt)"},
			{"ctrl+o", "back to hop"},
			{"esc esc", "back to hop"},
			{"alt+0", "another shell, same host"},
			{"alt+← →", "switch shell tab"},
			{"alt+1…9", "jump to shell tab"},
			{"shift+↑", "scroll back through history"},
			{"…anything", "goes to the remote shell"},
		}},
		{"SFTP BROWSER", [][2]string{
			open,
			up,
			{"o", "open the file locally"},
			{"d", "download the file"},
			{"r", "refresh"},
			{"ctrl+o", "back to hop"},
		}},
		{"EDITOR", [][2]string{
			{":q", "close the tab"},
			{"alt+← →", "switch file tab"},
			{"alt+1…9", "jump to file tab"},
			{"ctrl+o", "back to the browser"},
			{"…anything", "goes to the remote editor"},
		}},
	}
}

// motionKeys is how the list moves: the vim motions when they are switched on, and
// the plain ones plus a pointer at the switch when they are not.
func motionKeys(vim bool) [][2]string {
	if vim {
		return [][2]string{
			{"↑ ↓ / j k", "move"},
			{"gg / G", "top / bottom"},
			{"H M L", "high / mid / low in view"},
			{"ctrl+d / u", "half page"},
			{"ctrl+f / b", "full page"},
		}
	}
	return [][2]string{
		{"↑ ↓", "move"},
		{"pgup / pgdn", "page"},
		{"j k h l …", "vim keys: off — , to turn on"},
	}
}

// Help card geometry. helpKeyW is wide enough for the longest key hop names, and
// helpColW for the longest thing it says about one.
const (
	helpKeyW   = 12
	helpColW   = 42
	helpGutter = 4
)

// renderHelp draws the keybinding card: every key hop knows, on one screen. A
// reference you have to page through is one you stop opening — so it stands in
// two columns rather than scrolling, and falls back to one only when the window
// is too narrow to hold them side by side.
func (m *model) renderHelp() string {
	// What the window can hold once the card's own border and padding are paid for.
	room := max(m.width-2*cardPadX-2, 20)

	left, right := helpLeft(m.cfg.VimKeys), helpRight(m.cfg.VimKeys)

	w := min(2*helpColW+helpGutter, room)
	body := helpColumn(left, min(helpColW, room)) + "\n\n" +
		helpColumn(right, min(helpColW, room))

	if room >= 2*helpColW+helpGutter {
		body = lipgloss.JoinHorizontal(lipgloss.Top,
			helpColumn(left, helpColW),
			strings.Repeat(" ", helpGutter),
			helpColumn(right, helpColW),
		)
	}

	var b strings.Builder
	b.WriteString(titleStyle.Render("KEYS"))
	b.WriteString(faint.Render("  everything hop binds"))
	b.WriteString("\n\n")
	b.WriteString(m.fitHelp(body))
	b.WriteString("\n")
	b.WriteString(keyHint("esc", "close"))

	return cardBox.Width(w + 2*cardPadX).Render(b.String())
}

// fitHelp cuts the card's body to what the window can actually hold, so a short
// terminal gets a card with a bottom edge on it rather than one running off the
// screen. The rows it drops are the ones the footer is already showing.
func (m *model) fitHelp(body string) string {
	// The card's own chrome: border, padding, title, blank, and the hint line.
	const chrome = 2 + 2 + 2 + 2
	lines := strings.Split(body, "\n")
	if room := m.height - chrome; room > 0 && len(lines) > room {
		lines = append(lines[:room-1], faint.Render("…"))
	}
	return strings.Join(lines, "\n")
}

// helpColumn renders sections as one column: a capped title with a rule running
// out to the column's edge, then its keys with their labels aligned.
func helpColumn(sections []helpSection, w int) string {
	var b strings.Builder
	for i, sec := range sections {
		if i > 0 {
			b.WriteString("\n")
		}
		head := sectionCap.Render(sec.title) + " "
		b.WriteString(padTo(head+rule(max(w-lipgloss.Width(head), 0)), w))
		b.WriteString("\n")

		for _, k := range sec.keys {
			key := padTo(accentText.Render(k[0]), helpKeyW)
			b.WriteString(padTo(key+dimStyle.Render(k[1]), w))
			b.WriteString("\n")
		}
	}
	return strings.TrimRight(b.String(), "\n")
}
