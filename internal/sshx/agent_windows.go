//go:build windows

package sshx

import (
	"fmt"
	"net"

	"github.com/Microsoft/go-winio"
)

// agentPipe is the well-known named pipe exposed by the Windows OpenSSH agent.
// It is a var, not a const, so tests can point it at a pipe nobody serves: there
// is no environment variable to unset here the way there is on unix.
var agentPipe = `\\.\pipe\openssh-ssh-agent`

// dialAgent connects to the Windows OpenSSH agent over its named pipe.
func dialAgent() (net.Conn, error) {
	conn, err := winio.DialPipe(agentPipe, nil)
	if err != nil {
		return nil, fmt.Errorf("cannot reach OpenSSH agent at %s (is the ssh-agent service running?): %w", agentPipe, err)
	}
	return conn, nil
}
