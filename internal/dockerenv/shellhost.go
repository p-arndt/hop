package dockerenv

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

// The accounts the shell host runs, one per login shell. Which shell an account
// has is the whole point of it: hop's prompt hook is written for bash and zsh, and
// must not be typed into anything else.
const (
	BashUser = "bashy"
	ZshUser  = "zshy"
	FishUser = "fishy"

	// SpaceDir exists on the host so a test can cd into a directory whose name has
	// a space in it — the path most likely to be mangled on its way through an
	// escape sequence.
	SpaceDir = "/srv/hop test dir"
)

const shellHostImage = "hop-shellhost:test"

// shellHostContainer is named per process, for the same reason the two-factor one
// is: `go test ./...` runs each package in its own process, in parallel.
var shellHostContainer = fmt.Sprintf("hop-shellhost-e2e-%d", os.Getpid())

// ShellHost is a running SSH server with a real bash, a real zsh and a real fish
// to log into (see testdata/shellhost). It is what hop's working-directory
// tracking is tested against: the shells here emit OSC 7 because hop's prompt hook
// asked them to, not because a fixture printed it.
type ShellHost struct {
	// Port is the loopback port the single sshd landed on.
	Port int
	// ClientKey is the private key every account authorizes.
	ClientKey string

	keyDir string
}

// StartShellHost builds the image, generates the client key the accounts will
// trust, and starts the container on an ephemeral loopback port. The returned host
// is ready to dial: sshd has answered with a banner before this returns.
//
// Call Stop when done.
func StartShellHost() (*ShellHost, error) {
	dir, err := buildDir("shellhost")
	if err != nil {
		return nil, err
	}

	keyDir, err := os.MkdirTemp("", "hop-shellhost-keys")
	if err != nil {
		return nil, fmt.Errorf("dockerenv: key dir: %w", err)
	}
	s := &ShellHost{keyDir: keyDir, ClientKey: filepath.Join(keyDir, "id_ed25519")}

	if out, err := exec.Command("ssh-keygen", "-t", "ed25519", "-N", "", "-q",
		"-f", s.ClientKey, "-C", "hop-shellhost-test").CombinedOutput(); err != nil {
		s.Stop()
		return nil, fmt.Errorf("dockerenv: ssh-keygen: %v: %s", err, tail(out))
	}

	if out, err := exec.Command("docker", "build", "-t", shellHostImage, dir).CombinedOutput(); err != nil {
		s.Stop()
		return nil, fmt.Errorf("dockerenv: docker build: %v: %s", err, tail(out))
	}

	// A container left behind by an interrupted run would hold the name.
	exec.Command("docker", "rm", "-f", shellHostContainer).Run()

	if out, err := exec.Command("docker", "run", "-d", "--name", shellHostContainer,
		"-p", "127.0.0.1:0:2222",
		"-v", keyDir+":/keys:ro",
		shellHostImage).CombinedOutput(); err != nil {
		s.Stop()
		return nil, fmt.Errorf("dockerenv: docker run: %v: %s", err, tail(out))
	}

	port, err := publishedPort(shellHostContainer, 2222)
	if err != nil {
		s.Stop()
		return nil, err
	}
	s.Port = port

	if err := waitForSSH(shellHostContainer, s.Port); err != nil {
		s.Stop()
		return nil, err
	}
	return s, nil
}

// Stop removes the container and the generated key. It is safe to call on a
// half-started host.
func (s *ShellHost) Stop() {
	exec.Command("docker", "rm", "-f", shellHostContainer).Run()
	if s != nil && s.keyDir != "" {
		os.RemoveAll(s.keyDir)
	}
}

// Logs returns what the container has printed, which is where an sshd that
// refused to start says why.
func (s *ShellHost) Logs() string {
	out, _ := exec.Command("docker", "logs", shellHostContainer).CombinedOutput()
	return string(out)
}
