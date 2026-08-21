package tui

import (
	"time"

	"hop/internal/filebrowser"
	"hop/internal/sshx"
)

// redrawMsg fires when a live pane has parsed new server output.
type redrawMsg struct{}

// tickMsg drives the connect spinner, and runs only while a connect is in flight.
type tickMsg time.Time

// dragScrollMsg repeats one line of autoscroll during an edge drag; gen is the drag chain
// it was armed for, so leftover ticks are dropped.
type dragScrollMsg struct{ gen int }

// cursorBlinkMsg is one frame of the cursor blink. gen is the chain it belongs to, so
// toggling the setting off and on does not leave two clocks running out of step.
type cursorBlinkMsg struct{ gen int }

// updateAvailableMsg carries the update check's result; "" when current or not run.
type updateAvailableMsg struct{ latest string }

// statusExpiredMsg retires a status line; gen keeps it from retiring a replacement.
type statusExpiredMsg struct{ gen int }

// connectedMsg lands once an SSH shell is ready or failed; client is non-nil only when
// this connect dialed a new connection.
type connectedMsg struct {
	alias  string
	client *sshx.Client
	tab    *shellTab
	// extra carries the shell intent through a dial, so a host-key retry knows which.
	extra bool
	// restore marks a shell put back after a reconnect: no keyboard, no status line.
	restore bool
	err     error
}

// authPromptMsg parks a dial inside the handshake: exactly one value must be sent on
// reply or the connect never lands. See authprompt.go.
type authPromptMsg struct {
	alias string
	ch    sshx.Challenge
	reply chan authReply
}

// shellExitedMsg fires when a remote shell ends, so its tab can be dropped.
type shellExitedMsg struct {
	alias string
	id    int
}

// browserOpenedMsg is the SFTP-open result; client is non-nil only when a dedicated
// connection was made for browsing, so 'd' knows to tear it down.
type browserOpenedMsg struct {
	alias   string
	browser *filebrowser.Browser
	client  *sshx.Client
	// restore marks a browser put back after a reconnect: it does not take the keyboard.
	restore bool
	err     error
}

// sessionLostMsg says an SSH connection has gone; client says which, as it can arrive
// after the session was replaced.
type sessionLostMsg struct {
	alias  string
	client *sshx.Client
	err    error
}

// tunnelsStartedMsg is the result of starting listeners; client is non-nil only when the
// command had to establish the host connection.
type tunnelsStartedMsg struct {
	alias   string
	client  *sshx.Client
	tunnels map[int64]*sshx.Tunnel
	ids     []int64
	restore bool
	err     error
}

// tunnelStoppedMsg reports a listener ending on its own; a deliberate stop unmaps it
// first, so the model ignores that stale watcher.
type tunnelStoppedMsg struct {
	alias  string
	id     int64
	tunnel *sshx.Tunnel
	err    error
}

// editorOpenedMsg is returned once a remote editor is running, or has failed to start.
type editorOpenedMsg struct {
	alias string
	tab   *editorTab
	err   error
}

// editorExitedMsg fires when a remote editor ends; ":q" closes a tab, not a hop key.
type editorExitedMsg struct {
	alias string
	id    int
}
