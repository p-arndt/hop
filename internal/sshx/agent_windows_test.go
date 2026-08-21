//go:build windows

package sshx

import (
	"strings"
	"testing"
)

// disableAgent points dialAgent at a pipe nobody serves.
func disableAgent(t *testing.T) {
	t.Helper()
	orig := agentPipe
	agentPipe = `\\.\pipe\hop-test-no-such-agent`
	t.Cleanup(func() { agentPipe = orig })
}

// Must fail rather than hang, naming the pipe.
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

// AgentAuth must surface the dial failure instead of a nil method.
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
