package sshx

import (
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
	"sync"

	"hop/internal/store"
)

// Tunnel is one running local or remote TCP forward. It owns the listening
// socket and every connection accepted from it; Close releases all of them and
// waits for the forwarding goroutines to finish.
type Tunnel struct {
	definition store.Forward
	listener   net.Listener
	dial       func(string, string) (net.Conn, error)

	mu      sync.Mutex
	conns   map[net.Conn]struct{}
	closing bool
	err     error
	done    chan struct{}
	wg      sync.WaitGroup
	once    sync.Once
}

// StartForward starts f over this client's already-authenticated SSH transport.
// Local forwards listen on hop's machine and dial through SSH; remote forwards
// ask the SSH server to listen and dial their target on hop's machine.
func (c *Client) StartForward(f store.Forward) (*Tunnel, error) {
	if c == nil || c.ssh == nil {
		return nil, errors.New("sshx: client not initialized")
	}
	if err := f.Validate(); err != nil {
		return nil, fmt.Errorf("sshx: invalid forward: %w", err)
	}

	bindHost := f.BindHost
	if bindHost == "" {
		bindHost = "127.0.0.1"
	}
	bind := net.JoinHostPort(bindHost, strconv.Itoa(f.BindPort))
	target := net.JoinHostPort(f.TargetHost, strconv.Itoa(f.TargetPort))

	var (
		ln  net.Listener
		err error
	)
	switch f.Kind {
	case store.ForwardLocal:
		ln, err = net.Listen("tcp", bind)
		if err == nil {
			return startTunnel(f, ln, func(network, _ string) (net.Conn, error) {
				return c.ssh.Dial(network, target)
			}), nil
		}
	case store.ForwardRemote:
		ln, err = c.ssh.Listen("tcp", bind)
		if err == nil {
			return startTunnel(f, ln, func(network, _ string) (net.Conn, error) {
				return net.Dial(network, target)
			}), nil
		}
	default:
		return nil, fmt.Errorf("sshx: unsupported forward kind %q", f.Kind)
	}
	return nil, fmt.Errorf("sshx: listen %s: %w", bind, err)
}

// startTunnel takes an already-open listener and the dial function for the far
// side. Keeping that small boundary injectable makes both forwarding directions
// testable without teaching a test SSH server every forwarding request.
func startTunnel(f store.Forward, ln net.Listener, dial func(string, string) (net.Conn, error)) *Tunnel {
	t := &Tunnel{
		definition: f,
		listener:   ln,
		dial:       dial,
		conns:      make(map[net.Conn]struct{}),
		done:       make(chan struct{}),
	}
	go t.accept()
	return t
}

func (t *Tunnel) accept() {
	defer func() {
		t.closeConnections()
		t.wg.Wait()
		close(t.done)
	}()
	for {
		incoming, err := t.listener.Accept()
		if err != nil {
			t.mu.Lock()
			if !t.closing {
				t.err = err
				t.closing = true
			}
			t.mu.Unlock()
			return
		}
		if !t.register(incoming) {
			return
		}
		t.wg.Add(1)
		go t.forward(incoming)
	}
}

func (t *Tunnel) forward(incoming net.Conn) {
	defer t.wg.Done()
	defer t.unregister(incoming)

	outgoing, err := t.dial("tcp", "")
	if err != nil {
		return
	}
	if !t.register(outgoing) {
		return
	}
	defer t.unregister(outgoing)

	// Either half closing ends the pair. Closing both sockets is what releases the
	// opposite io.Copy, including a peer that went quiet without closing itself.
	copied := make(chan struct{}, 1)
	go func() {
		_, _ = io.Copy(outgoing, incoming)
		copied <- struct{}{}
	}()
	go func() {
		_, _ = io.Copy(incoming, outgoing)
		copied <- struct{}{}
	}()
	<-copied
	incoming.Close()
	outgoing.Close()
	<-copied
}

// register adds a connection unless shutdown has already started.
func (t *Tunnel) register(conn net.Conn) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.closing {
		conn.Close()
		return false
	}
	t.conns[conn] = struct{}{}
	return true
}

func (t *Tunnel) unregister(conn net.Conn) {
	conn.Close()
	t.mu.Lock()
	delete(t.conns, conn)
	t.mu.Unlock()
}

func (t *Tunnel) closeConnections() {
	t.mu.Lock()
	conns := make([]net.Conn, 0, len(t.conns))
	for conn := range t.conns {
		conns = append(conns, conn)
	}
	t.mu.Unlock()
	for _, conn := range conns {
		conn.Close()
	}
}

// Close stops accepting and closes all active forwarded connections. It is safe
// to call repeatedly.
func (t *Tunnel) Close() error {
	if t == nil {
		return nil
	}
	var err error
	t.once.Do(func() {
		t.mu.Lock()
		t.closing = true
		t.mu.Unlock()
		err = t.listener.Close()
		t.closeConnections()
	})
	<-t.done
	if errors.Is(err, net.ErrClosed) {
		return nil
	}
	return err
}

// Done closes when the listener stops, whether by Close, connection loss, or a
// listener error. Err is meaningful after Done closes.
func (t *Tunnel) Done() <-chan struct{} { return t.done }

// Err reports an unexpected listener failure. A tunnel closed by the user has no
// error.
func (t *Tunnel) Err() error {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.err
}

// Definition is the persisted definition this runtime represents.
func (t *Tunnel) Definition() store.Forward { return t.definition }

// ListenAddr is the address actually bound by the listener.
func (t *Tunnel) ListenAddr() net.Addr { return t.listener.Addr() }
