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

// Tunnel is one running local or remote TCP forward; Close releases the listener and every accepted connection.
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

// StartForward starts f over this client's authenticated transport.
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

// startTunnel takes an open listener and the dial function for the far side.
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

	// Either half closing ends the pair: closing both sockets releases the opposite io.Copy.
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

// Close stops accepting and closes every active forwarded connection, repeatably.
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

// Done closes when the listener stops; Err is meaningful after it closes.
func (t *Tunnel) Done() <-chan struct{} { return t.done }

// Err reports an unexpected listener failure; a tunnel closed by the user has none.
func (t *Tunnel) Err() error {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.err
}

func (t *Tunnel) Definition() store.Forward { return t.definition }

// ListenAddr is the address actually bound by the listener.
func (t *Tunnel) ListenAddr() net.Addr { return t.listener.Addr() }
