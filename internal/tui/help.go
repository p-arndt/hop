package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"

	"hop/internal/keys"
)

// helpSection is one group of bindings in the help card: a mode, and what the keys do in
// it. owner is the pane mode the section belongs to, which is how the card knows which
// section to lead with; sections that belong to no single mode (MOUSE, HOST) leave it at
// modeAny.
type helpSection struct {
	title string
	keys  []row
	owner paneMode
}

// modeAny marks a section that is not one mode's own. It is deliberately not a paneMode
// the model can be in, so it never matches the section the card is opening on.
const modeAny paneMode = -1

// The card is built from the registry in internal/keys rather than from a table of its
// own: it says what the keyboard *is*, and a card that had to be edited alongside the
// keyboard would eventually describe a different one. What it adds are the rows no
// binding could carry — a range of digits, a key the remote program owns, the pointer.

// row is one line of the card: a key as drawn, and what it does.
type row = [2]string

// rows renders bindings from one layer, in the order given, dropping any the user has
// unbound. An id the layer does not bind is skipped rather than drawn blank.
func (m *model) helpRows(l keys.Layer, ids ...keys.Action) []row {
	var out []row
	for _, id := range ids {
		b, ok := m.binds.BindingIn(l, id)
		if !ok || b.Keycap() == "" || (b.Vim && !m.cfg.VimKeys) {
			continue
		}
		out = append(out, row{b.Keycap(), b.Label})
	}
	return out
}

// chord renders a leader chord — the leader key and the key that follows it — for the
// pane sections, where hop's whole keyboard sits behind it.
func (m *model) chord(id keys.Action) []row {
	lead, b := m.binds.Keycap(keys.LeaderKey), keys.Binding{}
	b, ok := m.binds.BindingIn(keys.Leader, id)
	if !ok || lead == "" || b.Keycap() == "" {
		return nil
	}
	return []row{{lead + " " + b.Keycap(), b.Label}}
}

// chordRange is the digits behind the leader: a range rather than a binding, so it is
// spelled out here and nowhere else.
func (m *model) chordRange(label string) []row {
	lead := m.binds.Keycap(keys.LeaderKey)
	if lead == "" {
		return nil
	}
	return []row{{lead + " 1…9", label}}
}

func (m *model) helpLeft() []helpSection {
	list := m.helpRows(keys.List, keys.Up, keys.Down, keys.PageUp, keys.PageDown)
	if !m.cfg.VimKeys {
		// The one row that is about the setting rather than a key: the card shows the
		// keyboard you are actually holding, and says where the other one is.
		list = append(list, row{"j k h l", "vim keys: off — " + m.binds.Keycap(keys.Settings) + " to turn on"})
	}
	list = append(list, m.helpRows(keys.List,
		keys.Menu, keys.Palette, keys.Filter, keys.HostAdd, keys.HostImport)...)
	list = append(list, m.helpRows(keys.Global, keys.Sidebar)...)
	list = append(list, m.helpRows(keys.List, keys.Help, keys.Quit)...)

	host := m.helpRows(keys.List,
		keys.In, keys.HostNewShell, keys.HostShell, keys.HostBrowser, keys.HostTunnels,
		keys.HostTunnelUI, keys.HostVSCode, keys.HostEdit, keys.HostDelete, keys.HostPin)
	host = append(host, row{
		m.binds.Keycap(keys.HostPinUp) + " " + m.binds.Keycap(keys.HostPinDown),
		"move a pinned host in its section"})
	host = append(host, m.helpRows(keys.List, keys.HostDrop, keys.HostReconnec, keys.Settings)...)
	host = append(host, row{"1…9", "straight to that shell"})

	return []helpSection{
		{"LIST", list, modeList},
		{"HOST", host, modeAny},
		// The pointer does nothing the keyboard cannot, so this lists which key each
		// gesture stands in for.
		{"MOUSE", append([]row{
			{"wheel", "move / scroll what you point at"},
			{"click", "select it, or take the keyboard"},
			{"double-click", "open it — as " + m.binds.Keycap(keys.In)},
			{"drag in a pane", "select text; copies on release"},
		}, m.helpRows(keys.Global, keys.Mouse)...), modeAny},
	}
}

func (m *model) helpRight() []helpSection {
	shell := m.chord(keys.LeaderOut)
	shell = append(shell, m.helpRows(keys.Pane, keys.PaneLeave, keys.PaneNextTab)...)
	shell = append(shell, m.chordRange("straight to that shell")...)
	shell = append(shell, m.chord(keys.LeaderShell)...)
	shell = append(shell, m.chord(keys.LeaderVSCode)...)
	shell = append(shell, m.chord(keys.LeaderPalette)...)
	shell = append(shell, m.chord(keys.LeaderHelp)...)
	shell = append(shell, m.helpRows(keys.Pane, keys.PaneScroll)...)
	shell = append(shell, m.helpRows(keys.Global, keys.Sidebar)...)
	shell = append(shell, row{"…anything", "goes to the remote shell"})

	browser := m.helpRows(keys.Browser,
		keys.In, keys.Out, keys.BrowserOpen, keys.BrowserDownload, keys.BrowserUpload,
		keys.BrowserRename, keys.BrowserDelete, keys.BrowserMkdir, keys.BrowserSort,
		keys.BrowserRefresh, keys.BrowserPalette, keys.BrowserHelp, keys.BrowserLeave)

	editor := []row{{":q", "close the tab"}}
	editor = append(editor, m.helpRows(keys.Editor, keys.EditorNextTab)...)
	editor = append(editor, m.chordRange("straight to that tab")...)
	editor = append(editor, m.chord(keys.LeaderPalette)...)
	editor = append(editor, m.chord(keys.LeaderOut)...)
	editor = append(editor, m.chord(keys.LeaderHelp)...)
	editor = append(editor, row{"…anything", "goes to the remote editor"})

	return []helpSection{
		{"SHELL", shell, modeShell},
		{"SFTP BROWSER", browser, modeBrowser},
		{"DROPPED SESSION", m.helpRows(keys.DeadPane,
			keys.DeadReconnect, keys.DeadDrop, keys.DeadHelp, keys.DeadLeave), modeAny},
		{"EDITOR", editor, modeEditor},
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
func (m *model) helpFor(mode paneMode) (left, right []helpSection, lead string) {
	left, right = m.helpLeft(), m.helpRight()

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

	left, right, lead := m.helpFor(m.mode)

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
