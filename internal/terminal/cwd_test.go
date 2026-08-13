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

// probeServer is an in-process SSH server for the shell-integration half of this
// file: it answers the login-shell probe with a shell of the test's choosing,
// records everything typed into its shell channel, and can speak on that channel
// itself — which is how a test plays the part of the prompt hook the real thing
// would have installed.
type probeServer struct {
	addr  string
	shell string // what the exec probe answers

	mu    sync.Mutex
	input []byte
	live  ssh.Channel

	opened chan struct{} // closed once the shell channel exists
}

// shellInput is everything the client has typed into the shell channel.
func (s *probeServer) shellInput() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return string(s.input)
}

// waitForInput polls the shell channel's input for want. When the test expects
// nothing it still waits — briefly — since "it did not arrive yet" and "it will
// never arrive" look the same at the instant of asking.
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

// say writes to the shell channel, as a remote shell would.
func (s *probeServer) say(out string) {
	<-s.opened
	s.mu.Lock()
	ch := s.live
	s.mu.Unlock()
	if ch != nil {
		io.WriteString(ch, out)
	}
}

// startShellProbeServer brings up the server, answering the login-shell probe
// with shell.
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

// session answers a pty request, answers an exec with the configured login shell,
// and on a shell request keeps the channel and records what is typed into it.
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

// record appends everything the client sends on the shell channel.
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

// dialProbeServer connects to addr, trusting whatever key it presents.
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

// The network decides where the chunk boundaries fall, so the scanner has to
// carry its state across them — including a split inside the ESC ] introducer and
// inside the ST terminator.
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

// Every cd emits another report, and the last one is where the shell is.
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

// A sequence whose terminator never arrives must not grow the buffer without
// bound, and what follows the cap must not be mistaken for a directory.
func TestScannerCapsAnUnterminatedPayload(t *testing.T) {
	var s oscScanner
	s.feed([]byte("\x1b]7;file://web1/" + strings.Repeat("a", maxOSCPayload+2048)))
	if len(s.buf) > maxOSCPayload {
		t.Fatalf("buffered %d bytes, cap is %d", len(s.buf), maxOSCPayload)
	}
	if dir, ok := s.feed([]byte("\x07")); ok {
		t.Fatalf("an over-long payload was reported as %q", dir)
	}
	// And the scanner still works afterwards.
	if dir, ok := s.feed([]byte(osc7("web1", "/tmp"))); !ok || dir != "/tmp" {
		t.Fatalf("after an over-long payload: cwd = %q, ok = %v", dir, ok)
	}
}

// An OSC interrupted by another escape sequence is malformed; the one that starts
// immediately afterwards is not, and is still read.
func TestScannerRecoversFromAnInterruptedOSC(t *testing.T) {
	var s oscScanner
	if _, ok := s.feed([]byte("\x1b]7;file://web1/tmp\x1b[0m")); ok {
		t.Fatal("an OSC cut short by a CSI was reported as a cwd")
	}
	if dir, ok := s.feed([]byte(osc7("web1", "/srv"))); !ok || dir != "/srv" {
		t.Fatalf("after an interrupted OSC: cwd = %q, ok = %v", dir, ok)
	}
}

// A pane reports no directory until its shell says one, and the whole path from
// server bytes to Pane.Cwd works.
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

// Which shells the prompt hook may be typed into, and which line each gets.
// Everything else is left alone rather than sent a line it would answer with a
// parse error.
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

// The hooks themselves: one line each, submitted, and hung off the right shell's
// prompt machinery.
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
		// It is typed at a prompt and echoed there, so its length is a user-visible
		// cost. Two lines of an 80-column terminal is the budget.
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

// The clean-up after the injection: the rows the shell echoed the hook into are
// deleted from hop's own screen, and what was below them slides up into their place.
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

	// Row 0 is the banner; row 1 is where the prompt was when the hook was typed, and
	// the hook wraps from there. The prompt below it is where the cursor ends up.
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

// The span is measured before the hook is written, so the host is free to print into
// it afterwards — a slow dynamic MOTD does exactly that. Rows holding anything but
// the echo are left alone: a visible line is a blemish, deleting the host's own
// output would be a defect.
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

// Nor does it erase a span that holds no echo at all — the shell never ran the hook,
// or something else has since redrawn the screen.
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

// And it declines on geometry it cannot use: a span that reaches to or past the
// cursor covers nothing, and one taller than the screen is not a span at all.
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

// echoOf renders a hook the way a shell echoes it at a prompt: the text that was
// typed, after a prompt, without the kill-line prefix or the carriage return that
// submitted it.
func echoOf(hook string) string {
	return "prompt$ " + strings.TrimSuffix(strings.TrimPrefix(hook, "\x15"), "\r")
}

// drain empties a channel of whatever it has collected so far.
func drain(ch chan struct{}) {
	for {
		select {
		case <-ch:
		default:
			return
		}
	}
}

// The login shell hop probed is what the *account* has, not necessarily what is on
// the other end of the pty: a .bash_profile ending in `exec tmux attach`, or an sshd
// ForceCommand, both answer "bash". A full-screen program owns the alt screen, so
// nothing is typed while one does — typing a shell command into vim would edit
// somebody's file.
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

	// The far end takes the alt screen before the probe can come back, as a program
	// launched from a login file does.
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

// A shell tab is closed with one keystroke, and the injection sequence spans seconds.
// Closing the pane ends it: nothing is typed into the session afterwards, and nothing
// is written to the emulator Close has torn down.
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
	// Close while the goroutine is still waiting on the shell's first output.
	if err := p.Close(); err != nil && !strings.Contains(err.Error(), "closed") {
		t.Fatalf("Close: %v", err)
	}

	if srv.waitForInput(t, "hop_cwd", false) {
		t.Fatalf("the hook was typed into a closed pane; the far end saw %q", srv.shellInput())
	}
}

// waitForCwd polls Pane.Cwd until it reports want, or gives up.
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

// The startup line is one submitted line whatever it is made of, and the cd comes
// before the hook — so the hook's own trailing call reports the directory the
// session is actually starting in, not the one it logged into.
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

// The directory is one word to the shell whatever is in it: a space does not split
// it, and a quote cannot end the quoting and start a command. A leading ~ is the
// one thing left to the shell to expand, because "~/work" is what people type.
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

// A host with a default directory moves its shell there on connect. The shell here
// is fish, which gets no OSC 7 hook — the cd is a line every shell understands, so
// it goes in on its own.
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

// A bash host with a default directory gets both, as one line.
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

// A host with no default directory on a shell with no hook has nothing to say, and
// says nothing: the pane is left exactly as the login shell drew it.
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
