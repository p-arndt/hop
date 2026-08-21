//go:build !windows

package sshx

import (
	"net"
	"path/filepath"
	"strings"
	"testing"
)

// disableAgent makes dialAgent fail for the duration of the test.
func disableAgent(t *testing.T) {
	t.Helper()
	t.Setenv(agentSockEnv, "")
}

func TestDialAgentWithoutSockEnv(t *testing.T) {
	t.Setenv(agentSockEnv, "")

	conn, err := dialAgent()
	if err == nil {
		conn.Close()
		t.Fatal("dialAgent succeeded with no " + agentSockEnv + " set; it must report the missing agent")
	}
	if !strings.Contains(err.Error(), agentSockEnv) {
		t.Fatalf("error = %q, want it to name %s", err, agentSockEnv)
	}
}

// Must fail rather than hang, naming the stale socket path.
func TestDialAgentWithDeadSocket(t *testing.T) {
	sock := filepath.Join(t.TempDir(), "agent.sock")
	t.Setenv(agentSockEnv, sock)

	conn, err := dialAgent()
	if err == nil {
		conn.Close()
		t.Fatal("dialAgent succeeded against a socket nobody is listening on")
	}
	if !strings.Contains(err.Error(), sock) {
		t.Fatalf("error = %q, want it to name the socket path %s", err, sock)
	}
}

func TestDialAgentConnectsToUnixSocket(t *testing.T) {
	// macOS caps unix socket paths at ~104 bytes, so keep the name short.
	sock := filepath.Join(t.TempDir(), "a.sock")
	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()
	go func() {
		c, err := ln.Accept()
		if err != nil {
			return
		}
		c.Close()
	}()

	t.Setenv(agentSockEnv, sock)

	conn, err := dialAgent()
	if err != nil {
		t.Fatalf("dialAgent: %v", err)
	}
	conn.Close()
}

// AgentAuth must surface the dial failure instead of a nil method.
func TestAgentAuthReportsMissingAgent(t *testing.T) {
	t.Setenv(agentSockEnv, "")

	auth, err := AgentAuth()
	if err == nil {
		t.Fatal("AgentAuth succeeded with no agent available")
	}
	if auth != nil {
		t.Fatal("AgentAuth returned an auth method alongside an error")
	}
	if !strings.HasPrefix(err.Error(), "sshx: ") {
		t.Fatalf("error = %q, want it prefixed with the package name", err)
	}
}
