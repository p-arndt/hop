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
// redrawMsg. It re-arms on every redraw, so there is always exactly one subscriber.
func waitForOutput(notify chan struct{}) tea.Cmd {
	return func() tea.Msg {
		<-notify
		return redrawMsg{}
	}
}

// updateCheckCmd asks internal/update whether a newer release exists. It runs once at
// startup, off the UI thread; the lookup is cached on disk for a day and bounded by a
// short timeout.
func updateCheckCmd() tea.Cmd {
	return func() tea.Msg {
		return updateAvailableMsg{latest: update.Refresh(buildinfo.Version)}
	}
}

// spinnerRate is how often the connect spinner advances — the only periodic redraw in
// hop, and it stops the moment the last connect lands.
const spinnerRate = 90 * time.Millisecond

// tickCmd advances the spinner one frame.
func tickCmd() tea.Cmd {
	return tea.Tick(spinnerRate, func(t time.Time) tea.Msg { return tickMsg(t) })
}

// statusTTL is how long a status line stays on screen: long enough to read, short enough
// that a stale message is not still claiming to be news.
const statusTTL = 4 * time.Second

// expireStatusCmd retires the status generation it was armed for.
func expireStatusCmd(gen int) tea.Cmd {
	return tea.Tick(statusTTL, func(time.Time) tea.Msg { return statusExpiredMsg{gen: gen} })
}

// cursorBlinkRate is half a blink: the cell goes down for one and comes back for the
// next, which is the rate a terminal blinks its own cursor at.
const cursorBlinkRate = 530 * time.Millisecond

// cursorBlinkCmd arms the next blink frame of the chain it was given.
func cursorBlinkCmd(gen int) tea.Cmd {
	return tea.Tick(cursorBlinkRate, func(time.Time) tea.Msg { return cursorBlinkMsg{gen: gen} })
}

// dragScrollRate is how often a drag held against a pane edge steps the view by a line:
// slow enough to stop where you meant to, fast enough to cross a screen of history.
const dragScrollRate = 60 * time.Millisecond

// dragScrollCmd re-arms the autoscroll of the drag generation it was given.
func dragScrollCmd(gen int) tea.Cmd {
	return tea.Tick(dragScrollRate, func(time.Time) tea.Msg { return dragScrollMsg{gen: gen} })
}

// connectCmd performs the blocking SSH connect and shell start off the UI thread and
// returns a connectedMsg. notify is handed to the pane so its output pump can wake the
// UI. trustedFP is empty for a plain dial and the user-approved fingerprint when retrying
// after a host-key prompt; extra is echoed back so that retry keeps the shell intent.
// prompt answers anything the server asks interactively, from inside the handshake.
func connectCmd(h store.Host, trustedFP string, prompt sshx.Prompter, extra bool, id, cols, rows int, notify chan struct{}) tea.Cmd {
	return func() tea.Msg {
		cli, err := dialClient(h, trustedFP, prompt)
		if err != nil {
			return connectedMsg{alias: h.Alias, extra: extra, err: err}
		}
		tab, err := newShell(cli, h.DefaultDir, id, cols, rows, notify)
		if err != nil {
			cli.Close()
			return connectedMsg{alias: h.Alias, extra: extra, err: err}
		}
		return connectedMsg{alias: h.Alias, client: cli, tab: tab, extra: extra}
	}
}

// dialClient dials h, trusting the given fingerprint when it is non-empty and doing a
// plain TOFU-guarded dial otherwise.
func dialClient(h store.Host, trustedFP string, prompt sshx.Prompter) (*sshx.Client, error) {
	if trustedFP != "" {
		return sshx.ConnectTrusting(h, trustedFP, prompt)
	}
	return sshx.Connect(h, prompt)
}

// shellCmd opens another interactive shell over an already-established client. restore
// marks one being put back by a reconnect, which lands without taking the keyboard.
func shellCmd(alias, startDir string, cli *sshx.Client, id, cols, rows int, notify chan struct{}, restore bool) tea.Cmd {
	return func() tea.Msg {
		tab, err := newShell(cli, startDir, id, cols, rows, notify)
		if err != nil {
			return connectedMsg{alias: alias, restore: restore, err: err}
		}
		return connectedMsg{alias: alias, tab: tab, restore: restore}
	}
}

// watchClientCmd parks on a connection's Lost channel and reports the drop once it
// fires. Nothing else in hop polls for one, so it is armed as soon as a client is
// attached and lives as long as the connection does.
//
// A close from inside hop fires it too. The model sorts that out by identity: the message
// names the connection that died, and a re-dialed session no longer holds it.
func watchClientCmd(alias string, cli *sshx.Client) tea.Cmd {
	return func() tea.Msg {
		<-cli.Lost()
		return sessionLostMsg{alias: alias, client: cli, err: cli.LostErr()}
	}
}

// startTunnelsCmd starts defs over an existing connection, or dials h first when existing
// is nil. The group is atomic: if one listener fails, the ones already opened are closed
// and a newly-dialed client is released.
func startTunnelsCmd(h store.Host, existing *sshx.Client, trustedFP string, prompt sshx.Prompter, defs []store.Forward, restore bool) tea.Cmd {
	ids := make([]int64, len(defs))
	for i, f := range defs {
		ids[i] = f.ID
	}
	return func() tea.Msg {
		cli := existing
		var dialed *sshx.Client
		if cli == nil {
			c, err := dialClient(h, trustedFP, prompt)
			if err != nil {
				return tunnelsStartedMsg{alias: h.Alias, ids: ids, restore: restore, err: err}
			}
			cli, dialed = c, c
		}

		started := make(map[int64]*sshx.Tunnel, len(defs))
		for _, f := range defs {
			tunnel, err := cli.StartForward(f)
			if err != nil {
				for _, running := range started {
					_ = running.Close()
				}
				if dialed != nil {
					_ = dialed.Close()
				}
				return tunnelsStartedMsg{alias: h.Alias, ids: ids, restore: restore, err: err}
			}
			started[f.ID] = tunnel
		}
		return tunnelsStartedMsg{alias: h.Alias, client: dialed, tunnels: started, ids: ids, restore: restore}
	}
}

func watchTunnelCmd(alias string, id int64, tunnel *sshx.Tunnel) tea.Cmd {
	return func() tea.Msg {
		<-tunnel.Done()
		return tunnelStoppedMsg{alias: alias, id: id, tunnel: tunnel, err: tunnel.Err()}
	}
}

// newShell starts a shell on cli and wraps it in a terminal pane. startDir is the host's
// default directory, empty for a host that has none.
func newShell(cli *sshx.Client, startDir string, id, cols, rows int, notify chan struct{}) (*shellTab, error) {
	sess, err := cli.Shell(cols, rows)
	if err != nil {
		return nil, err
	}
	pane := terminal.New(sess, cols, rows, wake(notify))
	// Move the shell to the host's default directory and ask it to report where it
	// stands, so the VS Code binding opens the directory you are in. Both are best effort
	// and asynchronous.
	pane.TrackCwd(cli, startDir)
	return &shellTab{id: id, pane: pane, sess: sess}, nil
}

// wake returns the callback a pane calls when it has parsed new output. Non-blocking, so
// a burst coalesces into a single pending redraw.
func wake(notify chan struct{}) func() {
	return func() {
		select {
		case notify <- struct{}{}:
		default:
		}
	}
}

// waitShellCmd blocks until the remote shell exits, then reports it so its tab can be
// dropped: "exit" is how you close one.
func waitShellCmd(alias string, id int, sess *sshx.Session) tea.Cmd {
	return func() tea.Msg {
		_ = sess.Wait()
		return shellExitedMsg{alias: alias, id: id}
	}
}

// openBrowserCmd opens an SFTP file browser for h off the UI thread, reusing existing's
// connection when it is non-nil and otherwise dialing one (reported back so it can be
// closed later).
//
// startDir is where the browser opens, empty meaning the remote home; a reconnect passes
// the directory the old browser was standing in. restore marks such a reattachment, which
// does not take the keyboard.
func openBrowserCmd(h store.Host, existing *sshx.Client, trustedFP string, prompt sshx.Prompter, opts filebrowser.Options, startDir string, pw, ph int, restore bool) tea.Cmd {
	return func() tea.Msg {
		cli := existing
		var dialed *sshx.Client
		if cli == nil {
			c, err := dialClient(h, trustedFP, prompt)
			if err != nil {
				return browserOpenedMsg{alias: h.Alias, restore: restore, err: err}
			}
			cli = c
			dialed = c
		}

		sc, err := sftpx.Open(cli.SSHClient())
		if err != nil {
			if dialed != nil {
				dialed.Close()
			}
			return browserOpenedMsg{alias: h.Alias, restore: restore, err: err}
		}

		br, err := filebrowser.New(sc, h.Alias, startDir, opts, pw, ph)
		if err != nil {
			sc.Close()
			if dialed != nil {
				dialed.Close()
			}
			return browserOpenedMsg{alias: h.Alias, restore: restore, err: err}
		}

		return browserOpenedMsg{alias: h.Alias, browser: br, client: dialed, restore: restore}
	}
}

// openEditorCmd starts a remote editor on path over the session's existing SSH connection
// and wraps it in a terminal pane. A second channel on the same connection — no handshake
// and no download, so ":w" writes straight back to the server.
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

// waitEditorCmd blocks until the remote editor exits, then reports it so its tab can be
// dropped: quitting the editor is how you close one.
func waitEditorCmd(alias string, id int, sess *sshx.Session) tea.Cmd {
	return func() tea.Msg {
		_ = sess.Wait()
		return editorExitedMsg{alias: alias, id: id}
	}
}

// remoteEditorCmd builds the shell command that runs an editor on path on the remote
// host.
//
// The editor from the settings popover wins, passed through unquoted so it can carry
// flags. With none set, $EDITOR is preferred — but it is often only exported from an
// interactive rc-file a non-interactive SSH command never sources, so an empty one falls
// back to probing the remote PATH. vi is the last resort, since POSIX requires it.
func remoteEditorCmd(editor, path string) string {
	if editor = strings.TrimSpace(editor); editor != "" {
		return "exec " + editor + " " + shellQuote(path)
	}
	return `ed="${EDITOR:-${VISUAL:-}}"; ` +
		`[ -n "$ed" ] || for c in nvim vim vi nano; do ` +
		`command -v "$c" >/dev/null 2>&1 && { ed="$c"; break; }; done; ` +
		`exec ${ed:-vi} ` + shellQuote(path)
}

// shellQuote wraps s in single quotes for a POSIX shell, so spaces and glob characters
// in a filename cannot be read as syntax. A single quote is closed, escaped and reopened.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
