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

// waitForOutput re-arms on every redraw, so there is always exactly one subscriber.
func waitForOutput(notify chan struct{}) tea.Cmd {
	return func() tea.Msg {
		<-notify
		return redrawMsg{}
	}
}

func updateCheckCmd() tea.Cmd {
	return func() tea.Msg {
		return updateAvailableMsg{latest: update.Refresh(buildinfo.Version)}
	}
}

const spinnerRate = 90 * time.Millisecond

func tickCmd() tea.Cmd {
	return tea.Tick(spinnerRate, func(t time.Time) tea.Msg { return tickMsg(t) })
}

const statusTTL = 4 * time.Second

func expireStatusCmd(gen int) tea.Cmd {
	return tea.Tick(statusTTL, func(time.Time) tea.Msg { return statusExpiredMsg{gen: gen} })
}

// cursorBlinkRate is half a blink period, matching a terminal's own cursor.
const cursorBlinkRate = 530 * time.Millisecond

func cursorBlinkCmd(gen int) tea.Cmd {
	return tea.Tick(cursorBlinkRate, func(time.Time) tea.Msg { return cursorBlinkMsg{gen: gen} })
}

const dragScrollRate = 60 * time.Millisecond

func dragScrollCmd(gen int) tea.Cmd {
	return tea.Tick(dragScrollRate, func(time.Time) tea.Msg { return dragScrollMsg{gen: gen} })
}

// connectCmd dials off the UI thread; trustedFP is empty for a plain dial and set when
// retrying after a host-key prompt, and extra is echoed back so the retry keeps its intent.
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

func dialClient(h store.Host, trustedFP string, prompt sshx.Prompter) (*sshx.Client, error) {
	if trustedFP != "" {
		return sshx.ConnectTrusting(h, trustedFP, prompt)
	}
	return sshx.Connect(h, prompt)
}

// shellCmd opens another shell on an existing client; restore marks one put back by a
// reconnect, which lands without taking the keyboard.
func shellCmd(alias, startDir string, cli *sshx.Client, id, cols, rows int, notify chan struct{}, restore bool) tea.Cmd {
	return func() tea.Msg {
		tab, err := newShell(cli, startDir, id, cols, rows, notify)
		if err != nil {
			return connectedMsg{alias: alias, restore: restore, err: err}
		}
		return connectedMsg{alias: alias, tab: tab, restore: restore}
	}
}

// watchClientCmd names the connection that died, so the model can ignore a drop for a
// client the session has since re-dialed away from.
func watchClientCmd(alias string, cli *sshx.Client) tea.Cmd {
	return func() tea.Msg {
		<-cli.Lost()
		return sessionLostMsg{alias: alias, client: cli, err: cli.LostErr()}
	}
}

// startTunnelsCmd is atomic: one failed listener closes those already opened and releases
// a client it dialed itself.
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

func newShell(cli *sshx.Client, startDir string, id, cols, rows int, notify chan struct{}) (*shellTab, error) {
	sess, err := cli.Shell(cols, rows)
	if err != nil {
		return nil, err
	}
	pane := terminal.New(sess, cols, rows, wake(notify))
	// Best effort and async: tracks the shell's cwd so the VS Code binding opens it.
	pane.TrackCwd(cli, startDir)
	return &shellTab{id: id, pane: pane, sess: sess}, nil
}

// wake is non-blocking, so a burst of output coalesces into a single pending redraw.
func wake(notify chan struct{}) func() {
	return func() {
		select {
		case notify <- struct{}{}:
		default:
		}
	}
}

func waitShellCmd(alias string, id int, sess *sshx.Session) tea.Cmd {
	return func() tea.Msg {
		_ = sess.Wait()
		return shellExitedMsg{alias: alias, id: id}
	}
}

// openBrowserCmd reuses existing's connection when non-nil, else dials one and reports it
// back so it can be closed later; restore marks a reattachment that skips the keyboard.
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

// openEditorCmd runs the editor on a second channel of the same connection, so ":w" writes
// straight back to the server.
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

func waitEditorCmd(alias string, id int, sess *sshx.Session) tea.Cmd {
	return func() tea.Msg {
		_ = sess.Wait()
		return editorExitedMsg{alias: alias, id: id}
	}
}

// remoteEditorCmd passes a configured editor through unquoted so it can carry flags;
// $EDITOR is often unset under non-interactive SSH, hence the PATH probe and the vi floor.
func remoteEditorCmd(editor, path string) string {
	if editor = strings.TrimSpace(editor); editor != "" {
		return "exec " + editor + " " + shellQuote(path)
	}
	return `ed="${EDITOR:-${VISUAL:-}}"; ` +
		`[ -n "$ed" ] || for c in nvim vim vi nano; do ` +
		`command -v "$c" >/dev/null 2>&1 && { ed="$c"; break; }; done; ` +
		`exec ${ed:-vi} ` + shellQuote(path)
}

// shellQuote wraps s in single quotes for a POSIX shell.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
