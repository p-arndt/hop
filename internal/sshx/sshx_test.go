package sshx

import (
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"net"
	"os"
	"path/filepath"
	"testing"

	"github.com/skeema/knownhosts"
	"golang.org/x/crypto/ssh"
)

// testKey generates an ed25519 host key and returns it as an ssh.PublicKey. A
// fresh key per test keeps the cases independent and needs no fixtures on disk.
func testKey(t *testing.T) ssh.PublicKey {
	t.Helper()
	pub, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	sk, err := ssh.NewPublicKey(pub)
	if err != nil {
		t.Fatalf("wrap key: %v", err)
	}
	return sk
}

// callbackFor builds the TOFU callback against a fresh temp known_hosts file and
// returns it alongside the file path so a test can assert what (if anything) was
// appended.
func callbackFor(t *testing.T, trustedFP string, recorded *string) (ssh.HostKeyCallback, string) {
	t.Helper()
	khPath := filepath.Join(t.TempDir(), "known_hosts")
	if err := os.WriteFile(khPath, nil, 0o600); err != nil {
		t.Fatalf("create known_hosts: %v", err)
	}
	db, err := knownhosts.NewDB(khPath)
	if err != nil {
		t.Fatalf("load known_hosts: %v", err)
	}
	return tofuHostKeyCallback(db, khPath, &trustState{fingerprint: trustedFP}, recorded), khPath
}

var testAddr = &net.TCPAddr{IP: net.ParseIP("192.0.2.1"), Port: 22}

// fileNonEmpty reports whether the known_hosts file has any bytes, i.e. whether
// a host line was appended.
func fileNonEmpty(t *testing.T, path string) bool {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat known_hosts: %v", err)
	}
	return info.Size() > 0
}

// An unknown host with no approved fingerprint is refused with a typed error and
// nothing is written: the decision belongs to the user, not the callback.
func TestUnknownHostRejectedAndNotRecorded(t *testing.T) {
	key := testKey(t)
	var recorded string
	cb, khPath := callbackFor(t, "", &recorded)

	err := cb("example.com:22", testAddr, key)

	var unknown *UnknownHostKeyError
	if !errors.As(err, &unknown) {
		t.Fatalf("err = %v, want *UnknownHostKeyError", err)
	}
	if unknown.Fingerprint != ssh.FingerprintSHA256(key) {
		t.Fatalf("fingerprint = %q, want the presented key's", unknown.Fingerprint)
	}
	if unknown.KeyType != key.Type() {
		t.Fatalf("key type = %q, want %q", unknown.KeyType, key.Type())
	}
	if recorded != "" {
		t.Fatalf("recorded = %q, want nothing recorded", recorded)
	}
	if fileNonEmpty(t, khPath) {
		t.Fatal("an unknown key was appended to known_hosts; it must not be")
	}
}

// Trusting the fingerprint the user approved appends the key and accepts it.
func TestTrustingMatchingFingerprintAppendsAndAccepts(t *testing.T) {
	key := testKey(t)
	fp := ssh.FingerprintSHA256(key)
	var recorded string
	cb, khPath := callbackFor(t, fp, &recorded)

	if err := cb("example.com:22", testAddr, key); err != nil {
		t.Fatalf("cb = %v, want acceptance", err)
	}
	if recorded != fp {
		t.Fatalf("recorded = %q, want %q", recorded, fp)
	}
	if !fileNonEmpty(t, khPath) {
		t.Fatal("the trusted key was not appended to known_hosts")
	}
}

// Trusting one fingerprint but being presented a different key is refused — the
// swap the confirmation exists to catch — and nothing is written.
func TestTrustingWrongFingerprintRejected(t *testing.T) {
	key := testKey(t)
	var recorded string
	cb, khPath := callbackFor(t, "SHA256:not-the-key", &recorded)

	err := cb("example.com:22", testAddr, key)
	if err == nil {
		t.Fatal("a mismatched key was accepted; it must be refused")
	}
	var unknown *UnknownHostKeyError
	if errors.As(err, &unknown) {
		t.Fatal("a mismatched key should be a hard error, not a re-prompt")
	}
	if recorded != "" {
		t.Fatalf("recorded = %q, want nothing recorded", recorded)
	}
	if fileNonEmpty(t, khPath) {
		t.Fatal("a mismatched key was appended to known_hosts; it must not be")
	}
}
