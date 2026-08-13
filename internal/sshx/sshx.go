// Package sshx implements the pure-Go SSH engine for hop, over golang.org/x/crypto/ssh.
//
// It authenticates by public key, offering both the running OpenSSH agent (agent_*.go)
// and private keys from disk (keys.go). Host-key verification is a TOFU wrapper around
// ~/.ssh/known_hosts.
//
// With a Prompter (prompt.go), keys are followed by keyboard-interactive and password —
// how a 2FA host asks for its code. Those are answered inside the handshake rather than
// by retrying the dial, since a one-time code cannot be replayed.
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

// dialTimeout bounds the TCP connect, so an unreachable host fails rather than hangs.
// It deliberately does not bound the handshake: interactive authentication happens
// inside it, and a clock running while the user reads a code off their phone would time
// out the dials most in need of patience. The Prompter's cancel is the way out there.
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

	// NewHostKey is the SHA256 fingerprint of a host key recorded on first contact
	// during this dial, or "" when the host was already known. The UI shows it.
	NewHostKey string

	// lost is closed once the transport under this client has gone. waitErr is why,
	// written before the close and so safe to read after observing it (see LostErr). A
	// zero Client's nil channel never fires: it was never connected.
	lost    chan struct{}
	waitErr error
}

// newClient wraps an established ssh.Client and starts the two goroutines that watch
// the connection: one parked on Wait, which turns a dropped transport into a closed
// Lost channel, and the keepalive, which is what makes a blackholed connection reach it.
func newClient(cl *ssh.Client) *Client {
	c := &Client{ssh: cl, lost: make(chan struct{})}
	go func() {
		c.waitErr = cl.Wait()
		close(c.lost)
	}()
	go c.keepalive()
	return c
}

// Keepalive parameters, matching ssh's ServerAliveInterval / ServerAliveCountMax.
// Without them a blackholed connection is never noticed: nothing is written, so TCP
// never complains and the shell just stops updating.
const (
	keepaliveInterval = 15 * time.Second
	keepaliveTimeout  = 10 * time.Second
	keepaliveMisses   = 3
)

// keepalive probes the server until it stops answering, then closes the connection,
// which unblocks the Wait above and fires Lost.
func (c *Client) keepalive() {
	t := time.NewTicker(keepaliveInterval)
	defer t.Stop()

	misses := 0
	for {
		select {
		case <-c.lost:
			return
		case <-t.C:
		}
		if c.ping() {
			misses = 0
			continue
		}
		if misses++; misses >= keepaliveMisses {
			c.ssh.Close()
			return
		}
	}
}

// ping sends OpenSSH's keepalive global request and reports whether the server answered
// at all. A failure reply is an answer: every server replies to an unknown request type.
// Only a transport error, or no reply inside keepaliveTimeout, counts as a miss.
func (c *Client) ping() bool {
	done := make(chan error, 1)
	go func() {
		_, _, err := c.ssh.SendRequest("keepalive@openssh.com", true, nil)
		done <- err
	}()
	select {
	case err := <-done:
		return err == nil
	case <-time.After(keepaliveTimeout):
		return false
	}
}

// Lost is closed when the connection under this client has gone — how the UI learns a
// session died without polling. A zero Client's channel is nil, so it blocks forever.
func (c *Client) Lost() <-chan struct{} { return c.lost }

// IsLost reports, without blocking, whether the connection has gone — the question a
// shell's exit has to ask, since a transport death is not somebody typing "exit".
func (c *Client) IsLost() bool {
	select {
	case <-c.lost:
		return true
	default:
		return false
	}
}

// LostErr is why the connection went, or nil while it is still up. Reading waitErr is
// safe only after observing the channel's close, which is what publishes the write.
func (c *Client) LostErr() error {
	select {
	case <-c.lost:
		return c.waitErr
	default:
		return nil
	}
}

// AgentAuth builds an ssh.AuthMethod backed by the platform's OpenSSH agent, erroring
// clearly if the agent cannot be reached.
func AgentAuth() (ssh.AuthMethod, error) {
	conn, err := dialAgent()
	if err != nil {
		return nil, fmt.Errorf("sshx: %w", err)
	}
	ag := agent.NewClient(conn)
	return ssh.PublicKeysCallback(ag.Signers), nil
}

// authMethods assembles the auth to offer for h: the agent's identities plus private
// keys from disk (the host's IdentityFile, else the default ~/.ssh keys). Neither source
// is required, only their union, which is what makes hop connect wherever ssh does.
//
// The two are merged into a single publickey method, because the client tries each
// method name at most once: offered separately, an empty agent would swallow the attempt
// and the key files would never be reached.
//
// With a prompter, keyboard-interactive and password follow, in ssh's own order — a
// hardened `AuthenticationMethods publickey,keyboard-interactive` server answers the key
// with a partial success and then requires them. Both are wrapped in RetryableAuthMethod
// so a mistyped code is another prompt inside the same handshake.
func authMethods(h store.Host, p Prompter) ([]ssh.AuthMethod, error) {
	signers, agentErr := agentSigners()

	keys, skipped := keySigners(h.IdentityFile)
	signers = append(signers, keys...)

	var methods []ssh.AuthMethod
	if len(signers) > 0 {
		methods = append(methods, ssh.PublicKeys(signers...))
	}
	if p != nil {
		// One wrapper shared by both, so a cancel ends the dial rather than moving the
		// user on to the other method.
		sticky := &stickyCancel{p: p}
		methods = append(methods,
			ssh.RetryableAuthMethod(ssh.KeyboardInteractive(keyboardInteractive(sticky)), authRetries),
			ssh.RetryableAuthMethod(ssh.PasswordCallback(passwordCallback(sticky)), authRetries),
		)
	}

	if len(methods) == 0 {
		return nil, noAuthError(agentErr, skipped)
	}
	return methods, nil
}

// agentSigners returns the identities held by the OpenSSH agent. The connection is left
// open: each signer signs over it, so closing it here would break every signature.
func agentSigners() ([]ssh.Signer, error) {
	conn, err := dialAgent()
	if err != nil {
		return nil, fmt.Errorf("sshx: %w", err)
	}
	signers, err := agent.NewClient(conn).Signers()
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("sshx: read agent identities: %w", err)
	}
	return signers, nil
}

// noAuthError explains why nothing could be offered, naming both halves — an unusable
// agent and any key file found but skipped — since the fix depends on which one the user
// meant to use. Only reached without a prompter, which always has a method left to try.
func noAuthError(agentErr error, skipped []string) error {
	var b strings.Builder
	b.WriteString("sshx: no usable authentication: ")
	if agentErr != nil {
		b.WriteString(strings.TrimPrefix(agentErr.Error(), "sshx: "))
	} else {
		b.WriteString("the ssh-agent holds no identities")
	}
	if len(skipped) > 0 {
		b.WriteString("; skipped keys: " + strings.Join(skipped, ", "))
	} else {
		b.WriteString("; no usable private key found (try `ssh-add ~/.ssh/id_ed25519`)")
	}
	return errors.New(b.String())
}

// UnknownHostKeyError is returned by Connect when the host is met for the first time and
// no fingerprint has been approved. It carries what the UI needs to ask the user, and
// nothing is written to known_hosts: a first-contact MITM is not waved through silently.
type UnknownHostKeyError struct {
	Hostname    string // the host as presented to the host-key callback
	Fingerprint string // ssh.FingerprintSHA256 of the presented key
	KeyType     string // key.Type(), e.g. "ssh-ed25519"
}

func (e *UnknownHostKeyError) Error() string {
	return fmt.Sprintf("sshx: unknown host key for %s: %s %s", e.Hostname, e.KeyType, e.Fingerprint)
}

// Connect resolves auth, host-key policy and address from h and dials. An unknown host
// aborts with an error unwrapping to *UnknownHostKeyError and appends nothing to
// known_hosts — the caller decides whether to trust the key and retries through
// ConnectTrusting.
//
// p answers whatever the server asks interactively. It is called from inside the
// handshake, so it blocks the dial while the user types. A nil p offers public keys only.
func Connect(h store.Host, p Prompter) (*Client, error) {
	return connect(h, "", p)
}

// ConnectTrusting dials h like Connect, but an unknown host whose key matches
// fingerprint is appended to known_hosts and accepted. A key that does not match is
// refused: it changed between the prompt and this retry, which is the swap to catch.
func ConnectTrusting(h store.Host, fingerprint string, p Prompter) (*Client, error) {
	return connect(h, fingerprint, p)
}

// connect is the shared dial body. trustedFP is empty for a plain TOFU-guarded
// dial and the user-approved fingerprint for a trusting retry.
func connect(h store.Host, trustedFP string, p Prompter) (*Client, error) {
	auths, err := authMethods(h, p)
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
		Auth: auths,
		// Ask for the key types already trusted for this host; without it the server may
		// answer with a type we have no entry for, which reads as a key mismatch. Empty
		// for an unknown host means library defaults.
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

// ConnectAddr dials addr with the supplied config. Tests pass their own host-key
// callback and auth method.
func ConnectAddr(addr string, cfg *ssh.ClientConfig) (*Client, error) {
	cl, err := ssh.Dial("tcp", addr, cfg)
	if err != nil {
		return nil, fmt.Errorf("sshx: dial %s: %w", addr, err)
	}
	return newClient(cl), nil
}

// Shell opens an interactive login shell with a pty of the given size.
func (c *Client) Shell(cols, rows int) (*Session, error) {
	return c.startPTY(cols, rows, func(s *ssh.Session) error { return s.Shell() })
}

// Command runs cmd on a pty of the given size, as Shell does for a login shell. The pty
// is what a full-screen program needs to draw at all.
func (c *Client) Command(cmd string, cols, rows int) (*Session, error) {
	return c.startPTY(cols, rows, func(s *ssh.Session) error { return s.Start(cmd) })
}

// Output runs cmd on a channel of its own, without a pty, and returns its stdout — for
// the small questions hop asks a host about itself. stderr is discarded: callers treat
// "no answer" and "a bad answer" alike.
func (c *Client) Output(cmd string) (string, error) {
	sess, err := c.ssh.NewSession()
	if err != nil {
		return "", fmt.Errorf("sshx: new session: %w", err)
	}
	defer sess.Close()

	// Bounded, because these questions reach parts of a host that hang — a `getent passwd`
	// against an unreachable LDAP never returns. Closing the session is what unblocks the
	// read, so the timer closes it and Output returns the resulting error.
	done := make(chan struct{})
	defer close(done)
	go func() {
		t := time.NewTimer(outputTimeout)
		defer t.Stop()
		select {
		case <-done:
		case <-t.C:
			sess.Close()
		}
	}()

	out, err := sess.Output(cmd)
	if err != nil {
		return "", fmt.Errorf("sshx: run %q: %w", cmd, err)
	}
	return string(out), nil
}

// outputTimeout bounds a single Output call: generous for a healthy host, short enough
// that an unhealthy one is not waited on.
const outputTimeout = 10 * time.Second

// startPTY opens a session, requests a pty, wires the std streams with stdout and stderr
// merged into one ordered stream, and hands the prepared session to start.
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

	// Merge stdout and stderr into one ordered stream.
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
	// Close the write end once both copiers finish, so readers see EOF.
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

// hostKeyDB opens ~/.ssh/known_hosts, creating the file and its directory on first run,
// and returns the parsed database with its path.
func hostKeyDB() (*knownhosts.HostKeyDB, string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, "", fmt.Errorf("sshx: locate home dir: %w", err)
	}
	sshDir := filepath.Join(home, ".ssh")
	khPath := filepath.Join(sshDir, "known_hosts")

	// knownhosts.NewDB needs both to exist.
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

// tofuHostKeyCallback verifies the presented key against db: a known key is accepted, a
// changed one rejected outright.
//
// First contact is decided by trustedFP. Empty returns an *UnknownHostKeyError and
// writes nothing, so the caller can ask the user. With a user-approved fingerprint, a
// matching key is appended to khPath and reported through recorded; a non-matching one
// is refused, since the key changed after approval.
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
