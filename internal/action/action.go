// Package action provides side-effecting integrations with external tools
// such as VS Code Remote and Windows Terminal.
package action

import "os/exec"

// OpenVSCodeRemote launches VS Code connected to the given SSH host alias via
// the Remote-SSH extension. If remotePath is non-empty it is opened as the
// target folder/file; otherwise VS Code opens without a specific path.
func OpenVSCodeRemote(alias, remotePath string) error {
	args := []string{"/c", "code", "--remote", "ssh-remote+" + alias}
	if remotePath != "" {
		args = append(args, remotePath)
	}
	return exec.Command("cmd", args...).Start()
}

// NewTab opens a new Windows Terminal tab running the given command in pwsh,
// keeping the shell open afterwards.
func NewTab(cmd string) error {
	return exec.Command("wt.exe", "-w", "0", "nt", "pwsh", "-NoExit", "-Command", cmd).Start()
}
