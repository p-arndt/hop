//go:build !windows

package sshx

import (
	"errors"
	"fmt"
	"net"
	"os"
)

// agentSockEnv is the environment variable OpenSSH uses to advertise the path of
// the agent's unix socket. On macOS launchd sets it per-session; on Linux it is
// set by ssh-agent, gnome-keyring, or the systemd user unit.
const agentSockEnv = "SSH_AUTH_SOCK"

// dialAgent connects to the OpenSSH agent over the unix socket named by
// $SSH_AUTH_SOCK. An unset variable is its own diagnosis — there is no
// well-known fallback path to try, since the socket lives in a per-session
// temp directory — so it is reported as such rather than as a dial failure.
func dialAgent() (net.Conn, error) {
	sock := os.Getenv(agentSockEnv)
	if sock == "" {
		return nil, errors.New("$" + agentSockEnv + " is not set (is an ssh-agent running? try `ssh-add -l`)")
	}
	conn, err := net.Dial("unix", sock)
	if err != nil {
		return nil, fmt.Errorf("cannot reach OpenSSH agent at %s (is the ssh-agent running?): %w", sock, err)
	}
	return conn, nil
}
