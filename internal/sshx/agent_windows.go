//go:build windows

package sshx

import (
	"fmt"
	"net"

	"github.com/Microsoft/go-winio"
)

// agentPipe is the named pipe exposed by the Windows OpenSSH agent; a var so tests can repoint it, there being no env var to unset.
var agentPipe = `\\.\pipe\openssh-ssh-agent`

// dialAgent connects to the Windows OpenSSH agent over its named pipe.
func dialAgent() (net.Conn, error) {
	conn, err := winio.DialPipe(agentPipe, nil)
	if err != nil {
		return nil, fmt.Errorf("cannot reach OpenSSH agent at %s (is the ssh-agent service running?): %w", agentPipe, err)
	}
	return conn, nil
}
