package tui

import (
	"time"

	"hop/internal/filebrowser"
	"hop/internal/sshx"
)

// redrawMsg fires when a live pane has parsed new server output, so the UI
// repaints event-driven rather than on a polling ticker.
type redrawMsg struct{}

// tickMsg drives the connect spinner. It runs only while a connect is in flight:
// an idle hop draws nothing it does not have to.
type tickMsg time.Time

// statusExpiredMsg retires a status line after it has been on screen long enough.
// gen is the status it was armed for, so a message that has since been replaced
// does not take the new one down with it.
type statusExpiredMsg struct{ gen int }

// connectedMsg is returned by the connect command once an SSH shell is ready (or
// has failed). client is non-nil only when this connect dialed a new connection;
// a shell opened on a host that is already connected reuses its client and
// reports only the tab.
type connectedMsg struct {
	alias  string
	client *sshx.Client
	tab    *shellTab
	err    error
}

// shellExitedMsg fires when a remote shell ends ("exit"), so its tab can be
// dropped instead of lingering as a dead pane.
type shellExitedMsg struct {
	alias string
	id    int
}

// browserOpenedMsg is returned by the SFTP-open command once the file browser is
// ready (or has failed). client is non-nil only when a dedicated SSH connection
// was made for browsing (so 'd' knows it must tear it down).
type browserOpenedMsg struct {
	alias   string
	browser *filebrowser.Browser
	client  *sshx.Client
	err     error
}

// editorOpenedMsg is returned once a remote editor is running on its own SSH
// session (or has failed to start).
type editorOpenedMsg struct {
	alias string
	tab   *editorTab
	err   error
}

// editorExitedMsg fires when a remote editor process ends — ":q" is how a tab is
// closed, so hop watches for the exit rather than binding a close key.
type editorExitedMsg struct {
	alias string
	id    int
}
