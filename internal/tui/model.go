package tui

import (
	"fmt"
	"sync/atomic"
	"time"

	tea "charm.land/bubbletea/v2"

	"hop/internal/config"
	"hop/internal/filebrowser"
	"hop/internal/keys"
	"hop/internal/store"
)

// statusKind is carried alongside the text so a status's color is never sniffed back out of it.
type statusKind int

const (
	statusInfo statusKind = iota
	statusOK
	statusWarn
	statusErr
)

// paneMode says where the keystrokes go, and only that — not what is drawn.
type paneMode int

const (
	modeList paneMode = iota
	modeShell
	// modeScrollback is its own mode because a pane paused in its history forwards nothing to the far end.
	modeScrollback
	modeBrowser
	modeEditor
)

func (f *focus) focused() bool { return f.mode == modeShell || f.mode == modeScrollback }

func (f *focus) scrolling() bool { return f.mode == modeScrollback }

func (f *focus) browsing() bool { return f.mode == modeBrowser }

func (f *focus) editing() bool { return f.mode == modeEditor }

// inPane reports whether any column holds the keyboard, i.e. the host list does not.
func (f *focus) inPane() bool { return f.mode != modeList }

// layout is embedded so m.width and m.paneW keep reading as before; only a resize or a column toggle writes it.
type layout struct {
	// frame is a cache: nothing writes it but recomputeLayout, and View re-derives it before measuring.
	frame frame
	// sidebarHidden and treeHidden are session-only, never persisted settings.
	sidebarHidden bool
	treeHidden    bool
	width         int
	height        int
	paneW         int
	paneH         int
	ready         bool
}

// focus is where the keyboard is and what the pointer is holding. Also embedded.
type focus struct {
	chords chordState
	// sel stands in for the terminal's own selection, which never happens because hop reports the mouse.
	sel selection
	// dragGen numbers the autoscroll chains a drag starts, so a tick armed for a stale edge is dropped.
	dragGen int
	// active is the alias of the session shown in the right pane ("" means navigation/details mode).
	active string
	mode   paneMode
}

type model struct {
	layout
	focus

	st    *store.Store
	hosts []store.Host

	// filtered holds indices into hosts; highlights holds, per hosts-index, the matched rune offsets.
	filtered   []int
	highlights map[int][]int

	// rows is the sidebar's draw order with headings interleaved; the cursor stays an index into filtered.
	rows []listRow

	cursor int

	// binds is the keys registry with the user's config applied; handlers and legends both resolve against it.
	binds keys.Map

	// reader holds whatever sequence is half-typed. One per model: two readers would each hold half a chord.
	reader keys.Reader

	filtering bool
	filter    string

	sessions map[string]*session

	connecting map[string]bool

	// pending holds, per alias, what a reconnect in flight has to put back. See reconnect.go.
	pending map[string]reconnectPlan

	// notify is signalled by live panes so the UI repaints event-driven rather than on a ticker.
	notify chan struct{}

	// pasteCoalesce is only true on Windows, which delivers a paste as synthesised keystrokes. See paste.go.
	paste         pasteBuf
	pasteCoalesce bool
	// clock is nil in a running hop; tests set it, since they cannot type at human speed.
	clock func() time.Time

	// clipOK mirrors cfg.Clipboard for the panes, whose output pump cannot read cfg off this goroutine.
	clipOK atomic.Bool
	// clipWrite is nil in a running hop; tests set it rather than write the real machine's clipboard.
	clipWrite func(string) error

	// keycast holds and draws nothing unless built with `-tags hopdemo`.
	keycast keycastState

	help bool
	// helpScroll is clamped by renderHelp, since only the drawing knows how long the body came out.
	helpScroll int

	// updateLatest is the newer release the startup check found, or "".
	updateLatest string

	nextEdID int
	nextShID int

	cfg config.Config
	// mouseOn is what the terminal was last asked for, which is not cfg.Mouse until Init has run.
	mouseOn  bool
	settings settingsUI

	hostForm hostFormUI
	confirm  confirmUI

	// guidance is up only on an install that has never written a config file.
	guidance guidanceUI

	palette paletteUI
	menu    menuUI

	importer importUI

	tunnels tunnelUI

	hostKey hostKeyUI

	// auth holds a dial open rather than replaying it, so it must always answer (see authprompt.go).
	auth      authUI
	prompts   chan authPromptMsg
	authQueue []authPromptMsg

	// statusGen identifies the message, so the timer retiring it cannot retire its successor.
	status     string
	statusKind statusKind
	statusGen  int

	// ticking says the spinner ticker is already running, so a second connect does not start a second one.
	spinFrame int
	ticking   bool

	// blinkGen numbers the blink chain, so the setting switched off and on again cannot leave two clocks.
	cursorUp bool
	blinking bool
	blinkGen int
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
		st:            st,
		hosts:         hosts,
		sessions:      make(map[string]*session),
		connecting:    make(map[string]bool),
		pending:       make(map[string]reconnectPlan),
		highlights:    make(map[int][]int),
		notify:        make(chan struct{}, 1),
		prompts:       make(chan authPromptMsg),
		cfg:           cfg,
		binds:         keys.Defaults(),
		pasteCoalesce: coalescePastes(),
	}
	m.applyClipboard()
	m.applyFilter()
	// Guidance chains into the import card, so the two never stand on top of each other (see answerGuidance).
	switch {
	case !config.Exists():
		m.openGuidance()
	case len(hosts) == 0 && haveSSHConfig():
		m.openImport(true)
	}
	// Failing to update ~/.ssh/config costs only the ssh/scp integration, so it warns rather than refuses to start.
	if err := st.IncludeWarning(); err != nil {
		m.setStatus(statusWarn, "hosts saved, but ~/.ssh/config was not updated: %v", err)
	}
	// 120fps: the default 60 puts up to 16ms between a keystroke's echo arriving and the screen showing it.
	// The alt screen is a property of the view in v2; only the frame rate is an option.
	p := tea.NewProgram(m, tea.WithFPS(120))
	_, err = p.Run()
	return err
}

func (m *model) Init() tea.Cmd {
	// The mouse is view state in v2, so View is the only path deciding it.
	return tea.Batch(waitForOutput(m.notify), waitAuthPrompt(m.prompts), updateCheckCmd(), m.applyCursorBlink())
}

// Update dispatches the message, then arms the expiry timer for any status line it put up.
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
		m.relayout()
		return m, nil

	case redrawMsg:
		// Re-arm the single output subscriber; View() runs right after this returns.
		return m, waitForOutput(m.notify)

	case tickMsg:
		m.spinFrame++
		if len(m.connecting) == 0 {
			m.ticking = false
			return m, nil
		}
		return m, tickCmd()

	case updateAvailableMsg:
		m.updateLatest = msg.latest
		return m, nil

	case dragScrollMsg:
		return m, m.dragScrollTick(msg.gen)

	case cursorBlinkMsg:
		return m, m.cursorBlinkTick(msg.gen)

	case statusExpiredMsg:
		// Only if it is still the message this timer was armed for.
		if msg.gen == m.statusGen {
			m.status = ""
		}
		return m, nil

	case authPromptMsg:
		// Re-arm the subscriber first: a second host's dial must not be left with nobody listening.
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

	case filebrowser.Msg:
		s := m.sessions[msg.Alias]
		if s == nil || s.browser == nil {
			return m, nil
		}
		if open, ok := msg.Body.(filebrowser.OpenFileMsg); ok {
			return m, m.openFile(msg.Alias, open)
		}
		return m, s.browser.Update(msg)

	case editorOpenedMsg:
		return m.editorLanded(msg)

	case editorExitedMsg:
		s := m.sessions[msg.alias]
		if s == nil {
			return m, nil
		}
		// On a connection that has gone this is the channel being cut, not ":q". See shellExited.
		if s.deadConnection() {
			m.markDead(msg.alias, lostReason(s))
			return m, nil
		}
		if !s.dropEditor(msg.id) {
			return m, nil
		}
		// A tab closing out of a split may also have collapsed it, so the halves are re-measured either way.
		if len(s.editors) == 0 && m.editing() && m.active == msg.alias {
			m.leaveEditor()
		}
		m.relayout()
		return m, nil

	case pasteFlushMsg:
		// Only if nothing was typed since this flush was armed: a key in between armed one of its own.
		if msg.seq == m.paste.seq {
			m.flushPaste()
		}
		return m, nil

	// Presses only: tea.KeyMsg is an interface in v2 and a release satisfies it too.
	case tea.KeyPressMsg:
		m.keycastRecord(msg.String())
		return m.handleKey(msg)

	// A terminal that brackets its pastes says so; the Windows console does not, which is
	// what the burst detection in paste.go is for.
	case tea.PasteMsg:
		m.flushPaste()
		return m.handlePaste(msg.Content)

	case tea.MouseMsg:
		// The pointer moving ends the burst: whatever it does next must land after the keys that preceded it.
		m.flushPaste()
		return m.handleMouse(toMouseEvt(msg))
	}

	return m, nil
}

// ---- status ----

// setStatus stamps a fresh generation and strips control characters, since statuses embed remote-derived strings.
func (m *model) setStatus(kind statusKind, format string, args ...any) {
	m.status = stripControl(fmt.Sprintf(format, args...))
	m.statusKind = kind
	m.statusGen++
}

// reportInput warns on a refused write: dropped keystrokes must never be silent. See terminal.Pane.send.
func (m *model) reportInput(sent bool) {
	if sent {
		return
	}
	m.setStatus(statusWarn, "%s is not reading input: keystrokes dropped", m.active)
}

// clearStatus bumps the generation too, so a timer in flight cannot fire against whatever comes next.
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
