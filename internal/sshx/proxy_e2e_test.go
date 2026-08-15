package sshx

import (
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/skeema/knownhosts"
	"golang.org/x/crypto/ssh"

	"hop/internal/store"
)

// proxyEnvAddr names the address the re-executed test binary should pipe to when it
// stands in for a ProxyCommand.
const proxyEnvAddr = "HOP_TEST_PROXY_ADDR"

// runProxyHelperIfRequested lets this binary act as the proxy program when the env var is
// set: it dials the address and shovels bytes between the socket and its own stdio, which
// is the whole contract OpenSSH's ProxyCommand defines. Called first from TestMain (in
// twofactor_docker_test.go), so the re-executed child never reaches the test runner.
//
// Re-execing ourselves keeps the test free of nc, which neither Windows nor a minimal
// container is guaranteed to have.
func runProxyHelperIfRequested() {
	addr := os.Getenv(proxyEnvAddr)
	if addr == "" {
		return
	}

	conn, err := net.Dial("tcp", addr)
	if err != nil {
		fmt.Fprintln(os.Stderr, "test proxy: dial:", err)
		os.Exit(1)
	}
	go io.Copy(conn, os.Stdin)
	io.Copy(os.Stdout, conn)
	conn.Close()
	os.Exit(0)
}

// proxyCommandFor builds the ProxyCommand line that re-execs this test binary. The
// address travels in the environment (proxyEnvAddr), not on the command line. The path is
// quoted so a build directory containing a space still parses.
func proxyCommandFor(t *testing.T) string {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatalf("locate test binary: %v", err)
	}
	return strconv.Quote(exe) + " -test.run=TestNothingRuns"
}

// TestNothingRuns is the harmless target of the -test.run above: the re-executed binary
// never reaches the test runner, since TestMain diverts it, but a real name keeps the
// flag valid if it ever did.
func TestNothingRuns(t *testing.T) {}

// A ProxyCommand host must complete a real SSH handshake over the program's pipes and
// run a real command on the far side — the transport path that the parsing tests do not
// touch.
func TestProxyCommandCarriesRealSession(t *testing.T) {
	home := fakeHome(t)
	srv := startEchoSSHServer(t)

	// The proxy reaches the server; the host itself names an address nothing listens on,
	// so a leaked direct dial would fail rather than quietly pass the test.
	h := store.Host{
		Alias:        "ssm",
		HostName:     "proxied.invalid",
		Port:         22,
		User:         "deploy",
		ProxyCommand: proxyCommandFor(t),
	}
	trustHostKey(t, home, "proxied.invalid", 22, srv.hostKey)

	t.Setenv(proxyEnvAddr, srv.addr)

	cl, err := Connect(h, nopPrompter{})
	if err != nil {
		t.Fatalf("Connect through ProxyCommand: %v", err)
	}
	defer cl.Close()

	out, err := cl.Output("hello")
	if err != nil {
		t.Fatalf("Output over the proxied connection: %v", err)
	}
	if strings.TrimSpace(out) != "HELLO" {
		t.Errorf("Output = %q, want %q", strings.TrimSpace(out), "HELLO")
	}
}

// A ProxyCommand whose program cannot start must fail the dial with a message naming the
// program, not hang or report a bare EOF.
func TestProxyCommandMissingProgramFails(t *testing.T) {
	fakeHome(t)
	h := store.Host{Alias: "x", HostName: "h.invalid", Port: 22, ProxyCommand: "hop-no-such-proxy-program --target %h"}

	_, err := Connect(h, nopPrompter{})
	if err == nil {
		t.Fatal("Connect = nil error, want a failure")
	}
	if !strings.Contains(err.Error(), "hop-no-such-proxy-program") {
		t.Errorf("error = %v, want it to name the missing program", err)
	}
}

// A ProxyJump host must log into the bastion and reach the target through it. The target
// listens on a socket the test never dials directly, so only the jump can carry it.
func TestProxyJumpDialsThroughBastion(t *testing.T) {
	home := fakeHome(t)
	target := startEchoSSHServer(t)
	bastion := startEchoSSHServer(t)

	targetHost, targetPort := splitAddr(t, target.addr)
	bastionHost, bastionPort := splitAddr(t, bastion.addr)

	trustHostKey(t, home, targetHost, targetPort, target.hostKey)
	trustHostKey(t, home, bastionHost, bastionPort, bastion.hostKey)

	h := store.Host{
		Alias:     "db01",
		HostName:  targetHost,
		Port:      targetPort,
		User:      "deploy",
		ProxyJump: fmt.Sprintf("jump@%s:%d", bastionHost, bastionPort),
	}

	cl, err := Connect(h, nopPrompter{})
	if err != nil {
		t.Fatalf("Connect through ProxyJump: %v", err)
	}
	defer cl.Close()

	out, err := cl.Output("hello")
	if err != nil {
		t.Fatalf("Output over the jumped connection: %v", err)
	}
	if strings.TrimSpace(out) != "HELLO" {
		t.Errorf("Output = %q, want %q", strings.TrimSpace(out), "HELLO")
	}
}

// A ProxyJump naming a hop alias must dial that host's own address, so the bastion keeps
// the port and user its store entry defines.
func TestProxyJumpResolvesAlias(t *testing.T) {
	home := fakeHome(t)
	target := startEchoSSHServer(t)
	bastion := startEchoSSHServer(t)

	targetHost, targetPort := splitAddr(t, target.addr)
	bastionHost, bastionPort := splitAddr(t, bastion.addr)
	trustHostKey(t, home, targetHost, targetPort, target.hostKey)
	trustHostKey(t, home, bastionHost, bastionPort, bastion.hostKey)

	prev := jumpResolver
	t.Cleanup(func() { jumpResolver = prev })
	SetJumpResolver(func(name string) (store.Host, bool) {
		if name != "bastion" {
			return store.Host{}, false
		}
		return store.Host{Alias: "bastion", HostName: bastionHost, Port: bastionPort, User: "ops"}, true
	})

	h := store.Host{Alias: "db01", HostName: targetHost, Port: targetPort, ProxyJump: "bastion"}

	cl, err := Connect(h, nopPrompter{})
	if err != nil {
		t.Fatalf("Connect through an aliased ProxyJump: %v", err)
	}
	defer cl.Close()

	if out, err := cl.Output("hello"); err != nil || strings.TrimSpace(out) != "HELLO" {
		t.Fatalf("Output = %q, %v; want HELLO", out, err)
	}
}

// A bastion hop has never met must surface as an *UnknownHostKeyError the UI can put in
// front of the user, naming the bastion — otherwise the dial fails with nothing to act on.
func TestProxyJumpUnknownBastionKeyIsActionable(t *testing.T) {
	home := fakeHome(t)
	target := startEchoSSHServer(t)
	bastion := startEchoSSHServer(t)

	targetHost, targetPort := splitAddr(t, target.addr)
	bastionHost, bastionPort := splitAddr(t, bastion.addr)
	trustHostKey(t, home, targetHost, targetPort, target.hostKey)
	// The bastion is deliberately not trusted.

	h := store.Host{
		Alias:     "db01",
		HostName:  targetHost,
		Port:      targetPort,
		ProxyJump: net.JoinHostPort(bastionHost, strconv.Itoa(bastionPort)),
	}

	_, err := Connect(h, nopPrompter{})
	var unknown *UnknownHostKeyError
	if !errors.As(err, &unknown) {
		t.Fatalf("Connect = %v, want an *UnknownHostKeyError for the bastion", err)
	}
	if !strings.Contains(unknown.Hostname, bastionHost) {
		t.Errorf("UnknownHostKeyError.Hostname = %q, want the bastion %q", unknown.Hostname, bastionHost)
	}
}

// nopPrompter satisfies the Prompter interface without ever answering: the test servers
// accept without auth, so it is only there to keep authMethods from refusing a host with
// no keys and no agent.
type nopPrompter struct{}

func (nopPrompter) Ask(Challenge) ([]string, error) {
	return nil, errors.New("test: no interactive auth expected")
}

// echoSSHServer is a real SSH server for these tests: it accepts without auth, answers
// "exec" by upper-casing the command, and serves direct-tcpip so it can also stand in
// for a bastion.
type echoSSHServer struct {
	addr    string
	hostKey ssh.PublicKey
}

func startEchoSSHServer(t *testing.T) *echoSSHServer {
	t.Helper()

	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate host key: %v", err)
	}
	signer, err := ssh.NewSignerFromKey(priv)
	if err != nil {
		t.Fatalf("host key signer: %v", err)
	}

	cfg := &ssh.ServerConfig{NoClientAuth: true}
	cfg.AddHostKey(signer)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("ssh listen: %v", err)
	}
	t.Cleanup(func() { ln.Close() })

	go func() {
		for {
			nc, err := ln.Accept()
			if err != nil {
				return
			}
			go serveEchoConn(nc, cfg)
		}
	}()

	return &echoSSHServer{addr: ln.Addr().String(), hostKey: signer.PublicKey()}
}

func serveEchoConn(nc net.Conn, cfg *ssh.ServerConfig) {
	sc, chans, reqs, err := ssh.NewServerConn(nc, cfg)
	if err != nil {
		nc.Close()
		return
	}
	defer sc.Close()
	go ssh.DiscardRequests(reqs)

	for nch := range chans {
		switch nch.ChannelType() {
		case "session":
			go serveEchoSession(nch)
		case "direct-tcpip":
			go serveDirectTCPIP(nch)
		default:
			nch.Reject(ssh.UnknownChannelType, nch.ChannelType())
		}
	}
}

// serveEchoSession answers "exec" by writing the command back upper-cased, which is
// enough for a test to tell a live session from a handshake that merely completed.
func serveEchoSession(nch ssh.NewChannel) {
	ch, reqs, err := nch.Accept()
	if err != nil {
		return
	}
	defer ch.Close()
	for req := range reqs {
		if req.Type != "exec" {
			req.Reply(false, nil)
			continue
		}
		var payload struct{ Command string }
		ssh.Unmarshal(req.Payload, &payload)
		req.Reply(true, nil)
		io.WriteString(ch, strings.ToUpper(payload.Command)+"\n")
		ch.SendRequest("exit-status", false, ssh.Marshal(struct{ Status uint32 }{0}))
		return
	}
}

// serveDirectTCPIP is the bastion half: it dials what the client asked for and splices
// the two streams, which is what x/crypto/ssh Client.Dial expects of a jump host.
func serveDirectTCPIP(nch ssh.NewChannel) {
	var payload struct {
		Host  string
		Port  uint32
		Orig  string
		OPort uint32
	}
	if err := ssh.Unmarshal(nch.ExtraData(), &payload); err != nil {
		nch.Reject(ssh.ConnectionFailed, "bad direct-tcpip payload")
		return
	}
	conn, err := net.Dial("tcp", net.JoinHostPort(payload.Host, strconv.Itoa(int(payload.Port))))
	if err != nil {
		nch.Reject(ssh.ConnectionFailed, err.Error())
		return
	}
	ch, reqs, err := nch.Accept()
	if err != nil {
		conn.Close()
		return
	}
	go ssh.DiscardRequests(reqs)
	go func() {
		io.Copy(ch, conn)
		ch.Close()
	}()
	go func() {
		io.Copy(conn, ch)
		conn.Close()
	}()
}

// trustHostKey pre-approves key for host:port in the fake home's known_hosts, standing in
// for a user who has already answered the fingerprint card.
func trustHostKey(t *testing.T, home, host string, port int, key ssh.PublicKey) {
	t.Helper()
	kh := filepath.Join(home, ".ssh", "known_hosts")
	if err := os.MkdirAll(filepath.Dir(kh), 0o700); err != nil {
		t.Fatalf("known_hosts dir: %v", err)
	}
	f, err := os.OpenFile(kh, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatalf("open known_hosts: %v", err)
	}
	defer f.Close()
	addr := net.JoinHostPort(host, strconv.Itoa(port))
	if _, err := io.WriteString(f, knownhosts.Line([]string{addr}, key)+"\n"); err != nil {
		t.Fatalf("write known_hosts: %v", err)
	}
}

func splitAddr(t *testing.T, addr string) (string, int) {
	t.Helper()
	host, portText, err := net.SplitHostPort(addr)
	if err != nil {
		t.Fatalf("split %q: %v", addr, err)
	}
	port, err := strconv.Atoi(portText)
	if err != nil {
		t.Fatalf("port in %q: %v", addr, err)
	}
	return host, port
}

// The full first-contact path through a bastion: the user approves the bastion's key,
// then the target's, and the second retry connects. Before the approved fingerprint was
// offered to the bastion's own dial, this looped — the retry met the same unknown bastion
// key and raised the same card again.
func TestProxyJumpTrustsBastionOnRetry(t *testing.T) {
	home := fakeHome(t)
	target := startEchoSSHServer(t)
	bastion := startEchoSSHServer(t)

	targetHost, targetPort := splitAddr(t, target.addr)
	bastionHost, bastionPort := splitAddr(t, bastion.addr)

	h := store.Host{
		Alias:     "db01",
		HostName:  targetHost,
		Port:      targetPort,
		ProxyJump: net.JoinHostPort(bastionHost, strconv.Itoa(bastionPort)),
	}
	_ = home

	// First contact: the bastion is the unknown one, since it is dialled first.
	_, err := Connect(h, nopPrompter{})
	var unknown *UnknownHostKeyError
	if !errors.As(err, &unknown) {
		t.Fatalf("first Connect = %v, want an *UnknownHostKeyError", err)
	}
	if unknown.Fingerprint != ssh.FingerprintSHA256(bastion.hostKey) {
		t.Fatalf("first prompt is not for the bastion")
	}

	// The user approves it. The bastion is now recorded, so the target becomes the
	// unknown one — a second card, not the same one again.
	_, err = ConnectTrusting(h, unknown.Fingerprint, nopPrompter{})
	if !errors.As(err, &unknown) {
		t.Fatalf("second Connect = %v, want an *UnknownHostKeyError for the target", err)
	}
	if unknown.Fingerprint != ssh.FingerprintSHA256(target.hostKey) {
		t.Fatalf("second prompt is not for the target: the bastion approval did not stick")
	}

	// Approving the target's key completes the dial.
	cl, err := ConnectTrusting(h, unknown.Fingerprint, nopPrompter{})
	if err != nil {
		t.Fatalf("third Connect: %v", err)
	}
	defer cl.Close()

	if out, err := cl.Output("hello"); err != nil || strings.TrimSpace(out) != "HELLO" {
		t.Fatalf("Output = %q, %v; want HELLO", out, err)
	}
}

// A ProxyJump that names its own host, directly or around a ring of aliases, must end in
// an error. Before the chain was recorded this recursed until the stack gave out, since
// each hop resolved to a host carrying the same directive.
func TestProxyJumpLoopIsRefused(t *testing.T) {
	fakeHome(t)

	prev := jumpResolver
	t.Cleanup(func() { jumpResolver = prev })
	// a jumps to b, b jumps back to a.
	SetJumpResolver(func(name string) (store.Host, bool) {
		switch name {
		case "a":
			return store.Host{Alias: "a", HostName: "a.invalid", Port: 22, ProxyJump: "b"}, true
		case "b":
			return store.Host{Alias: "b", HostName: "b.invalid", Port: 22, ProxyJump: "a"}, true
		case "self":
			return store.Host{Alias: "self", HostName: "self.invalid", Port: 22, ProxyJump: "self"}, true
		}
		return store.Host{}, false
	})

	for _, h := range []store.Host{
		{Alias: "self", HostName: "self.invalid", Port: 22, ProxyJump: "self"},
		{Alias: "a", HostName: "a.invalid", Port: 22, ProxyJump: "b"},
	} {
		t.Run(h.Alias, func(t *testing.T) {
			_, err := Connect(h, nopPrompter{})
			if err == nil {
				t.Fatal("Connect = nil error, want a refused jump chain")
			}
			if !strings.Contains(err.Error(), "loop") {
				t.Errorf("error = %v, want it to name the jump loop", err)
			}
		})
	}
}

// A broker that forks a helper — `aws ssm` starts session-manager-plugin — leaves the
// grandchild holding the stderr pipe after the child is killed. Close must still return:
// with stderr as a plain io.Writer, os/exec's own copier kept cmd.Wait blocked on that
// descriptor forever.
func TestProxyCommandCloseSurvivesGrandchild(t *testing.T) {
	if forkingProxyCommand() == "" {
		t.Skip("no shell available to fork a grandchild")
	}

	conn, err := dialProxyCommand(forkingProxyCommand(), "h", 22, "u", "alias")
	if err != nil {
		t.Fatalf("dialProxyCommand: %v", err)
	}

	done := make(chan struct{})
	go func() {
		conn.Close()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("Close blocked: a grandchild still holding stderr must not hang the reap")
	}
}
