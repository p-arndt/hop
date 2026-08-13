package tui

import (
	"time"

	"hop/internal/filebrowser"
	"hop/internal/sshx"
)

// redrawMsg fires when a live pane has parsed new server output, so the UI
// repaints event-driven rather than on a polling ticker.
type redrawMsg struct{}

// tickMsg drives the connect spinner, and runs only while a connect is in flight.
type tickMsg time.Time

// updateAvailableMsg carries the result of the startup update check: the newer release's
// version, or "" when hop is current or the check did not run.
type updateAvailableMsg struct{ latest string }

// statusExpiredMsg retires a status line. gen is the status it was armed for, so a
// message that has since been replaced does not take the new one down with it.
type statusExpiredMsg struct{ gen int }

// connectedMsg is returned by the connect command once an SSH shell is ready or has
// failed. client is non-nil only when this connect dialed a new connection; a shell on an
// already-connected host reuses its client and reports only the tab.
type connectedMsg struct {
	alias  string
	client *sshx.Client
	tab    *shellTab
	// extra carries the shell intent through a dial, so a retry after the host-key prompt
	// knows whether it was for another shell or a host's first. Unused once it lands.
	extra bool
	// restore marks a shell being put back after a reconnect. It lands quietly, taking
	// neither the keyboard nor the status line.
	restore bool
	err     error
}

// authPromptMsg is a question a dial in flight needs answered before it can finish. It is
// the only message carrying a channel back to its sender: the dial is parked on reply
// inside the handshake, so exactly one value must be sent or the connect never lands.
// See authprompt.go.
type authPromptMsg struct {
	alias string
	ch    sshx.Challenge
	reply chan authReply
}

// shellExitedMsg fires when a remote shell ends, so its tab can be dropped instead of
// lingering as a dead pane.
type shellExitedMsg struct {
	alias string
	id    int
}

// browserOpenedMsg is returned by the SFTP-open command once the file browser is ready or
// has failed. client is non-nil only when a dedicated connection was made for browsing,
// so 'd' knows it must tear it down.
type browserOpenedMsg struct {
	alias   string
	browser *filebrowser.Browser
	client  *sshx.Client
	// restore marks a browser being put back after a reconnect: it reattaches without
	// taking the keyboard.
	restore bool
	err     error
}

// sessionLostMsg says the SSH connection under a session has gone. client identifies
// which connection died, since the message can arrive long after the session it belonged
// to was replaced or torn down.
type sessionLostMsg struct {
	alias  string
	client *sshx.Client
	err    error
}

// tunnelsStartedMsg is the result of starting forwarding listeners. client is non-nil
// only when the command had to establish the host connection.
type tunnelsStartedMsg struct {
	alias   string
	client  *sshx.Client
	tunnels map[int64]*sshx.Tunnel
	ids     []int64
	restore bool
	err     error
}

// tunnelStoppedMsg reports a listener ending on its own. A deliberate stop removes it
// from the session map first, so the model ignores that stale watcher.
type tunnelStoppedMsg struct {
	alias  string
	id     int64
	tunnel *sshx.Tunnel
	err    error
}

// editorOpenedMsg is returned once a remote editor is running on its own SSH session, or
// has failed to start.
type editorOpenedMsg struct {
	alias string
	tab   *editorTab
	err   error
}

// editorExitedMsg fires when a remote editor ends: ":q" is how a tab is closed, so hop
// watches for the exit rather than binding a close key.
type editorExitedMsg struct {
	alias string
	id    int
}
