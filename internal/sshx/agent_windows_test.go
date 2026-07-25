//go:build windows

package sshx

import (
	"strings"
	"testing"
)

// disableAgent makes dialAgent fail for the duration of the test. Windows has no
// $SSH_AUTH_SOCK to unset — the agent is found at a fixed named pipe — so the
// pipe name is swapped for one nobody serves.
func disableAgent(t *testing.T) {
	t.Helper()
	orig := agentPipe
	agentPipe = `\\.\pipe\hop-test-no-such-agent`
	t.Cleanup(func() { agentPipe = orig })
}

// A pipe nobody serves must fail rather than hang, and the message must name the
// pipe so a stopped ssh-agent service is visible as the cause.
func TestDialAgentWithoutPipe(t *testing.T) {
	disableAgent(t)

	conn, err := dialAgent()
	if err == nil {
		conn.Close()
		t.Fatal("dialAgent succeeded against a pipe nobody is serving")
	}
	if !strings.Contains(err.Error(), agentPipe) {
		t.Fatalf("error = %q, want it to name the pipe %s", err, agentPipe)
	}
}

// AgentAuth is the only caller; it must surface the dial failure rather than
// return a nil method that would fail later inside the handshake.
func TestAgentAuthReportsMissingAgent(t *testing.T) {
	disableAgent(t)

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
