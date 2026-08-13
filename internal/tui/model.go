package tui

import (
	"fmt"
	"sync/atomic"

	tea "github.com/charmbracelet/bubbletea"

	"hop/internal/config"
	"hop/internal/filebrowser"
	"hop/internal/keymap"
	"hop/internal/store"
)

// statusKind is what a status line means, which is what decides its color. It is
// carried alongside the text rather than sniffed back out of it, so a host called
// "failed-over-2" cannot make a successful connect look like an error.
type statusKind int

const (
	statusInfo statusKind = iota
	statusOK
	statusWarn
	statusErr
)

// paneMode says which thing on screen the keyboard belongs to: the host list, or
// one of the four things a session's pane can be. The modes are exclusive by
// construction — moving to one leaves whichever one you were in, and there is no
// way to spell "browsing a file *and* focused on a shell".
//
// Scrollback is its own mode rather than a flag on top of the shell, because a
// pane paused in its history forwards nothing to the far end: it is a different
// keyboard, not the shell's with a modifier. The shell predicates below still
// count it as focused, since the pane is still the one holding the accent.
type paneMode int

const (
	// modeList is the host list (with the details card): no pane has the keyboard.
	modeList paneMode = iota
	// modeShell forwards keys to the active session's visible shell pane.
	modeShell
	// modeScrollback drives the focused shell's history viewport instead of the
	// shell. See handleScrollbackKey.
	modeScrollback
	// modeBrowser forwards keys to the active session's SFTP file browser.
	modeBrowser
	// modeEditor forwards keys to the open remote editor tab.
	modeEditor
)

// The mode predicates. They exist so the routing and rendering switches read as
// questions about the screen ("is a shell focused?") rather than comparisons
// against an enum, and so scrollback can answer to both of the things it is.

// focused reports whether a shell pane holds the keyboard — live or paused in its
// scrollback.
func (m *model) focused() bool { return m.mode == modeShell || m.mode == modeScrollback }

// scrolling reports whether the focused shell is paused in its history.
func (m *model) scrolling() bool { return m.mode == modeScrollback }

// browsing reports whether the SFTP browser holds the keyboard.
func (m *model) browsing() bool { return m.mode == modeBrowser }

// editing reports whether an editor tab holds the keyboard.
func (m *model) editing() bool { return m.mode == modeEditor }

// inPane reports whether any pane holds the keyboard, i.e. the host list does not.
func (m *model) inPane() bool { return m.mode != modeList }

type model struct {
	st    *store.Store
	hosts []store.Host

	// filtered holds indices into hosts that match the current filter, and
	// highlights holds, per hosts-index, the rune offsets in the alias that the
	// filter matched — so the list can show *why* a row is a hit.
	filtered   []int
	highlights map[int][]int

	// rows is what the sidebar actually draws, in order: the host rows of filtered,
	// with the PINNED and HOSTS section headings interleaved once anything is
	// pinned. Everything that has to count rows — the scroll window, the scrollbar,
	// the mouse — works in this space, so a heading is a row to all three or to none
	// of them. The cursor stays an index into filtered: headings are not selectable.
	rows []listRow

	cursor int

	// keys resolves the list's motion keys (and holds a half-typed "gg"). What the
	// motions then do to the cursor is model.move.
	keys keymap.Reader

	// chords is every half-typed key sequence hop is holding. See chordState.
	chords chordState

	// sel is the text selection made with the pointer over a pane — the drag in
	// progress, or the highlight the last one left behind. hop reports the mouse, so
	// the terminal's own selection never happens and this is what stands in for it.
	// See selection.go.
	sel selection

	// filter input state.
	filtering bool
	filter    string

	sessions map[string]*session

	// connecting holds aliases with an in-flight connect (for the spinner).
	connecting map[string]bool

	// pending holds, per alias, what a reconnect in flight has to put back once its
	// new connection lands — the shell tabs and the browser a dropped session was
	// holding. See reconnect.go.
	pending map[string]reconnectPlan

	// notify is signalled by live panes whenever new server output has been
	// parsed, so the UI repaints event-driven (no polling ticker).
	notify chan struct{}

	// paste is the keystroke burst being collected into a paste, and pasteCoalesce
	// says whether this platform needs that at all — only Windows does. See paste.go.
	paste         pasteBuf
	pasteCoalesce bool

	// clipOK mirrors cfg.Clipboard for the panes: a clipboard write arriving from a
	// remote host is handled on the pane's output pump, off this goroutine, so the
	// setting it is checked against cannot be read from cfg directly. It is written
	// here (applyClipboard) and read there. See clipboard.go.
	clipOK atomic.Bool
	// clipWrite overrides how the local clipboard is written. It is nil in a running
	// hop — the real clipboard — and set by tests, which have no business putting
	// anything on the clipboard of the machine they run on.
	clipWrite func(string) error

	// keycast is the on-screen trail of recent keys used when recording the demo.
	// It holds nothing and draws nothing unless hop was built with `-tags hopdemo`
	// (see keycast.go / keycast_off.go).
	keycast keycastState

	// active is the alias of the session shown/focused in the right pane
	// ("" means navigation/details mode).
	active string

	// mode is where the keystrokes are going. It is one value rather than the four
	// bools it replaced, because those were never independent: exactly one of them
	// could be true, nothing enforced that, and every call site that moved between
	// modes had to remember to clear the other three by hand. See paneMode.
	mode paneMode

	// sidebarHidden is true while the host list is collapsed (ctrl+b), giving the
	// right pane the whole window. It is deliberately session-only and not a
	// setting: hop opens on its host list, which is where you start from.
	sidebarHidden bool

	// help is true while the keybinding card is up.
	help bool

	// updateLatest is the newer release the startup check found, or "" — the
	// footer mentions it in navigation mode. The check runs once, off the UI
	// thread, and is cached on disk for a day (see internal/update).
	updateLatest string

	// nextEdID hands out editorTab ids; nextShID hands out shellTab ids.
	nextEdID int
	nextShID int

	// cfg is the user's settings, as loaded at startup and edited in the popover.
	cfg config.Config
	// mouseOn is what hop last asked the *terminal* for, which is not cfg.Mouse until
	// Init has run: reporting is switched on by a sequence, so the setting and the
	// state of the world are tracked separately and reconciled in applyMouse.
	mouseOn bool
	// settings is the settings popover's own state (cursor, text entry).
	settings settingsUI

	// hostForm is the add/edit host card's state; confirm is the delete
	// confirmation's.
	hostForm hostFormUI
	confirm  confirmUI

	// importer is the SSH-config import card's state. It is the one card hop opens by
	// itself: a first run with no hosts comes up on it rather than on an empty list
	// telling you to go and run `hop import`.
	importer importUI

	// tunnels is the per-host forwarding manager. In list mode it starts/stops and
	// removes definitions; in edit mode it owns the five fields of one definition.
	tunnels tunnelUI

	// hostKey is the new-host-key confirmation card's state: an unknown key pauses the
	// dial here until the user approves the fingerprint, rather than being trusted
	// silently on first use.
	hostKey hostKeyUI

	// auth is the interactive-authentication card's state — the 2FA code or password a
	// dial is waiting on. Unlike the host-key card it holds a dial *open* rather than
	// replaying it, so it must always answer (see authprompt.go). prompts is the
	// channel dials ask over, with one permanently-armed receiver; authQueue holds
	// challenges that arrived from other hosts while the card was busy.
	auth      authUI
	prompts   chan authPromptMsg
	authQueue []authPromptMsg

	// status is the message shown in the header. kind colors it; gen identifies
	// it, so the timer that retires it cannot retire its successor. See setStatus.
	status     string
	statusKind statusKind
	statusGen  int

	// frame advances the connect spinner; ticking says the ticker driving it is
	// already running, so a second connect does not start a second one (which
	// would spin at double speed).
	frame   int
	ticking bool

	width  int
	height int

	// paneW/paneH are the last computed inner dimensions of the right pane.
	paneW int
	paneH int

	ready bool
}

// Run builds the model, loads hosts, and runs the Bubble Tea program.
func Run(st *store.Store) error {
	hosts, err := st.Hosts()
	if err != nil {
		return err
	}
	// Settings are advisory: a missing or unreadable config file yields defaults
	// rather than keeping hop from starting.
	cfg := config.Load()
	setAccent(cfg.Accent)

	m := &model{
		st:         st,
		hosts:      hosts,
		sessions:   make(map[string]*session),
		connecting: make(map[string]bool),
		pending:    make(map[string]reconnectPlan),
		highlights: make(map[int][]int),
		notify:     make(chan struct{}, 1),
		prompts:    make(chan authPromptMsg),
		cfg:        cfg,
		// Windows delivers a paste as synthesised keystrokes, so there it has to be
		// recognised by its shape. See paste.go.
		pasteCoalesce: coalescePastes(),
	}
	m.applyClipboard()
	m.applyFilter()
	// First run: an empty store usually means the hosts are in an OpenSSH config hop
	// has not been pointed at yet, so offer the import instead of sending the user
	// back to the shell for `hop import`. Only when there is a config to read — with
	// nothing to import the card would be a dead end.
	if len(hosts) == 0 && haveSSHConfig() {
		m.openImport(true)
	}
	p := tea.NewProgram(m, tea.WithAltScreen())
	_, err = p.Run()
	return err
}

func (m *model) Init() tea.Cmd {
	// A single perpetual subscriber to pane output: blocks until a live pane signals,
	// emits a redraw, re-arms (see the redrawMsg case). Alongside it a permanent
	// subscriber to auth challenges, and a one-shot update check off the UI thread so
	// a slow GitHub never delays the first paint.
	//
	// The mouse is switched on here rather than as a program option, so applyMouse is
	// the one path deciding it at startup and on every later settings change.
	return tea.Batch(waitForOutput(m.notify), waitAuthPrompt(m.prompts), updateCheckCmd(), m.applyMouse())
}

// Update dispatches the message, then arms the expiry timer for any status line the
// dispatch put up — so no handler has to remember to retire its own message.
func (m *model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	gen := m.statusGen
	next, cmd := m.update(msg)
	if m.statusGen != gen && m.status != "" {
		cmd = tea.Batch(cmd, expireStatusCmd(m.statusGen))
	}
	return next, cmd
}

func (m *model) update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.ready = true
		m.recomputeLayout()
		m.resizeAll()
		return m, nil

	case redrawMsg:
		// Re-arm the single output subscriber. View() is called by Bubble Tea
		// right after this returns, repainting the latest emulator state.
		return m, waitForOutput(m.notify)

	case tickMsg:
		m.frame++
		if len(m.connecting) == 0 {
			// Nothing is dialing any more: stop the only clock hop runs.
			m.ticking = false
			return m, nil
		}
		return m, tickCmd()

	case updateAvailableMsg:
		// Purely informational: it colors a footer hint and nothing else, so an
		// empty version (no update, or the check failed) simply leaves it off.
		m.updateLatest = msg.latest
		return m, nil

	case statusExpiredMsg:
		// Only if it is still the message this timer was armed for.
		if msg.gen == m.statusGen {
			m.status = ""
		}
		return m, nil

	case authPromptMsg:
		// Re-arm the single challenge subscriber before anything else: the card
		// this opens can queue behind another one, and a second host's dial must
		// not be left with nobody listening.
		m.openAuthPrompt(msg)
		return m, waitAuthPrompt(m.prompts)

	case connectedMsg:
		return m.shellLanded(msg)

	case shellExitedMsg:
		return m.shellExited(msg)

	case sessionLostMsg:
		return m.sessionLost(msg)

	case tunnelsStartedMsg:
		return m.tunnelsLanded(msg)

	case tunnelStoppedMsg:
		return m.tunnelStopped(msg)

	case browserOpenedMsg:
		return m.browserLanded(msg)

	case filebrowser.OpenFileMsg:
		return m, m.openFile(msg)

	case editorOpenedMsg:
		return m.editorLanded(msg)

	case editorExitedMsg:
		s := m.sessions[msg.alias]
		if s == nil {
			return m, nil
		}
		// Like a shell's exit: on a connection that has gone, this is the channel being
		// cut, not ":q". See shellExited.
		if s.deadConnection() {
			m.markDead(msg.alias, lostReason(s))
			return m, nil
		}
		if !s.dropEditor(msg.id) {
			return m, nil
		}
		// The last tab closing drops back where the file was opened from.
		if len(s.editors) == 0 && m.editing() && m.active == msg.alias {
			m.leaveEditor()
		}
		return m, nil

	case pasteFlushMsg:
		// Only if nothing has been typed since this flush was armed: a key that
		// arrived in between armed one of its own, and the burst has not ended yet.
		if msg.seq == m.paste.seq {
			m.flushPaste()
		}
		return m, nil

	case tea.KeyMsg:
		m.keycastRecord(msg.String())
		return m.handleKey(msg)

	case tea.MouseMsg:
		// The pointer moving is the burst over: whatever it does next — switching
		// tabs, standing on a host, reaching a remote program — must land after the
		// keys that preceded it.
		m.flushPaste()
		return m.handleMouse(msg)
	}

	return m, nil
}

// ---- status ----

// setStatus puts a message in the header and stamps it with a fresh generation,
// which is what Update uses to arm its expiry. The text is stripped of control
// characters: statuses routinely embed remote-derived strings (file names, SFTP
// and SSH error texts), and an escape sequence smuggled into one would be
// interpreted by the user's terminal rather than displayed.
func (m *model) setStatus(kind statusKind, format string, args ...any) {
	m.status = stripControl(fmt.Sprintf(format, args...))
	m.statusKind = kind
	m.statusGen++
}

// clearStatus takes the message down now. Bumping the generation is what stops a
// timer already in flight from firing against whatever comes next.
func (m *model) clearStatus() {
	m.status = ""
	m.statusKind = statusInfo
	m.statusGen++
}

// withSpinner starts the spinner clock alongside cmd, unless it is already
// running — two tickers would drive the frame counter at double speed.
func (m *model) withSpinner(cmd tea.Cmd) tea.Cmd {
	if m.ticking {
		return cmd
	}
	m.ticking = true
	return tea.Batch(cmd, tickCmd())
}
