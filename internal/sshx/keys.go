package sshx

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/crypto/ssh"

	"hop/internal/pathx"
)

// defaultKeyNames are the private keys OpenSSH tries when a host names no IdentityFile, in its order.
var defaultKeyNames = []string{"id_ed25519", "id_ecdsa", "id_rsa", "id_dsa"}

// keySigners loads private keys from disk; a missing or passphrase-protected key is skipped but named in skipped.
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

// keyPaths resolves the keys to try; an IdentityFile is explicit, so a typo in it surfaces rather than falling back.
func keyPaths(identityFile string) (paths []string, explicit bool) {
	if f := strings.TrimSpace(identityFile); f != "" {
		return []string{pathx.ExpandHome(f)}, true
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
