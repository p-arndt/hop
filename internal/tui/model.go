package tui

import (
	"errors"
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/sahilm/fuzzy"

	"hop/internal/config"
	"hop/internal/filebrowser"
	"hop/internal/keymap"
	"hop/internal/sshx"
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

type model struct {
	st    *store.Store
	hosts []store.Host

	// filtered holds indices into hosts that match the current filter, and
	// highlights holds, per hosts-index, the rune offsets in the alias that the
	// filter matched — so the list can show *why* a row is a hit.
	filtered   []int
	highlights map[int][]int

	cursor int

	// keys resolves the list's motion keys (and holds a half-typed "gg"). What the
	// motions then do to the cursor is model.move.
	keys keymap.Reader

	// lastEsc is when the most recent esc was forwarded to the focused pane.
	// A second esc within doubleEscWindow leaves the pane. Zero means no esc is
	// pending.
	lastEsc time.Time

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

	// keycast is the on-screen trail of recent keys used when recording the demo.
	// It holds nothing and draws nothing unless hop was built with `-tags hopdemo`
	// (see keycast.go / keycast_off.go).
	keycast keycastState

	// active is the alias of the session shown/focused in the right pane
	// ("" means navigation/details mode).
	active string
	// focused is true when keystrokes are forwarded to the active pane.
	focused bool
	// browsing is true when the right pane shows the active session's SFTP file
	// browser and keystrokes are forwarded to it. Mutually exclusive with focused.
	browsing bool
	// editing is true when the right pane shows the active session's editor tabs
	// and keystrokes are forwarded to the open one. Mutually exclusive with both
	// focused and browsing.
	editing bool

	// scrolling is true when the focused shell pane is in scrollback mode: keys drive
	// the history viewport (see handleScrollbackKey) instead of going to the remote
	// shell, and the pane renders ViewScrollback() rather than View(). Only meaningful
	// while focused; entering requires a shell on the main screen with scrollback to show.
	scrolling bool

	// sidebarHidden is true while the host list is collapsed (ctrl+b), giving the
	// right pane the whole window. It is deliberately session-only and not a
	// setting: hop opens on its host list, which is where you start from.
	sidebarHidden bool

	// help is true while the keybinding card is up. Like the settings popover it
	// is modal, and it floats over the screen rather than replacing it.
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
	// settings is the settings popover's own state (cursor, text entry).
	settings settingsUI

	// hostForm is the add/edit host card's own state; confirm is the delete
	// confirmation's. Like settings and help they are modal — each takes every key
	// while it is up — and float over the screen rather than replacing it.
	hostForm hostFormUI
	confirm  confirmUI

	// importer is the SSH-config import card's state. It is modal like the rest, and
	// it is the one card hop opens by itself: a first run with no hosts comes up on
	// it rather than on an empty list telling you to go and run `hop import`.
	importer importUI

	// hostKey is the new-host-key confirmation card's state. It is modal like the
	// others, and it stands in for what used to be a silent trust-on-first-use: an
	// unknown key now pauses the dial here until the user approves the fingerprint.
	hostKey hostKeyUI

	// auth is the interactive-authentication card's state — the 2FA verification
	// code or password a dial is waiting on. It is modal like the rest, but unlike
	// the host-key card it holds a dial *open* rather than replaying it, so it must
	// always answer (see authprompt.go). prompts is the channel dials ask over, with
	// one permanently-armed receiver; authQueue holds challenges from other hosts
	// that arrived while the card was busy.
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
	}
	m.applyFilter()
	// First run: an empty store almost always means the hosts are sitting in an
	// OpenSSH config hop has not been pointed at yet, so offer the import here
	// instead of sending the user back to the shell for `hop import`. The offer is
	// only made when there is a config file to import — with nothing to read, the
	// card would be a dead end and the empty list says what to do instead.
	if len(hosts) == 0 && haveSSHConfig() {
		m.openImport(true)
	}
	p := tea.NewProgram(m, tea.WithAltScreen())
	_, err = p.Run()
	return err
}

func (m *model) Init() tea.Cmd {
	// A single perpetual subscriber to pane output: it blocks until a live pane
	// signals new output, emits a redraw, and re-arms (see the redrawMsg case).
	// Alongside it, a permanent subscriber to authentication challenges (a dial
	// asking for a 2FA code blocks until it is received), and a one-shot update
	// check — off the UI thread, so a slow or unreachable GitHub never delays the
	// first paint.
	return tea.Batch(waitForOutput(m.notify), waitAuthPrompt(m.prompts), updateCheckCmd())
}

// Update dispatches the message and then arms the timer for any status line the
// dispatch put up, so every message that reports something gets its message
// retired without each of them having to remember to say so.
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
		if len(s.editors) == 0 && m.editing && m.active == msg.alias {
			m.leaveEditor()
		}
		return m, nil

	case tea.KeyMsg:
		m.keycastRecord(msg.String())
		return m.handleKey(msg)
	}

	return m, nil
}

// shellLanded merges a newly-started shell into its host's session and focuses
// it.
func (m *model) shellLanded(msg connectedMsg) (tea.Model, tea.Cmd) {
	delete(m.connecting, msg.alias)
	if msg.err != nil {
		// A first-contact host key is no longer trusted silently: pause and ask,
		// carrying the shell intent (S/alt+0 vs a first shell) into the retry.
		var unknown *sshx.UnknownHostKeyError
		if errors.As(msg.err, &unknown) {
			m.openHostKeyConfirm(msg.alias, unknown, hostKeyShell, msg.extra)
			return m, nil
		}
		// The dial is over, so a reconnect's plan has nothing left to land on. Dropping
		// it here keeps a failed reconnect from restoring tabs on some later, ordinary
		// connect to the same host. (The host-key path above returns before this: that
		// dial is about to be replayed, and the plan is what it replays with.)
		m.dropPlan(msg.alias)
		// Dismissing the authentication card already said so; repeating it as a
		// connect failure would make the user's own choice look like a fault.
		if errors.Is(msg.err, sshx.ErrAuthCanceled) {
			return m, nil
		}
		m.setStatus(statusErr, "connect %s failed: %v", msg.alias, msg.err)
		return m, nil
	}
	// Merge the new shell into any existing session (a browser-only one, or one
	// that already has shells) so what is open there survives.
	s := m.sessions[msg.alias]
	if s == nil {
		s = &session{}
		m.sessions[msg.alias] = s
	}
	if msg.client != nil {
		s.client = msg.client
	}
	s.shells = append(s.shells, msg.tab)
	s.activeSh = len(s.shells) - 1
	m.st.Touch(msg.alias)
	m.reloadHosts()

	cmds := []tea.Cmd{waitShellCmd(msg.alias, msg.tab.id, msg.tab.sess)}
	if msg.client != nil {
		// A new connection: watch it, so its loss is noticed rather than leaving a
		// pane that has quietly stopped updating.
		cmds = append(cmds, watchClientCmd(msg.alias, msg.client))
	}

	// A shell a reconnect is putting back lands quietly: the reconnect has already
	// decided where the keyboard goes, and its own status line says what came back.
	if !msg.restore {
		m.focusShell(msg.alias)
		// First contact with this host: say which key was just trusted, so TOFU is
		// at least visible — a fingerprint you can compare beats a silent accept.
		if msg.client != nil && msg.client.NewHostKey != "" {
			m.setStatus(statusWarn, "%s: new host key trusted (%s)", msg.alias, msg.client.NewHostKey)
		} else {
			m.setStatus(statusOK, "connected to %s", msg.alias)
		}
		if cmd := m.applyPlan(msg.alias); cmd != nil {
			cmds = append(cmds, cmd)
		}
	} else {
		m.resizeShells(s)
	}
	return m, tea.Batch(cmds...)
}

// shellExited drops the tab of a shell that has ended, and decides what is left
// of the session behind it.
func (m *model) shellExited(msg shellExitedMsg) (tea.Model, tea.Cmd) {
	s := m.sessions[msg.alias]
	if s == nil {
		return m, nil
	}
	// A shell whose connection has already gone did not exit — it was cut off, along
	// with every other channel on that connection. Treating it as an exit would drop
	// the tab, and the last one would close the session and take the reconnect offer
	// with it. So the loss is recorded instead, and the tabs stay as they were: the
	// last screen the host drew, which is what you want to read when a link drops.
	if s.deadConnection() {
		m.markDead(msg.alias, lostReason(s))
		return m, nil
	}
	if !s.dropShell(msg.id) {
		return m, nil
	}
	m.resizeShells(s)
	if len(s.shells) > 0 {
		return m, nil
	}
	// The last shell exited. Keep the session alive only for what is still open
	// on its connection; with nothing left, the connection is done — closing it
	// is what "exit" meant, and the host goes back to idle in the list.
	if s.browser == nil && len(s.editors) == 0 {
		s.close()
		delete(m.sessions, msg.alias)
		if m.active == msg.alias {
			m.leaveAll()
		}
		return m, nil
	}
	if m.active == msg.alias && m.focused {
		m.focused = false
		m.browsing = s.browser != nil
	}
	return m, nil
}

// browserLanded attaches a newly-opened SFTP browser to its host's session and
// shows it.
func (m *model) browserLanded(msg browserOpenedMsg) (tea.Model, tea.Cmd) {
	delete(m.connecting, msg.alias)
	if msg.err != nil {
		var unknown *sshx.UnknownHostKeyError
		if errors.As(msg.err, &unknown) {
			m.openHostKeyConfirm(msg.alias, unknown, hostKeyBrowser, false)
			return m, nil
		}
		m.dropPlan(msg.alias)
		if errors.Is(msg.err, sshx.ErrAuthCanceled) {
			return m, nil
		}
		m.setStatus(statusErr, "sftp %s failed: %v", msg.alias, msg.err)
		return m, nil
	}
	s := m.sessions[msg.alias]
	if s == nil {
		s = &session{}
		m.sessions[msg.alias] = s
	}
	if s.browser != nil {
		s.browser.Close()
	}
	s.browser = msg.browser
	if msg.client != nil {
		s.client = msg.client
	}
	m.st.Touch(msg.alias)
	m.reloadHosts()

	var cmds []tea.Cmd
	if msg.client != nil {
		cmds = append(cmds, watchClientCmd(msg.alias, msg.client))
	}

	// A browser a reconnect is reattaching goes back on the session without taking
	// the keyboard — the shell the reconnect landed first still has it.
	if !msg.restore {
		m.active = msg.alias
		m.browsing = true
		m.focused = false
		m.editing = false
		if msg.client != nil && msg.client.NewHostKey != "" {
			m.setStatus(statusWarn, "%s: new host key trusted (%s)", msg.alias, msg.client.NewHostKey)
		} else {
			m.setStatus(statusOK, "sftp %s", msg.alias)
		}
		if cmd := m.applyPlan(msg.alias); cmd != nil {
			cmds = append(cmds, cmd)
		}
	}
	msg.browser.Resize(m.paneW, m.paneH)
	return m, tea.Batch(cmds...)
}

// editorLanded shows a newly-started remote editor as a tab.
func (m *model) editorLanded(msg editorOpenedMsg) (tea.Model, tea.Cmd) {
	if msg.err != nil {
		m.setStatus(statusErr, "edit %s failed: %v", msg.alias, msg.err)
		return m, nil
	}
	s := m.sessions[msg.alias]
	if s == nil {
		// The session went away while the editor was starting.
		msg.tab.pane.Close()
		return m, nil
	}
	s.editors = append(s.editors, msg.tab)
	s.activeEd = len(s.editors) - 1
	m.active = msg.alias
	m.editing = true
	m.browsing = false
	m.focused = false
	m.clearStatus()
	ew, eh := m.editorSize()
	msg.tab.pane.Resize(ew, eh)
	return m, waitEditorCmd(msg.alias, msg.tab.id, msg.tab.sess)
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

// ---- hosts & filtering ----

// reloadHosts re-reads the host list, so a connect's bump to visits and
// last-connect shows up in the list's frecency order and in the details card
// without a restart. A read failure leaves the list hop already has.
func (m *model) reloadHosts() {
	// Hold the cursor on the host it is on, even when the new order moved it.
	alias := ""
	if h, ok := m.selectedHost(); ok {
		alias = h.Alias
	}
	m.reloadHostsSelecting(alias)
}

// reloadHostsSelecting re-reads the list and then parks the cursor on the host with
// the given alias, or leaves it clamped in place when that alias is gone (a delete)
// or filtered out. It is the shared body of reloadHosts and the one thing a save
// needs on top of it: land the cursor on a host that was not selected a moment ago
// — a brand-new one, or one whose alias just changed.
func (m *model) reloadHostsSelecting(alias string) {
	if m.st == nil {
		return
	}
	hosts, err := m.st.Hosts()
	if err != nil {
		return
	}
	m.hosts = hosts
	m.applyFilter()
	if alias == "" {
		return
	}
	for i, idx := range m.filtered {
		if m.hosts[idx].Alias == alias {
			m.cursor = i
			return
		}
	}
}

// applyFilter recomputes m.filtered from the current filter text, records which
// characters of each alias matched (for highlighting), and clamps the cursor into
// range.
func (m *model) applyFilter() {
	if m.highlights == nil {
		m.highlights = make(map[int][]int)
	}
	clear(m.highlights)

	if strings.TrimSpace(m.filter) == "" {
		m.filtered = m.filtered[:0]
		for i := range m.hosts {
			m.filtered = append(m.filtered, i)
		}
		m.clampCursor()
		return
	}

	// The haystack is what a host *is* — its alias, its user and its hostname —
	// so "root" finds a host whose alias says nothing about who you log in as.
	hay := make([]string, len(m.hosts))
	for i, h := range m.hosts {
		hay[i] = h.Alias + " " + h.User + " " + h.HostName
	}
	matches := fuzzy.Find(m.filter, hay)

	m.filtered = m.filtered[:0]
	for _, mt := range matches {
		m.filtered = append(m.filtered, mt.Index)
		// Only the offsets that landed in the alias are of any use to the row
		// renderer: it is the one part of the haystack it draws character by
		// character.
		alias := len(m.hosts[mt.Index].Alias)
		var in []int
		for _, at := range mt.MatchedIndexes {
			if at < alias {
				in = append(in, at)
			}
		}
		if len(in) > 0 {
			m.highlights[mt.Index] = in
		}
	}
	m.clampCursor()
}

// selectedHost returns the host under the cursor, or false if the list is empty.
func (m *model) selectedHost() (store.Host, bool) {
	if m.cursor < 0 || m.cursor >= len(m.filtered) {
		return store.Host{}, false
	}
	i := m.filtered[m.cursor]
	if i < 0 || i >= len(m.hosts) {
		return store.Host{}, false
	}
	return m.hosts[i], true
}

// hostByAlias returns the host with this alias. It looks over every host rather
// than the filtered ones, because the modes that ask — a focused pane, a browser
// — are on a host the filter may well have hidden since.
func (m *model) hostByAlias(alias string) (store.Host, bool) {
	for _, h := range m.hosts {
		if h.Alias == alias {
			return h, true
		}
	}
	return store.Host{}, false
}

// clampCursor holds the list cursor inside the filtered host list.
func (m *model) clampCursor() {
	m.cursor = clamp(m.cursor, 0, len(m.filtered)-1)
}

// clamp holds v inside [lo, hi], and returns lo for an empty range.
func clamp(v, lo, hi int) int {
	if hi < lo {
		return lo
	}
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
