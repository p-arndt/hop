//go:build !windows

package sshx

import (
	"errors"
	"fmt"
	"net"
	"os"
)

// agentSockEnv is how OpenSSH advertises the path of the agent's unix socket.
const agentSockEnv = "SSH_AUTH_SOCK"

// dialAgent connects over $SSH_AUTH_SOCK; there is no fallback path, as the socket lives in a per-session temp directory.
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
