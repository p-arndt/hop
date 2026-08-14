package sshx

import (
	"errors"
	"strings"
	"testing"
)

func TestExpandProxyTokens(t *testing.T) {
	tests := []struct {
		name, in, want string
	}{
		{"ssm", "aws ssm start-session --target %h --parameters portNumber=%p", "aws ssm start-session --target i-0abc --parameters portNumber=2222"},
		{"user", "connect %r@%h", "connect deploy@i-0abc"},
		{"literal percent", "x %% %h", "x % i-0abc"},
		{"percent then h is literal", "%%h", "%h"},
		{"unknown token untouched", "cmd %q %h", "cmd %q i-0abc"},
		{"trailing percent", "cmd %", "cmd %"},
		{"alias", "proxy --name %n", "proxy --name ssm-box"},
		{"no tokens", "cloudflared access ssh", "cloudflared access ssh"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := expandProxyTokens(tc.in, "i-0abc", 2222, "deploy", "ssm-box"); got != tc.want {
				t.Errorf("expandProxyTokens() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestSplitProxyCommand(t *testing.T) {
	tests := []struct {
		name, in string
		want     []string
	}{
		{"plain", "aws ssm start-session --target i-0abc", []string{"aws", "ssm", "start-session", "--target", "i-0abc"}},
		{"double quotes", `"C:\Program Files\aws\aws.exe" ssm`, []string{`C:\Program Files\aws\aws.exe`, "ssm"}},
		{"single quotes", "cmd 'two words' x", []string{"cmd", "two words", "x"}},
		{"escaped space", `cmd a\ b`, []string{"cmd", "a b"}},
		{"collapsed whitespace", "  cmd \t  arg  ", []string{"cmd", "arg"}},
		{"quoted empty arg", `cmd ""`, []string{"cmd", ""}},
		// The issue's own line: an unquoted "=" is argv content, not shell syntax.
		{"issue 13 ssm line", "aws ssm start-session --target %h --document-name AWS-StartSSHSession --parameters portNumber=%p",
			[]string{"aws", "ssm", "start-session", "--target", "%h", "--document-name", "AWS-StartSSHSession", "--parameters", "portNumber=%p"}},
		{"glob stays literal argv", "cmd a*b", []string{"cmd", "a*b"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := splitProxyCommand(tc.in)
			if err != nil {
				t.Fatalf("splitProxyCommand(%q) error: %v", tc.in, err)
			}
			if len(got) != len(tc.want) {
				t.Fatalf("splitProxyCommand(%q) = %q, want %q", tc.in, got, tc.want)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Fatalf("splitProxyCommand(%q) = %q, want %q", tc.in, got, tc.want)
				}
			}
		})
	}
}

// A command line only a shell could run is refused rather than handed to `sh -c`: a
// ProxyCommand can arrive from an imported config, so that path must not run arbitrary
// shell.
func TestSplitProxyCommandRejectsShell(t *testing.T) {
	for _, in := range []string{
		"nc %h %p | tee /tmp/log",
		"cmd && other",
		"cmd > /tmp/out",
		"cmd $(whoami)",
		"cmd `id`",
	} {
		if _, err := splitProxyCommand(in); !errors.Is(err, ErrProxyNeedsShell) {
			t.Errorf("splitProxyCommand(%q) error = %v, want ErrProxyNeedsShell", in, err)
		}
	}
}

func TestSplitProxyCommandErrors(t *testing.T) {
	for _, in := range []string{"", "   ", `cmd "unterminated`, `cmd 'x`} {
		if _, err := splitProxyCommand(in); err == nil {
			t.Errorf("splitProxyCommand(%q) = nil error, want one", in)
		}
	}
}

func TestParseJump(t *testing.T) {
	tests := []struct {
		name, in string
		want     JumpHost
	}{
		{"bare host", "bastion.example.com", JumpHost{Host: "bastion.example.com"}},
		{"user and port", "jump@bastion:2222", JumpHost{User: "jump", Host: "bastion", Port: 2222}},
		{"user only", "jump@bastion", JumpHost{User: "jump", Host: "bastion"}},
		{"first of a chain", "a, b, c", JumpHost{Host: "a"}},
		{"ipv6", "[2001:db8::1]", JumpHost{Host: "2001:db8::1"}},
		{"ipv6 with port", "u@[2001:db8::1]:2200", JumpHost{User: "u", Host: "2001:db8::1", Port: 2200}},
		{"padded", "  bastion  ", JumpHost{Host: "bastion"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseJump(tc.in)
			if err != nil {
				t.Fatalf("parseJump(%q) error: %v", tc.in, err)
			}
			if got != tc.want {
				t.Errorf("parseJump(%q) = %+v, want %+v", tc.in, got, tc.want)
			}
		})
	}
}

func TestParseJumpErrors(t *testing.T) {
	for _, in := range []string{"", "none", "host:0", "host:99999", "host:abc", "[2001:db8::1"} {
		if _, err := parseJump(in); err == nil {
			t.Errorf("parseJump(%q) = nil error, want one", in)
		}
	}
}

func TestBoundedBufferCaps(t *testing.T) {
	b := &boundedBuffer{limit: 8}
	b.Write([]byte("hello "))
	n, err := b.Write([]byte("world and much more"))
	if err != nil || n != len("world and much more") {
		t.Fatalf("Write() = %d, %v; want a full write with no error", n, err)
	}
	if got := b.String(); got != "hello wo" {
		t.Errorf("String() = %q, want %q", got, "hello wo")
	}
}

// A proxy that dies before speaking SSH must surface its own stderr, not a bare EOF:
// that message is the only account of why the broker refused.
func TestProcConnReportsStderr(t *testing.T) {
	conn, err := dialProxyCommand(failingProxyCommand(), "h", 22, "u", "alias")
	if err != nil {
		t.Fatalf("dialProxyCommand() error: %v", err)
	}
	defer conn.Close()

	buf := make([]byte, 64)
	for {
		_, rerr := conn.Read(buf)
		if rerr == nil {
			continue
		}
		if !strings.Contains(rerr.Error(), "TargetNotConnected") {
			t.Fatalf("Read() error = %v, want it to carry the proxy's stderr", rerr)
		}
		return
	}
}

// Real ProxyCommand lines from vendor documentation, as a guard on where the
// no-shell line is drawn: everything a broker actually ships must run, and only genuine
// shell syntax may be turned away.
func TestRealWorldProxyCommands(t *testing.T) {
	runnable := []struct{ name, line string }{
		{"aws ssm", "aws ssm start-session --target %h --document-name AWS-StartSSHSession --parameters portNumber=%p"},
		{"cloudflared", "cloudflared access ssh --hostname %h"},
		{"gcloud iap", "gcloud compute start-iap-tunnel %h %p --listen-on-stdin --project=my-proj --zone=europe-west1-b"},
		{"teleport", "tsh proxy ssh --cluster=prod --proxy=teleport.example.com:443 %r@%h:%p"},
		{"azure bastion", "az network bastion tunnel --name b --resource-group rg --target-resource-id %h --resource-port %p"},
		{"boundary", "boundary connect ssh -target-id ttcp_1234 -- -W %h:%p"},
		{"openssh -W", "ssh -q -W %h:%p bastion.example.com"},
		{"openssh nc", "ssh bastion.example.com nc %h %p"},
		{"corkscrew", "corkscrew proxy.example.com 8080 %h %p"},
		{"netcat socks", "nc -X 5 -x localhost:1080 %h %p"},
		{"socat", "socat - PROXY:proxy.example.com:%h:%p,proxyport=8080"},
		{"tailscale", "tailscale nc %h %p"},
		{"plink", "plink -batch -T %h"},
		{"windows openssh", `"C:\Windows\System32\OpenSSH\ssh.exe" -W %h:%p bastion`},
		{"home relative", "~/bin/my-tunnel %h %p"},
		// A quoted shell invocation is fine: sh is then the program hop runs, chosen by
		// the user in the config, not a shell hop wrapped around their line.
		{"explicit sh wrapper", `sh -c "exec openssl s_client -connect %h:%p"`},
	}
	for _, tc := range runnable {
		t.Run(tc.name, func(t *testing.T) {
			line := expandProxyTokens(tc.line, "target.example.com", 22, "deploy", "alias")
			argv, err := splitProxyCommand(line)
			if err != nil {
				t.Fatalf("splitProxyCommand(%q) = %v, want it to run without a shell", line, err)
			}
			if len(argv) == 0 || argv[0] == "" {
				t.Fatalf("splitProxyCommand(%q) produced no program", line)
			}
		})
	}

	refused := []struct{ name, line string }{
		{"pipe", "nc %h %p | tee /tmp/log"},
		{"env var", "proxy --token $TOKEN %h"},
	}
	for _, tc := range refused {
		t.Run(tc.name, func(t *testing.T) {
			line := expandProxyTokens(tc.line, "target.example.com", 22, "deploy", "alias")
			if _, err := splitProxyCommand(line); !errors.Is(err, ErrProxyNeedsShell) {
				t.Errorf("splitProxyCommand(%q) = %v, want ErrProxyNeedsShell", line, err)
			}
		})
	}
}
