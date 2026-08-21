// Package action provides side-effecting integrations with external tools.
package action

import (
	"os/exec"
	"strings"
)

// OpenVSCodeRemote launches VS Code on the SSH host alias via Remote-SSH.
// "code" is run directly, not through "cmd /c", so metacharacters in an alias are never re-parsed.
func OpenVSCodeRemote(alias, remotePath string) error {
	return exec.Command("code", vscodeArgs(alias, remotePath)...).Start()
}

// vscodeArgs builds OpenVSCodeRemote's argument list.
func vscodeArgs(alias, remotePath string) []string {
	args := []string{"--remote", "ssh-remote+" + alias}
	if remotePath != "" {
		args = append(args, remotePath)
	}
	return args
}

// NewTab opens a new Windows Terminal tab running program with args in pwsh.
func NewTab(program string, args ...string) error {
	return exec.Command("wt.exe", newTabArgs(program, args)...).Start()
}

// newTabArgs builds the wt.exe argument list for NewTab.
func newTabArgs(program string, args []string) []string {
	parts := []string{"&", psQuote(program)}
	for _, a := range args {
		parts = append(parts, psQuote(a))
	}
	return []string{"-w", "0", "nt", "pwsh", "-NoExit", "-Command", strings.Join(parts, " ")}
}

// psQuote wraps s in single quotes for PowerShell, doubling any literal quote.
func psQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}
