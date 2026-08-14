//go:build !windows

package sshx

// failingProxyCommand is a proxy that writes a diagnosis to stderr and exits, standing in
// for a broker that refuses — `aws ssm` against a stopped instance, say.
func failingProxyCommand() string {
	return `sh -c 'echo TargetNotConnected >&2'`
}
