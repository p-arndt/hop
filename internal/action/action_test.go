package action

import (
	"slices"
	"strings"
	"testing"
)

// Remote authority plus optional folder, each as its own argv element.
func TestVSCodeArgs(t *testing.T) {
	cases := []struct {
		name       string
		alias      string
		remotePath string
		want       []string
	}{
		{"no path", "web1", "", []string{"--remote", "ssh-remote+web1"}},
		{"with path", "web1", "/srv/app", []string{"--remote", "ssh-remote+web1", "/srv/app"}},
		{"path with spaces", "web1", "/srv/my app", []string{"--remote", "ssh-remote+web1", "/srv/my app"}},
		{"alias from an imported config", "prod db", "~", []string{"--remote", "ssh-remote+prod db", "~"}},
		{"metacharacters stay one argument", "web1", "/srv/$(whoami); rm -rf /",
			[]string{"--remote", "ssh-remote+web1", "/srv/$(whoami); rm -rf /"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := vscodeArgs(tc.alias, tc.remotePath); !slices.Equal(got, tc.want) {
				t.Fatalf("vscodeArgs(%q, %q) = %q, want %q", tc.alias, tc.remotePath, got, tc.want)
			}
		})
	}
}

// Current window, and the shell stays open after the program exits.
func TestNewTabArgsShape(t *testing.T) {
	got := newTabArgs("ssh", []string{"web1"})
	want := []string{"-w", "0", "nt", "pwsh", "-NoExit", "-Command", `& 'ssh' 'web1'`}
	if !slices.Equal(got, want) {
		t.Fatalf("newTabArgs = %q, want %q", got, want)
	}
}

// No arguments is still a quoted call operator invocation, not a bare command line.
func TestNewTabArgsWithoutArguments(t *testing.T) {
	got := newTabArgs("pwsh", nil)
	if cmd := got[len(got)-1]; cmd != `& 'pwsh'` {
		t.Fatalf("-Command = %q, want %q", cmd, `& 'pwsh'`)
	}
}

func TestNewTabArgsQuotesEveryPart(t *testing.T) {
	cases := []struct {
		name    string
		program string
		args    []string
		want    string
	}{
		{"plain", "ssh", []string{"web1"}, `& 'ssh' 'web1'`},
		{"spaces", `C:\Program Files\tool.exe`, []string{"my file.txt"}, `& 'C:\Program Files\tool.exe' 'my file.txt'`},
		{"metacharacters", "ssh", []string{"host; calc", "$(whoami)"}, `& 'ssh' 'host; calc' '$(whoami)'`},
		{"single quotes", "ssh", []string{"o'brien"}, `& 'ssh' 'o''brien'`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := newTabArgs(tc.program, tc.args)
			if got[len(got)-2] != "-Command" {
				t.Fatalf("args = %q, want -Command as the second-to-last", got)
			}
			if cmd := got[len(got)-1]; cmd != tc.want {
				t.Fatalf("-Command = %q, want %q", cmd, tc.want)
			}
		})
	}
}

func TestPsQuoteCannotEscape(t *testing.T) {
	for _, s := range []string{"", "plain", "it's", "'''", "a'b'c", "$env:PATH;&|"} {
		q := psQuote(s)
		if !strings.HasPrefix(q, "'") || !strings.HasSuffix(q, "'") {
			t.Fatalf("psQuote(%q) = %q, want it enclosed in single quotes", s, q)
		}
		inner := strings.ReplaceAll(q[1:len(q)-1], "''", "")
		if strings.Contains(inner, "'") {
			t.Fatalf("psQuote(%q) = %q leaves an unescaped quote", s, q)
		}
	}
}
