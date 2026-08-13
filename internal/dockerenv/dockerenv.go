// Package dockerenv brings up the throwaway servers hop's end-to-end tests need, in
// Docker, so more than one package can test against the same real server. There are two:
//
//   - StartTwoFactor: an Ubuntu box running OpenSSH and pam_google_authenticator, for
//     proving the SSH engine and the auth card answer a real two-factor challenge.
//   - StartShellHost: an Ubuntu box with one account per login shell, for proving the
//     working-directory tracking works against real bash, zsh and fish.
//
// Nothing here runs unless a test asks for it, and those tests are opt-in on
// HOP_DOCKER_E2E: Docker is not on every machine and the image takes a minute to build.
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

// The throwaway account and its fixed TOTP secret, baked into the image. The secret is
// fixed so a test can compute codes the server accepts, which is why the container only
// ever listens on loopback.
const (
	User     = "deploy"
	Password = "hunter2"
	Secret   = "ZVUV2W2ZPPJXPKMKV4L2UAFPQY"
)

const image = "hop-twofactor:test"

// container is named per process: `go test ./...` runs each package in its own process,
// and two sharing a name would race to recreate each other's server.
var container = fmt.Sprintf("hop-twofactor-e2e-%d", os.Getpid())

// TwoFactor is a running two-factor SSH server. Each port is a different shape of login,
// since "the host has 2FA" means at least three different handshakes.
type TwoFactor struct {
	// CodePort wants a verification code and nothing else.
	CodePort int
	// KeyPort wants a public key and then a code — the hardened
	// `AuthenticationMethods publickey,keyboard-interactive` setup.
	KeyPort int
	// PasswordPort wants the account password and then a code, as two prompts.
	PasswordPort int
	// EitherPort offers keyboard-interactive and password as alternatives — the only
	// shape that shows what happens after a dismissed prompt, since the client still has
	// the other method to try. The password method is there to be offered, not to
	// succeed: it runs the code-only PAM stack. Use PasswordPort to log in for real.
	EitherPort int
	// ClientKey is the private key KeyPort's account authorizes.
	ClientKey string

	keyDir string
}

// Enabled reports whether the environment has opted into Docker-backed tests.
func Enabled() bool { return os.Getenv(EnvVar) != "" }

// StartTwoFactor builds the image, generates the client key the publickey+code instance
// trusts, and starts the container on ephemeral loopback ports. Every daemon has answered
// with an SSH banner before it returns. Call Stop when done, typically from TestMain.
func StartTwoFactor() (*TwoFactor, error) {
	dir, err := buildDir("twofactor")
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

	// An interrupted run may have left one holding the name.
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
		p, err := publishedPort(container, m.inside)
		if err != nil {
			s.Stop()
			return nil, err
		}
		*m.out = p
	}

	if err := waitForSSH(container, s.CodePort, s.KeyPort, s.PasswordPort, s.EitherPort); err != nil {
		s.Stop()
		return nil, err
	}
	return s, nil
}

// Stop removes the container and the generated key, safe on a half-started server.
func (s *TwoFactor) Stop() {
	exec.Command("docker", "rm", "-f", container).Run()
	if s != nil && s.keyDir != "" {
		os.RemoveAll(s.keyDir)
	}
}

// Logs returns what the container has printed — where a daemon that refused to start
// says why.
func (s *TwoFactor) Logs() string {
	out, _ := exec.Command("docker", "logs", container).CombinedOutput()
	return string(out)
}

// buildDir locates testdata/<name> relative to this source file, so the test's own
// directory does not matter.
func buildDir(name string) (string, error) {
	_, self, _, ok := runtime.Caller(0)
	if !ok {
		return "", errors.New("dockerenv: cannot locate the package source")
	}
	dir := filepath.Join(filepath.Dir(self), "testdata", name)
	if _, err := os.Stat(filepath.Join(dir, "Dockerfile")); err != nil {
		return "", fmt.Errorf("dockerenv: %w", err)
	}
	return dir, nil
}

// publishedPort asks Docker which loopback port a container port landed on.
func publishedPort(container string, inside int) (int, error) {
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

// waitForSSH blocks until every daemon is actually serving. Readiness is the SSH banner,
// not a TCP connect: Docker's port proxy accepts connections whether or not anything
// inside is listening, so a connect test passes against an empty container.
func waitForSSH(container string, ports ...int) error {
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

// banner reads the version string a live sshd sends on connect. Anything else means the
// daemon is not there yet.
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

// tail keeps the last lines of a command's output, where it says what went wrong.
func tail(b []byte) string {
	lines := strings.Split(strings.TrimSpace(string(b)), "\n")
	if len(lines) > 12 {
		lines = lines[len(lines)-12:]
	}
	return strings.Join(lines, "\n")
}
