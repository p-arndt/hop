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

// statusKind is what a status line means, and so its color. Carried alongside the text
// rather than sniffed back out of it, so a host called "failed-over-2" cannot make a
// successful connect look like an error.
type statusKind int

const (
	statusInfo statusKind = iota
	statusOK
	statusWarn
	statusErr
)

// paneMode says which thing on screen the keyboard belongs to: the host list, or one of
// the four things a session's pane can be. The modes are exclusive by construction.
//
// Scrollback is its own mode rather than a flag on the shell, because a pane paused in
// its history forwards nothing to the far end. The predicates below still count it as
// focused, since the pane still holds the accent.
type paneMode int

const (
	// modeList is the host list (with the details card): no pane has the keyboard.
	modeList paneMode = iota
	// modeShell forwards keys to the active session's visible shell pane.
	modeShell
	// modeScrollback drives the focused shell's history viewport. See handleScrollbackKey.
	modeScrollback
	// modeBrowser forwards keys to the active session's SFTP file browser.
	modeBrowser
	// modeEditor forwards keys to the open remote editor tab.
	modeEditor
)

// The mode predicates, so the routing and rendering switches read as questions about
// the screen rather than comparisons against an enum.

// focused reports whether a shell pane holds the keyboard, live or in scrollback.
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

	// filtered holds indices into hosts matching the current filter; highlights holds,
	// per hosts-index, the rune offsets the filter matched, so the list can show why a
	// row is a hit.
	filtered   []int
	highlights map[int][]int

	// rows is what the sidebar draws, in order: the host rows of filtered with the
	// PINNED and HOSTS headings interleaved. Everything that counts rows — scroll
	// window, scrollbar, mouse — works in this space. The cursor stays an index into
	// filtered: headings are not selectable.
	rows []listRow

	cursor int

	// keys resolves the list's motion keys and holds a half-typed "gg". What they do to
	// the cursor is model.move.
	keys keymap.Reader

	// chords is every half-typed key sequence hop is holding. See chordState.
	chords chordState

	// sel is the text selection made with the pointer over a pane. hop reports the mouse,
	// so the terminal's own selection never happens and this stands in for it. See
	// selection.go.
	sel selection

	// filter input state.
	filtering bool
	filter    string

	sessions map[string]*session

	// connecting holds aliases with an in-flight connect (for the spinner).
	connecting map[string]bool

	// pending holds, per alias, what a reconnect in flight has to put back once its new
	// connection lands. See reconnect.go.
	pending map[string]reconnectPlan

	// notify is signalled by live panes when new server output has been parsed, so the
	// UI repaints event-driven rather than on a ticker.
	notify chan struct{}

	// paste is the keystroke burst being collected into a paste, and pasteCoalesce
	// says whether this platform needs that at all — only Windows does. See paste.go.
	paste         pasteBuf
	pasteCoalesce bool

	// clipOK mirrors cfg.Clipboard for the panes: a remote clipboard write is handled on
	// the pane's output pump, off this goroutine, so cfg cannot be read there directly.
	// Written by applyClipboard. See clipboard.go.
	clipOK atomic.Bool
	// clipWrite overrides how the local clipboard is written: nil in a running hop, set
	// by tests, which have no business writing the clipboard of the machine they run on.
	clipWrite func(string) error

	// keycast is the on-screen trail of recent keys used when recording the demo. It
	// holds and draws nothing unless built with `-tags hopdemo`.
	keycast keycastState

	// active is the alias of the session shown/focused in the right pane
	// ("" means navigation/details mode).
	active string

	// mode is where the keystrokes are going — one value rather than four bools that
	// were never independent and had to be cleared by hand. See paneMode.
	mode paneMode

	// sidebarHidden is true while the host list is collapsed (ctrl+b). Session-only and
	// not a setting: hop opens on its host list, which is where you start from.
	sidebarHidden bool

	// help is true while the keybinding card is up.
	help bool

	// updateLatest is the newer release the startup check found, or "". The check runs
	// once off the UI thread and is cached on disk for a day (see internal/update).
	updateLatest string

	// nextEdID hands out editorTab ids; nextShID hands out shellTab ids.
	nextEdID int
	nextShID int

	// cfg is the user's settings, as loaded at startup and edited in the popover.
	cfg config.Config
	// mouseOn is what hop last asked the terminal for, which is not cfg.Mouse until Init
	// has run. The two are reconciled in applyMouse.
	mouseOn bool
	// settings is the settings popover's own state (cursor, text entry).
	settings settingsUI

	// hostForm is the add/edit host card's state; confirm is the delete
	// confirmation's.
	hostForm hostFormUI
	confirm  confirmUI

	// importer is the SSH-config import card's state — the one card hop opens by itself,
	// on a first run with no hosts.
	importer importUI

	// tunnels is the per-host forwarding manager. In list mode it starts/stops and
	// removes definitions; in edit mode it owns the five fields of one definition.
	tunnels tunnelUI

	// hostKey is the new-host-key confirmation card's state: an unknown key pauses the
	// dial until the user approves the fingerprint.
	hostKey hostKeyUI

	// auth is the interactive-authentication card's state — the 2FA code or password a
	// dial is waiting on. Unlike the host-key card it holds a dial open rather than
	// replaying it, so it must always answer (see authprompt.go). prompts is the channel
	// dials ask over; authQueue holds challenges that arrived while the card was busy.
	auth      authUI
	prompts   chan authPromptMsg
	authQueue []authPromptMsg

	// status is the message shown in the header. kind colors it; gen identifies it, so
	// the timer retiring it cannot retire its successor. See setStatus.
	status     string
	statusKind statusKind
	statusGen  int

	// frame advances the connect spinner; ticking says its ticker is already running, so
	// a second connect does not start a second one.
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
	// Settings are advisory: an unreadable config yields defaults rather than an error.
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
		// Windows delivers a paste as synthesised keystrokes. See paste.go.
		pasteCoalesce: coalescePastes(),
	}
	m.applyClipboard()
	m.applyFilter()
	// First run: an empty store usually means the hosts are in an OpenSSH config hop has
	// not been pointed at yet. Only offered when there is a config to read.
	if len(hosts) == 0 && haveSSHConfig() {
		m.openImport(true)
	}
	p := tea.NewProgram(m, tea.WithAltScreen())
	_, err = p.Run()
	return err
}

func (m *model) Init() tea.Cmd {
	// A single perpetual subscriber to pane output: blocks until a live pane signals,
	// emits a redraw, re-arms (see redrawMsg). Alongside it a permanent subscriber to
	// auth challenges, and a one-shot update check off the UI thread.
	//
	// The mouse is switched on here rather than as a program option, so applyMouse is the
	// one path deciding it at startup and on every later settings change.
	return tea.Batch(waitForOutput(m.notify), waitAuthPrompt(m.prompts), updateCheckCmd(), m.applyMouse())
}

// Update dispatches the message, then arms the expiry timer for any status line it put
// up, so no handler has to retire its own message.
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
		// Re-arm the single output subscriber; View() runs right after this returns.
		return m, waitForOutput(m.notify)

	case tickMsg:
		m.frame++
		if len(m.connecting) == 0 {
			// Nothing is dialing: stop the only clock hop runs.
			m.ticking = false
			return m, nil
		}
		return m, tickCmd()

	case updateAvailableMsg:
		// Informational only: an empty version leaves the footer hint off.
		m.updateLatest = msg.latest
		return m, nil

	case statusExpiredMsg:
		// Only if it is still the message this timer was armed for.
		if msg.gen == m.statusGen {
			m.status = ""
		}
		return m, nil

	case authPromptMsg:
		// Re-arm the subscriber first: this card can queue behind another one, and a
		// second host's dial must not be left with nobody listening.
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
		// On a connection that has gone this is the channel being cut, not ":q". See
		// shellExited.
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
		// Only if nothing was typed since this flush was armed: a key in between armed
		// one of its own.
		if msg.seq == m.paste.seq {
			m.flushPaste()
		}
		return m, nil

	case tea.KeyMsg:
		m.keycastRecord(msg.String())
		return m.handleKey(msg)

	case tea.MouseMsg:
		// The pointer moving ends the burst: whatever it does next must land after the
		// keys that preceded it.
		m.flushPaste()
		return m.handleMouse(msg)
	}

	return m, nil
}

// ---- status ----

// setStatus puts a message in the header and stamps it with a fresh generation, which
// is what Update uses to arm its expiry. The text is stripped of control characters:
// statuses embed remote-derived strings, and an escape sequence in one would be
// interpreted by the user's terminal rather than displayed.
func (m *model) setStatus(kind statusKind, format string, args ...any) {
	m.status = stripControl(fmt.Sprintf(format, args...))
	m.statusKind = kind
	m.statusGen++
}

// clearStatus takes the message down now. Bumping the generation stops a timer already
// in flight from firing against whatever comes next.
func (m *model) clearStatus() {
	m.status = ""
	m.statusKind = statusInfo
	m.statusGen++
}

// withSpinner starts the spinner clock alongside cmd, unless it is already running.
func (m *model) withSpinner(cmd tea.Cmd) tea.Cmd {
	if m.ticking {
		return cmd
	}
	m.ticking = true
	return tea.Batch(cmd, tickCmd())
}
