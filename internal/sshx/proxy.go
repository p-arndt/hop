// Proxy dialling: reaching a host that no direct TCP connect can, either through a local
// broker program (ProxyCommand) or through a bastion hop already knows how to log into
// (ProxyJump). Both end in the same place — an established net.Conn handed to
// ssh.NewClientConn — because x/crypto/ssh cares about the stream, not how it was opened.

package sshx

import (
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"hop/internal/store"
)

// proxyStderrLimit caps how much of a proxy program's stderr is kept for the error
// message. Enough for the sentence that explains the failure ("An error occurred (
// TargetNotConnected)"), not enough for a runaway program to eat memory.
const proxyStderrLimit = 4 << 10

// dialProxyCommand runs cmdline and returns its stdin/stdout as a net.Conn carrying the
// SSH transport, which is exactly what OpenSSH's ProxyCommand contract is.
//
// The tokens ssh expands are expanded here too: %h the hostname dialled, %p the port, %r
// the remote user, %n the name as typed (hop's alias), %% a literal percent. Anything
// else is left alone rather than guessed at.
func dialProxyCommand(cmdline, host string, port int, user, alias string) (net.Conn, error) {
	expanded := expandProxyTokens(cmdline, host, port, user, alias)

	argv, err := splitProxyCommand(expanded)
	if err != nil {
		return nil, err
	}

	for i := range argv {
		argv[i] = expandTilde(argv[i])
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
	// The proxy's own diagnostics go here, not to hop's terminal: a broker that refuses
	// explains itself on stderr, and without keeping it the dial fails as a bare EOF.
	errBuf := &boundedBuffer{limit: proxyStderrLimit}
	cmd.Stderr = errBuf

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("sshx: start proxy %q: %w", argv[0], err)
	}

	return &procConn{
		cmd:    cmd,
		r:      stdout,
		w:      stdin,
		stderr: errBuf,
		name:   argv[0],
		target: tcpAddr(net.JoinHostPort(host, strconv.Itoa(port))),
	}, nil
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
			// An unknown token is passed through untouched; inventing a value for it
			// would silently change what the user's command means.
			b.WriteByte('%')
			b.WriteByte(s[i+1])
		}
		i++
	}
	return b.String()
}

// shellMeta are the characters a shell reads as control flow or substitution. hop runs
// the proxy program directly, with no shell in between, so a command line containing one
// is refused rather than handed to `sh -c`: a ProxyCommand can arrive from an imported
// config file, and that path must not become one that runs arbitrary shell.
//
// Deliberately absent are the characters a shell would merely *expand*: globs, `=`, `#`
// and `~`. They occur unquoted in ordinary commands — the issue's own
// `--parameters portNumber=%p` is one — and passing them through as literal argv is what
// running without a shell means. A leading `~/` is the one expansion done here (see
// expandTilde), because a path is useless without it.
const shellMeta = "|&;<>()$`\n"

// ErrProxyNeedsShell is returned for a ProxyCommand that only a shell could run.
var ErrProxyNeedsShell = errors.New("sshx: proxy command needs a shell")

// splitProxyCommand parses cmdline into argv the way a shell would for the simple case:
// whitespace separates words, and single or double quotes group them. Backslash escapes
// are honoured inside double quotes and outside quotes, matching sh closely enough that
// a quoted Windows path survives.
//
// Anything beyond that — a pipe, a redirect, a variable, a glob — is refused with
// ErrProxyNeedsShell, since running it would need the shell hop deliberately omits.
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
			case c == '\\' && i+1 < len(cmdline) && (cmdline[i+1] == '"' || cmdline[i+1] == '\\'):
				escaped = true
			default:
				cur.WriteByte(c)
			}
		case c == '\'' || c == '"':
			quote = c
			inWord = true
		case c == '\\':
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

// expandTilde resolves a leading "~/" against the home directory, the one piece of shell
// expansion hop does for a proxy command: without it a `~/bin/tunnel` fails to exec, and
// there is nothing ambiguous about what it means. A bare "~user/…" is left alone.
func expandTilde(s string) string {
	if !strings.HasPrefix(s, "~/") && !strings.HasPrefix(s, `~\`) {
		return s
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return s
	}
	return filepath.Join(home, s[2:])
}

// procConn adapts a running program's pipes to net.Conn. Only Read, Write and Close
// carry meaning for an SSH transport; the address and deadline methods exist to satisfy
// the interface, and the deadlines report failure rather than pretending to be set.
type procConn struct {
	cmd    *exec.Cmd
	r      io.ReadCloser
	w      io.WriteCloser
	stderr *boundedBuffer
	name   string
	// target is the host:port this stream reaches. RemoteAddr must report it rather than
	// the proxy program, because the host-key check reads the remote address to decide
	// which known_hosts entry applies — a program name there fails the lookup outright.
	target tcpAddr

	once sync.Once
}

func (p *procConn) Read(b []byte) (int, error) {
	n, err := p.r.Read(b)
	if err != nil && err != io.EOF {
		return n, err
	}
	if err == io.EOF && n == 0 {
		// The proxy hung up. Its stderr is the only account of why, so it becomes the
		// error the user sees instead of a bare "EOF" from the handshake.
		if msg := p.stderr.String(); msg != "" {
			return 0, fmt.Errorf("sshx: proxy %s exited: %s", p.name, msg)
		}
	}
	return n, err
}

func (p *procConn) Write(b []byte) (int, error) { return p.w.Write(b) }

// Close shuts both pipes and kills the program. The wait is what reaps it; without one
// every failed dial would leave a broker process behind.
func (p *procConn) Close() error {
	var err error
	p.once.Do(func() {
		p.w.Close()
		p.r.Close()
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

// proxyAddr names the local end of a proxy-command connection — the program, since there
// is no socket on this side. Nothing in the SSH client reads it.
type proxyAddr string

func (a proxyAddr) Network() string { return "proxycommand" }
func (a proxyAddr) String() string  { return string(a) }

// tcpAddr is a host:port the SSH machinery can parse. It is a plain string rather than a
// *net.TCPAddr because the host may be a name the proxy resolves and hop never does.
type tcpAddr string

func (a tcpAddr) Network() string { return "tcp" }
func (a tcpAddr) String() string  { return string(a) }

// boundedBuffer collects at most limit bytes and silently drops the rest — a proxy that
// writes endlessly to stderr must not grow hop's memory with it.
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
	return len(p), nil // report a full write: dropping output is not the writer's problem
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
// chain; hop takes the first and ignores the rest, since a second bastion needs a second
// authenticated dial that the first one's credentials do not describe.
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

// JumpResolver turns a ProxyJump target into the store entry hop should dial. It lets the
// bastion be named by its hop alias — so its own key, port and user apply — and returns
// ok=false when the name is not one, leaving parseJump's bare host to stand on its own.
type JumpResolver func(name string) (store.Host, bool)

// jumpTarget builds the host to dial for the bastion: the resolver's entry when the name
// is a known alias, otherwise a bare host. Explicit user and port from the directive win
// over the entry's, since that is what the user wrote at the point of use.
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
