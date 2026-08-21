package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"

	"hop/internal/keys"
)

// helpSection is one group of bindings in the help card; owner is the mode it leads with.
type helpSection struct {
	title string
	keys  []row
	owner paneMode
}

// modeAny marks a section owned by no mode; not a paneMode the model can be in, so it never
// matches the mode the card opens on.
const modeAny paneMode = -1

// row is one line of the card: a key as drawn, and what it does.
type row = [2]string

// browserHelpActions is the browser section, in draw order. A named list so a test can walk
// it — see TestEveryActionIsDiscoverable.
var browserHelpActions = []keys.Action{
	keys.In, keys.Out, keys.BrowserOpen, keys.BrowserDownload, keys.BrowserUpload,
	keys.BrowserMark, keys.BrowserMarkAll, keys.BrowserTarget,
	keys.BrowserCopy, keys.BrowserMoveTo,
	keys.BrowserRename, keys.BrowserDelete, keys.BrowserMkdir, keys.BrowserSort,
	keys.BrowserRefresh, keys.BrowserFocusPane, keys.BrowserSplit, keys.BrowserTree,
	keys.BrowserPalette, keys.BrowserHelp, keys.BrowserLeave,
}

// editorHelpActions is the editor section's, on the same terms.
var editorHelpActions = []keys.Action{
	keys.EditorNextTab, keys.EditorFocusTree, keys.EditorUnsplit, keys.BrowserTree,
}

// helpRows renders bindings from one layer in the order given, skipping unbound ids.
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

// chord renders a leader chord: the leader key and the key that follows it.
func (m *model) chord(id keys.Action) []row {
	lead, b := m.binds.Keycap(keys.LeaderKey), keys.Binding{}
	b, ok := m.binds.BindingIn(keys.Leader, id)
	if !ok || lead == "" || b.Keycap() == "" {
		return nil
	}
	return []row{{lead + " " + b.Keycap(), b.Label}}
}

// chordRange is the digits behind the leader: a range rather than a binding.
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

	browser := m.helpRows(keys.Browser, browserHelpActions...)

	editor := []row{{":q", "close the tab"}}
	editor = append(editor, m.helpRows(keys.Editor, editorHelpActions...)...)
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

// helpFor arranges the card for mode: that mode's section is lifted to the top of the left
// column and named so helpColumn can mark it.
func (m *model) helpFor(mode paneMode) (left, right []helpSection, lead string) {
	left, right = m.helpLeft(), m.helpRight()

	// Scrollback has no section of its own; its keys are listed with the shell's.
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

// Help card geometry.
const (
	helpKeyW   = 13 // "ctrl+o ctrl+o", the widest key name hop has
	helpColW   = 42
	helpGutter = 4
)

// renderHelp draws the keybinding card in two columns, or one on a narrow window.
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

	shown, more := m.fitHelp(body)

	var b strings.Builder
	b.WriteString(titleStyle.Render("KEYS"))
	b.WriteString(faint.Render("  everything hop binds"))
	b.WriteString("\n\n")
	b.WriteString(shown)
	b.WriteString("\n")
	b.WriteString(keyHint("esc", "close"))
	// Only while the window is actually hiding something: otherwise these keys do nothing.
	if more {
		b.WriteString("   ")
		b.WriteString(keyHint("↑ ↓", "scroll"))
	}

	return cardBox.Width(w + 2*cardPadX).Render(b.String())
}

// helpChrome is what the card costs before a body line fits: border, padding, title, hint.
const helpChrome = 2 + 2 + 2 + 2

// helpPage is how many body lines the window has room for, and how far a page key moves.
func (m *model) helpPage() int { return max(m.height-helpChrome, 0) }

// fitHelp cuts the body to what the window holds from the current scroll, reporting whether
// anything is hidden.
func (m *model) fitHelp(body string) (string, bool) {
	lines := strings.Split(body, "\n")
	room := m.helpPage()
	if room <= 0 || len(lines) <= room {
		m.helpScroll = 0
		return body, false
	}

	m.helpScroll = min(max(m.helpScroll, 0), len(lines)-room)
	return strings.Join(lines[m.helpScroll:m.helpScroll+room], "\n"), true
}

// helpColumn renders sections as one column: a capped title with a rule, then its keys.
func helpColumn(sections []helpSection, w int, lead string) string {
	var b strings.Builder
	for i, sec := range sections {
		if i > 0 {
			b.WriteString("\n")
		}
		head := sectionCap.Render(sec.title) + " "
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
