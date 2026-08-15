//go:build windows

package sshx

// failingProxyCommand is the Windows twin of the helper in proxy_helper_test.go.
func failingProxyCommand() string {
	return `cmd /c "echo TargetNotConnected 1>&2"`
}

// forkingProxyCommand has no cheap Windows twin: cmd's START detaches rather than sharing
// the handle, so the case the unix test covers cannot be staged here.
func forkingProxyCommand() string { return "" }

// silentProxyCommand's Windows twin.
func silentProxyCommand() string { return `cmd /c "timeout /t 60 >nul"` }
