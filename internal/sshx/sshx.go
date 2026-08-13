// Package sshx implements the pure-Go SSH engine for hop, over
// golang.org/x/crypto/ssh.
//
// It authenticates by public key, offering both the running OpenSSH agent (see
// agent_windows.go / agent_unix.go) and private keys from disk (see keys.go).
// Host-key verification is a TOFU wrapper around ~/.ssh/known_hosts.
//
// With a Prompter (see prompt.go), keys are followed by keyboard-interactive and
// password — how a 2FA host asks for its verification code. Those are answered
// inside the handshake rather than by retrying the dial, since a one-time code
// cannot be replayed.
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

// dialTimeout bounds the TCP connect for a connection attempt — an unreachable
// host fails here rather than hanging. It deliberately does not bound the
// handshake (ssh.ClientConfig.Timeout does not either): an interactive
// authentication is part of the handshake, and a clock running while the user
// reads a code off their phone would time out the dials most in need of
// patience. A prompt the user walks away from is ended by dismissing it, which
// is what makes the Prompter's cancel the way out.
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

	// lost is closed once the transport under this client has gone — the server
	// went away, the network dropped it, or it was closed from here. waitErr is
	// why, written before the close and therefore safe to read after observing it
	// (see LostErr). A zero Client has a nil channel, which never fires: it is not
	// connected, so it cannot be lost.
	lost    chan struct{}
	waitErr error
}

// newClient wraps an established ssh.Client and starts the two goroutines that
// watch the connection: one parked on Wait, which is what turns a dropped
// transport into a closed Lost channel, and the keepalive below, which is what
// makes a silently blackholed connection reach that Wait at all.
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
// Without them a blackholed connection — suspended laptop, dropped VPN, expired NAT
// entry — is never noticed: nothing is written, so TCP never complains and the
// shell just stops updating. The probe is what the UI's reconnect offer stands on.
const (
	keepaliveInterval = 15 * time.Second
	keepaliveTimeout  = 10 * time.Second
	keepaliveMisses   = 3
)

// keepalive probes the server until it stops answering, then closes the
// connection — which unblocks the Wait above and fires Lost. It returns as soon
// as the connection is gone for any other reason.
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

// ping sends OpenSSH's keepalive global request and reports whether the server
// answered at all. A *failure* reply is an answer: the request type is only there
// to be replied to, and every server that speaks SSH replies to an unknown one.
// Only a transport error — or no reply inside keepaliveTimeout, which is the case
// on a connection that is silently gone — counts as a miss.
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

// Lost is closed when the connection under this client has gone. Blocking on it
// is how the UI learns a session died without polling; a zero Client's channel is
// nil, so it blocks forever, which is the honest answer for a client that never
// connected.
func (c *Client) Lost() <-chan struct{} { return c.lost }

// IsLost reports, without blocking, whether the connection has gone. It is the
// question a shell's exit has to ask: a shell that ended because the transport
// died is a dropped session, not somebody typing "exit".
func (c *Client) IsLost() bool {
	select {
	case <-c.lost:
		return true
	default:
		return false
	}
}

// LostErr is why the connection went, or nil while it is still up. Reading
// waitErr is safe only after the channel's close has been observed — which is
// exactly what the select does — because that close is what publishes the write.
func (c *Client) LostErr() error {
	select {
	case <-c.lost:
		return c.waitErr
	default:
		return nil
	}
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

// authMethods assembles the auth to offer for h: the agent's identities plus
// private keys from disk (the host's IdentityFile, else the default ~/.ssh keys).
//
// Neither source is required, only their union — an agent holding no identities is
// normal on macOS, and an agent with the right key makes the files irrelevant.
// Failing only when both are empty is what makes hop connect wherever ssh does.
//
// The two are merged into a *single* publickey method, because the client tries
// each method name at most once: offered separately, an empty agent would swallow
// the attempt and the key files would never be reached.
//
// With a prompter, keyboard-interactive and password follow, in ssh's own order. A
// hardened `AuthenticationMethods publickey,keyboard-interactive` server answers
// the key with a *partial* success and then requires them. Both are wrapped in
// ssh.RetryableAuthMethod, so a mistyped code is another prompt inside the same
// handshake — a fresh dial would need a fresh code. A prompter also makes "no keys
// at all" survivable.
func authMethods(h store.Host, p Prompter) ([]ssh.AuthMethod, error) {
	signers, agentErr := agentSigners()

	keys, skipped := keySigners(h.IdentityFile)
	signers = append(signers, keys...)

	var methods []ssh.AuthMethod
	if len(signers) > 0 {
		methods = append(methods, ssh.PublicKeys(signers...))
	}
	if p != nil {
		// One wrapper shared by both methods, so a cancel on either ends the dial
		// rather than moving the user on to the other one.
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

// agentSigners returns the identities held by the OpenSSH agent. The connection
// is deliberately left open: each signer signs over it, so closing it here would
// break every signature it produces. It is released when the process exits.
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

// noAuthError explains why nothing could be offered, naming both halves of the
// failure — an unusable agent and any key file that was found but skipped —
// since the fix ("add your key to the agent", "fix that path") depends on which
// one the user actually meant to use. It is only reached without a prompter: a
// caller that can ask the user something always has an interactive method left
// to try, so an empty agent is no longer a dead end there.
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
//
// p answers whatever the server asks interactively (a 2FA verification code, a
// password). It is called from inside the handshake, so it blocks the dial while
// the user types. A nil p offers public keys only, which is what a caller with
// no way to ask a human should pass.
func Connect(h store.Host, p Prompter) (*Client, error) {
	return connect(h, "", p)
}

// ConnectTrusting dials h like Connect, but when the host is unknown and the
// presented key's fingerprint equals fingerprint, the key is appended to
// known_hosts and accepted. A presented key that does not match fingerprint is
// refused: it means the key changed between the prompt that produced fingerprint
// and this retry, which is exactly the swap the confirmation was there to catch.
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
	return newClient(cl), nil
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

// Output runs cmd on a channel of its own — no pty — and returns its stdout. It is
// for the small questions hop asks a host about itself, like which login shell the
// account has. stderr is discarded: callers treat "no answer" and "a bad answer"
// alike.
func (c *Client) Output(cmd string) (string, error) {
	sess, err := c.ssh.NewSession()
	if err != nil {
		return "", fmt.Errorf("sshx: new session: %w", err)
	}
	defer sess.Close()

	// Bounded, because the questions asked here reach parts of a host that hang: a
	// `getent passwd` on a box whose NSS talks to an unreachable LDAP or sssd never
	// returns, and an unbounded wait would strand this channel and the caller's
	// goroutine for the life of the process. Closing the session is what unblocks the
	// read, so the timer closes it and Output returns the error that comes of it.
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

// outputTimeout bounds a single Output call. Generous for a question a healthy host
// answers instantly, short enough that an unhealthy one is not waited on.
const outputTimeout = 10 * time.Second

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

// tofuHostKeyCallback verifies the presented key against db: a known key is
// accepted, a changed one rejected outright.
//
// First contact is decided by trustedFP. Empty means untrusted — the callback
// returns an *UnknownHostKeyError and writes nothing, so the caller can ask the
// user first. Holding a user-approved fingerprint, a matching key is appended to
// khPath and accepted (reported through recorded); a non-matching one is refused,
// since the key changed after the fingerprint was approved.
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
