package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// helpSection is one group of bindings in the help card: a mode, and what the keys do in
// it. owner is the pane mode the section belongs to, which is how the card knows which
// section to lead with; sections that belong to no single mode (MOUSE, HOST) leave it at
// modeAny.
type helpSection struct {
	title string
	keys  [][2]string
	owner paneMode
}

// modeAny marks a section that is not one mode's own. It is deliberately not a paneMode
// the model can be in, so it never matches the section the card is opening on.
const modeAny paneMode = -1

// The whole of hop's keyboard, grouped by the mode that owns it and split into the card's
// two columns. The footer shows a slice of the same set: it says what is live now, this
// says what exists.
//
// The two motion sections are built rather than declared, because what they bind depends
// on the "Vim keys" setting: the card shows the keyboard you are actually holding, and
// when vim is off it says where to turn it on.

func helpLeft(vim bool) []helpSection {
	return []helpSection{
		{"LIST", append(motionKeys(vim), [][2]string{
			{"space", "this host's actions"},
			{"ctrl+k", "search every action"},
			{"/", "filter the hosts"},
			{"a", "add a new host"},
			{"i", "import an ssh config"},
			{"ctrl+b", "hide / show the sidebar"},
			{"?", "this card"},
			{"q", "quit hop"},
		}...), modeList},
		{"HOST", [][2]string{
			{"enter", "connect / focus its shell"},
			{"S", "another shell, same connection"},
			{"s", "focus the host's shell"},
			{"f", "sftp file browser"},
			{"t", "start / stop all tunnels"},
			{"shift+t", "manage tunnel definitions"},
			{"o", "open in VS Code Remote (its shell's dir)"},
			{"e", "edit this host"},
			{"x", "delete this host"},
			{"p", "pin it to the top / unpin"},
			{"shift+j k", "move a pinned host in its section"},
			{"d", "disconnect everything on it"},
			{"r", "reconnect a dropped session"},
			{",", "settings"},
		}, modeAny},
		// The pointer does nothing the keyboard cannot, so this lists which key each
		// gesture stands in for.
		{"MOUSE", [][2]string{
			{"wheel", "move / scroll what you point at"},
			{"click", "select it, or take the keyboard"},
			{"double-click", "open it — as enter"},
			{"drag in a pane", "select text; copies on release"},
			{"ctrl+g", "hand the mouse to your terminal"},
		}, modeAny},
	}
}

func helpRight(vim bool) []helpSection {
	open, up := [2]string{"enter", "open dir / edit file"}, [2]string{"←", "up a directory"}
	if vim {
		open, up = [2]string{"enter / l", "open dir / edit file"}, [2]string{"h / ←", "up a directory"}
	}

	return []helpSection{
		{"SHELL", [][2]string{
			{"ctrl+o o", "back to hop"},
			{"esc esc", "back to hop"},
			{"shift+← →", "switch shell tab"},
			{"ctrl+o 1…9", "straight to that shell"},
			{"ctrl+o 0", "another shell, same host"},
			{"ctrl+o c", "this dir in VS Code"},
			{"ctrl+o ctrl+k", "search every action"},
			{"ctrl+o ?", "this card"},
			{"shift+↑", "scroll back through history"},
			{"ctrl+b", "hide / show the sidebar"},
			{"…anything", "goes to the remote shell"},
		}, modeShell},
		{"SFTP BROWSER", [][2]string{
			open,
			up,
			{"o", "open the file locally"},
			{"d", "download the file"},
			{"u", "upload a local file here"},
			{"R", "rename"},
			{"x", "delete"},
			{"m", "new directory"},
			{"s", "sort by name / size / modified"},
			{"r", "refresh"},
			{"ctrl+k", "search every action"},
			{"?", "this card"},
			{"ctrl+o", "back to hop"},
		}, modeBrowser},
		{"DROPPED SESSION", [][2]string{
			{"r", "reconnect and reopen"},
			{"d", "drop it"},
			{"?", "this card"},
			{"ctrl+o", "back to hop"},
		}, modeAny},
		{"EDITOR", [][2]string{
			{":q", "close the tab"},
			{"shift+← →", "switch file tab"},
			{"ctrl+o 1…9", "straight to that tab"},
			{"ctrl+o ctrl+k", "search every action"},
			{"ctrl+o o", "back to the browser"},
			{"ctrl+o ?", "this card"},
			{"…anything", "goes to the remote editor"},
		}, modeEditor},
	}
}

// motionKeys is how the list moves: the vim step keys when they are on, and the plain
// ones plus a pointer at the switch when they are not. The jumps and ctrl chords belong
// to the browser (see keymap.Scope).
func motionKeys(vim bool) [][2]string {
	if vim {
		return [][2]string{
			{"↑ ↓ / j k", "move"},
			{"pgup / pgdn", "page"},
		}
	}
	return [][2]string{
		{"↑ ↓", "move"},
		{"pgup / pgdn", "page"},
		{"j k h l", "vim keys: off — , to turn on"},
	}
}

// helpFor is the card's contents arranged for the mode you opened it from: the section
// that owns that mode is lifted to the top of the left column, where it is the first
// thing read, and named so helpColumn can mark it. The rest keep their order.
//
// This is what lets the footer be as short as it is. The footer names the two or three
// keys a mode cannot be worked without and points here for the rest; that only holds if
// "here" starts on the mode you were in, rather than on whichever section was written
// first.
func helpFor(vim bool, mode paneMode) (left, right []helpSection, lead string) {
	left, right = helpLeft(vim), helpRight(vim)

	// Scrollback has no section of its own — it is the shell's history, and its keys are
	// listed with the shell's.
	if mode == modeScrollback {
		mode = modeShell
	}

	pull := func(col []helpSection) ([]helpSection, *helpSection) {
		for i, sec := range col {
			if sec.owner == mode {
				return append(append([]helpSection{}, col[:i]...), col[i+1:]...), &sec
			}
		}
		return col, nil
	}

	if rest, sec := pull(left); sec != nil {
		return append([]helpSection{*sec}, rest...), right, sec.title
	}
	if rest, sec := pull(right); sec != nil {
		return append([]helpSection{*sec}, left...), rest, sec.title
	}
	return left, right, ""
}

// Help card geometry: helpKeyW fits the longest key hop names, helpColW the longest
// thing it says about one.
const (
	helpKeyW   = 13 // "ctrl+o ctrl+o", the widest key name hop has
	helpColW   = 42
	helpGutter = 4
)

// renderHelp draws the keybinding card: every key hop knows, on one screen. It stands in
// two columns rather than scrolling, falling back to one on a window too narrow to hold
// them side by side.
func (m *model) renderHelp() string {
	// What the window holds once the card's border and padding are paid for.
	room := max(m.width-2*cardPadX-2, 20)

	left, right, lead := helpFor(m.cfg.VimKeys, m.mode)

	w := min(2*helpColW+helpGutter, room)
	body := helpColumn(left, min(helpColW, room), lead) + "\n\n" +
		helpColumn(right, min(helpColW, room), lead)

	if room >= 2*helpColW+helpGutter {
		body = lipgloss.JoinHorizontal(lipgloss.Top,
			helpColumn(left, helpColW, lead),
			strings.Repeat(" ", helpGutter),
			helpColumn(right, helpColW, lead),
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

// fitHelp cuts the card's body to what the window can hold, so a short terminal gets a
// card with a bottom edge rather than one running off the screen.
func (m *model) fitHelp(body string) string {
	// The card's chrome: border, padding, title, blank, and the hint line.
	const chrome = 2 + 2 + 2 + 2
	lines := strings.Split(body, "\n")
	if room := m.height - chrome; room > 0 && len(lines) > room {
		lines = append(lines[:room-1], faint.Render("…"))
	}
	return strings.Join(lines, "\n")
}

// helpColumn renders sections as one column: a capped title with a rule out to the
// column's edge, then its keys with the labels aligned.
func helpColumn(sections []helpSection, w int, lead string) string {
	var b strings.Builder
	for i, sec := range sections {
		if i > 0 {
			b.WriteString("\n")
		}
		head := sectionCap.Render(sec.title) + " "
		// The mode you opened the card from, called out: the card shows everything, and
		// this is the part of it that is about the screen behind it.
		if sec.title == lead {
			head = titleStyle.Render(sec.title) + faint.Render(" · you are here") + " "
		}
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
