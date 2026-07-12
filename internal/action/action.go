// Package action provides side-effecting integrations with external tools
// such as VS Code Remote and Windows Terminal.
package action

import (
	"os/exec"
	"strings"
)

// OpenVSCodeRemote launches VS Code connected to the given SSH host alias via
// the Remote-SSH extension. If remotePath is non-empty it is opened as the
// target folder/file; otherwise VS Code opens without a specific path.
//
// "code" is invoked directly rather than through "cmd /c": exec resolves it to
// code.cmd itself and refuses arguments that cannot be escaped safely for a
// batch file, whereas an explicit cmd line would re-parse metacharacters in the
// alias (which can come from an imported ssh config) as shell syntax.
func OpenVSCodeRemote(alias, remotePath string) error {
	args := []string{"--remote", "ssh-remote+" + alias}
	if remotePath != "" {
		args = append(args, remotePath)
	}
	return exec.Command("code", args...).Start()
}

// NewTab opens a new Windows Terminal tab running program with args in pwsh,
// keeping the shell open afterwards. The program and each argument are quoted
// individually before they reach -Command, so nothing in them — a path with
// spaces, a name carrying "$()" or ";" — is re-parsed as PowerShell syntax.
func NewTab(program string, args ...string) error {
	return exec.Command("wt.exe", newTabArgs(program, args)...).Start()
}

// newTabArgs builds the wt.exe argument list for NewTab: a new tab ("nt") in
// the current window ("-w 0") running pwsh, whose -Command is "& 'program'
// 'arg' ..." with every part psQuoted.
func newTabArgs(program string, args []string) []string {
	parts := []string{"&", psQuote(program)}
	for _, a := range args {
		parts = append(parts, psQuote(a))
	}
	return []string{"-w", "0", "nt", "pwsh", "-NoExit", "-Command", strings.Join(parts, " ")}
}

// psQuote wraps s in single quotes for PowerShell, inside which nothing is
// expanded or interpreted; a literal single quote is escaped by doubling it.
func psQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}
