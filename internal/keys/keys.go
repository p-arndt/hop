// Package keys is hop's keyboard, written down once.
//
// Before this package the same binding lived in four places — the switch that ran it, the
// action registry the palette and the context menu render from, the help card's table,
// and the footer's hints — and they were kept in step by hand. A key that meant two
// things in two of them was a bug nobody could see from any one file.
//
// Here a binding is one row: what it does (Action), where it applies (Layer), the keys
// that trigger it, and the words the UI says about it. Everything else is derived —
// dispatch resolves a key to an Action, the card and the footer read the labels, and a
// user's config.json overrides the keys without any of those knowing.
//
// What is deliberately *not* here: the modal cards (add-host form, settings, importer,
// confirmations). Their keyboard is dialog convention — tab moves, enter accepts, esc
// cancels — rather than hop's own, and a card that let you rebind esc would be a card you
// could lock yourself into.
package keys

import (
	"fmt"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"
)

// Layer is which keyboard is asking. A key means whatever the layer that owns the
// keystroke says it means, and layers are consulted in the order handleKey consults
// them: the leader above everything, then the two window-wide bindings, then the mode.
//
// Layers are not a fallback chain inside this package — the caller asks the layer it is
// in, and asks Global itself where Global applies. That keeps "which layer was this key
// read in" a fact at the call site rather than a search order to reason about.
type Layer int

const (
	// Global is the pair hop holds in every mode below the modal cards: they belong to
	// the window rather than to whatever owns the keyboard.
	Global Layer = iota
	// List is the host list.
	List
	// Browser is the SFTP browser — the tui's half (the exits) and the filebrowser's own
	// half (the file operations) are one layer, because to the user they are one keyboard.
	Browser
	// Pane is a live shell. Nearly every key belongs to the remote program, so this layer
	// is small and everything it does not claim is forwarded.
	Pane
	// Scrollback is a shell pane scrolled back through its history.
	Scrollback
	// Editor is an open remote-editor tab.
	Editor
	// Leader is the second key of the leader chord — hop's keyboard inside a pane.
	Leader
	// DeadPane is a session whose connection dropped: the ways back and nothing else.
	DeadPane
)

// layerNames are the layer's spelling in config.json and in error messages.
var layerNames = map[Layer]string{
	Global: "global", List: "list", Browser: "browser", Pane: "pane",
	Scrollback: "scrollback", Editor: "editor", Leader: "leader", DeadPane: "dead",
}

func (l Layer) String() string { return layerNames[l] }

// Action is what a key does, as a stable id: it is what the handlers switch on, and what
// a user writes in config.json to rebind something. The ids outlive the keys, which is
// the point — "browser.download" is still that action whichever key runs it.
type Action string

// None is "no binding in this layer": the caller is free to do whatever it does with a
// key hop has no meaning for, which in a pane is forwarding it to the remote program.
const None Action = ""

// The actions, grouped by the layer that owns them. The string values are the config
// file's vocabulary, so they are renamed only with a migration.
const (
	// Global.
	Sidebar Action = "global.sidebar"
	Mouse   Action = "global.mouse"

	// Motions, shared by the list and the browser (see MotionLayers). The list binds the
	// steps, pages and in/out; the browser binds all of them.
	Up        Action = "motion.up"
	Down      Action = "motion.down"
	Top       Action = "motion.top"
	Bottom    Action = "motion.bottom"
	HalfUp    Action = "motion.half-up"
	HalfDown  Action = "motion.half-down"
	PageUp    Action = "motion.page-up"
	PageDown  Action = "motion.page-down"
	ScreenTop Action = "motion.screen-top"
	ScreenMid Action = "motion.screen-middle"
	ScreenBot Action = "motion.screen-bottom"
	In        Action = "motion.in"
	Out       Action = "motion.out"

	// List.
	Quit         Action = "list.quit"
	Filter       Action = "list.filter"
	Settings     Action = "list.settings"
	Help         Action = "list.help"
	Palette      Action = "list.palette"
	Menu         Action = "list.menu"
	Back         Action = "list.back"
	HostAdd      Action = "list.add-host"
	HostEdit     Action = "list.edit-host"
	HostDelete   Action = "list.delete-host"
	HostImport   Action = "list.import"
	HostPin      Action = "list.pin"
	HostPinUp    Action = "list.pin-up"
	HostPinDown  Action = "list.pin-down"
	HostShell    Action = "list.focus-shell"
	HostNewShell Action = "list.new-shell"
	HostBrowser  Action = "list.browser"
	HostTunnels  Action = "list.tunnels"
	HostTunnelUI Action = "list.tunnel-manager"
	HostVSCode   Action = "list.vscode"
	HostReconnec Action = "list.reconnect"
	HostDrop     Action = "list.disconnect"

	// Browser. The exits and the three hop-wide cards are the tui's half of this layer;
	// the rest is the filebrowser's own.
	BrowserLeave    Action = "browser.leave"
	BrowserSettings Action = "browser.settings"
	BrowserHelp     Action = "browser.help"
	BrowserPalette  Action = "browser.palette"
	BrowserUp       Action = "browser.up-a-directory"
	BrowserRefresh  Action = "browser.refresh"
	BrowserOpen     Action = "browser.open-locally"
	BrowserDownload Action = "browser.download"
	BrowserUpload   Action = "browser.upload"
	BrowserDelete   Action = "browser.delete"
	BrowserRename   Action = "browser.rename"
	BrowserMkdir    Action = "browser.mkdir"
	BrowserSort     Action = "browser.sort"

	// Browser, the multi-selection and the target. Marking is what makes an operation
	// plural; the target is the directory a copy or a move lands in, so that neither has
	// to be typed as a path. Both live in the filebrowser, not the tui.
	BrowserMark      Action = "browser.mark"
	BrowserMarkAll   Action = "browser.mark-all"
	BrowserTarget    Action = "browser.set-target"
	BrowserCopy      Action = "browser.copy-to-target"
	BrowserMoveTo    Action = "browser.move-to-target"
	BrowserFocusPane Action = "browser.focus-pane"
	BrowserTree      Action = "browser.tree-column"
	BrowserSplit     Action = "browser.split"

	// Pane. LeaderKey opens the Leader layer; PaneLeave is the other way out, and the two
	// are what the escape-hatch check (see Validate) insists on keeping.
	LeaderKey    Action = "pane.leader"
	PaneLeave    Action = "pane.leave"
	PaneNewShell Action = "pane.new-shell"
	PaneNextTab  Action = "pane.next-tab"
	PanePrevTab  Action = "pane.previous-tab"
	PaneScroll   Action = "pane.scrollback"
	PaneScrollPg Action = "pane.scrollback-page"

	// Scrollback. The motions are this layer's own rather than the shared ones: it scrolls
	// a viewport of text, not a list of things, and "in" and "out" mean nothing to it.
	ScrollUp       Action = "scrollback.up"
	ScrollDown     Action = "scrollback.down"
	ScrollPageUp   Action = "scrollback.page-up"
	ScrollPageDown Action = "scrollback.page-down"
	ScrollHalfUp   Action = "scrollback.half-up"
	ScrollHalfDown Action = "scrollback.half-down"
	ScrollTop      Action = "scrollback.top"
	ScrollBottom   Action = "scrollback.bottom"
	ScrollHelp     Action = "scrollback.help"
	ScrollLeave    Action = "scrollback.leave"

	// Editor.
	EditorLeave   Action = "editor.leave"
	EditorNextTab Action = "editor.next-tab"
	EditorPrevTab Action = "editor.previous-tab"
	// EditorFocusTree hands the keyboard back to the tree column without closing the
	// editor: the whole point of the column is that it stays visible while a file is open.
	EditorFocusTree Action = "editor.focus-tree"
	// EditorUnsplit is the way out of a split content area, and the counterpart to
	// BrowserSplit: without it the only exit is closing files until one half runs out,
	// which makes "two files side by side" a state you can enter and not leave.
	EditorUnsplit Action = "editor.unsplit"

	// Leader — hop's keyboard inside a pane.
	LeaderOut     Action = "leader.out"
	LeaderVSCode  Action = "leader.vscode"
	LeaderPalette Action = "leader.palette"
	LeaderHelp    Action = "leader.help"
	LeaderShell   Action = "leader.new-shell"

	// DeadPane.
	DeadReconnect Action = "dead.reconnect"
	DeadLeave     Action = "dead.leave"
	DeadHelp      Action = "dead.help"
	DeadDrop      Action = "dead.drop"
)

// Binding is one row of the keyboard.
type Binding struct {
	// Action is what it does, and the id config.json rebinds it by.
	Action Action
	// Layer is where it applies.
	Layer Layer
	// Keys are what triggers it. The first is the canonical one — what the help card and
	// the footer draw — and the rest are aliases, which exist because a stock macOS
	// terminal never sends alt+<key> as a meta escape (see KEYBINDINGS.md).
	Keys []string
	// Show overrides how the canonical key is drawn, for the ones whose name is longer
	// than their symbol ("shift+→"). Empty means the key itself.
	Show string
	// Label says what it does, in the words the help card uses.
	Label string
	// Vim marks a key the "Vim keys" setting owns. Those are plain letters, and hop
	// holding them silently would surprise anyone who did not ask for vim.
	Vim bool
	// Window is how long a multi-key sequence waits for its second key. Zero waits for
	// ever, which is right for a chord nothing else could be — "g" alone does nothing —
	// and wrong for one whose first key has a meaning of its own: an esc bound for a
	// remote program must not turn into half a chord for the rest of the session.
	Window time.Duration
}

// key returns the canonical key, or "" for a binding a user unbound.
func (b Binding) key() string {
	if len(b.Keys) == 0 {
		return ""
	}
	return b.Keys[0]
}

// Keycap is how the binding's key is drawn.
func (b Binding) Keycap() string {
	if b.Show != "" {
		return b.Show
	}
	return b.key()
}

// defaults is hop's keyboard as shipped. One row per action; the layer decides where it
// applies and the order within a layer is the order the help card and the palette show,
// so the common things stand at the top.
//
// Digits are not here. "1".."9" in the list, alt+1..alt+9 in a pane and 1..9 behind the
// leader all address a tab by its number, which is a range rather than a binding: there
// is nothing to rebind that would still mean "the third one".
var defaults = []Binding{
	{Action: Sidebar, Layer: Global, Keys: []string{"ctrl+b"}, Label: "hide / show the sidebar"},
	{Action: Mouse, Layer: Global, Keys: []string{"ctrl+g"}, Label: "hand the mouse to your terminal"},

	// ---- host list ----
	{Action: In, Layer: List, Keys: []string{"enter", "right"}, Label: "connect / focus its shell"},
	{Action: In, Layer: List, Keys: []string{"l"}, Vim: true, Label: "connect / focus its shell"},
	{Action: Out, Layer: List, Keys: []string{"left"}, Label: "back out of the host"},
	{Action: Out, Layer: List, Keys: []string{"h"}, Vim: true, Label: "back out of the host"},
	{Action: Up, Layer: List, Keys: []string{"up"}, Label: "move up"},
	{Action: Up, Layer: List, Keys: []string{"k"}, Vim: true, Label: "move up"},
	{Action: Down, Layer: List, Keys: []string{"down"}, Label: "move down"},
	{Action: Down, Layer: List, Keys: []string{"j"}, Vim: true, Label: "move down"},
	{Action: PageUp, Layer: List, Keys: []string{"pgup"}, Label: "a screen up"},
	{Action: PageDown, Layer: List, Keys: []string{"pgdown"}, Label: "a screen down"},
	{Action: HostNewShell, Layer: List, Keys: []string{"S"}, Label: "another shell, same connection"},
	{Action: HostShell, Layer: List, Keys: []string{"s"}, Label: "focus the host's shell"},
	{Action: HostBrowser, Layer: List, Keys: []string{"f"}, Label: "sftp file browser"},
	{Action: HostTunnels, Layer: List, Keys: []string{"t"}, Label: "start / stop all tunnels"},
	{Action: HostTunnelUI, Layer: List, Keys: []string{"T"}, Show: "shift+t", Label: "manage tunnel definitions"},
	{Action: HostVSCode, Layer: List, Keys: []string{"o"}, Label: "open in VS Code Remote"},
	{Action: HostEdit, Layer: List, Keys: []string{"e"}, Label: "edit this host"},
	{Action: HostPin, Layer: List, Keys: []string{"p"}, Label: "pin it to the top / unpin"},
	{Action: HostPinUp, Layer: List, Keys: []string{"K"}, Show: "shift+k", Label: "move a pinned host up"},
	{Action: HostPinDown, Layer: List, Keys: []string{"J"}, Show: "shift+j", Label: "move a pinned host down"},
	{Action: HostDrop, Layer: List, Keys: []string{"d"}, Label: "disconnect everything on it"},
	{Action: HostReconnec, Layer: List, Keys: []string{"r"}, Label: "reconnect a dropped session"},
	{Action: HostDelete, Layer: List, Keys: []string{"x"}, Label: "delete this host"},
	{Action: HostAdd, Layer: List, Keys: []string{"a"}, Label: "add a new host"},
	{Action: HostImport, Layer: List, Keys: []string{"i"}, Label: "import an ssh config"},
	{Action: Filter, Layer: List, Keys: []string{"/"}, Label: "filter the hosts"},
	{Action: Menu, Layer: List, Keys: []string{"space"}, Label: "this host's actions"},
	{Action: Palette, Layer: List, Keys: []string{"ctrl+k"}, Label: "search every action"},
	{Action: Settings, Layer: List, Keys: []string{","}, Label: "settings"},
	{Action: Help, Layer: List, Keys: []string{"?"}, Label: "all the keys"},
	{Action: Back, Layer: List, Keys: []string{"esc"}, Label: "back out / quit"},
	{Action: Quit, Layer: List, Keys: []string{"esc esc"}, Window: doubleEscWindow, Label: "quit hop"},
	{Action: Quit, Layer: List, Keys: []string{"q", "ctrl+c"}, Label: "quit hop"},

	// ---- sftp browser ----
	{Action: In, Layer: Browser, Keys: []string{"enter", "right"}, Label: "open the directory / edit the file"},
	{Action: In, Layer: Browser, Keys: []string{"l"}, Vim: true, Label: "open the directory / edit the file"},
	{Action: Out, Layer: Browser, Keys: []string{"left"}, Show: "←", Label: "up a directory"},
	{Action: Out, Layer: Browser, Keys: []string{"h"}, Vim: true, Label: "up a directory"},
	{Action: BrowserUp, Layer: Browser, Keys: []string{"backspace"}, Label: "up a directory"},
	{Action: Up, Layer: Browser, Keys: []string{"up"}, Label: "move up"},
	{Action: Up, Layer: Browser, Keys: []string{"k"}, Vim: true, Label: "move up"},
	{Action: Down, Layer: Browser, Keys: []string{"down"}, Label: "move down"},
	{Action: Down, Layer: Browser, Keys: []string{"j"}, Vim: true, Label: "move down"},
	{Action: PageUp, Layer: Browser, Keys: []string{"pgup"}, Label: "a screen up"},
	{Action: PageDown, Layer: Browser, Keys: []string{"pgdown"}, Label: "a screen down"},
	{Action: PageDown, Layer: Browser, Keys: []string{"ctrl+f"}, Vim: true, Label: "a screen down"},
	{Action: HalfUp, Layer: Browser, Keys: []string{"ctrl+u"}, Vim: true, Label: "half a screen up"},
	{Action: HalfDown, Layer: Browser, Keys: []string{"ctrl+d"}, Vim: true, Label: "half a screen down"},
	{Action: Top, Layer: Browser, Keys: []string{"g g"}, Vim: true, Label: "the first entry"},
	{Action: Bottom, Layer: Browser, Keys: []string{"G"}, Vim: true, Label: "the last entry"},
	{Action: ScreenTop, Layer: Browser, Keys: []string{"H"}, Vim: true, Label: "the first entry in view"},
	{Action: ScreenMid, Layer: Browser, Keys: []string{"M"}, Vim: true, Label: "the middle entry in view"},
	{Action: ScreenBot, Layer: Browser, Keys: []string{"L"}, Vim: true, Label: "the last entry in view"},
	{Action: BrowserDownload, Layer: Browser, Keys: []string{"d"}, Label: "download the file"},
	{Action: BrowserUpload, Layer: Browser, Keys: []string{"u"}, Label: "upload a local file here"},
	{Action: BrowserOpen, Layer: Browser, Keys: []string{"o"}, Label: "open the file locally"},
	{Action: BrowserRename, Layer: Browser, Keys: []string{"R"}, Show: "shift+r", Label: "rename"},
	{Action: BrowserDelete, Layer: Browser, Keys: []string{"x"}, Label: "delete"},
	{Action: BrowserMkdir, Layer: Browser, Keys: []string{"m"}, Label: "new directory"},
	{Action: BrowserSort, Layer: Browser, Keys: []string{"s"}, Label: "sort by name / size / modified"},
	{Action: BrowserMark, Layer: Browser, Keys: []string{"space"}, Label: "mark / unmark the entry"},
	{Action: BrowserMarkAll, Layer: Browser, Keys: []string{"a"}, Label: "mark / unmark everything here"},
	{Action: BrowserTarget, Layer: Browser, Keys: []string{"t"}, Label: "make this directory the target"},
	{Action: BrowserCopy, Layer: Browser, Keys: []string{"c"}, Label: "copy to the target"},
	{Action: BrowserMoveTo, Layer: Browser, Keys: []string{"v"}, Label: "move to the target"},
	{Action: BrowserFocusPane, Layer: Browser, Keys: []string{"tab"}, Label: "focus the content pane"},
	{Action: BrowserTree, Layer: Browser, Keys: []string{"ctrl+t"}, Label: "hide / show the tree column"},
	{Action: BrowserSplit, Layer: Browser, Keys: []string{"\\"}, Label: "open beside the current file"},
	{Action: BrowserRefresh, Layer: Browser, Keys: []string{"r"}, Label: "refresh the listing"},
	{Action: BrowserLeave, Layer: Browser, Keys: []string{"ctrl+o", "esc esc"}, Window: doubleEscWindow, Label: "back to the host list"},
	{Action: BrowserPalette, Layer: Browser, Keys: []string{"ctrl+k"}, Label: "search every action"},
	{Action: BrowserSettings, Layer: Browser, Keys: []string{","}, Label: "settings"},
	{Action: BrowserHelp, Layer: Browser, Keys: []string{"?"}, Label: "all the keys"},

	// ---- shell pane ----
	{Action: LeaderKey, Layer: Pane, Keys: []string{"ctrl+o"}, Label: "hop's keyboard in a pane"},
	{Action: PaneLeave, Layer: Pane, Keys: []string{"esc esc"}, Window: doubleEscWindow, Label: "back to hop"},
	{Action: PaneNewShell, Layer: Pane, Keys: []string{"alt+0"}, Label: "another shell on this host"},
	{Action: PaneNextTab, Layer: Pane, Keys: []string{"shift+right", "alt+right"}, Show: "shift+→", Label: "next shell"},
	{Action: PanePrevTab, Layer: Pane, Keys: []string{"shift+left", "alt+left"}, Show: "shift+←", Label: "previous shell"},
	{Action: PaneScroll, Layer: Pane, Keys: []string{"shift+up"}, Show: "shift+↑", Label: "scroll back through history"},
	{Action: PaneScrollPg, Layer: Pane, Keys: []string{"shift+pgup"}, Label: "scroll back a page"},

	// ---- scrollback ----
	{Action: ScrollUp, Layer: Scrollback, Keys: []string{"up", "shift+up"}, Label: "a line up"},
	{Action: ScrollUp, Layer: Scrollback, Keys: []string{"k"}, Vim: true, Label: "a line up"},
	{Action: ScrollDown, Layer: Scrollback, Keys: []string{"down", "shift+down"}, Label: "a line down"},
	{Action: ScrollDown, Layer: Scrollback, Keys: []string{"j"}, Vim: true, Label: "a line down"},
	{Action: ScrollPageUp, Layer: Scrollback, Keys: []string{"pgup", "shift+pgup"}, Label: "a page up"},
	{Action: ScrollPageDown, Layer: Scrollback, Keys: []string{"pgdown", "shift+pgdown"}, Label: "a page down"},
	{Action: ScrollPageDown, Layer: Scrollback, Keys: []string{"ctrl+f"}, Vim: true, Label: "a page down"},
	{Action: ScrollHalfUp, Layer: Scrollback, Keys: []string{"ctrl+u"}, Vim: true, Label: "half a page up"},
	{Action: ScrollHalfDown, Layer: Scrollback, Keys: []string{"ctrl+d"}, Vim: true, Label: "half a page down"},
	{Action: ScrollTop, Layer: Scrollback, Keys: []string{"home"}, Label: "the oldest line"},
	{Action: ScrollTop, Layer: Scrollback, Keys: []string{"g"}, Vim: true, Label: "the oldest line"},
	{Action: ScrollBottom, Layer: Scrollback, Keys: []string{"end"}, Label: "back to the live shell"},
	{Action: ScrollBottom, Layer: Scrollback, Keys: []string{"G"}, Vim: true, Label: "back to the live shell"},
	{Action: ScrollHelp, Layer: Scrollback, Keys: []string{"?"}, Label: "all the keys"},
	// esc leads: it is the one the legend names, and the one every other mode's way out
	// already is.
	{Action: ScrollLeave, Layer: Scrollback, Keys: []string{"esc", "q", "enter", "i", "ctrl+o", "left", "right"}, Label: "back to the live shell"},

	// ---- editor tab ----
	{Action: LeaderKey, Layer: Editor, Keys: []string{"ctrl+o"}, Label: "hop's keyboard in a pane"},
	{Action: EditorLeave, Layer: Editor, Keys: []string{"esc esc"}, Window: doubleEscWindow, Label: "back to the file browser"},
	{Action: EditorNextTab, Layer: Editor, Keys: []string{"shift+right", "alt+right", "alt+l"}, Show: "shift+→", Label: "next tab"},
	{Action: EditorPrevTab, Layer: Editor, Keys: []string{"shift+left", "alt+left", "alt+h"}, Show: "shift+←", Label: "previous tab"},
	{Action: EditorFocusTree, Layer: Editor, Keys: []string{"alt+t"}, Label: "focus the tree"},
	// ctrl+\ closes the split \ opened: the same key with the modifier the rule about
	// alt chords asks for, so the pair reads as one gesture and its way back is the one
	// thing a user would guess. It is also a key a terminal editor almost never claims —
	// unlike ctrl+w or ctrl+s, which this layer forwards and must keep forwarding.
	{Action: EditorUnsplit, Layer: Editor, Keys: []string{"ctrl+\\"}, Label: "close the split, keep this file"},
	{Action: BrowserTree, Layer: Editor, Keys: []string{"ctrl+t"}, Label: "hide / show the tree column"},

	// ---- leader ----
	{Action: LeaderOut, Layer: Leader, Keys: []string{"o"}, Label: "back to hop"},
	{Action: LeaderShell, Layer: Leader, Keys: []string{"0"}, Label: "another shell on this host"},
	{Action: LeaderVSCode, Layer: Leader, Keys: []string{"c"}, Label: "open this directory in VS Code"},
	{Action: LeaderPalette, Layer: Leader, Keys: []string{"ctrl+k"}, Label: "search every action"},
	{Action: LeaderHelp, Layer: Leader, Keys: []string{"?"}, Label: "all the keys"},

	// ---- dropped session ----
	{Action: DeadReconnect, Layer: DeadPane, Keys: []string{"r", "enter"}, Label: "reconnect and reopen"},
	{Action: DeadDrop, Layer: DeadPane, Keys: []string{"d", "x"}, Label: "drop the session"},
	{Action: DeadLeave, Layer: DeadPane, Keys: []string{"ctrl+o", "esc", "q"}, Label: "back to the host list"},
	{Action: DeadHelp, Layer: DeadPane, Keys: []string{"?"}, Label: "all the keys"},
}

// Defaults returns hop's keyboard as shipped — the map a config with no "keys" object
// produces, and what a settings UI would reset to.
func Defaults() Map { return defaultMap() }

// defaultMap builds the shipped keyboard once. It is also what the zero Map resolves to:
// a Map nobody configured is hop's keyboard, not a keyboard that binds nothing, so a
// caller that forgot to pass one gets working keys rather than a dead window.
var defaultMap = sync.OnceValue(func() Map {
	return build(slices.Clone(defaults))
})

// resolved is the zero-value guard every reader goes through.
func (m Map) resolved() Map {
	if m.byKey == nil {
		return defaultMap()
	}
	return m
}

// Map is a resolved keyboard: the bindings hop shipped with a user's overrides applied.
// It is immutable once built, so every reader — dispatch, the help card, the footer, the
// palette — is looking at the same keyboard.
type Map struct {
	bindings []Binding
	// byKey resolves a keystroke; the string key is a layer and a key, joined.
	byKey map[string]Action
	// prefixes are the first halves of the multi-key sequences, per layer, so a Reader
	// knows to wait rather than resolving "g" as nothing.
	prefixes map[string]bool
}

// Normalize is the one spelling of a keystroke hop uses. Bubble Tea names the space bar
// " ", which cannot be told from the separator between the keys of a sequence, and which
// no legend would print as itself; everywhere inside this package and on a legend it is
// "space".
func Normalize(key string) string {
	if key == " " {
		return "space"
	}
	return key
}

// cutSeq splits a multi-key sequence into its first key and the rest, reporting whether
// it was one. A sequence is written with its keys separated by a space ("g g", "esc esc").
func cutSeq(k string) (head, rest string, ok bool) { return strings.Cut(k, " ") }

// mapKey is the composite key of byKey and prefixes: a layer and a keystroke.
func mapKey(l Layer, key string) string { return l.String() + "\x00" + key }

// New builds a Map from hop's defaults with overrides applied, and returns whatever it
// had to refuse alongside a usable map.
//
// It never fails: a config that cannot be honoured in full still yields a keyboard, with
// the bad rows left at their defaults. Locking someone out of their own hosts over a typo
// in a JSON file is the one outcome worth designing against — the same reason
// config.Load falls back rather than erroring.
//
// An override maps an action id to a key ("browser.download": "y"), or to "" to unbind
// it. A key already spoken for in the same layer is refused; so is an unknown action id,
// and so is any override that would leave a pane with no way out.
func New(overrides map[string]string) (Map, []error) {
	bindings := make([]Binding, len(defaults))
	copy(bindings, defaults)

	var errs []error
	for _, id := range sortedKeys(overrides) {
		// Normalized before trimming: the space bar's own name is " ", which TrimSpace would
		// read as "unbind this".
		key := strings.TrimSpace(Normalize(overrides[id]))
		idx := indexOf(bindings, Action(id))
		if idx < 0 {
			errs = append(errs, fmt.Errorf("keys: no action %q", id))
			continue
		}
		if clash := claimedBy(bindings, bindings[idx].Layer, key, Action(id)); key != "" && clash != None {
			errs = append(errs, fmt.Errorf("keys: %q is already %s in the %s layer",
				key, clash, bindings[idx].Layer))
			continue
		}
		bindings[idx].Keys = nil
		if key != "" {
			bindings[idx].Keys = []string{key}
		}
		// A rebound key is drawn as itself: the symbol was chosen for the default.
		bindings[idx].Show = ""
	}

	if err := escapeHatch(bindings); err != nil {
		errs = append(errs, err)
		return Defaults(), errs
	}
	return build(bindings), errs
}

// build indexes bindings into a Map.
func build(bindings []Binding) Map {
	m := Map{
		bindings: bindings,
		byKey:    make(map[string]Action, len(bindings)*2),
		prefixes: make(map[string]bool),
	}
	for _, b := range bindings {
		for _, k := range b.Keys {
			if head, _, isSeq := cutSeq(k); isSeq {
				m.prefixes[mapKey(b.Layer, head)] = true
			}
			m.byKey[mapKey(b.Layer, k)] = b.Action
		}
	}
	return m
}

// indexOf finds the first binding for an action. An action bound twice — a motion with a
// vim alias on its own row — is rebound through its first row, and the alias row keeps
// its letter, which is what the vim setting is for.
func indexOf(bindings []Binding, a Action) int {
	for i := range bindings {
		if bindings[i].Action == a {
			return i
		}
	}
	return -1
}

// claimedBy reports which other action in this layer already binds key.
func claimedBy(bindings []Binding, l Layer, key string, self Action) Action {
	for _, b := range bindings {
		if b.Layer != l || b.Action == self {
			continue
		}
		for _, k := range b.Keys {
			if k == key {
				return b.Action
			}
		}
	}
	return None
}

// escapeHatch refuses a keyboard that cannot be left. A pane forwards nearly every key to
// the remote program, so the two ways back out of one — the leader and the double-esc —
// are the only keys standing between a user and a config file they have to edit blind.
// Unbinding one is allowed; unbinding both is not.
func escapeHatch(bindings []Binding) error {
	for _, l := range []Layer{Pane, Editor} {
		out := false
		for _, b := range bindings {
			if b.Layer != l || len(b.Keys) == 0 {
				continue
			}
			switch b.Action {
			case LeaderKey, PaneLeave, EditorLeave:
				out = true
			}
		}
		if !out {
			return fmt.Errorf("keys: the %s layer would have no way out; keeping the defaults", l)
		}
	}
	return nil
}

// sortedKeys returns m's keys in order, so a config with two faults reports them the same
// way twice rather than in map order.
func sortedKeys(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// Action resolves one keystroke in one layer, and reports None for a key this layer does
// not bind — which in a pane means "the remote program is owed it".
//
// vim is the "Vim keys" setting: with it off, the keys it owns resolve to None, so those
// letters mean whatever the caller makes of them rather than silently moving a cursor.
// It is passed per call rather than held, so the setting has one home and turning it off
// in the popover is live on the next keystroke.
//
// A multi-key sequence never resolves here — Reader owns the waiting.
func (m Map) Action(l Layer, key string, vim bool) Action {
	m = m.resolved()
	key = Normalize(key)
	a, ok := m.byKey[mapKey(l, key)]
	if !ok {
		return None
	}
	if !vim && m.isVim(l, key) {
		return None
	}
	return a
}

// Vim reports whether key is one the "Vim keys" setting owns anywhere on the keyboard —
// the question a card with its own keymap asks. The settings popover has no motions to
// resolve but still has to honour the setting, being the card that holds the switch.
func (m Map) Vim(key string) bool {
	m = m.resolved()
	key = Normalize(key)
	for _, b := range m.bindings {
		if !b.Vim {
			continue
		}
		for _, k := range b.Keys {
			if k == key || strings.HasPrefix(k, key+" ") {
				return true
			}
		}
	}
	return false
}

// isVim reports whether this key in this layer is one the vim setting owns.
func (m Map) isVim(l Layer, key string) bool {
	m = m.resolved()
	for _, b := range m.bindings {
		if b.Layer != l || !b.Vim {
			continue
		}
		for _, k := range b.Keys {
			if k == key || strings.HasPrefix(k, key+" ") {
				return true
			}
		}
	}
	return false
}

// pending reports whether key starts a sequence in this layer — "g" where "g g" is bound.
func (m Map) pending(l Layer, key string, vim bool) bool {
	m = m.resolved()
	key = Normalize(key)
	if !m.prefixes[mapKey(l, key)] {
		return false
	}
	return vim || !m.isVim(l, key)
}

// Key is the keystroke that runs an action, for the UI to draw. It is the canonical key
// of the action's first binding, "" when the user unbound it.
func (m Map) Key(a Action) string {
	if b, ok := m.Binding(a); ok {
		return b.key()
	}
	return ""
}

// Keycap is Key as the UI draws it, with the symbol for the keys that have one.
func (m Map) Keycap(a Action) string {
	if b, ok := m.Binding(a); ok {
		return b.Keycap()
	}
	return ""
}

// Binding returns an action's first binding.
func (m Map) Binding(a Action) (Binding, bool) {
	m = m.resolved()
	if i := indexOf(m.bindings, a); i >= 0 {
		return m.bindings[i], true
	}
	return Binding{}, false
}

// BindingIn returns an action's binding in one layer. The motions are the reason this is
// not Binding: "in" is a row of the list layer and a row of the browser layer, and which
// key draws it depends on which keyboard is asking.
func (m Map) BindingIn(l Layer, a Action) (Binding, bool) {
	m = m.resolved()
	for _, b := range m.bindings {
		if b.Layer == l && b.Action == a {
			return b, true
		}
	}
	return Binding{}, false
}

// Layer returns the layer's bindings in registry order, skipping unbound ones and — when
// vim is off — the keys that setting owns. It is what the help card, the palette and the
// footer are built from, so none of them can name a key that does not work.
func (m Map) Layer(l Layer, vim bool) []Binding {
	m = m.resolved()
	var out []Binding
	for _, b := range m.bindings {
		if b.Layer != l || len(b.Keys) == 0 || (b.Vim && !vim) {
			continue
		}
		out = append(out, b)
	}
	return out
}
