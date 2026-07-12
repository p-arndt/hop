package action

import (
	"strings"
	"testing"
)

// Whatever the program and arguments carry, the -Command handed to pwsh must be
// a single "& '…' '…'" invocation in which every part sits inside single quotes
// — the one PowerShell context where nothing is expanded or re-parsed.
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

// psQuote must leave no way out of the quoted region: after stripping doubled
// quotes (PowerShell's escaped literal), the only single quotes left are the
// enclosing pair.
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
