package sshx

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/crypto/ssh"
)

// defaultKeyNames are the private keys OpenSSH tries when a host names no
// IdentityFile, in the same order and for the same reason: newer, cheaper
// algorithms first. They live in ~/.ssh and are only used when they exist.
var defaultKeyNames = []string{"id_ed25519", "id_ecdsa", "id_rsa", "id_dsa"}

// keySigners loads private keys from disk: the host's IdentityFile when it names
// one, otherwise the default ~/.ssh keys.
//
// This is the fallback for the very common case of an agent that is running but
// holds no identities — on macOS launchd always provides $SSH_AUTH_SOCK, so
// agent-only auth fails there for anyone who has not run `ssh-add`, even though
// plain `ssh` connects fine off the key file.
//
// Missing files are skipped silently (a default key that is not there is not an
// error). A key that needs a passphrase is skipped too, but reported through
// skipped so the caller can tell the user to `ssh-add` it rather than leaving
// them with a bare "no supported methods remain".
func keySigners(identityFile string) (signers []ssh.Signer, skipped []string) {
	paths, explicit := keyPaths(identityFile)

	for _, p := range paths {
		data, err := os.ReadFile(p)
		if err != nil {
			if explicit && !errors.Is(err, os.ErrNotExist) {
				skipped = append(skipped, fmt.Sprintf("%s (%v)", p, err))
			} else if explicit {
				skipped = append(skipped, fmt.Sprintf("%s (no such file)", p))
			}
			continue
		}
		signer, err := ssh.ParsePrivateKey(data)
		if err != nil {
			var pmErr *ssh.PassphraseMissingError
			if errors.As(err, &pmErr) {
				skipped = append(skipped, fmt.Sprintf("%s (passphrase-protected; run `ssh-add %s`)", p, p))
			} else if explicit {
				skipped = append(skipped, fmt.Sprintf("%s (%v)", p, err))
			}
			continue
		}
		signers = append(signers, signer)
	}
	return signers, skipped
}

// keyPaths resolves the private keys to try for a host. An IdentityFile named by
// the host wins outright and is reported as explicit, so a typo in it surfaces as
// an error instead of being masked by a default key that happens to exist.
func keyPaths(identityFile string) (paths []string, explicit bool) {
	if f := strings.TrimSpace(identityFile); f != "" {
		return []string{expandHome(f)}, true
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, false
	}
	for _, name := range defaultKeyNames {
		paths = append(paths, filepath.Join(home, ".ssh", name))
	}
	return paths, false
}

// expandHome resolves a leading ~ in a path, the form ssh_config uses for
// IdentityFile and the form the import writes into the store verbatim.
func expandHome(p string) string {
	if p != "~" && !strings.HasPrefix(p, "~/") {
		return p
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return p
	}
	if p == "~" {
		return home
	}
	return filepath.Join(home, p[2:])
}
