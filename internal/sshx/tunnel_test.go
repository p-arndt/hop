package sshx

import (
	"bufio"
	"crypto/ed25519"
	"crypto/rand"
	"io"
	"net"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"

	"hop/internal/store"
)

// Close must release the listener and an established pair, not only stop accepting.
func TestTunnelForwardsAndClosesConnections(t *testing.T) {
	target, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("target listen: %v", err)
	}
	defer target.Close()
	go func() {
		conn, err := target.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		r := bufio.NewReader(conn)
		line, _ := r.ReadString('\n')
		_, _ = conn.Write([]byte(strings.ToUpper(line)))
		// Stay open so the test can prove Tunnel.Close tears this connection down.
		_, _ = r.ReadByte()
	}()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("forward listen: %v", err)
	}
	tunnel := startTunnel(store.Forward{ID: 7, Kind: store.ForwardLocal}, listener,
		func(network, _ string) (net.Conn, error) { return net.Dial(network, target.Addr().String()) })

	conn, err := net.Dial("tcp", tunnel.ListenAddr().String())
	if err != nil {
		t.Fatalf("dial tunnel: %v", err)
	}
	if _, err := conn.Write([]byte("hello\n")); err != nil {
		t.Fatalf("write tunnel: %v", err)
	}
	line, err := bufio.NewReader(conn).ReadString('\n')
	if err != nil {
		t.Fatalf("read tunnel: %v", err)
	}
	if line != "HELLO\n" {
		t.Fatalf("forwarded response = %q", line)
	}

	if err := tunnel.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	conn.SetReadDeadline(time.Now().Add(time.Second))
	if _, err := conn.Read(make([]byte, 1)); err == nil {
		t.Fatal("active forwarded connection survived Tunnel.Close")
	}
	select {
	case <-tunnel.Done():
	default:
		t.Fatal("Done is still open after Close returned")
	}
	if err := tunnel.Err(); err != nil {
		t.Fatalf("user-closed tunnel Err = %v, want nil", err)
	}
}

func TestTunnelDialFailureOnlyDropsThatConnection(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	tunnel := startTunnel(store.Forward{Kind: store.ForwardRemote}, listener,
		func(string, string) (net.Conn, error) { return nil, net.ErrClosed })
	defer tunnel.Close()

	conn, err := net.Dial("tcp", listener.Addr().String())
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()
	conn.SetReadDeadline(time.Now().Add(time.Second))
	if _, err := conn.Read(make([]byte, 1)); err == nil {
		t.Fatal("connection whose target dial failed stayed open")
	}
	select {
	case <-tunnel.Done():
		t.Fatal("one target dial failure stopped the listener")
	default:
	}
}

func TestLocalAndRemoteForwardRoundTripOverSSH(t *testing.T) {
	client := forwardingSSHClient(t)
	defer client.Close()

	for _, kind := range []store.ForwardKind{store.ForwardLocal, store.ForwardRemote} {
		t.Run(string(kind), func(t *testing.T) {
			target := echoListener(t)
			_, targetPortText, _ := net.SplitHostPort(target.Addr().String())
			targetPort, _ := strconv.Atoi(targetPortText)
			bindPort := availablePort(t)

			forward := store.Forward{
				ID: 1, Kind: kind, BindHost: "127.0.0.1", BindPort: bindPort,
				TargetHost: "127.0.0.1", TargetPort: targetPort,
			}
			tunnel, err := client.StartForward(forward)
			if err != nil {
				t.Fatalf("StartForward: %v", err)
			}
			defer tunnel.Close()

			conn, err := net.DialTimeout("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(bindPort)), time.Second)
			if err != nil {
				t.Fatalf("dial %s listener: %v", kind, err)
			}
			defer conn.Close()
			if _, err := conn.Write([]byte("through ssh\n")); err != nil {
				t.Fatalf("write: %v", err)
			}
			line, err := bufio.NewReader(conn).ReadString('\n')
			if err != nil || line != "THROUGH SSH\n" {
				t.Fatalf("round trip = %q, %v", line, err)
			}
		})
	}
}

func echoListener(t *testing.T) net.Listener {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("echo listen: %v", err)
	}
	t.Cleanup(func() { ln.Close() })
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				defer conn.Close()
				r := bufio.NewReader(conn)
				for {
					line, err := r.ReadString('\n')
					if err != nil {
						return
					}
					_, _ = conn.Write([]byte(strings.ToUpper(line)))
				}
			}()
		}
	}()
	return ln
}

func availablePort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserve port: %v", err)
	}
	_, portText, _ := net.SplitHostPort(ln.Addr().String())
	ln.Close()
	port, _ := strconv.Atoi(portText)
	return port
}

// forwardingSSHClient serves the two RFC 4254 forwarding paths x/crypto/ssh.Client uses.
func forwardingSSHClient(t *testing.T) *Client {
	t.Helper()
	_, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("host key: %v", err)
	}
	signer, err := ssh.NewSignerFromKey(private)
	if err != nil {
		t.Fatalf("signer: %v", err)
	}
	cfg := &ssh.ServerConfig{NoClientAuth: true}
	cfg.AddHostKey(signer)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("ssh listen: %v", err)
	}
	t.Cleanup(func() { ln.Close() })

	go func() {
		nc, err := ln.Accept()
		if err != nil {
			return
		}
		sc, channels, requests, err := ssh.NewServerConn(nc, cfg)
		if err != nil {
			nc.Close()
			return
		}
		var remoteMu sync.Mutex
		remote := map[string]net.Listener{}
		go func() {
			for req := range requests {
				switch req.Type {
				case "tcpip-forward":
					var p struct {
						Host string
						Port uint32
					}
					if ssh.Unmarshal(req.Payload, &p) != nil {
						req.Reply(false, nil)
						continue
					}
					addr := net.JoinHostPort(p.Host, strconv.Itoa(int(p.Port)))
					rln, err := net.Listen("tcp", addr)
					if err != nil {
						req.Reply(false, nil)
						continue
					}
					remoteMu.Lock()
					remote[addr] = rln
					remoteMu.Unlock()
					req.Reply(true, nil)
					go serveRemoteForward(sc, rln, p.Host, p.Port)
				case "cancel-tcpip-forward":
					var p struct {
						Host string
						Port uint32
					}
					_ = ssh.Unmarshal(req.Payload, &p)
					addr := net.JoinHostPort(p.Host, strconv.Itoa(int(p.Port)))
					remoteMu.Lock()
					rln := remote[addr]
					delete(remote, addr)
					remoteMu.Unlock()
					if rln != nil {
						rln.Close()
					}
					req.Reply(true, nil)
				default:
					req.Reply(false, nil)
				}
			}
		}()
		for newChannel := range channels {
			if newChannel.ChannelType() != "direct-tcpip" {
				newChannel.Reject(ssh.UnknownChannelType, "unsupported")
				continue
			}
			var p struct {
				Host       string
				Port       uint32
				OriginHost string
				OriginPort uint32
			}
			if ssh.Unmarshal(newChannel.ExtraData(), &p) != nil {
				newChannel.Reject(ssh.ConnectionFailed, "bad payload")
				continue
			}
			upstream, err := net.Dial("tcp", net.JoinHostPort(p.Host, strconv.Itoa(int(p.Port))))
			if err != nil {
				newChannel.Reject(ssh.ConnectionFailed, err.Error())
				continue
			}
			channel, reqs, err := newChannel.Accept()
			if err != nil {
				upstream.Close()
				continue
			}
			go ssh.DiscardRequests(reqs)
			go bridgeTestConn(channel, upstream)
		}
	}()

	raw, err := ssh.Dial("tcp", ln.Addr().String(), &ssh.ClientConfig{
		User: "test", HostKeyCallback: ssh.InsecureIgnoreHostKey(), Timeout: time.Second,
	})
	if err != nil {
		t.Fatalf("ssh dial: %v", err)
	}
	return newClient(raw)
}

func serveRemoteForward(sc *ssh.ServerConn, ln net.Listener, host string, port uint32) {
	for {
		incoming, err := ln.Accept()
		if err != nil {
			return
		}
		originHost, originPortText, _ := net.SplitHostPort(incoming.RemoteAddr().String())
		originPort, _ := strconv.Atoi(originPortText)
		payload := ssh.Marshal(struct {
			Host       string
			Port       uint32
			OriginHost string
			OriginPort uint32
		}{host, port, originHost, uint32(originPort)})
		channel, reqs, err := sc.OpenChannel("forwarded-tcpip", payload)
		if err != nil {
			incoming.Close()
			continue
		}
		go ssh.DiscardRequests(reqs)
		go bridgeTestConn(channel, incoming)
	}
}

func bridgeTestConn(a io.ReadWriteCloser, b net.Conn) {
	defer a.Close()
	defer b.Close()
	done := make(chan struct{}, 1)
	go func() { _, _ = io.Copy(a, b); done <- struct{}{} }()
	go func() { _, _ = io.Copy(b, a); done <- struct{}{} }()
	<-done
}
