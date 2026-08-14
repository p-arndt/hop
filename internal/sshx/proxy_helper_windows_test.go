//go:build windows

package sshx

// failingProxyCommand is the Windows twin of the helper in proxy_helper_test.go.
func failingProxyCommand() string {
	return `cmd /c "echo TargetNotConnected 1>&2"`
}
