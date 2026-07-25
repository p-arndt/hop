package tui

import (
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"hop/internal/buildinfo"
	"hop/internal/filebrowser"
	"hop/internal/sftpx"
	"hop/internal/sshx"
	"hop/internal/store"
	"hop/internal/terminal"
	"hop/internal/update"
)

// waitForOutput blocks until a live pane signals new server output, then emits a
// redrawMsg. It re-arms itself on every redraw, so there is always exactly one
// subscriber and repaints happen the instant bytes arrive (no polling latency).
func waitForOutput(notify chan struct{}) tea.Cmd {
	return func() tea.Msg {
		<-notify
		return redrawMsg{}
	}
}

// updateCheckCmd asks internal/update whether a newer release exists. It runs
// once at startup, off the UI thread — the lookup is cached on disk for a day,
// and bounded by a short timeout, so at worst it costs a second of goroutine and
// reports nothing.
func updateCheckCmd() tea.Cmd {
	return func() tea.Msg {
		return updateAvailableMsg{latest: update.Refresh(buildinfo.Version)}
	}
}

// spinnerRate is how often the connect spinner advances. It is the only periodic
// redraw in hop, and it stops the moment the last connect lands.
const spinnerRate = 90 * time.Millisecond

// tickCmd advances the spinner one frame.
func tickCmd() tea.Cmd {
	return tea.Tick(spinnerRate, func(t time.Time) tea.Msg { return tickMsg(t) })
}

// statusTTL is how long a status line stays on screen. Long enough to read,
// short enough that a stale "connected to web1" is not still sitting there ten
// minutes later claiming to be news.
const statusTTL = 4 * time.Second

// expireStatusCmd retires the status generation it was armed for.
func expireStatusCmd(gen int) tea.Cmd {
	return tea.Tick(statusTTL, func(time.Time) tea.Msg { return statusExpiredMsg{gen: gen} })
}

// connectCmd performs the (blocking) SSH connect + shell start off the UI thread
// and returns a connectedMsg. notify is handed to the pane so its output pump can
// wake the UI for an immediate repaint. trustedFP is empty for a plain dial and
// the user-approved fingerprint when retrying after a new-host-key prompt; extra
// is echoed back on the message so that prompt's retry keeps the shell intent.
func connectCmd(h store.Host, trustedFP string, extra bool, id, cols, rows int, notify chan struct{}) tea.Cmd {
	return func() tea.Msg {
		cli, err := dialClient(h, trustedFP)
		if err != nil {
			return connectedMsg{alias: h.Alias, extra: extra, err: err}
		}
		tab, err := newShell(cli, id, cols, rows, notify)
		if err != nil {
			cli.Close()
			return connectedMsg{alias: h.Alias, extra: extra, err: err}
		}
		return connectedMsg{alias: h.Alias, client: cli, tab: tab, extra: extra}
	}
}

// dialClient dials h, trusting the given fingerprint (one the user just approved
// in the host-key card) when it is non-empty, and doing a plain TOFU-guarded dial
// otherwise.
func dialClient(h store.Host, trustedFP string) (*sshx.Client, error) {
	if trustedFP != "" {
		return sshx.ConnectTrusting(h, trustedFP)
	}
	return sshx.Connect(h)
}

// shellCmd opens another interactive shell over an already-established client —
// the connection a browser-only session dialed, or the one the host's other
// shells are already running on.
func shellCmd(alias string, cli *sshx.Client, id, cols, rows int, notify chan struct{}) tea.Cmd {
	return func() tea.Msg {
		tab, err := newShell(cli, id, cols, rows, notify)
		if err != nil {
			return connectedMsg{alias: alias, err: err}
		}
		return connectedMsg{alias: alias, tab: tab}
	}
}

// newShell starts a shell on cli and wraps it in a terminal pane.
func newShell(cli *sshx.Client, id, cols, rows int, notify chan struct{}) (*shellTab, error) {
	sess, err := cli.Shell(cols, rows)
	if err != nil {
		return nil, err
	}
	return &shellTab{id: id, pane: terminal.New(sess, cols, rows, wake(notify)), sess: sess}, nil
}

// wake returns the callback a pane calls when it has parsed new output. It is
// non-blocking: a burst of output coalesces into a single pending redraw.
func wake(notify chan struct{}) func() {
	return func() {
		select {
		case notify <- struct{}{}:
		default:
		}
	}
}

// waitShellCmd blocks until the remote shell exits, then reports it so its tab
// can be dropped — "exit" is how you close a shell tab.
func waitShellCmd(alias string, id int, sess *sshx.Session) tea.Cmd {
	return func() tea.Msg {
		_ = sess.Wait()
		return shellExitedMsg{alias: alias, id: id}
	}
}

// openBrowserCmd opens an SFTP file browser for h off the UI thread. When
// existing is non-nil its SSH connection is reused; otherwise a dedicated
// connection is dialed (and reported back so it can later be closed).
func openBrowserCmd(h store.Host, existing *sshx.Client, trustedFP string, opts filebrowser.Options, pw, ph int) tea.Cmd {
	return func() tea.Msg {
		cli := existing
		var dialed *sshx.Client
		if cli == nil {
			c, err := dialClient(h, trustedFP)
			if err != nil {
				return browserOpenedMsg{alias: h.Alias, err: err}
			}
			cli = c
			dialed = c
		}

		sc, err := sftpx.Open(cli.SSHClient())
		if err != nil {
			if dialed != nil {
				dialed.Close()
			}
			return browserOpenedMsg{alias: h.Alias, err: err}
		}

		br, err := filebrowser.New(sc, "", opts, pw, ph)
		if err != nil {
			sc.Close()
			if dialed != nil {
				dialed.Close()
			}
			return browserOpenedMsg{alias: h.Alias, err: err}
		}

		return browserOpenedMsg{alias: h.Alias, browser: br, client: dialed}
	}
}

// openEditorCmd starts a remote editor on path over the session's existing SSH
// connection and wraps it in a terminal pane. It is a second SSH channel on the
// same connection — no new handshake, and no download: the editor opens the real
// remote file, so ":w" writes straight back to the server.
func openEditorCmd(alias string, cli *sshx.Client, id int, path, name, editor string, cols, rows int, notify chan struct{}) tea.Cmd {
	return func() tea.Msg {
		sess, err := cli.Command(remoteEditorCmd(editor, path), cols, rows)
		if err != nil {
			return editorOpenedMsg{alias: alias, err: err}
		}
		pane := terminal.New(sess, cols, rows, wake(notify))
		return editorOpenedMsg{alias: alias, tab: &editorTab{
			id: id, name: name, path: path, pane: pane, sess: sess,
		}}
	}
}

// waitEditorCmd blocks until the remote editor exits, then reports it so its tab
// can be dropped. Quitting the editor is how you close a tab.
func waitEditorCmd(alias string, id int, sess *sshx.Session) tea.Cmd {
	return func() tea.Msg {
		_ = sess.Wait()
		return editorExitedMsg{alias: alias, id: id}
	}
}

// remoteEditorCmd builds the shell command that runs an editor on path on the
// remote host.
//
// The editor configured in the settings popover wins, and is passed through
// unquoted so it can carry flags ("vim -R"). With none set, $EDITOR is preferred
// — but it is often only exported from an interactive rc-file that a
// non-interactive SSH command never sources, so when it is empty we probe the
// remote PATH ourselves rather than assuming. vi is the last resort because POSIX
// requires it to exist.
func remoteEditorCmd(editor, path string) string {
	if editor = strings.TrimSpace(editor); editor != "" {
		return "exec " + editor + " " + shellQuote(path)
	}
	return `ed="${EDITOR:-${VISUAL:-}}"; ` +
		`[ -n "$ed" ] || for c in nvim vim vi nano; do ` +
		`command -v "$c" >/dev/null 2>&1 && { ed="$c"; break; }; done; ` +
		`exec ${ed:-vi} ` + shellQuote(path)
}

// shellQuote wraps s in single quotes for a POSIX shell, so spaces and glob
// characters in a filename cannot be reinterpreted as syntax. A single quote in
// the name is closed, escaped and reopened — the standard '\” dance.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
