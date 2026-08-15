package tui

import "hop/internal/action"

// openVSCode is the action the VS Code bindings run. It is a variable rather than
// a direct call so a test can see what hop asks VS Code to open — the path is the
// whole feature, and there is no other way to observe it without a VS Code on the
// machine running the tests.
var openVSCode = action.OpenVSCodeRemote

// openVSCodeAt opens VS Code Remote on the host, in the directory its shell is
// standing in. That is the point of it: `code --remote` on its own lands in the
// host's default directory, which is rarely the one you were just working in.
//
// A host with no live shell — or one whose shell reports no directory, because it
// is a shell hop could not install the prompt hook into — falls back to exactly
// that default-directory open. It is what the binding did before it learned about
// directories, so the worst case is the old behaviour, said out loud in the status
// line rather than passed off as the new one.
func (m *model) openVSCodeAt(alias string) {
	dir := m.shellCwd(alias)
	if err := openVSCode(alias, dir); err != nil {
		m.setStatus(statusErr, "vscode: %v", err)
		return
	}
	if dir == "" {
		m.setStatus(statusOK, "opening VS Code remote → %s (default dir)", alias)
		return
	}
	m.setStatus(statusOK, "opening VS Code remote → %s:%s", alias, dir)
}

// shellCwd is where the host's visible shell stands, or "" when there is nothing
// to ask: no session, a session whose connection has dropped (its panes are
// pictures, and the directory in them is where the shell *was*), no shell, or a
// shell that has reported no directory.
func (m *model) shellCwd(alias string) string {
	s := m.sessions[alias]
	if s == nil || s.dead || s.shell() == nil {
		return ""
	}
	return s.shell().pane.Cwd()
}
