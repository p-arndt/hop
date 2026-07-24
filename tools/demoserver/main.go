// Command demoserver is a throwaway SSH server for recording hop's README demo.
//
// It exists so a recording can show hop connecting, running commands, browsing
// files and editing one — without a real host, real credentials or real data
// anywhere near the GIF. Everything it serves is invented (see content.go): a fake
// interactive shell, an in-memory filesystem over SFTP, and a fake vi.
//
// It accepts every client without authentication and serves a host key generated
// on the spot, so it must only ever listen on loopback. That is not a
// configuration choice: the listen address is checked at startup and a non-loopback
// address is refused.
//
//	demoserver -addr 127.0.0.1:2222 -known-hosts <path> -seed-db <path>
//
// -seed-db writes the sample host list into a hop database (all of whose hosts
// point at this server) and -known-hosts writes the generated host key, so hop
// connects without its first-contact prompt. scripts/demo.mjs drives all of it;
// see demo/hop.tape for the recording itself.
package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"github.com/pkg/sftp"
	"github.com/skeema/knownhosts"
	"golang.org/x/crypto/ssh"

	"hop/internal/store"
)

func main() {
	addr := flag.String("addr", "127.0.0.1:2222", "loopback address to listen on")
	khPath := flag.String("known-hosts", "", "write the generated host key to this known_hosts file")
	seedDB := flag.String("seed-db", "", "write the sample host list into this hop database")
	clientKey := flag.String("client-key", "", "write a throwaway private key here for hop to authenticate with")
	flag.Parse()

	if err := run(*addr, *khPath, *seedDB, *clientKey); err != nil {
		log.Fatalln("demoserver:", err)
	}
}

func run(addr, khPath, seedDB, clientKey string) error {
	if err := requireLoopback(addr); err != nil {
		return err
	}

	// hop needs at least one signer to offer before it will dial (an agent identity
	// or a key on disk), and the recording runs with a HOME of its own that has
	// neither. A throwaway key put there keeps the demo off the user's real keys and
	// their agent entirely — this server accepts whatever is offered anyway.
	if clientKey != "" {
		if err := writeClientKey(clientKey); err != nil {
			return err
		}
	}

	signer, err := newHostKey()
	if err != nil {
		return err
	}

	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("listen: %w", err)
	}
	defer ln.Close()

	if khPath != "" {
		if err := writeKnownHosts(khPath, ln.Addr(), signer.PublicKey()); err != nil {
			return err
		}
	}
	if seedDB != "" {
		if err := seed(seedDB, ln.Addr()); err != nil {
			return err
		}
	}

	// The runner script waits for this line before starting the recording.
	fmt.Printf("demoserver: listening on %s (fingerprint %s)\n", ln.Addr(), ssh.FingerprintSHA256(signer.PublicKey()))
	os.Stdout.Sync()

	acceptLoop(ln, signer)
	return nil
}

// acceptLoop serves every connection on ln until it is closed. It is separate from
// run so a test can drive the server over a listener of its own.
func acceptLoop(ln net.Listener, signer ssh.Signer) {
	cfg := &ssh.ServerConfig{NoClientAuth: true}
	cfg.AddHostKey(signer)

	fs := newDemoFS()
	for {
		nc, err := ln.Accept()
		if err != nil {
			return // listener closed: the recording is over
		}
		go serve(nc, cfg, fs)
	}
}

// requireLoopback refuses to listen anywhere a stranger could reach, because this
// server authenticates nobody.
func requireLoopback(addr string) error {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return fmt.Errorf("addr %q: %w", addr, err)
	}
	if ip := net.ParseIP(host); ip == nil || !ip.IsLoopback() {
		return fmt.Errorf("addr %q is not a loopback address; this server accepts every client without auth and must not be exposed", addr)
	}
	return nil
}

// newHostKey generates a throwaway ed25519 host key. It is never written to disk:
// each run gets a fresh key, and the known_hosts file the runner points hop at is
// rewritten to match.
func newHostKey() (ssh.Signer, error) {
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("generate host key: %w", err)
	}
	signer, err := ssh.NewSignerFromKey(priv)
	if err != nil {
		return nil, fmt.Errorf("host key signer: %w", err)
	}
	return signer, nil
}

// writeClientKey generates an unencrypted ed25519 key at p, in the OpenSSH format
// hop's keySigners reads. It is regenerated on every run and lives only in the
// recording's throwaway HOME.
func writeClientKey(p string) error {
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return fmt.Errorf("client key dir: %w", err)
	}
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return fmt.Errorf("generate client key: %w", err)
	}
	block, err := ssh.MarshalPrivateKey(priv, "hop demo")
	if err != nil {
		return fmt.Errorf("marshal client key: %w", err)
	}
	if err := os.WriteFile(p, pem.EncodeToMemory(block), 0o600); err != nil {
		return fmt.Errorf("write client key: %w", err)
	}
	return nil
}

// writeKnownHosts records the generated key for the address we are listening on, so
// hop's first-contact confirmation card does not appear mid-recording. (The card has
// its own scene in the tape, driven separately.)
func writeKnownHosts(p string, addr net.Addr, key ssh.PublicKey) error {
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return fmt.Errorf("known_hosts dir: %w", err)
	}
	host, port, err := net.SplitHostPort(addr.String())
	if err != nil {
		return fmt.Errorf("split listen addr: %w", err)
	}

	// hop dials the alias' hostname, so the entry has to name the same host:port
	// pair the demo hosts use — "localhost", not the resolved 127.0.0.1.
	addrs := []string{net.JoinHostPort("localhost", port), net.JoinHostPort(host, port)}
	var b strings.Builder
	for _, a := range addrs {
		b.WriteString(knownhosts.Line([]string{a}, key))
		b.WriteString("\n")
	}
	if err := os.WriteFile(p, []byte(b.String()), 0o600); err != nil {
		return fmt.Errorf("write known_hosts: %w", err)
	}
	return nil
}

// seed writes the sample host list into a hop database. Every host points at this
// server (so any of them connects), but only the aliases are on screen — which is
// what makes the list look like somebody's real fleet without being one.
func seed(dbPath string, addr net.Addr) error {
	_, portStr, err := net.SplitHostPort(addr.String())
	if err != nil {
		return fmt.Errorf("split listen addr: %w", err)
	}
	port, err := strconv.Atoi(portStr)
	if err != nil {
		return fmt.Errorf("parse listen port: %w", err)
	}

	st, err := store.OpenAt(dbPath)
	if err != nil {
		return fmt.Errorf("open seed db: %w", err)
	}
	defer st.Close()

	// Visits drive the frecency order, so they are what puts prod-web-1 at the top.
	hosts := []struct {
		alias, user, group string
		tags               []string
		visits             int
	}{
		{alias: "prod-web-1", user: "deploy", group: "prod", visits: 42},
		{alias: "prod-web-2", user: "deploy", group: "prod", visits: 31},
		{alias: "prod-db", user: "postgres", group: "prod", visits: 18},
		{alias: "staging", user: "deploy", group: "staging", visits: 12},
		{alias: "build-box", user: "ci", tags: []string{"ci"}, visits: 7},
		{alias: "nas", user: "admin", group: "home", visits: 4},
		{alias: "pi-hole", user: "pi", group: "home", visits: 2},
		{alias: "router", user: "root", group: "home", visits: 1},
	}

	for _, h := range hosts {
		if _, err := st.Upsert(store.Host{
			Alias: h.alias, HostName: "localhost", User: h.user, Port: port,
			Group: h.group, Tags: h.tags,
		}); err != nil {
			return fmt.Errorf("seed %s: %w", h.alias, err)
		}
		// Touch once per visit: frecency is stored, not computed from a field we
		// could set directly.
		for i := 0; i < h.visits; i++ {
			if err := st.Touch(h.alias); err != nil {
				return fmt.Errorf("touch %s: %w", h.alias, err)
			}
		}
	}
	return nil
}

// ---- connection handling ----

func serve(nc net.Conn, cfg *ssh.ServerConfig, fs *demoFS) {
	sc, chans, reqs, err := ssh.NewServerConn(nc, cfg)
	if err != nil {
		nc.Close()
		return
	}
	defer sc.Close()

	go ssh.DiscardRequests(reqs)

	var wg sync.WaitGroup
	for nch := range chans {
		if nch.ChannelType() != "session" {
			nch.Reject(ssh.UnknownChannelType, "only sessions here")
			continue
		}
		ch, chReqs, err := nch.Accept()
		if err != nil {
			continue
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			handleSession(ch, chReqs, fs)
		}()
	}
	wg.Wait()
}

// ptyReq is the payload of a pty-req, whose first fields are all we need: the size
// the fake editor lays out to.
type ptyReq struct {
	Term          string
	Cols, Rows    uint32
	WidthPx, HiPx uint32
	Modes         string
}

type stringReq struct{ Value string }

type winChange struct {
	Cols, Rows    uint32
	WidthPx, HiPx uint32
}

// handleSession runs one channel: a shell, an exec (which for hop means an editor),
// or the SFTP subsystem.
func handleSession(ch ssh.Channel, reqs <-chan *ssh.Request, fs *demoFS) {
	defer ch.Close()

	cols, rows := 80, 24
	for req := range reqs {
		switch req.Type {
		case "pty-req":
			var p ptyReq
			if err := ssh.Unmarshal(req.Payload, &p); err == nil {
				cols, rows = int(p.Cols), int(p.Rows)
			}
			req.Reply(true, nil)

		case "window-change":
			var w winChange
			if err := ssh.Unmarshal(req.Payload, &w); err == nil {
				cols, rows = int(w.Cols), int(w.Rows)
			}
			req.Reply(true, nil)

		case "env":
			req.Reply(true, nil)

		case "shell":
			req.Reply(true, nil)
			runShell(ch, ch, true)
			exit(ch, 0)
			return

		case "exec":
			var c stringReq
			if err := ssh.Unmarshal(req.Payload, &c); err != nil {
				req.Reply(false, nil)
				return
			}
			req.Reply(true, nil)
			runExec(ch, fs, c.Value, cols, rows)
			exit(ch, 0)
			return

		case "subsystem":
			var s stringReq
			if err := ssh.Unmarshal(req.Payload, &s); err != nil || s.Value != "sftp" {
				req.Reply(false, nil)
				return
			}
			req.Reply(true, nil)
			srv := sftp.NewRequestServer(ch, fs.handlers())
			srv.Serve()
			srv.Close()
			exit(ch, 0)
			return

		default:
			req.Reply(false, nil)
		}
	}
}

// runExec answers an exec request. hop's only exec is the editor command it builds
// in remoteEditorCmd — a shell one-liner ending in `exec ${ed:-vi} '<path>'` — so
// the path is pulled out of it and the fake editor opens that file. Anything else
// falls through to the command table, which is what makes `hop`'s own probes (and a
// stray command in a tape) behave.
func runExec(ch ssh.Channel, fs *demoFS, cmd string, cols, rows int) {
	if p, ok := editorTarget(cmd); ok {
		f, err := fs.lookup(p)
		if err != nil {
			io.WriteString(ch, "vi: "+p+": No such file or directory\r\n")
			return
		}
		runEditor(ch, ch, p, f.content, cols, rows)
		return
	}

	if out, ok := commands[strings.TrimSpace(cmd)]; ok {
		io.WriteString(ch, out)
		return
	}
	io.WriteString(ch, "bash: "+firstWord(cmd)+": command not found\r\n")
}

// editorTarget extracts the file path from hop's editor command: the last
// single-quoted run in it, which is how remoteEditorCmd quotes the path.
func editorTarget(cmd string) (string, bool) {
	end := strings.LastIndex(cmd, "'")
	if end < 0 {
		return "", false
	}
	start := strings.LastIndex(cmd[:end], "'")
	if start < 0 {
		return "", false
	}
	p := cmd[start+1 : end]
	if p == "" {
		return "", false
	}
	return p, true
}

// exit sends the exit-status the client waits for before treating the channel as
// finished — hop closes an editor tab when its command exits.
func exit(ch ssh.Channel, code uint32) {
	ch.SendRequest("exit-status", false, ssh.Marshal(struct{ Code uint32 }{code}))
}
