// Package sshx implements the pure-Go SSH engine for hop.
//
// It authenticates exclusively through the Windows OpenSSH agent (named pipe
// \\.\pipe\openssh-ssh-agent) and speaks SSH via golang.org/x/crypto/ssh.
// Host-key verification uses a TOFU (trust-on-first-use) wrapper around the
// user's ~/.ssh/known_hosts file.
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

	"github.com/Microsoft/go-winio"
	"github.com/skeema/knownhosts"
	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"

	"hop/internal/store"
)

// agentPipe is the well-known named pipe exposed by the Windows OpenSSH agent.
const agentPipe = `\\.\pipe\openssh-ssh-agent`

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
}

// AgentAuth builds an ssh.AuthMethod backed by the Windows OpenSSH agent.
// It returns a clear error if the agent pipe cannot be reached.
func AgentAuth() (ssh.AuthMethod, error) {
	conn, err := winio.DialPipe(agentPipe, nil)
	if err != nil {
		return nil, fmt.Errorf("sshx: cannot reach OpenSSH agent at %s (is the ssh-agent service running?): %w", agentPipe, err)
	}
	ag := agent.NewClient(conn)
	return ssh.PublicKeysCallback(ag.Signers), nil
}

// Connect resolves auth, host-key policy and address from h and dials.
func Connect(h store.Host) (*Client, error) {
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

	cfg := &ssh.ClientConfig{
		User: username,
		Auth: []ssh.AuthMethod{auth},
		// Ask the server for the key types we already trust for this host.
		// Without this the server may answer with a type we have no entry for
		// (e.g. ecdsa when known_hosts holds ed25519), which knownhosts reports
		// as a key mismatch. Empty for an unknown host => library defaults.
		HostKeyAlgorithms: db.HostKeyAlgorithms(addr),
		HostKeyCallback:   tofuHostKeyCallback(db, khPath),
		Timeout:           dialTimeout,
	}

	return ConnectAddr(addr, cfg)
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

	if err := sess.Shell(); err != nil {
		sess.Close()
		pw.Close()
		return nil, fmt.Errorf("sshx: start shell: %w", err)
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

// tofuHostKeyCallback verifies the presented key against db, and on first
// contact appends it to khPath and accepts it. A genuine key change is
// rejected.
func tofuHostKeyCallback(db *knownhosts.HostKeyDB, khPath string) ssh.HostKeyCallback {
	inner := db.HostKeyCallback()

	return func(hostname string, remote net.Addr, key ssh.PublicKey) error {
		err := inner(hostname, remote, key)
		switch {
		case err == nil:
			return nil
		case knownhosts.IsHostKeyChanged(err):
			return fmt.Errorf("sshx: host key mismatch for %s: %w", hostname, err)
		case knownhosts.IsHostUnknown(err):
			if aerr := appendKnownHost(khPath, hostname, remote, key); aerr != nil {
				return fmt.Errorf("sshx: record new host key for %s: %w", hostname, aerr)
			}
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
