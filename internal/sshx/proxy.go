// Proxy dialling: ProxyCommand runs a local broker program, ProxyJump goes through a
// bastion. Both end at ssh.NewClientConn, which takes any net.Conn.

package sshx

import (
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"hop/internal/store"
)

// proxyStderrLimit bounds the stderr kept for the error message, so a runaway program
// cannot eat memory.
const proxyStderrLimit = 4 << 10

// stderrDrainGrace is how long Read waits, once the proxy's stdout has ended, for the
// copier to finish draining stderr. The two pipes close independently, so without the
// wait the diagnosis is usually still in flight and the dial reports a bare EOF instead.
// Bounded, because a forked grandchild can hold the write end open indefinitely.
const stderrDrainGrace = 500 * time.Millisecond

// proxyFirstByteTimeout stands in for ClientConfig.Timeout, which only ssh.Dial reads.
// The first byte is the server's version banner, sent before authentication, so bounding
// it does not cut short a user typing a 2FA code. Atomic so a test may shorten it while
// an earlier watchdog still reads it.
var proxyFirstByteTimeout = func() *atomic.Int64 {
	v := new(atomic.Int64)
	v.Store(int64(30 * time.Second))
	return v
}()

func firstByteTimeout() time.Duration { return time.Duration(proxyFirstByteTimeout.Load()) }

// proxyFirstByteTimeoutForTest shortens the watchdog and returns the restore func.
func proxyFirstByteTimeoutForTest(d time.Duration) func() {
	prev := proxyFirstByteTimeout.Swap(int64(d))
	return func() { proxyFirstByteTimeout.Store(prev) }
}

// dialProxyCommand runs cmdline and returns its stdin/stdout as a net.Conn, which is
// OpenSSH's ProxyCommand contract. Tokens: %h host, %p port, %r remote user, %n alias,
// %% a literal percent; anything else is left alone.
func dialProxyCommand(cmdline, host string, port int, user, alias string) (net.Conn, error) {
	expanded := expandProxyTokens(cmdline, host, port, user, alias)

	argv, err := splitProxyCommand(expanded)
	if err != nil {
		return nil, err
	}

	// The only shell expansion hop does here: `~/bin/tunnel` would otherwise fail to exec.
	for i := range argv {
		argv[i] = expandHome(argv[i])
	}

	cmd := exec.Command(argv[0], argv[1:]...)

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("sshx: proxy stdin: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("sshx: proxy stdout: %w", err)
	}
	// A broker that refuses explains itself on stderr; without it the dial fails as a bare
	// EOF. The pipe is an *os.File hop owns, not a plain io.Writer: os/exec would then run
	// its own copier and cmd.Wait would block on it for as long as a forked grandchild
	// holds the pipe — which `aws ssm` (session-manager-plugin) does.
	errBuf := &boundedBuffer{limit: proxyStderrLimit}
	errRead, errWrite, err := os.Pipe()
	if err != nil {
		return nil, fmt.Errorf("sshx: proxy stderr pipe: %w", err)
	}
	cmd.Stderr = errWrite

	if err := cmd.Start(); err != nil {
		errRead.Close()
		errWrite.Close()
		return nil, fmt.Errorf("sshx: start proxy %q: %w", argv[0], err)
	}
	// The child has its own descriptor; hop's copy must go or the reader never sees EOF.
	errWrite.Close()
	stderrDone := make(chan struct{})
	go func() {
		defer close(stderrDone)
		io.Copy(errBuf, errRead)
		errRead.Close()
	}()

	pc := &procConn{
		cmd:        cmd,
		r:          stdout,
		w:          stdin,
		stderr:     errBuf,
		errRead:    errRead,
		stderrDone: stderrDone,
		name:       argv[0],
		target:     tcpAddr(net.JoinHostPort(host, strconv.Itoa(port))),
		alive:      make(chan struct{}),
	}
	pc.watchFirstByte()
	return pc, nil
}

// expandProxyTokens substitutes ssh's ProxyCommand tokens. It walks the string once so a
// literal %% cannot have its second percent read as the start of another token.
func expandProxyTokens(s, host string, port int, user, alias string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] != '%' || i+1 >= len(s) {
			b.WriteByte(s[i])
			continue
		}
		switch s[i+1] {
		case 'h':
			b.WriteString(host)
		case 'p':
			b.WriteString(strconv.Itoa(port))
		case 'r':
			b.WriteString(user)
		case 'n':
			b.WriteString(alias)
		case '%':
			b.WriteByte('%')
		default:
			// Unknown tokens pass through: inventing a value would change what the
			// command means.
			b.WriteByte('%')
			b.WriteByte(s[i+1])
		}
		i++
	}
	return b.String()
}

// shellMeta are control-flow and substitution characters. A command line containing one
// is refused rather than handed to `sh -c`: a ProxyCommand can come from an imported
// config, and that path must not run arbitrary shell.
//
// Characters a shell would merely expand (globs, `=`, `#`, `~`) are absent on purpose —
// they occur unquoted in ordinary commands, `--parameters portNumber=%p` among them.
const shellMeta = "|&;<>()$`\n"

// ErrProxyNeedsShell is returned for a ProxyCommand that only a shell could run.
var ErrProxyNeedsShell = errors.New("sshx: proxy command needs a shell")

// splitProxyCommand parses cmdline into argv: whitespace separates words, quotes group
// them, backslash escapes per isEscapable. A pipe, redirect or variable is refused with
// ErrProxyNeedsShell.
func splitProxyCommand(cmdline string) ([]string, error) {
	var (
		argv    []string
		cur     strings.Builder
		inWord  bool
		quote   byte // 0, '\'' or '"'
		escaped bool
	)

	flush := func() {
		if inWord {
			argv = append(argv, cur.String())
			cur.Reset()
			inWord = false
		}
	}

	for i := 0; i < len(cmdline); i++ {
		c := cmdline[i]
		switch {
		case escaped:
			cur.WriteByte(c)
			inWord = true
			escaped = false
		case quote == '\'':
			if c == '\'' {
				quote = 0
			} else {
				cur.WriteByte(c)
			}
		case quote == '"':
			switch {
			case c == '"':
				quote = 0
			case c == '\\' && i+1 < len(cmdline) && isEscapable(cmdline[i+1]):
				escaped = true
			default:
				cur.WriteByte(c)
			}
		case c == '\'' || c == '"':
			quote = c
			inWord = true
		case c == '\\' && i+1 < len(cmdline) && isEscapable(cmdline[i+1]):
			escaped = true
		case c == ' ' || c == '\t':
			flush()
		case strings.IndexByte(shellMeta, c) >= 0:
			return nil, fmt.Errorf("%w: %q in %q — run it through a wrapper script instead", ErrProxyNeedsShell, string(c), cmdline)
		default:
			cur.WriteByte(c)
			inWord = true
		}
	}
	if quote != 0 || escaped {
		return nil, fmt.Errorf("sshx: unterminated quote in proxy command %q", cmdline)
	}
	flush()

	if len(argv) == 0 {
		return nil, errors.New("sshx: proxy command is empty")
	}
	return argv, nil
}

// isEscapable reports whether a backslash before c means "take c literally". Anywhere
// else the backslash stands, so an unquoted Windows path keeps its separators.
func isEscapable(c byte) bool {
	return c == '"' || c == '\'' || c == ' ' || c == '\t' || c == '\\'
}

// procConn adapts a running program's pipes to net.Conn. Only Read, Write and Close
// carry meaning; the rest satisfies the interface.
type procConn struct {
	cmd    *exec.Cmd
	r      io.ReadCloser
	w      io.WriteCloser
	stderr *boundedBuffer
	// errRead is hop's end of the stderr pipe. Close shuts it, so the copier stops even
	// while a grandchild holds the write end.
	errRead *os.File
	// stderrDone is closed once the copier has drained the pipe, so Read can wait for the
	// diagnosis instead of racing it.
	stderrDone chan struct{}
	name       string
	// alive is closed by the first successful read, stopping the watchdog.
	alive     chan struct{}
	aliveOnce sync.Once
	timedOut  atomic.Bool
	silentFor atomic.Int64 // the window that elapsed, for the message
	// target is what RemoteAddr reports. It must be the host:port, not the program: the
	// host-key check reads it to pick the known_hosts entry.
	target tcpAddr

	once sync.Once
}

// watchFirstByte closes the connection if the proxy says nothing in time, which unblocks
// the read parked in the handshake.
func (p *procConn) watchFirstByte() {
	go func() {
		window := firstByteTimeout()
		t := time.NewTimer(window)
		defer t.Stop()
		select {
		case <-p.alive:
		case <-t.C:
			p.timedOut.Store(true)
			p.silentFor.Store(int64(window))
			p.Close()
		}
	}()
}

func (p *procConn) Read(b []byte) (int, error) {
	n, err := p.r.Read(b)
	if n > 0 {
		p.aliveOnce.Do(func() { close(p.alive) })
	}
	if p.timedOut.Load() {
		return n, fmt.Errorf("sshx: proxy %s sent nothing within %s", p.name, time.Duration(p.silentFor.Load()))
	}
	if err != nil && err != io.EOF {
		return n, err
	}
	if err == io.EOF && n == 0 {
		// The proxy hung up; its stderr is the only account of why, and it travels on a
		// pipe of its own that has not necessarily drained yet.
		p.waitStderr()
		if msg := p.stderr.String(); msg != "" {
			return 0, fmt.Errorf("sshx: proxy %s exited: %s", p.name, msg)
		}
	}
	return n, err
}

// waitStderr gives the copier a bounded moment to finish before its buffer is read.
func (p *procConn) waitStderr() {
	if p.stderrDone == nil {
		return
	}
	t := time.NewTimer(stderrDrainGrace)
	defer t.Stop()
	select {
	case <-p.stderrDone:
	case <-t.C:
	}
}

func (p *procConn) Write(b []byte) (int, error) { return p.w.Write(b) }

// Close shuts the pipes and kills the program; the wait reaps it, or a failed dial would
// leave the broker running. It cannot hang on a grandchild — see the stderr pipe above.
func (p *procConn) Close() error {
	var err error
	p.once.Do(func() {
		p.aliveOnce.Do(func() { close(p.alive) })
		p.w.Close()
		p.r.Close()
		if p.errRead != nil {
			p.errRead.Close()
		}
		if p.cmd.Process != nil {
			p.cmd.Process.Kill()
		}
		err = p.cmd.Wait()
	})
	return err
}

func (p *procConn) LocalAddr() net.Addr  { return proxyAddr(p.name) }
func (p *procConn) RemoteAddr() net.Addr { return p.target }

func (p *procConn) SetDeadline(time.Time) error {
	return errors.New("sshx: deadlines are not supported on a proxy-command connection")
}
func (p *procConn) SetReadDeadline(t time.Time) error  { return p.SetDeadline(t) }
func (p *procConn) SetWriteDeadline(t time.Time) error { return p.SetDeadline(t) }

// proxyAddr names the local end: the program, since there is no socket on this side.
type proxyAddr string

func (a proxyAddr) Network() string { return "proxycommand" }
func (a proxyAddr) String() string  { return string(a) }

// tcpAddr is a host:port the SSH machinery can parse. A string rather than a
// *net.TCPAddr, because the proxy may resolve a name hop never does.
type tcpAddr string

func (a tcpAddr) Network() string { return "tcp" }
func (a tcpAddr) String() string  { return string(a) }

// boundedBuffer keeps at most limit bytes and drops the rest.
type boundedBuffer struct {
	limit int
	mu    sync.Mutex
	buf   []byte
}

func (b *boundedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if room := b.limit - len(b.buf); room > 0 {
		if len(p) < room {
			room = len(p)
		}
		b.buf = append(b.buf, p[:room]...)
	}
	return len(p), nil // a full write: dropping output is not the writer's problem
}

func (b *boundedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return strings.TrimSpace(string(b.buf))
}

// JumpHost is a bastion parsed out of a ProxyJump directive.
type JumpHost struct {
	User string // empty means: whatever the jump's own store entry or the OS user says
	Host string
	Port int // 0 means 22
}

// parseJump reads one ProxyJump value: [user@]host[:port]. ssh accepts a comma-separated
// chain; hop takes the first and ignores the rest.
func parseJump(v string) (JumpHost, error) {
	v = strings.TrimSpace(v)
	if i := strings.IndexByte(v, ','); i >= 0 {
		v = strings.TrimSpace(v[:i])
	}
	if v == "" || strings.EqualFold(v, "none") {
		return JumpHost{}, errors.New("sshx: empty proxy jump")
	}

	var j JumpHost
	if i := strings.LastIndexByte(v, '@'); i >= 0 {
		j.User, v = v[:i], v[i+1:]
	}

	// A bracketed literal is IPv6; a bare colon in a plain host is the port separator.
	if strings.HasPrefix(v, "[") {
		end := strings.IndexByte(v, ']')
		if end < 0 {
			return JumpHost{}, fmt.Errorf("sshx: unterminated [ in proxy jump %q", v)
		}
		j.Host = v[1:end]
		rest := v[end+1:]
		if strings.HasPrefix(rest, ":") {
			p, err := strconv.Atoi(rest[1:])
			if err != nil || p < 1 || p > 65535 {
				return JumpHost{}, fmt.Errorf("sshx: bad port in proxy jump %q", v)
			}
			j.Port = p
		}
	} else if i := strings.LastIndexByte(v, ':'); i >= 0 && !strings.Contains(v[:i], ":") {
		p, err := strconv.Atoi(v[i+1:])
		if err != nil || p < 1 || p > 65535 {
			return JumpHost{}, fmt.Errorf("sshx: bad port in proxy jump %q", v)
		}
		j.Host, j.Port = v[:i], p
	} else {
		j.Host = v
	}

	if j.Host == "" {
		return JumpHost{}, fmt.Errorf("sshx: no host in proxy jump %q", v)
	}
	return j, nil
}

// JumpResolver looks a ProxyJump name up as a hop alias, so the bastion's own key, port
// and user apply. ok=false leaves parseJump's bare host to stand on its own.
type JumpResolver func(name string) (store.Host, bool)

// jumpTarget builds the bastion to dial. An explicit user or port in the directive wins
// over the resolved entry's, being what the user wrote at the point of use.
func jumpTarget(j JumpHost, resolve JumpResolver) store.Host {
	var h store.Host
	if resolve != nil {
		if found, ok := resolve(j.Host); ok {
			h = found
			h.Forwards = nil // the bastion is a transport here, not a session
		}
	}
	if h.HostName == "" {
		h.HostName = j.Host
	}
	if j.User != "" {
		h.User = j.User
	}
	if j.Port != 0 {
		h.Port = j.Port
	}
	return h
}
