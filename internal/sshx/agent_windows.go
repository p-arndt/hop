//go:build windows

package sshx

import (
	"fmt"
	"net"

	"github.com/Microsoft/go-winio"
)

// agentPipe is the well-known named pipe exposed by the Windows OpenSSH agent.
const agentPipe = `\\.\pipe\openssh-ssh-agent`

// dialAgent connects to the Windows OpenSSH agent over its named pipe.
func dialAgent() (net.Conn, error) {
	conn, err := winio.DialPipe(agentPipe, nil)
	if err != nil {
		return nil, fmt.Errorf("cannot reach OpenSSH agent at %s (is the ssh-agent service running?): %w", agentPipe, err)
	}
	return conn, nil
}
