//go:build !windows

package sshx

import (
	"net"
	"path/filepath"
	"strings"
	"testing"
)

// With no agent advertised there is nothing to dial, and the error has to say so
// in the user's terms: a bare "connection refused" for an empty path would send
// them looking at the network instead of at their agent.
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

// A path that names no listener must fail rather than hang, and the message must
// carry the path so a stale $SSH_AUTH_SOCK is visible as the cause.
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

// The happy path: a unix socket with a listener behind it connects. This is the
// case that never compiled on non-Windows before, so it is worth pinning.
func TestDialAgentConnectsToUnixSocket(t *testing.T) {
	// macOS caps unix socket paths at ~104 bytes, and t.TempDir() names embed the
	// test name — short-name the file so the path stays under the limit.
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

// AgentAuth is the only caller; it must surface the dial failure rather than
// return a nil method that would fail later inside the handshake.
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
