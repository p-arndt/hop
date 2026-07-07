package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/sahilm/fuzzy"

	"hop/internal/action"
	"hop/internal/sshx"
	"hop/internal/store"
	"hop/internal/terminal"
)

// accent is the primary highlight color used across the UI.
var accent = lipgloss.Color("212")

// ---- styles ----

var (
	footerStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("245")).
			Padding(0, 1)

	statusStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("203")).
			Padding(0, 1)

	statusOkStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("42")).
			Padding(0, 1)

	statusInfoStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("245")).
			Padding(0, 1)

	selectedAliasStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(accent)

	leftBorder = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("240"))

	leftBorderActive = lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				BorderForeground(accent)

	rightBorder = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("240"))

	rightBorderActive = lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				BorderForeground(accent)

	aliasStyle = lipgloss.NewStyle().Bold(true)

	dimStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("245"))

	connectedDot = lipgloss.NewStyle().Foreground(lipgloss.Color("42")).Render("●")
	idleDot      = lipgloss.NewStyle().Foreground(lipgloss.Color("240")).Render("○")

	titleStyle = lipgloss.NewStyle().Bold(true).Foreground(accent)
)

// ---- redesigned palette & helpers ----

var (
	headerBadge = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("16")).Background(accent).Padding(0, 1)
	subtitle    = lipgloss.NewStyle().Foreground(lipgloss.Color("245"))

	chipStyle   = lipgloss.NewStyle().Bold(true).Foreground(accent).Background(lipgloss.Color("238")).Padding(0, 1)
	keycapStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("231")).Background(lipgloss.Color("238")).Padding(0, 1)

	listTitle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("245"))
	faint     = lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	kvKey     = lipgloss.NewStyle().Foreground(lipgloss.Color("245"))

	greenText  = lipgloss.NewStyle().Foreground(lipgloss.Color("42"))
	yellowText = lipgloss.NewStyle().Foreground(lipgloss.Color("214"))

	selBar        = lipgloss.NewStyle().Foreground(accent).Render("▎")
	connectingDot = lipgloss.NewStyle().Foreground(lipgloss.Color("214")).Render("◐")
)

// kc renders a keycap "pill" for legends and help bars.
func kc(key string) string { return keycapStyle.Render(key) }

// clampLines truncates every line to w cells so styled content can never wrap and
// break out of its bordered box.
func clampLines(s string, w int) string {
	lines := strings.Split(s, "\n")
	for i, ln := range lines {
		lines[i] = truncate(ln, w)
	}
	return strings.Join(lines, "\n")
}

// session bundles a live SSH client with its embedded terminal pane.
type session struct {
	client *sshx.Client
	pane   *terminal.Pane
}

// ---- messages ----

// redrawMsg fires on a ticker so asynchronously-updated panes repaint.
type redrawMsg struct{}

// connectedMsg is returned by the connect command once the SSH shell is ready
// (or has failed).
type connectedMsg struct {
	alias string
	sess  *session
	err   error
}

// ---- model ----

type model struct {
	st    *store.Store
	hosts []store.Host

	// filtered holds indices into hosts that match the current filter.
	filtered []int

	cursor int

	// filter input state.
	filtering bool
	filter    string

	sessions map[string]*session

	// connecting holds aliases with an in-flight connect (for a spinner dot).
	connecting map[string]bool

	// notify is signalled by live panes whenever new server output has been
	// parsed, so the UI repaints event-driven (no polling ticker).
	notify chan struct{}

	// active is the alias of the session shown/focused in the right pane
	// ("" means navigation/details mode).
	active string
	// focused is true when keystrokes are forwarded to the active pane.
	focused bool

	status string

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
	m := &model{
		st:         st,
		hosts:      hosts,
		sessions:   make(map[string]*session),
		connecting: make(map[string]bool),
		notify:     make(chan struct{}, 1),
	}
	m.applyFilter()
	p := tea.NewProgram(m, tea.WithAltScreen())
	_, err = p.Run()
	return err
}

func (m *model) Init() tea.Cmd {
	// A single perpetual subscriber to pane output: it blocks until a live pane
	// signals new output, emits a redraw, and re-arms (see the redrawMsg case).
	return waitForOutput(m.notify)
}

// ---- filtering ----

// applyFilter recomputes m.filtered from the current filter text and clamps the
// cursor into range.
func (m *model) applyFilter() {
	if strings.TrimSpace(m.filter) == "" {
		m.filtered = m.filtered[:0]
		for i := range m.hosts {
			m.filtered = append(m.filtered, i)
		}
	} else {
		hay := make([]string, len(m.hosts))
		for i, h := range m.hosts {
			hay[i] = h.Alias + " " + h.User + " " + h.HostName
		}
		matches := fuzzy.Find(m.filter, hay)
		m.filtered = m.filtered[:0]
		for _, mt := range matches {
			m.filtered = append(m.filtered, mt.Index)
		}
	}
	if m.cursor >= len(m.filtered) {
		m.cursor = len(m.filtered) - 1
	}
	if m.cursor < 0 {
		m.cursor = 0
	}
}

// selectedHost returns the host under the cursor, or false if the list is empty.
func (m *model) selectedHost() (store.Host, bool) {
	if m.cursor < 0 || m.cursor >= len(m.filtered) {
		return store.Host{}, false
	}
	return m.hosts[m.filtered[m.cursor]], true
}

// ---- commands ----

// waitForOutput blocks until a live pane signals new server output, then emits a
// redrawMsg. It re-arms itself on every redraw, so there is always exactly one
// subscriber and repaints happen the instant bytes arrive (no polling latency).
func waitForOutput(notify chan struct{}) tea.Cmd {
	return func() tea.Msg {
		<-notify
		return redrawMsg{}
	}
}

// connectCmd performs the (blocking) SSH connect + shell start off the UI thread
// and returns a connectedMsg. notify is handed to the pane so its output pump can
// wake the UI for an immediate repaint.
func connectCmd(h store.Host, cols, rows int, notify chan struct{}) tea.Cmd {
	return func() tea.Msg {
		cli, err := sshx.Connect(h)
		if err != nil {
			return connectedMsg{alias: h.Alias, err: err}
		}
		sess, err := cli.Shell(cols, rows)
		if err != nil {
			cli.Close()
			return connectedMsg{alias: h.Alias, err: err}
		}
		onOutput := func() {
			// Non-blocking: coalesce bursts into a single pending redraw.
			select {
			case notify <- struct{}{}:
			default:
			}
		}
		pane := terminal.New(sess, cols, rows, onOutput)
		return connectedMsg{alias: h.Alias, sess: &session{client: cli, pane: pane}}
	}
}

// ---- update ----

func (m *model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.ready = true
		m.recomputeLayout()
		// Resize every live pane to the new right-pane inner size.
		for _, s := range m.sessions {
			s.pane.Resize(m.paneW, m.paneH)
		}
		return m, nil

	case redrawMsg:
		// Re-arm the single output subscriber. View() is called by Bubble Tea
		// right after this returns, repainting the latest emulator state.
		return m, waitForOutput(m.notify)

	case connectedMsg:
		delete(m.connecting, msg.alias)
		if msg.err != nil {
			m.status = fmt.Sprintf("connect %s failed: %v", msg.alias, msg.err)
			return m, nil
		}
		m.sessions[msg.alias] = msg.sess
		m.st.Touch(msg.alias)
		m.active = msg.alias
		m.focused = true
		m.status = "connected to " + msg.alias
		// Match the pane to the current layout; repaints are event-driven.
		msg.sess.pane.Resize(m.paneW, m.paneH)
		return m, nil

	case tea.KeyMsg:
		return m.handleKey(msg)
	}

	return m, nil
}

func (m *model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// Pane-focused mode: forward everything to the pane except ctrl+o.
	if m.focused && m.active != "" {
		if msg.String() == "ctrl+o" {
			m.focused = false
			m.status = ""
			return m, nil
		}
		if s := m.sessions[m.active]; s != nil {
			s.pane.SendKey(msg)
		}
		return m, nil
	}

	// Filter-entry mode.
	if m.filtering {
		switch msg.String() {
		case "esc":
			m.filtering = false
			m.filter = ""
			m.applyFilter()
		case "enter":
			m.filtering = false
			m.applyFilter()
		case "backspace":
			if len(m.filter) > 0 {
				m.filter = m.filter[:len(m.filter)-1]
			}
			m.applyFilter()
		default:
			if len(msg.Runes) > 0 {
				m.filter += string(msg.Runes)
				m.applyFilter()
			}
		}
		return m, nil
	}

	// Navigation mode.
	switch msg.String() {
	case "q", "ctrl+c":
		m.closeAll()
		return m, tea.Quit

	case "up", "k":
		if m.cursor > 0 {
			m.cursor--
		}

	case "down", "j":
		if m.cursor < len(m.filtered)-1 {
			m.cursor++
		}

	case "/":
		m.filtering = true
		m.filter = ""
		m.applyFilter()

	case "esc":
		// Leave the details/active view, back to plain navigation.
		m.active = ""
		m.status = ""

	case "enter":
		h, ok := m.selectedHost()
		if !ok {
			return m, nil
		}
		if s, live := m.sessions[h.Alias]; live {
			// Already connected: just focus it.
			m.active = h.Alias
			m.focused = true
			s.pane.Resize(m.paneW, m.paneH)
			return m, nil
		}
		m.status = "connecting to " + h.Alias + "…"
		m.connecting[h.Alias] = true
		return m, connectCmd(h, m.paneW, m.paneH, m.notify)

	case "s":
		h, ok := m.selectedHost()
		if !ok {
			return m, nil
		}
		if s, live := m.sessions[h.Alias]; live {
			m.active = h.Alias
			m.focused = true
			s.pane.Resize(m.paneW, m.paneH)
			return m, nil
		}
		m.status = "no live session for " + h.Alias

	case "o":
		h, ok := m.selectedHost()
		if !ok {
			return m, nil
		}
		if err := action.OpenVSCodeRemote(h.Alias, ""); err != nil {
			m.status = "vscode: " + err.Error()
		} else {
			m.status = "opening VS Code remote → " + h.Alias
		}

	case "d":
		h, ok := m.selectedHost()
		if !ok {
			return m, nil
		}
		if s, live := m.sessions[h.Alias]; live {
			s.pane.Close()
			s.client.Close()
			delete(m.sessions, h.Alias)
			if m.active == h.Alias {
				m.active = ""
				m.focused = false
			}
			m.status = "disconnected " + h.Alias
		} else {
			m.status = "no live session for " + h.Alias
		}
	}

	return m, nil
}

// closeAll tears down every live session.
func (m *model) closeAll() {
	for _, s := range m.sessions {
		s.pane.Close()
		s.client.Close()
	}
	m.sessions = make(map[string]*session)
	m.active = ""
	m.focused = false
}

// recomputeLayout derives the left/right pane inner sizes from the window size.
func (m *model) recomputeLayout() {
	if m.width <= 0 || m.height <= 0 {
		return
	}
	// Reserve one row for header and one for footer.
	bodyH := m.height - 2
	if bodyH < 3 {
		bodyH = 3
	}
	// Left list gets a fixed-ish width; right pane gets the rest.
	leftW := 30
	if leftW > m.width/2 {
		leftW = m.width / 2
	}
	if leftW < 16 {
		leftW = 16
	}
	// Account for borders (2 cols each side, 2 rows each pane).
	rightInnerW := m.width - leftW - 4 - 2
	if rightInnerW < 10 {
		rightInnerW = 10
	}
	rightInnerH := bodyH - 2
	if rightInnerH < 3 {
		rightInnerH = 3
	}
	m.paneW = rightInnerW
	m.paneH = rightInnerH
}

// ---- view ----

func (m *model) View() string {
	if !m.ready {
		return "loading hop…"
	}

	bodyH := m.height - 2
	if bodyH < 3 {
		bodyH = 3
	}
	leftW := 30
	if leftW > m.width/2 {
		leftW = m.width / 2
	}
	if leftW < 16 {
		leftW = 16
	}

	left := m.renderList(leftW, bodyH)
	right := m.renderRight(bodyH)

	body := lipgloss.JoinHorizontal(lipgloss.Top, left, right)

	header := m.renderHeader()
	footer := m.renderFooter()

	return lipgloss.JoinVertical(lipgloss.Left, header, body, footer)
}

func (m *model) renderHeader() string {
	left := lipgloss.JoinHorizontal(lipgloss.Center,
		headerBadge.Render("hop"),
		subtitle.Render(" ssh manager"),
	)

	var chips []string
	if m.focused && m.active != "" {
		chips = append(chips, greenText.Bold(true).Render("● "+m.active))
	}
	if n := len(m.sessions); n > 0 {
		word := "session"
		if n > 1 {
			word = "sessions"
		}
		chips = append(chips, chipStyle.Render(fmt.Sprintf("%d %s", n, word)))
	}
	if st := m.styledStatus(); st != "" {
		chips = append(chips, st)
	}
	right := strings.Join(chips, " ")

	gap := m.width - lipgloss.Width(left) - lipgloss.Width(right)
	if gap < 0 {
		gap = 0
	}
	return lipgloss.JoinHorizontal(lipgloss.Center, left, strings.Repeat(" ", gap), right)
}

// styledStatus colors the status line by meaning: green for success, red for
// failures/disconnects, dim for transient info, empty when there is none.
func (m *model) styledStatus() string {
	if m.status == "" {
		return ""
	}
	switch {
	case strings.HasPrefix(m.status, "connected"):
		return statusOkStyle.Render(m.status)
	case strings.Contains(m.status, "failed"),
		strings.HasPrefix(m.status, "disconnected"),
		strings.HasPrefix(m.status, "no live"),
		strings.HasPrefix(m.status, "vscode:"):
		return statusStyle.Render(m.status)
	default:
		return statusInfoStyle.Render(m.status)
	}
}

func (m *model) renderList(w, h int) string {
	// Inner content width/height (inside the border).
	innerW := w - 2
	innerH := h - 2
	if innerW < 4 {
		innerW = 4
	}
	if innerH < 1 {
		innerH = 1
	}

	var b strings.Builder

	// Section title with host count.
	title := listTitle.Render("HOSTS")
	if len(m.hosts) > 0 {
		title += faint.Render(fmt.Sprintf("  %d", len(m.hosts)))
	}
	b.WriteString(truncate(title, innerW))
	b.WriteString("\n")
	innerH--

	if m.filtering || m.filter != "" {
		prompt := faint.Render("/") + m.filter
		if m.filtering {
			prompt += "▏"
		}
		b.WriteString(truncate(prompt, innerW))
		b.WriteString("\n")
		innerH--
	}

	if innerH < 1 {
		innerH = 1
	}

	if len(m.hosts) == 0 {
		b.WriteString(dimStyle.Render(truncate("No hosts yet.", innerW)))
		b.WriteString("\n\n")
		b.WriteString(faint.Render(truncate("Run: hop import", innerW)))
	} else if len(m.filtered) == 0 {
		b.WriteString(faint.Render(truncate("no matches", innerW)))
	} else {
		// Simple scroll window so the cursor stays visible.
		start := 0
		if m.cursor >= innerH {
			start = m.cursor - innerH + 1
		}
		end := start + innerH
		if end > len(m.filtered) {
			end = len(m.filtered)
		}
		for i := start; i < end; i++ {
			h := m.hosts[m.filtered[i]]
			b.WriteString(m.renderRow(h, i == m.cursor, innerW))
			if i < end-1 {
				b.WriteString("\n")
			}
		}
	}

	style := leftBorder
	if !m.focused && m.active == "" {
		style = leftBorderActive
	}
	return style.Width(innerW).Height(h - 2).Render(b.String())
}

// dotFor returns the status dot for a host: green connected, yellow connecting,
// dim idle.
func (m *model) dotFor(alias string) string {
	if _, live := m.sessions[alias]; live {
		return connectedDot
	}
	if m.connecting[alias] {
		return connectingDot
	}
	return idleDot
}

func (m *model) renderRow(h store.Host, selected bool, w int) string {
	dot := m.dotFor(h.Alias)
	who := h.User
	if who != "" {
		who += "@"
	}
	who += h.HostName

	tag := ""
	if h.Group != "" {
		tag = " " + faint.Render("["+h.Group+"]")
	} else if len(h.Tags) > 0 {
		tag = " " + faint.Render("#"+h.Tags[0])
	}

	// A leading accent bar + bright bold alias marks the selection (no full-width
	// background block, which nests badly with the inner styles).
	if selected {
		line := selBar + " " + dot + " " +
			selectedAliasStyle.Render(h.Alias) + "  " + dimStyle.Render(who) + tag
		return truncate(line, w)
	}
	line := "  " + dot + " " + aliasStyle.Render(h.Alias) + "  " + dimStyle.Render(who) + tag
	return truncate(line, w)
}

func (m *model) renderRight(h int) string {
	innerH := h - 2
	if innerH < 1 {
		innerH = 1
	}

	active := m.focused && m.active != ""
	style := rightBorder
	if active {
		style = rightBorderActive
	}

	var content string
	if s, ok := m.sessions[m.active]; ok && m.active != "" {
		content = s.pane.View()
	} else {
		content = m.renderDetails(m.paneW)
	}

	return style.Width(m.paneW).Height(innerH).Render(content)
}

func (m *model) renderDetails(w int) string {
	h, ok := m.selectedHost()
	if !ok {
		return "\n" + dimStyle.Render("  Select a host on the left.")
	}

	port := h.Port
	if port == 0 {
		port = 22
	}

	// Status badge.
	badge := idleDot + " " + dimStyle.Render("idle")
	switch {
	case m.sessions[h.Alias] != nil:
		badge = connectedDot + " " + greenText.Render("connected")
	case m.connecting[h.Alias]:
		badge = connectingDot + " " + yellowText.Render("connecting…")
	}

	const pad = "  "
	rule := faint.Render(strings.Repeat("─", min(max(w-4, 0), 34)))

	var b strings.Builder
	b.WriteString("\n")
	b.WriteString(pad + titleStyle.Render(h.Alias) + "   " + badge + "\n")
	b.WriteString(pad + rule + "\n\n")

	writeKV := func(k, v string) {
		if v == "" {
			return
		}
		b.WriteString(pad + kvKey.Render(fmt.Sprintf("%-9s", k)) + v + "\n")
	}
	writeKV("host", fmt.Sprintf("%s:%d", h.HostName, port))
	writeKV("user", h.User)
	writeKV("identity", h.IdentityFile)
	if h.Group != "" {
		writeKV("group", h.Group)
	}
	if len(h.Tags) > 0 {
		writeKV("tags", strings.Join(h.Tags, ", "))
	}
	writeKV("visits", fmt.Sprintf("%d", h.Visits))

	b.WriteString("\n")
	b.WriteString(pad + dimStyle.Render("actions") + "\n")
	b.WriteString(pad + kc("enter") + " " + dimStyle.Render("connect") + "   " +
		kc("s") + " " + dimStyle.Render("focus") + "\n")
	b.WriteString(pad + kc("o") + " " + dimStyle.Render("vscode") + "    " +
		kc("d") + " " + dimStyle.Render("disconnect") + "\n")

	return clampLines(b.String(), w)
}

func (m *model) renderFooter() string {
	sep := "  "
	item := func(k, label string) string { return kc(k) + " " + dimStyle.Render(label) }

	var help string
	switch {
	case m.focused && m.active != "":
		help = item("ctrl+o", "back to hop") + sep +
			dimStyle.Render("keys → ") + greenText.Render(m.active)
	case m.filtering:
		help = item("type", "filter") + sep + item("enter", "apply") + sep + item("esc", "clear")
	default:
		help = item("↑↓", "move") + sep +
			item("enter", "connect") + sep +
			item("s", "session") + sep +
			item("o", "code") + sep +
			item("d", "disconnect") + sep +
			item("/", "filter") + sep +
			item("q", "quit")
	}
	return footerStyle.Render(truncate(help, m.width-2))
}

// truncate shortens s (measured by display width) to at most w cells, adding an
// ellipsis when it must cut.
func truncate(s string, w int) string {
	if w <= 0 {
		return ""
	}
	if lipgloss.Width(s) <= w {
		return s
	}
	// Cut rune-by-rune until it fits, leaving room for the ellipsis.
	target := w - 1
	if target < 0 {
		target = 0
	}
	var b strings.Builder
	for _, r := range s {
		if lipgloss.Width(b.String()+string(r)) > target {
			break
		}
		b.WriteRune(r)
	}
	return b.String() + "…"
}
