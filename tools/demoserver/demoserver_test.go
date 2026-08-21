package main

import (
	"io"
	"net"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"

	"hop/internal/sftpx"
	"hop/internal/sshx"
	"hop/internal/store"
)

// start brings the demo server up on a loopback port and returns a connected hop SSH client.
func start(t *testing.T) *sshx.Client {
	t.Helper()

	signer, err := newHostKey()
	if err != nil {
		t.Fatalf("host key: %v", err)
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { ln.Close() })
	go acceptLoop(ln, signer)

	cl, err := sshx.ConnectAddr(ln.Addr().String(), &ssh.ClientConfig{
		User:            demoUser,
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         5 * time.Second,
	})
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(func() { cl.Close() })
	return cl
}

// The fake shell greets, prompts, echoes, answers a command and exits on `exit`.
func TestShellAnswersCommands(t *testing.T) {
	cl := start(t)

	sess, err := cl.Shell(100, 30)
	if err != nil {
		t.Fatalf("shell: %v", err)
	}
	defer sess.Close()

	if _, err := io.WriteString(sess.Stdin, "uptime\r"); err != nil {
		t.Fatalf("write: %v", err)
	}

	out := readUntil(t, sess.Stdout, "load average")
	for _, want := range []string{"Welcome to Ubuntu", demoUser + "@" + demoHost, "uptime", "load average"} {
		if !strings.Contains(out, want) {
			t.Errorf("shell output missing %q:\n%s", want, out)
		}
	}

	if _, err := io.WriteString(sess.Stdin, "exit\r"); err != nil {
		t.Fatalf("write exit: %v", err)
	}
	if err := sess.Wait(); err != nil {
		t.Fatalf("shell did not exit cleanly: %v", err)
	}
}

// The browser opens with two questions: where home is, and what is in it.
func TestSFTPServesTheDemoTree(t *testing.T) {
	cl := start(t)

	sc, err := sftpx.Open(cl.SSHClient())
	if err != nil {
		t.Fatalf("sftp open: %v", err)
	}
	defer sc.Close()

	home, err := sc.Home()
	if err != nil {
		t.Fatalf("home: %v", err)
	}
	if home != demoHome {
		t.Errorf("home = %q, want %q", home, demoHome)
	}

	entries, err := sc.List(home)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	got := map[string]bool{}
	for _, e := range entries {
		got[e.Name] = e.IsDir
	}
	for name, wantDir := range map[string]bool{
		"app": true, "logs": true, "backups": true,
		"docker-compose.yml": false, "deploy.sh": false, "README.md": false,
	} {
		isDir, ok := got[name]
		if !ok {
			t.Errorf("listing is missing %q: %v", name, got)
			continue
		}
		if isDir != wantDir {
			t.Errorf("%q: IsDir = %v, want %v", name, isDir, wantDir)
		}
	}

	local := filepath.Join(t.TempDir(), "README.md")
	if _, err := sc.Download(demoHome+"/README.md", local); err != nil {
		t.Fatalf("download: %v", err)
	}

	// The tree is read-only on purpose, so a stray upload during a recording fails cleanly.
	if _, err := sc.Upload(local, demoHome+"/nope.md"); err == nil {
		t.Error("upload succeeded; the demo tree is supposed to be read-only")
	}
}

// hop's editor command reaches the fake vi with the right file, and `:q` ends the channel.
func TestEditorOpensAndQuits(t *testing.T) {
	cl := start(t)

	// The exact command hop builds when no editor is configured.
	cmd := `ed="${EDITOR:-${VISUAL:-}}"; [ -n "$ed" ] || for c in nvim vim vi nano; do ` +
		`command -v "$c" >/dev/null 2>&1 && { ed="$c"; break; }; done; ` +
		`exec ${ed:-vi} '` + demoHome + `/docker-compose.yml'`

	sess, err := cl.Command(cmd, 100, 30)
	if err != nil {
		t.Fatalf("command: %v", err)
	}
	defer sess.Close()

	out := readUntil(t, sess.Stdout, "docker-compose.yml")
	if !strings.Contains(out, "\x1b[?1049h") {
		t.Error("editor did not take the alt screen; hop decides a program owns the keyboard by exactly that")
	}
	if !strings.Contains(out, "ghcr.io/example/web") {
		t.Errorf("editor did not draw the file:\n%q", out)
	}

	if _, err := io.WriteString(sess.Stdin, ":q\r"); err != nil {
		t.Fatalf("write :q: %v", err)
	}
	if err := sess.Wait(); err != nil {
		t.Fatalf("editor did not exit on :q: %v", err)
	}
}

// Every sample host, pointing at the demo server, ordered by frecency.
func TestSeedWritesTheSampleFleet(t *testing.T) {
	hostsPath := filepath.Join(t.TempDir(), "hop.config")
	addr := &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 2222}
	if err := seed(hostsPath, addr); err != nil {
		t.Fatalf("seed: %v", err)
	}

	st, err := store.OpenAt(hostsPath, "")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer st.Close()

	hosts, err := st.Hosts()
	if err != nil {
		t.Fatalf("hosts: %v", err)
	}
	if len(hosts) != 8 {
		t.Fatalf("seeded %d hosts, want 8", len(hosts))
	}
	if hosts[0].Alias != "prod-web-1" {
		t.Errorf("first host is %q, want prod-web-1 (the most-visited one)", hosts[0].Alias)
	}
	for _, h := range hosts {
		if h.HostName != "localhost" || h.Port != 2222 {
			t.Errorf("%s points at %s:%d, want localhost:2222", h.Alias, h.HostName, h.Port)
		}
	}
}

func TestRequireLoopback(t *testing.T) {
	for _, addr := range []string{"127.0.0.1:2222", "[::1]:2222"} {
		if err := requireLoopback(addr); err != nil {
			t.Errorf("requireLoopback(%q) = %v, want nil", addr, err)
		}
	}
	for _, addr := range []string{"0.0.0.0:2222", "10.0.0.4:2222", ":2222", "nonsense"} {
		if err := requireLoopback(addr); err == nil {
			t.Errorf("requireLoopback(%q) = nil, want an error", addr)
		}
	}
}

func TestEditorTarget(t *testing.T) {
	if p, ok := editorTarget(`exec vim '/home/deploy/app/main.py'`); !ok || p != "/home/deploy/app/main.py" {
		t.Errorf("editorTarget = %q, %v", p, ok)
	}
	if _, ok := editorTarget("uptime"); ok {
		t.Error("editorTarget matched a plain command")
	}
}

// readUntil reads from r until want shows up, regardless of write chunking.
func readUntil(t *testing.T, r io.Reader, want string) string {
	t.Helper()

	var sb strings.Builder
	buf := make([]byte, 4096)
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		n, err := r.Read(buf)
		sb.Write(buf[:n])
		if strings.Contains(sb.String(), want) {
			return sb.String()
		}
		if err != nil {
			break
		}
	}
	t.Fatalf("never saw %q in:\n%s", want, sb.String())
	return ""
}
