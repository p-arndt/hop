//go:build !windows

package sshx

// failingProxyCommand stands in for a broker that refuses.
func failingProxyCommand() string {
	return `sh -c 'echo TargetNotConnected >&2'`
}

// forkingProxyCommand leaves a grandchild holding the stderr it inherited, then blocks.
func forkingProxyCommand() string {
	return `sh -c 'sleep 60 & cat'`
}

// silentProxyCommand holds the connection open and says nothing.
func silentProxyCommand() string {
	return `sh -c 'sleep 60'`
}
