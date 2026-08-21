package terminal

import (
	"io"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"

	"hop/internal/sshx"
)

// probeServer is an in-process SSH server that answers the login-shell probe, records
// what is typed into the shell channel, and can speak on it.
type probeServer struct {
	addr  string
	shell string // what the exec probe answers

	mu    sync.Mutex
	input []byte
	live  ssh.Channel

	opened chan struct{} // closed once the shell channel exists
}

func (s *probeServer) shellInput() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return string(s.input)
}

// waitForInput polls the shell channel's input for want, waiting briefly even when the
// test expects nothing.
func (s *probeServer) waitForInput(t *testing.T, want string, expected bool) bool {
	t.Helper()
	wait := 3 * time.Second
	if !expected {
		wait = 500 * time.Millisecond
	}
	deadline := time.Now().Add(wait)
	for time.Now().Before(deadline) {
		if strings.Contains(s.shellInput(), want) {
			return true
		}
		time.Sleep(10 * time.Millisecond)
	}
	return strings.Contains(s.shellInput(), want)
}

func (s *probeServer) say(out string) {
	<-s.opened
	s.mu.Lock()
	ch := s.live
	s.mu.Unlock()
	if ch != nil {
		io.WriteString(ch, out)
	}
}

// startShellProbeServer brings up the server, answering the login-shell probe with shell.
func startShellProbeServer(t *testing.T, shell string) *probeServer {
	t.Helper()
	srv := &probeServer{shell: shell, opened: make(chan struct{})}

	cfg := &ssh.ServerConfig{
		PublicKeyCallback: func(ssh.ConnMetadata, ssh.PublicKey) (*ssh.Permissions, error) { return nil, nil },
	}
	cfg.AddHostKey(newSigner(t))

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { ln.Close() })
	srv.addr = ln.Addr().String()

	go func() {
		for {
			nc, err := ln.Accept()
			if err != nil {
				return
			}
			go srv.serve(nc, cfg)
		}
	}()
	return srv
}

func (s *probeServer) serve(nc net.Conn, cfg *ssh.ServerConfig) {
	sc, chans, reqs, err := ssh.NewServerConn(nc, cfg)
	if err != nil {
		nc.Close()
		return
	}
	defer sc.Close()
	go ssh.DiscardRequests(reqs)

	for newCh := range chans {
		if newCh.ChannelType() != "session" {
			newCh.Reject(ssh.UnknownChannelType, "only session supported")
			continue
		}
		ch, chReqs, err := newCh.Accept()
		if err != nil {
			return
		}
		go s.session(ch, chReqs)
	}
}

func (s *probeServer) session(ch ssh.Channel, reqs <-chan *ssh.Request) {
	for req := range reqs {
		switch req.Type {
		case "pty-req":
			req.Reply(true, nil)

		case "exec":
			req.Reply(true, nil)
			io.WriteString(ch, s.shell)
			ch.SendRequest("exit-status", false, []byte{0, 0, 0, 0})
			ch.Close()

		case "shell":
			req.Reply(true, nil)
			s.mu.Lock()
			s.live = ch
			s.mu.Unlock()
			close(s.opened)
			go s.record(ch)

		default:
			if req.WantReply {
				req.Reply(false, nil)
			}
		}
	}
}

func (s *probeServer) record(ch ssh.Channel) {
	buf := make([]byte, 1024)
	for {
		n, err := ch.Read(buf)
		if n > 0 {
			s.mu.Lock()
			s.input = append(s.input, buf[:n]...)
			s.mu.Unlock()
		}
		if err != nil {
			return
		}
	}
}

func dialProbeServer(t *testing.T, addr string) *sshx.Client {
	t.Helper()
	cli, err := sshx.ConnectAddr(addr, &ssh.ClientConfig{
		User:            "tester",
		Auth:            []ssh.AuthMethod{ssh.PublicKeys(newSigner(t))},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         5 * time.Second,
	})
	if err != nil {
		t.Fatalf("ConnectAddr: %v", err)
	}
	return cli
}

// osc7 builds the sequence a shell's prompt hook emits, BEL-terminated.
func osc7(host, dir string) string {
	return "\x1b]7;file://" + host + dir + "\x07"
}

// The shapes of OSC 7 a real shell emits, and the shapes that are not one.
func TestScannerReadsOSC7(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string // "" means: nothing reported
	}{
		{"bel terminated", osc7("web1", "/srv/app"), "/srv/app"},
		{"st terminated", "\x1b]7;file://web1/srv/app\x1b\\", "/srv/app"},
		{"no hostname", "\x1b]7;file:///home/deploy\x07", "/home/deploy"},
		{"fqdn hostname", "\x1b]7;file://web1.example.com/var/log\x07", "/var/log"},
		{"root", osc7("web1", "/"), "/"},
		{"percent encoded space", "\x1b]7;file://web1/srv/my%20app\x07", "/srv/my app"},
		{"literal space", "\x1b]7;file://web1/srv/my app\x07", "/srv/my app"},
		{"stray percent taken literally", "\x1b]7;file://web1/srv/100%done\x07", "/srv/100%done"},
		{"surrounded by ordinary output", "hello\r\n" + osc7("web1", "/tmp") + "deploy@web1:/tmp$ ", "/tmp"},

		{"a window title is not a cwd", "\x1b]0;deploy@web1: /tmp\x07", ""},
		{"a hyperlink is not a cwd", "\x1b]8;;https://example.com\x07link\x1b]8;;\x07", ""},
		{"another scheme", "\x1b]7;http://web1/tmp\x07", ""},
		{"no path at all", "\x1b]7;file://web1\x07", ""},
		{"unterminated", "\x1b]7;file://web1/tmp", ""},
		{"control characters refused", "\x1b]7;file://web1/tmp/a%0Ab\x07", ""},
		{"plain text", "just output, no escapes at all\r\n", ""},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var s oscScanner
			dir, ok := s.feed([]byte(c.in))
			if c.want == "" {
				if ok {
					t.Fatalf("reported %q for input that carries no cwd", dir)
				}
				return
			}
			if !ok {
				t.Fatalf("nothing reported; want %q", c.want)
			}
			if dir != c.want {
				t.Fatalf("cwd = %q, want %q", dir, c.want)
			}
		})
	}
}

func TestScannerSurvivesChunkSplits(t *testing.T) {
	full := "output\r\n" + osc7("web1", "/srv/app") + "prompt$ "
	for i := 1; i < len(full); i++ {
		var s oscScanner
		var got string
		for _, chunk := range []string{full[:i], full[i:]} {
			if dir, ok := s.feed([]byte(chunk)); ok {
				got = dir
			}
		}
		if got != "/srv/app" {
			t.Fatalf("split after %d bytes: cwd = %q, want %q", i, got, "/srv/app")
		}
	}
}

func TestScannerKeepsTheLatestReport(t *testing.T) {
	var s oscScanner
	if _, ok := s.feed([]byte(osc7("web1", "/home/deploy"))); !ok {
		t.Fatal("the first report was not read")
	}
	dir, ok := s.feed([]byte(osc7("web1", "/etc") + "x" + osc7("web1", "/var/tmp")))
	if !ok {
		t.Fatal("nothing reported from a chunk carrying two")
	}
	if dir != "/var/tmp" {
		t.Fatalf("cwd = %q, want the last report %q", dir, "/var/tmp")
	}
}

// An unterminated payload must not grow the buffer without bound nor be reported.
func TestScannerCapsAnUnterminatedPayload(t *testing.T) {
	var s oscScanner
	s.feed([]byte("\x1b]7;file://web1/" + strings.Repeat("a", maxOSCPayload+2048)))
	if len(s.buf) > maxOSCPayload {
		t.Fatalf("buffered %d bytes, cap is %d", len(s.buf), maxOSCPayload)
	}
	if dir, ok := s.feed([]byte("\x07")); ok {
		t.Fatalf("an over-long payload was reported as %q", dir)
	}
	if dir, ok := s.feed([]byte(osc7("web1", "/tmp"))); !ok || dir != "/tmp" {
		t.Fatalf("after an over-long payload: cwd = %q, ok = %v", dir, ok)
	}
}

// An OSC cut short by another escape is malformed; the next one is still read.
func TestScannerRecoversFromAnInterruptedOSC(t *testing.T) {
	var s oscScanner
	if _, ok := s.feed([]byte("\x1b]7;file://web1/tmp\x1b[0m")); ok {
		t.Fatal("an OSC cut short by a CSI was reported as a cwd")
	}
	if dir, ok := s.feed([]byte(osc7("web1", "/srv"))); !ok || dir != "/srv" {
		t.Fatalf("after an interrupted OSC: cwd = %q, ok = %v", dir, ok)
	}
}

func TestPaneCwdFollowsTheStream(t *testing.T) {
	pr, pw := io.Pipe()
	p := New(&sshx.Session{Stdin: nopWriteCloser{io.Discard}, Stdout: pr}, 80, 24, nil)
	defer p.Close()

	if dir := p.Cwd(); dir != "" {
		t.Fatalf("a pane that has heard nothing reports %q", dir)
	}

	io.WriteString(pw, "deploy@web1:~$ "+osc7("web1", "/home/deploy"))
	if !waitForCwd(p, "/home/deploy") {
		t.Fatalf("cwd = %q, want %q", p.Cwd(), "/home/deploy")
	}

	io.WriteString(pw, "cd /srv/app\r\n"+osc7("web1", "/srv/app"))
	if !waitForCwd(p, "/srv/app") {
		t.Fatalf("after a cd: cwd = %q, want %q", p.Cwd(), "/srv/app")
	}
}

// Which shells get a prompt hook, and which line each gets.
func TestCwdHookFor(t *testing.T) {
	for _, c := range []struct{ shell, want string }{
		{"/bin/bash", bashCwdHook},
		{"bash", bashCwdHook},
		{"-bash", bashCwdHook},
		{" /bin/bash ", bashCwdHook},
		{"/usr/bin/zsh", zshCwdHook},
		{"/bin/zsh\n", zshCwdHook},
		{"/usr/bin/fish", ""},
		{"/bin/sh", ""},
		{"/usr/bin/pwsh", ""},
		{"/usr/bin/csh", ""},
		{"", ""},
		{"/sbin/nologin", ""},
	} {
		if got := cwdHookFor(c.shell); got != c.want {
			t.Errorf("cwdHookFor(%q) = %q, want %q", c.shell, got, c.want)
		}
	}
}

func TestCwdHooksAreOneSubmittedLine(t *testing.T) {
	for name, hook := range map[string]string{"bash": bashCwdHook, "zsh": zshCwdHook} {
		if !strings.HasSuffix(hook, "\r") {
			t.Errorf("%s: the hook is never submitted — it does not end in a carriage return", name)
		}
		if strings.Count(hook, "\r") != 1 || strings.Contains(hook, "\n") {
			t.Errorf("%s: the hook runs as more than one line", name)
		}
		if !strings.HasPrefix(hook, "\x15 ") {
			t.Errorf("%s: the hook does not start with the kill-line and the space that keeps it out of history", name)
		}
		if !strings.Contains(hook, `]7;file://`) {
			t.Errorf("%s: the hook emits no OSC 7", name)
		}
		if len(hook) > 160 {
			t.Errorf("%s: the hook is %d bytes; it is echoed at the prompt, keep it under 160", name, len(hook))
		}
	}
	if !strings.Contains(bashCwdHook, "PROMPT_COMMAND") {
		t.Error("the bash hook does not hang off PROMPT_COMMAND")
	}
	if !strings.Contains(zshCwdHook, "precmd_functions") {
		t.Error("the zsh hook does not hang off precmd_functions")
	}
}

// The echoed rows are deleted from hop's screen and what was below slides up.
func TestEraseEchoTakesTheEchoOffTheScreen(t *testing.T) {
	pr, pw := io.Pipe()
	repaints := make(chan struct{}, 8)
	p := New(&sshx.Session{Stdin: nopWriteCloser{io.Discard}, Stdout: pr}, 60, 10, func() {
		select {
		case repaints <- struct{}{}:
		default:
		}
	})
	defer p.Close()

	// Row 0 banner, row 1 the prompt the hook wraps from, then the prompt below it.
	io.WriteString(pw, "banner\r\n"+echoOf(bashCwdHook)+"\r\nprompt$ ")
	if !waitForView(p, "prompt$", 3*time.Second) {
		t.Fatalf("the screen never filled; view:\n%s", p.View())
	}
	drain(repaints)

	p.eraseEcho(1, 0, bashCwdHook)

	view := p.View()
	if strings.Contains(view, "hop_cwd") {
		t.Fatalf("the echo is still on screen; view:\n%s", view)
	}
	for _, kept := range []string{"banner", "prompt$"} {
		if !strings.Contains(view, kept) {
			t.Fatalf("the erase took %q with it; view:\n%s", kept, view)
		}
	}
	if pos := p.emu.CursorPosition(); pos.Y != 1 {
		t.Fatalf("the cursor is on row %d, want row 1 — the prompt it was on moved up there", pos.Y)
	}
	select {
	case <-repaints:
	case <-time.After(time.Second):
		t.Fatal("erasing the echo did not ask the UI to repaint")
	}
}

// Rows the host printed into (a slow MOTD) are left alone.
func TestEraseEchoDeclinesWhenTheHostPrintedIntoTheSpan(t *testing.T) {
	pr, pw := io.Pipe()
	p := New(&sshx.Session{Stdin: nopWriteCloser{io.Discard}, Stdout: pr}, 60, 12, nil)
	defer p.Close()

	io.WriteString(pw, "banner\r\n"+echoOf(bashCwdHook)+"\r\n*** System restart required ***\r\nprompt$ ")
	if !waitForView(p, "prompt$", 3*time.Second) {
		t.Fatalf("the screen never filled; view:\n%s", p.View())
	}
	before := p.View()

	p.eraseEcho(1, 0, bashCwdHook)

	if p.View() != before {
		t.Fatalf("the erase ran over the host's own output; view:\n%s", p.View())
	}
}

// Nor does it erase a span holding no echo at all.
func TestEraseEchoDeclinesWithoutAnEcho(t *testing.T) {
	pr, pw := io.Pipe()
	p := New(&sshx.Session{Stdin: nopWriteCloser{io.Discard}, Stdout: pr}, 60, 8, nil)
	defer p.Close()

	io.WriteString(pw, "banner\r\nsome output\r\nmore output\r\nprompt$ ")
	if !waitForView(p, "prompt$", 3*time.Second) {
		t.Fatalf("the screen never filled; view:\n%s", p.View())
	}
	before := p.View()

	p.eraseEcho(1, 0, bashCwdHook)

	if p.View() != before {
		t.Fatalf("rows with no echo on them were erased; view:\n%s", p.View())
	}
}

// A span reaching past the cursor, or taller than the screen, is refused.
func TestEraseEchoDeclinesOnUntrustworthyGeometry(t *testing.T) {
	pr, pw := io.Pipe()
	p := New(&sshx.Session{Stdin: nopWriteCloser{io.Discard}, Stdout: pr}, 60, 8, nil)
	defer p.Close()

	io.WriteString(pw, "banner\r\n"+echoOf(bashCwdHook)+"\r\nprompt$ ")
	if !waitForView(p, "prompt$", 3*time.Second) {
		t.Fatalf("the screen never filled; view:\n%s", p.View())
	}
	before := p.View()

	cur := p.emu.CursorPosition().Y
	for _, top := range []int{cur, cur + 1, cur - p.emu.Height()} {
		p.eraseEcho(top, 0, bashCwdHook)
		if p.View() != before {
			t.Fatalf("eraseEcho(%d, 0) changed the screen; view:\n%s", top, p.View())
		}
	}
}

// echoOf renders a hook as a shell echoes it: no kill-line prefix, no carriage return.
func echoOf(hook string) string {
	return "prompt$ " + strings.TrimSuffix(strings.TrimPrefix(hook, "\x15"), "\r")
}

func drain(ch chan struct{}) {
	for {
		select {
		case <-ch:
		default:
			return
		}
	}
}

// A full-screen program owns the alt screen, so nothing is typed while one does.
func TestTrackCwdTypesNothingOntoTheAltScreen(t *testing.T) {
	srv := startShellProbeServer(t, "/bin/bash")
	cli := dialProbeServer(t, srv.addr)
	defer cli.Close()

	sess, err := cli.Shell(80, 24)
	if err != nil {
		t.Fatalf("Shell: %v", err)
	}
	p := New(sess, 80, 24, nil)
	defer p.Close()

	// The far end takes the alt screen before the probe comes back.
	srv.say("\x1b[?1049h")
	deadline := time.Now().Add(3 * time.Second)
	for !p.AltScreen() && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if !p.AltScreen() {
		t.Fatal("the pane never reported the alt screen")
	}

	p.TrackCwd(cli, "")

	if srv.waitForInput(t, "hop_cwd", false) {
		t.Fatalf("the hook was typed into a full-screen program; it saw %q", srv.shellInput())
	}
}

// Closing the pane ends the injection: nothing is typed afterwards.
func TestTrackCwdStopsWhenThePaneCloses(t *testing.T) {
	srv := startShellProbeServer(t, "/bin/bash")
	cli := dialProbeServer(t, srv.addr)
	defer cli.Close()

	sess, err := cli.Shell(80, 24)
	if err != nil {
		t.Fatalf("Shell: %v", err)
	}
	p := New(sess, 80, 24, nil)

	p.TrackCwd(cli, "")
	// Close while the goroutine is still waiting on the first output.
	if err := p.Close(); err != nil && !strings.Contains(err.Error(), "closed") {
		t.Fatalf("Close: %v", err)
	}

	if srv.waitForInput(t, "hop_cwd", false) {
		t.Fatalf("the hook was typed into a closed pane; the far end saw %q", srv.shellInput())
	}
}

func waitForCwd(p *Pane, want string) bool {
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if p.Cwd() == want {
			return true
		}
		time.Sleep(10 * time.Millisecond)
	}
	return p.Cwd() == want
}

// The startup line is always one submitted line, cd first.
func TestStartupLine(t *testing.T) {
	for _, c := range []struct{ name, dir, hook, want string }{
		{"nothing to do", "", "", ""},
		{"hook only", "", bashCwdHook, bashCwdHook},
		{"cd only", "/srv/app", "", "\x15 cd '/srv/app'\r"},
		{"cd and hook", "/srv/app", zshCwdHook, "\x15 cd '/srv/app'; " + strings.TrimPrefix(zshCwdHook, "\x15 ")},
	} {
		got := startupLine(c.dir, c.hook)
		if got != c.want {
			t.Errorf("%s: startupLine(%q, hook) = %q, want %q", c.name, c.dir, got, c.want)
		}
		if got == "" {
			continue
		}
		if !strings.HasPrefix(got, "\x15 ") {
			t.Errorf("%s: %q does not open with the kill-line and the history-hiding space", c.name, got)
		}
		if strings.Count(got, "\r") != 1 || !strings.HasSuffix(got, "\r") || strings.Contains(got, "\n") {
			t.Errorf("%s: %q is not one submitted line", c.name, got)
		}
		if strings.Count(got, "\x15") != 1 {
			t.Errorf("%s: %q kills the line more than once", c.name, got)
		}
	}
}

// The directory stays one shell word; a leading ~ is left for the shell to expand.
func TestShellQuotePath(t *testing.T) {
	for _, c := range []struct{ dir, want string }{
		{"/srv/app", "'/srv/app'"},
		{"/srv/my app", "'/srv/my app'"},
		{"~", "~"},
		{"~/work", "~/'work'"},
		{"/tmp/x'; rm -rf /; '", `'/tmp/x'\''; rm -rf /; '\'''`},
		{"$HOME/x", "'$HOME/x'"},
	} {
		if got := shellQuotePath(c.dir); got != c.want {
			t.Errorf("shellQuotePath(%q) = %q, want %q", c.dir, got, c.want)
		}
	}
}

// fish gets no hook, so the cd goes in on its own.
func TestTrackCwdCdsIntoTheDefaultDirectory(t *testing.T) {
	srv := startShellProbeServer(t, "/usr/bin/fish")
	cli := dialProbeServer(t, srv.addr)
	defer cli.Close()

	sess, err := cli.Shell(80, 24)
	if err != nil {
		t.Fatalf("Shell: %v", err)
	}
	p := New(sess, 80, 24, nil)
	defer p.Close()

	srv.say("prompt$ ")
	p.TrackCwd(cli, "/srv/app")

	if !srv.waitForInput(t, "cd '/srv/app'", true) {
		t.Fatalf("the cd never arrived; the far end saw %q", srv.shellInput())
	}
	if strings.Contains(srv.shellInput(), "hop_cwd") {
		t.Fatalf("a hook was typed into fish; it saw %q", srv.shellInput())
	}
}

func TestTrackCwdSendsTheCdAndTheHookTogether(t *testing.T) {
	srv := startShellProbeServer(t, "/bin/bash")
	cli := dialProbeServer(t, srv.addr)
	defer cli.Close()

	sess, err := cli.Shell(80, 24)
	if err != nil {
		t.Fatalf("Shell: %v", err)
	}
	p := New(sess, 80, 24, nil)
	defer p.Close()

	srv.say("prompt$ ")
	p.TrackCwd(cli, "/srv/app")

	if !srv.waitForInput(t, "hop_cwd", true) {
		t.Fatalf("the hook never arrived; the far end saw %q", srv.shellInput())
	}
	in := srv.shellInput()
	if !strings.Contains(in, "cd '/srv/app'; hop_cwd()") {
		t.Fatalf("the cd does not lead the hook on one line; the far end saw %q", in)
	}
	if strings.Count(in, "\r") != 1 {
		t.Fatalf("more than one line was submitted; the far end saw %q", in)
	}
}

func TestTrackCwdTypesNothingWithNoDirAndNoHook(t *testing.T) {
	srv := startShellProbeServer(t, "/usr/bin/fish")
	cli := dialProbeServer(t, srv.addr)
	defer cli.Close()

	sess, err := cli.Shell(80, 24)
	if err != nil {
		t.Fatalf("Shell: %v", err)
	}
	p := New(sess, 80, 24, nil)
	defer p.Close()

	srv.say("prompt$ ")
	p.TrackCwd(cli, "")

	if srv.waitForInput(t, "cd ", false) {
		t.Fatalf("something was typed into a shell with nothing to do; it saw %q", srv.shellInput())
	}
}
