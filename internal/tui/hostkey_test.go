package tui

import (
	"fmt"
	"strings"
	"testing"

	"hop/internal/sshx"
	"hop/internal/store"
)

// unknownKeyErr wraps an *sshx.UnknownHostKeyError the way a real dial does, so
// the model's errors.As detection is exercised through a realistic error chain.
func unknownKeyErr(host, fp, keyType string) error {
	return fmt.Errorf("sshx: dial %s: %w", host, &sshx.UnknownHostKeyError{
		Hostname: host, Fingerprint: fp, KeyType: keyType,
	})
}

// A first-contact host key opens the confirmation card (rather than a red error
// status), clears the connecting spinner, and shows the fingerprint to compare.
func TestHostKeyCardOpensOnUnknownKey(t *testing.T) {
	m := hostMgmtModel(t, store.Host{Alias: "web", HostName: "h", Port: 22})
	m.connecting = map[string]bool{"web": true}

	const fp = "SHA256:abc123def"
	m.shellLanded(connectedMsg{alias: "web", err: unknownKeyErr("h:22", fp, "ssh-ed25519")})

	if !m.hostKey.open {
		t.Fatal("an unknown host key did not open the card")
	}
	if m.connecting["web"] {
		t.Fatal("the connecting spinner was not cleared")
	}
	if m.statusKind == statusErr {
		t.Fatal("an unknown host key showed an error status instead of the card")
	}
	if card := m.renderHostKeyConfirm(); !strings.Contains(card, fp) {
		t.Fatalf("the card does not show the fingerprint; card = %q", card)
	}
}

// "n" trusts nothing: the card closes, no retry is dispatched, and the host is
// not marked connecting.
func TestHostKeyCardCancel(t *testing.T) {
	m := hostMgmtModel(t, store.Host{Alias: "web", HostName: "h", Port: 22})
	m.shellLanded(connectedMsg{alias: "web", err: unknownKeyErr("h:22", "SHA256:zzz", "ssh-ed25519")})

	_, cmd := m.handleKey(key(t, "n"))
	if m.hostKey.open {
		t.Fatal("n did not close the card")
	}
	if cmd != nil {
		t.Fatal("cancelling dispatched a command; it must trust nothing")
	}
	if m.connecting["web"] {
		t.Fatal("cancelling started a connect anyway")
	}
}

// "y" trusts the shown key: the card closes and a retry command is dispatched.
func TestHostKeyCardAcceptRetries(t *testing.T) {
	m := hostMgmtModel(t, store.Host{Alias: "web", HostName: "h", Port: 22})
	m.connecting = map[string]bool{}
	m.shellLanded(connectedMsg{alias: "web", err: unknownKeyErr("h:22", "SHA256:zzz", "ssh-ed25519")})

	_, cmd := m.handleKey(key(t, "y"))
	if m.hostKey.open {
		t.Fatal("y did not close the card")
	}
	if cmd == nil {
		t.Fatal("y did not dispatch a retry command")
	}
	if !m.connecting["web"] {
		t.Fatal("the retry did not mark the host connecting")
	}
}
