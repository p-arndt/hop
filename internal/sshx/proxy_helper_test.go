//go:build !windows

package sshx

// failingProxyCommand is a proxy that writes a diagnosis to stderr and exits, standing in
// for a broker that refuses — `aws ssm` against a stopped instance, say.
func failingProxyCommand() string {
	return `sh -c 'echo TargetNotConnected >&2'`
}

// forkingProxyCommand is a proxy that leaves a long-lived grandchild holding the stderr
// it inherited, then blocks — the shape `aws ssm` has, since it starts
// session-manager-plugin and stays attached to it.
func forkingProxyCommand() string {
	return `sh -c 'sleep 60 & cat'`
}
