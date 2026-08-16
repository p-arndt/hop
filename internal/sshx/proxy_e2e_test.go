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

// runProxyHelperIfRequested lets this binary act as the proxy program: it dials the
// address and shovels bytes between the socket and its own stdio. Called first from
// TestMain, so the re-executed child never reaches the test runner. Re-execing ourselves
// avoids depending on nc.
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

// proxyCommandFor builds the ProxyCommand line re-execing this test binary; the address
// travels in the environment. Quoted, so a build directory with a space still parses.
func proxyCommandFor(t *testing.T) string {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatalf("locate test binary: %v", err)
	}
	return strconv.Quote(exe) + " -test.run=TestNothingRuns"
}

// TestNothingRuns is the harmless target of the -test.run above.
func TestNothingRuns(t *testing.T) {}

// A ProxyCommand host must complete a real handshake over the program's pipes and run a
// command on the far side — the transport path the parsing tests do not touch.
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

// nopPrompter never answers: the test servers accept without auth, so it only keeps
// authMethods from refusing a host with no keys and no agent.
type nopPrompter struct{}

func (nopPrompter) Ask(Challenge) ([]string, error) {
	return nil, errors.New("test: no interactive auth expected")
}

// echoSSHServer accepts without auth, answers "exec" by upper-casing the command, and
// serves direct-tcpip so it can stand in for a bastion.
type echoSSHServer struct {
	addr    string
	hostKey ssh.PublicKey
	// refuseForwarding rejects direct-tcpip, as a hardened sshd with
	// `AllowTcpForwarding no` does. Fixed at construction: the accept loop reads it.
	refuseForwarding bool
}

func startEchoSSHServer(t *testing.T) *echoSSHServer { return newEchoSSHServer(t, false) }

// startRefusingSSHServer is a bastion that turns every forwarding request away.
func startRefusingSSHServer(t *testing.T) *echoSSHServer { return newEchoSSHServer(t, true) }

func newEchoSSHServer(t *testing.T, refuseForwarding bool) *echoSSHServer {
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

	srv := &echoSSHServer{addr: ln.Addr().String(), hostKey: signer.PublicKey(), refuseForwarding: refuseForwarding}
	go func() {
		for {
			nc, err := ln.Accept()
			if err != nil {
				return
			}
			go serveEchoConn(nc, cfg, srv.refuseForwarding)
		}
	}()
	return srv
}

func serveEchoConn(nc net.Conn, cfg *ssh.ServerConfig, refuseForwarding bool) {
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
			if refuseForwarding {
				nch.Reject(ssh.Prohibited, "administratively prohibited: open failed")
				continue
			}
			go serveDirectTCPIP(nch)
		default:
			nch.Reject(ssh.UnknownChannelType, nch.ChannelType())
		}
	}
}

// serveEchoSession answers "exec" upper-cased — enough to tell a live session from a
// handshake that merely completed.
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

// serveDirectTCPIP is the bastion half: dial what was asked for, splice the streams.
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

// trustHostKey pre-approves key in the fake home's known_hosts.
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

// First contact through a bastion: approve the bastion's key, then the target's, then
// connect. This used to loop — the retry met the same unknown bastion key.
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

// A ProxyJump naming its own host, directly or around a ring, must error. This used to
// recurse until the stack gave out.
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

// A broker that forks a helper (`aws ssm` starts session-manager-plugin) leaves the
// grandchild holding stderr after the child is killed. Close must still return.
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

// A proxy that starts and stays silent must fail rather than hang: ClientConfig.Timeout
// does not reach a proxied dial.
func TestProxyCommandSilentProxyTimesOut(t *testing.T) {
	if silentProxyCommand() == "" {
		t.Skip("no shell available to stage a silent proxy")
	}

	prev := proxyFirstByteTimeoutForTest(200 * time.Millisecond)
	t.Cleanup(prev)

	fakeHome(t)
	h := store.Host{Alias: "quiet", HostName: "quiet.invalid", Port: 22, ProxyCommand: silentProxyCommand()}

	done := make(chan error, 1)
	go func() {
		_, err := Connect(h, nopPrompter{})
		done <- err
	}()

	select {
	case err := <-done:
		if err == nil {
			t.Fatal("Connect = nil error, want the silent proxy to time out")
		}
	case <-time.After(20 * time.Second):
		t.Fatal("Connect hung on a silent proxy")
	}
}

// A hardened bastion (`AllowTcpForwarding no`) rejects direct-tcpip — the most common
// reason a real ProxyJump fails. The error must carry the bastion's reason, or nothing
// points the user at its sshd_config.
func TestProxyJumpBastionRefusesForwarding(t *testing.T) {
	home := fakeHome(t)
	target := startEchoSSHServer(t)
	bastion := startRefusingSSHServer(t)

	targetHost, targetPort := splitAddr(t, target.addr)
	bastionHost, bastionPort := splitAddr(t, bastion.addr)
	trustHostKey(t, home, targetHost, targetPort, target.hostKey)
	trustHostKey(t, home, bastionHost, bastionPort, bastion.hostKey)

	h := store.Host{
		Alias:     "db01",
		HostName:  targetHost,
		Port:      targetPort,
		ProxyJump: net.JoinHostPort(bastionHost, strconv.Itoa(bastionPort)),
	}

	_, err := Connect(h, nopPrompter{})
	if err == nil {
		t.Fatal("Connect = nil error, want the refused forwarding to fail the dial")
	}
	if !strings.Contains(err.Error(), "proxy jump") {
		t.Errorf("error = %v, want it to name the proxy jump", err)
	}
	if !strings.Contains(err.Error(), "administratively prohibited") {
		t.Errorf("error = %v, want it to carry the bastion's reason", err)
	}
}
