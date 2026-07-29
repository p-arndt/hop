// Package dockerenv brings up the throwaway servers hop's end-to-end tests need,
// in Docker.
//
// It exists so that more than one package can test against the same real server
// without each of them re-implementing the container plumbing. Today that server
// is an Ubuntu box running OpenSSH and pam_google_authenticator (see
// testdata/twofactor): internal/sshx uses it to prove the SSH engine answers a
// real two-factor challenge, and internal/tui uses it to prove the card a user
// actually types into produces a real connected session.
//
// Nothing here runs unless a test asks for it. The tests that do are opt-in on
// HOP_DOCKER_E2E, because Docker is not on every machine and the image takes a
// minute to build the first time.
package dockerenv

import (
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// EnvVar is the environment variable that opts a test run into using Docker.
const EnvVar = "HOP_DOCKER_E2E"

// The throwaway account and its fixed TOTP secret, as baked into the image. The
// secret is fixed so a test can compute codes the server accepts; that is the
// one thing about this container that is not how a real host is set up, and it
// is why it only ever listens on loopback.
const (
	User     = "deploy"
	Password = "hunter2"
	Secret   = "ZVUV2W2ZPPJXPKMKV4L2UAFPQY"
)

const image = "hop-twofactor:test"

// container is named per process, because `go test ./...` runs each package's
// tests in a process of its own and in parallel: two packages sharing one
// container name would race to remove and recreate each other's server.
var container = fmt.Sprintf("hop-twofactor-e2e-%d", os.Getpid())

// TwoFactor is a running two-factor SSH server. Each port is a different shape
// of login, because "the host has 2FA" means at least three different handshakes
// and hop has to survive all of them.
type TwoFactor struct {
	// CodePort wants a verification code and nothing else.
	CodePort int
	// KeyPort wants a public key *and then* a code — the hardened
	// `AuthenticationMethods publickey,keyboard-interactive` setup, where the key
	// earns only a partial success.
	KeyPort int
	// PasswordPort wants the account password and then a code, as two prompts.
	PasswordPort int
	// EitherPort offers keyboard-interactive *and* password as two alternative
	// methods. It is the only shape that shows what happens after a user
	// dismisses a prompt, since a client that gives up on one method still has
	// the other to try.
	//
	// The password method is there to be offered, not to succeed: it runs the
	// code-only PAM stack, so what is sent as a password is checked against
	// pam_google_authenticator and refused. Use PasswordPort to log in with the
	// account password.
	EitherPort int
	// ClientKey is the private key KeyPort's account authorizes.
	ClientKey string

	keyDir string
}

// Enabled reports whether the environment has opted into Docker-backed tests.
func Enabled() bool { return os.Getenv(EnvVar) != "" }

// StartTwoFactor builds the image, generates the client key the publickey+code
// instance will trust, and starts the container on ephemeral loopback ports. The
// returned server is ready to dial: every daemon has answered with an SSH banner
// before this returns.
//
// Call Stop when done — typically from TestMain, since the container is worth
// sharing across a package's tests.
func StartTwoFactor() (*TwoFactor, error) {
	dir, err := buildDir()
	if err != nil {
		return nil, err
	}

	keyDir, err := os.MkdirTemp("", "hop-2fa-keys")
	if err != nil {
		return nil, fmt.Errorf("dockerenv: key dir: %w", err)
	}
	s := &TwoFactor{keyDir: keyDir, ClientKey: filepath.Join(keyDir, "id_ed25519")}

	if out, err := exec.Command("ssh-keygen", "-t", "ed25519", "-N", "", "-q",
		"-f", s.ClientKey, "-C", "hop-2fa-test").CombinedOutput(); err != nil {
		s.Stop()
		return nil, fmt.Errorf("dockerenv: ssh-keygen: %v: %s", err, tail(out))
	}

	if out, err := exec.Command("docker", "build", "-t", image, dir).CombinedOutput(); err != nil {
		s.Stop()
		return nil, fmt.Errorf("dockerenv: docker build: %v: %s", err, tail(out))
	}

	// A container left behind by an interrupted run would hold the name.
	exec.Command("docker", "rm", "-f", container).Run()

	if out, err := exec.Command("docker", "run", "-d", "--name", container,
		"-p", "127.0.0.1:0:2222",
		"-p", "127.0.0.1:0:2223",
		"-p", "127.0.0.1:0:2224",
		"-p", "127.0.0.1:0:2225",
		"-v", keyDir+":/keys:ro",
		image).CombinedOutput(); err != nil {
		s.Stop()
		return nil, fmt.Errorf("dockerenv: docker run: %v: %s", err, tail(out))
	}

	for _, m := range []struct {
		inside int
		out    *int
	}{{2222, &s.CodePort}, {2223, &s.KeyPort}, {2224, &s.PasswordPort}, {2225, &s.EitherPort}} {
		p, err := publishedPort(m.inside)
		if err != nil {
			s.Stop()
			return nil, err
		}
		*m.out = p
	}

	if err := waitForSSH(s.CodePort, s.KeyPort, s.PasswordPort, s.EitherPort); err != nil {
		s.Stop()
		return nil, err
	}
	return s, nil
}

// Stop removes the container and the generated key. It is safe to call on a
// half-started server.
func (s *TwoFactor) Stop() {
	exec.Command("docker", "rm", "-f", container).Run()
	if s != nil && s.keyDir != "" {
		os.RemoveAll(s.keyDir)
	}
}

// Logs returns what the container has printed, which is where a daemon that
// refused to start says why.
func (s *TwoFactor) Logs() string {
	out, _ := exec.Command("docker", "logs", container).CombinedOutput()
	return string(out)
}

// buildDir locates testdata/twofactor relative to this source file, so it does
// not matter which package's directory the test happens to run in.
func buildDir() (string, error) {
	_, self, _, ok := runtime.Caller(0)
	if !ok {
		return "", errors.New("dockerenv: cannot locate the package source")
	}
	dir := filepath.Join(filepath.Dir(self), "testdata", "twofactor")
	if _, err := os.Stat(filepath.Join(dir, "Dockerfile")); err != nil {
		return "", fmt.Errorf("dockerenv: %w", err)
	}
	return dir, nil
}

// publishedPort asks Docker which loopback port a container port landed on.
func publishedPort(inside int) (int, error) {
	out, err := exec.Command("docker", "port", container, strconv.Itoa(inside)).Output()
	if err != nil {
		return 0, fmt.Errorf("dockerenv: docker port %d: %w", inside, err)
	}
	line := strings.TrimSpace(strings.Split(string(out), "\n")[0])
	_, portStr, err := net.SplitHostPort(line)
	if err != nil {
		return 0, fmt.Errorf("dockerenv: parse %q: %w", line, err)
	}
	return strconv.Atoi(portStr)
}

// waitForSSH blocks until every daemon is actually serving.
//
// Readiness is the SSH banner, not a successful TCP connect: Docker's port proxy
// binds the published port the moment the container starts and accepts
// connections whether or not anything inside is listening yet, so a connect test
// passes against an empty container — and the dial that follows dies with
// "handshake failed: EOF".
func waitForSSH(ports ...int) error {
	deadline := time.Now().Add(90 * time.Second)
	for _, p := range ports {
		for {
			if err := banner(p); err == nil {
				break
			}
			if time.Now().After(deadline) {
				logs, _ := exec.Command("docker", "logs", container).CombinedOutput()
				return fmt.Errorf("dockerenv: sshd on %d never sent a banner: %s", p, tail(logs))
			}
			time.Sleep(250 * time.Millisecond)
		}
	}
	return nil
}

// banner reads the version string a live sshd sends as soon as a connection is
// made. Anything else means the daemon is not there yet.
func banner(port int) error {
	c, err := net.DialTimeout("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)), 2*time.Second)
	if err != nil {
		return err
	}
	defer c.Close()

	c.SetReadDeadline(time.Now().Add(2 * time.Second))
	buf := make([]byte, 8)
	if _, err := io.ReadFull(c, buf); err != nil {
		return err
	}
	if string(buf) != "SSH-2.0-" {
		return fmt.Errorf("port %d answered %q, not an SSH banner", port, buf)
	}
	return nil
}

// tail keeps the last lines of a command's output — the part that says what went
// wrong.
func tail(b []byte) string {
	lines := strings.Split(strings.TrimSpace(string(b)), "\n")
	if len(lines) > 12 {
		lines = lines[len(lines)-12:]
	}
	return strings.Join(lines, "\n")
}
