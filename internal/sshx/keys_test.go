package sshx

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/crypto/ssh"

	"hop/internal/store"
)

// writeKey generates an ed25519 private key and writes it to path in OpenSSH
// format, encrypting it with passphrase when that is non-empty.
func writeKey(t *testing.T, path, passphrase string) {
	t.Helper()
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	var block *pem.Block
	if passphrase == "" {
		block, err = ssh.MarshalPrivateKey(priv, "")
	} else {
		block, err = ssh.MarshalPrivateKeyWithPassphrase(priv, "", []byte(passphrase))
	}
	if err != nil {
		t.Fatalf("marshal key: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, pem.EncodeToMemory(block), 0o600); err != nil {
		t.Fatalf("write key: %v", err)
	}
}

// fakeHome points os.UserHomeDir at a temp dir for the duration of the test, so
// the default-key lookup sees a controlled ~/.ssh instead of the real one. It
// sets every variable that call consults — $HOME on unix, %USERPROFILE% on
// Windows — because setting only $HOME leaves the Windows runner reading the
// real profile, where the keys these tests write do not exist.
func fakeHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	return home
}

// A host naming an IdentityFile authenticates with that key, which is the whole
// point of storing it: agent-only auth left it unused.
func TestKeyAuthUsesIdentityFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "custom_key")
	writeKey(t, path, "")

	signers, skipped := keySigners(path)
	if len(signers) != 1 {
		t.Fatalf("len(signers) = %d, want 1 for %s (skipped: %v)", len(signers), path, skipped)
	}
	if len(skipped) != 0 {
		t.Fatalf("skipped = %v, want none", skipped)
	}
}

// A ~-prefixed IdentityFile is what ssh_config uses and what the importer stores
// verbatim, so it has to resolve rather than be opened literally.
func TestKeyAuthExpandsTilde(t *testing.T) {
	home := fakeHome(t)
	writeKey(t, filepath.Join(home, ".ssh", "work_key"), "")

	signers, skipped := keySigners("~/.ssh/work_key")
	if len(signers) != 1 {
		t.Fatalf("len(signers) = %d, want 1 for ~-path (skipped: %v)", len(signers), skipped)
	}
}

// With no IdentityFile, the default ~/.ssh keys are tried — the case that makes
// hop work on a fresh macOS install where launchd's agent holds no identities.
func TestKeyAuthFallsBackToDefaultKeys(t *testing.T) {
	home := fakeHome(t)
	writeKey(t, filepath.Join(home, ".ssh", "id_ed25519"), "")

	signers, _ := keySigners("")
	if len(signers) != 1 {
		t.Fatalf("len(signers) = %d, want 1 default key from ~/.ssh", len(signers))
	}
}

// An empty ~/.ssh is not an error, just nothing to offer: the agent may still
// have the identity.
func TestKeyAuthNoKeysIsSilent(t *testing.T) {
	fakeHome(t)

	signers, skipped := keySigners("")
	if len(signers) != 0 {
		t.Fatalf("len(signers) = %d, want 0 with no keys on disk", len(signers))
	}
	if len(skipped) != 0 {
		t.Fatalf("skipped = %v, want none for absent default keys", skipped)
	}
}

// A passphrase-protected key cannot be used unattended, but silently ignoring it
// leaves the user with an unexplained auth failure — it must be reported.
func TestKeyAuthReportsPassphraseProtectedKey(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "locked_key")
	writeKey(t, path, "hunter2")

	signers, skipped := keySigners(path)
	if len(signers) != 0 {
		t.Fatal("keySigners used a passphrase-protected key")
	}
	if len(skipped) != 1 || !strings.Contains(skipped[0], "ssh-add") {
		t.Fatalf("skipped = %v, want one entry mentioning ssh-add", skipped)
	}
}

// An IdentityFile the user typed wrong must surface, unlike a missing default
// key: they named that path deliberately.
func TestKeyAuthReportsMissingIdentityFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "absent_key")

	signers, skipped := keySigners(path)
	if len(signers) != 0 {
		t.Fatal("keySigners returned a signer for a nonexistent identity file")
	}
	if len(skipped) != 1 || !strings.Contains(skipped[0], "no such file") {
		t.Fatalf("skipped = %v, want one entry reporting the missing file", skipped)
	}
}

// The regression this fixes: agent reachable but empty (macOS launchd) plus a
// key on disk must still yield a usable auth method.
func TestAuthMethodsUsesKeysWithoutAgent(t *testing.T) {
	home := fakeHome(t)
	writeKey(t, filepath.Join(home, ".ssh", "id_ed25519"), "")
	disableAgent(t)

	auths, err := authMethods(store.Host{HostName: "example.com"}, nil)
	if err != nil {
		t.Fatalf("authMethods: %v", err)
	}
	if len(auths) != 1 {
		t.Fatalf("len(auths) = %d, want 1 combined publickey method", len(auths))
	}
}

// With neither an agent nor a key there is nothing to offer, and the error has
// to say so in terms the user can act on.
func TestAuthMethodsErrorsWithNoAgentAndNoKeys(t *testing.T) {
	fakeHome(t)
	disableAgent(t)

	auths, err := authMethods(store.Host{HostName: "example.com"}, nil)
	if err == nil {
		t.Fatalf("authMethods succeeded with no agent and no keys (got %d methods)", len(auths))
	}
	if !strings.Contains(err.Error(), "ssh-add") {
		t.Fatalf("error %q does not tell the user how to fix it", err)
	}
}
