package tui

// Aliased: the tui package has an action type of its own (actions.go).
import actionpkg "hop/internal/action"

// openVSCode is a variable so a test can observe the path hop asks VS Code to open.
var openVSCode = actionpkg.OpenVSCodeRemote

// openVSCodeAt opens VS Code Remote on the host, in the directory its shell is standing in;
// with no live shell or no reported directory it falls back to the host's default directory.
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

// shellCwd is where the host's visible shell stands, or "" when there is nothing to ask.
// A dead session's panes are pictures, so their directory is where the shell *was*.
func (m *model) shellCwd(alias string) string {
	s := m.sessions[alias]
	if s == nil || s.dead || s.shell() == nil {
		return ""
	}
	return s.shell().pane.Cwd()
}
