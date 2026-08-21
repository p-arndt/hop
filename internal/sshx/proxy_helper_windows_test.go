//go:build windows

package sshx

// failingProxyCommand is the Windows twin of the helper in proxy_helper_test.go.
func failingProxyCommand() string {
	return `cmd /c "echo TargetNotConnected 1>&2"`
}

// forkingProxyCommand has no cheap Windows twin: START detaches rather than sharing the handle.
func forkingProxyCommand() string { return "" }

// silentProxyCommand's Windows twin.
func silentProxyCommand() string { return `cmd /c "timeout /t 60 >nul"` }
