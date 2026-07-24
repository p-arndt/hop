// Package sshx implements the pure-Go SSH engine for hop.
//
// It authenticates exclusively through the running OpenSSH agent — the named
// pipe \\.\pipe\openssh-ssh-agent on Windows, the $SSH_AUTH_SOCK unix socket
// everywhere else (see agent_windows.go / agent_unix.go) — and speaks SSH via
// golang.org/x/crypto/ssh. Host-key verification uses a TOFU
// (trust-on-first-use) wrapper around the user's ~/.ssh/known_hosts file.
package sshx

import (
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/user"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/skeema/knownhosts"
	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"

	"hop/internal/store"
)

// dialTimeout bounds the whole TCP+handshake for a connection attempt.
const dialTimeout = 15 * time.Second

// Session wraps an interactive SSH shell session. Stdin accepts bytes to send
// to the remote pty; Stdout yields the merged stdout+stderr stream.
type Session struct {
	Stdin  io.WriteCloser
	Stdout io.Reader

	sess   *ssh.Session
	pipeWr *io.PipeWriter // write end of the merged output pipe (closed on Close)
}

// Resize informs the remote pty of a new window size.
func (se *Session) Resize(cols, rows int) error {
	if se.sess == nil {
		return errors.New("sshx: session not initialized")
	}
	return se.sess.WindowChange(rows, cols)
}

// Close terminates the session and releases the merged output pipe.
func (se *Session) Close() error {
	var err error
	if se.sess != nil {
		err = se.sess.Close()
	}
	if se.pipeWr != nil {
		se.pipeWr.Close()
	}
	return err
}

// Wait blocks until the remote shell exits.
func (se *Session) Wait() error {
	if se.sess == nil {
		return errors.New("sshx: session not initialized")
	}
	return se.sess.Wait()
}

// Client wraps an established *ssh.Client.
type Client struct {
	ssh *ssh.Client

	// NewHostKey is the SHA256 fingerprint of a host key recorded on first
	// contact (TOFU) during this dial, or "" when the host was already known.
	// The UI shows it, so a silent first trust is at least a visible one.
	NewHostKey string
}

// AgentAuth builds an ssh.AuthMethod backed by the platform's OpenSSH agent.
// It returns a clear error if the agent cannot be reached — the transport
// differs per platform, but the failure the user has to act on ("no agent") is
// the same one either way.
func AgentAuth() (ssh.AuthMethod, error) {
	conn, err := dialAgent()
	if err != nil {
		return nil, fmt.Errorf("sshx: %w", err)
	}
	ag := agent.NewClient(conn)
	return ssh.PublicKeysCallback(ag.Signers), nil
}

// UnknownHostKeyError is returned by Connect when the host is met for the first
// time and no fingerprint has been approved for it yet. It carries what the UI
// needs to ask the user to trust the key, and nothing is written to known_hosts:
// a first-contact MITM must not be waved through silently, so the decision is
// handed back to the caller instead of taken here.
type UnknownHostKeyError struct {
	Hostname    string // the host as presented to the host-key callback
	Fingerprint string // ssh.FingerprintSHA256 of the presented key
	KeyType     string // key.Type(), e.g. "ssh-ed25519"
}

func (e *UnknownHostKeyError) Error() string {
	return fmt.Sprintf("sshx: unknown host key for %s: %s %s", e.Hostname, e.KeyType, e.Fingerprint)
}

// Connect resolves auth, host-key policy and address from h and dials. An unknown
// host aborts the dial with an error that unwraps to *UnknownHostKeyError, and
// appends nothing to known_hosts — the caller decides whether to trust the key
// and retries through ConnectTrusting.
func Connect(h store.Host) (*Client, error) {
	return connect(h, "")
}

// ConnectTrusting dials h like Connect, but when the host is unknown and the
// presented key's fingerprint equals fingerprint, the key is appended to
// known_hosts and accepted. A presented key that does not match fingerprint is
// refused: it means the key changed between the prompt that produced fingerprint
// and this retry, which is exactly the swap the confirmation was there to catch.
func ConnectTrusting(h store.Host, fingerprint string) (*Client, error) {
	return connect(h, fingerprint)
}

// connect is the shared dial body. trustedFP is empty for a plain TOFU-guarded
// dial and the user-approved fingerprint for a trusting retry.
func connect(h store.Host, trustedFP string) (*Client, error) {
	auth, err := AgentAuth()
	if err != nil {
		return nil, err
	}

	username := h.User
	if username == "" {
		username = currentUsername()
	}

	port := h.Port
	if port == 0 {
		port = 22
	}
	addr := net.JoinHostPort(h.HostName, fmt.Sprintf("%d", port))

	db, khPath, err := hostKeyDB()
	if err != nil {
		return nil, err
	}

	var newKey string
	cfg := &ssh.ClientConfig{
		User: username,
		Auth: []ssh.AuthMethod{auth},
		// Ask the server for the key types we already trust for this host.
		// Without this the server may answer with a type we have no entry for
		// (e.g. ecdsa when known_hosts holds ed25519), which knownhosts reports
		// as a key mismatch. Empty for an unknown host => library defaults.
		HostKeyAlgorithms: db.HostKeyAlgorithms(addr),
		HostKeyCallback:   tofuHostKeyCallback(db, khPath, trustedFP, &newKey),
		Timeout:           dialTimeout,
	}

	cl, err := ConnectAddr(addr, cfg)
	if err != nil {
		return nil, err
	}
	cl.NewHostKey = newKey
	return cl, nil
}

// ConnectAddr dials addr with the supplied config. Tests may pass a config
// using ssh.InsecureIgnoreHostKey() and their own auth method.
func ConnectAddr(addr string, cfg *ssh.ClientConfig) (*Client, error) {
	cl, err := ssh.Dial("tcp", addr, cfg)
	if err != nil {
		return nil, fmt.Errorf("sshx: dial %s: %w", addr, err)
	}
	return &Client{ssh: cl}, nil
}

// Shell opens an interactive login shell with a pty of the given size.
func (c *Client) Shell(cols, rows int) (*Session, error) {
	return c.startPTY(cols, rows, func(s *ssh.Session) error { return s.Shell() })
}

// Command runs cmd on a pty of the given size, exactly as Shell does for a login
// shell. A pty is what makes it usable for full-screen programs — an editor needs
// one to draw at all, and needs its size to lay out.
func (c *Client) Command(cmd string, cols, rows int) (*Session, error) {
	return c.startPTY(cols, rows, func(s *ssh.Session) error { return s.Start(cmd) })
}

// startPTY opens a session, requests a pty, wires the three std streams (stdout
// and stderr merged into one ordered stream), and hands the prepared session to
// start — which either opens a shell or launches a command on it.
func (c *Client) startPTY(cols, rows int, start func(*ssh.Session) error) (*Session, error) {
	sess, err := c.ssh.NewSession()
	if err != nil {
		return nil, fmt.Errorf("sshx: new session: %w", err)
	}

	modes := ssh.TerminalModes{
		ssh.ECHO:          1,
		ssh.TTY_OP_ISPEED: 14400,
		ssh.TTY_OP_OSPEED: 14400,
	}
	if err := sess.RequestPty("xterm-256color", rows, cols, modes); err != nil {
		sess.Close()
		return nil, fmt.Errorf("sshx: request pty: %w", err)
	}

	stdin, err := sess.StdinPipe()
	if err != nil {
		sess.Close()
		return nil, fmt.Errorf("sshx: stdin pipe: %w", err)
	}
	stdout, err := sess.StdoutPipe()
	if err != nil {
		sess.Close()
		return nil, fmt.Errorf("sshx: stdout pipe: %w", err)
	}
	stderr, err := sess.StderrPipe()
	if err != nil {
		sess.Close()
		return nil, fmt.Errorf("sshx: stderr pipe: %w", err)
	}

	// Merge stdout and stderr into a single ordered stream via an io.Pipe.
	pr, pw := io.Pipe()
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		io.Copy(pw, stdout)
	}()
	go func() {
		defer wg.Done()
		io.Copy(pw, stderr)
	}()
	// Close the write end once both copiers finish so readers see EOF.
	go func() {
		wg.Wait()
		pw.Close()
	}()

	if err := start(sess); err != nil {
		sess.Close()
		pw.Close()
		return nil, fmt.Errorf("sshx: start: %w", err)
	}

	return &Session{
		Stdin:  stdin,
		Stdout: pr,
		sess:   sess,
		pipeWr: pw,
	}, nil
}

// SSHClient returns the underlying *ssh.Client.
func (c *Client) SSHClient() *ssh.Client { return c.ssh }

// Close closes the underlying SSH client connection.
func (c *Client) Close() error {
	if c.ssh == nil {
		return nil
	}
	return c.ssh.Close()
}

// currentUsername returns the current OS user, stripping any DOMAIN\ prefix.
func currentUsername() string {
	u, err := user.Current()
	if err != nil {
		return ""
	}
	name := u.Username
	if i := strings.LastIndex(name, `\`); i >= 0 {
		name = name[i+1:]
	}
	return name
}

// hostKeyDB opens ~/.ssh/known_hosts, creating the file and its directory on
// first run, and returns the parsed database alongside its path.
func hostKeyDB() (*knownhosts.HostKeyDB, string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, "", fmt.Errorf("sshx: locate home dir: %w", err)
	}
	sshDir := filepath.Join(home, ".ssh")
	khPath := filepath.Join(sshDir, "known_hosts")

	// Ensure the file and its directory exist so knownhosts.NewDB can open it.
	if err := os.MkdirAll(sshDir, 0o700); err != nil {
		return nil, "", fmt.Errorf("sshx: create .ssh dir: %w", err)
	}
	if _, err := os.Stat(khPath); errors.Is(err, os.ErrNotExist) {
		f, err := os.OpenFile(khPath, os.O_CREATE|os.O_WRONLY, 0o600)
		if err != nil {
			return nil, "", fmt.Errorf("sshx: create known_hosts: %w", err)
		}
		f.Close()
	}

	db, err := knownhosts.NewDB(khPath)
	if err != nil {
		return nil, "", fmt.Errorf("sshx: load known_hosts: %w", err)
	}
	return db, khPath, nil
}

// tofuHostKeyCallback verifies the presented key against db. A key already known
// is accepted; a genuine key change is rejected outright.
//
// A first-contact host is handled by trustedFP. When it is empty the key is not
// trusted: the callback returns an *UnknownHostKeyError and writes nothing, so
// the caller can ask the user before anything is committed. When it holds a
// fingerprint the user has approved, a presented key matching it is appended to
// khPath and accepted (its fingerprint reported through recorded so the UI can
// confirm what was trusted); a presented key that does not match is refused, as
// it means the key changed since the fingerprint was approved.
func tofuHostKeyCallback(db *knownhosts.HostKeyDB, khPath, trustedFP string, recorded *string) ssh.HostKeyCallback {
	inner := db.HostKeyCallback()

	return func(hostname string, remote net.Addr, key ssh.PublicKey) error {
		err := inner(hostname, remote, key)
		switch {
		case err == nil:
			return nil
		case knownhosts.IsHostKeyChanged(err):
			return fmt.Errorf("sshx: host key mismatch for %s: %w", hostname, err)
		case knownhosts.IsHostUnknown(err):
			fp := ssh.FingerprintSHA256(key)
			if trustedFP == "" {
				return &UnknownHostKeyError{Hostname: hostname, Fingerprint: fp, KeyType: key.Type()}
			}
			if fp != trustedFP {
				return fmt.Errorf("sshx: host key for %s does not match the approved fingerprint (got %s, expected %s); possible key swap", hostname, fp, trustedFP)
			}
			if aerr := appendKnownHost(khPath, hostname, remote, key); aerr != nil {
				return fmt.Errorf("sshx: record new host key for %s: %w", hostname, aerr)
			}
			*recorded = fp
			return nil
		}
		return err
	}
}

// appendKnownHost appends a normalized known_hosts line for the given host key.
func appendKnownHost(path, hostname string, remote net.Addr, key ssh.PublicKey) error {
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	return knownhosts.WriteKnownHost(f, hostname, remote, key)
}
