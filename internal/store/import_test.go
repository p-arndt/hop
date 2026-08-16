package store

import (
	"os"
	"path/filepath"
	"testing"
)

// writeConfig writes an ssh config into a temp dir and returns its path.
func writeConfig(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return path
}

// importConfig imports body into s and returns the count, failing on any error.
func importConfig(t *testing.T, s *Store, body string) int {
	t.Helper()
	n, err := s.ImportSSHConfig(writeConfig(t, body))
	if err != nil {
		t.Fatalf("ImportSSHConfig: %v", err)
	}
	return n
}

// The directives hop stores are read off each concrete Host block, and the defaults an
// omitted directive implies are ssh's own: port 22, and the alias standing in as the
// hostname when there is no HostName.
func TestImportSSHConfigReadsDirectivesAndDefaults(t *testing.T) {
	s := newStore(t)
	n := importConfig(t, s, `
Host full
  HostName full.example.com
  User deploy
  Port 2222
  IdentityFile ~/.ssh/id_full

Host bare
`)
	if n != 2 {
		t.Fatalf("imported %d hosts, want 2", n)
	}

	full := findHost(t, s, "full")
	if full == nil {
		t.Fatal("host full missing")
	}
	if full.HostName != "full.example.com" || full.User != "deploy" || full.Port != 2222 ||
		full.IdentityFile != "~/.ssh/id_full" {
		t.Fatalf("full = %+v", *full)
	}

	bare := findHost(t, s, "bare")
	if bare == nil {
		t.Fatal("host bare missing")
	}
	if bare.HostName != "bare" || bare.Port != 22 || bare.User != "" {
		t.Fatalf("bare = %+v, want the alias as hostname on ssh's default port", *bare)
	}
}

// A pattern is not a host: "*" and "web?" name whatever matches them, and importing one
// would put a row in the list that cannot be dialled. Their directives still reach the
// concrete aliases through ssh_config's own matching.
func TestImportSSHConfigSkipsWildcardPatterns(t *testing.T) {
	s := newStore(t)
	n := importConfig(t, s, `
Host *
  User root

Host web?
  Port 2200

Host web1
  HostName web1.example.com
`)
	if n != 1 {
		t.Fatalf("imported %d hosts, want only the concrete one", n)
	}
	if h := findHost(t, s, "*"); h != nil {
		t.Fatalf("wildcard was imported: %+v", *h)
	}
	h := findHost(t, s, "web1")
	if h == nil {
		t.Fatal("web1 missing")
	}
	if h.User != "root" || h.Port != 2200 {
		t.Fatalf("web1 = %+v, want the pattern blocks' User and Port applied", *h)
	}
}

// One Host line can carry several aliases, and each becomes its own row sharing the
// block's settings.
func TestImportSSHConfigSplitsMultiAliasBlocks(t *testing.T) {
	s := newStore(t)
	n := importConfig(t, s, `
Host db1 db2
  HostName db.example.com
  User dba
`)
	if n != 2 {
		t.Fatalf("imported %d hosts, want 2", n)
	}
	for _, alias := range []string{"db1", "db2"} {
		h := findHost(t, s, alias)
		if h == nil {
			t.Fatalf("%s missing", alias)
		}
		if h.HostName != "db.example.com" || h.User != "dba" {
			t.Fatalf("%s = %+v", alias, *h)
		}
	}
}

// A Port that is not a usable number is ignored rather than stored: ssh's default is a
// better guess than 0, which nothing can connect to.
func TestImportSSHConfigIgnoresUnusablePorts(t *testing.T) {
	s := newStore(t)
	importConfig(t, s, `
Host wordy
  Port ssh

Host zero
  Port 0

Host negative
  Port -1
`)
	for _, alias := range []string{"wordy", "zero", "negative"} {
		h := findHost(t, s, alias)
		if h == nil {
			t.Fatalf("%s missing", alias)
		}
		if h.Port != 22 {
			t.Fatalf("%s port = %d, want ssh's default", alias, h.Port)
		}
	}
}

// Re-importing after an edit refreshes the connection details, and — being an Upsert —
// leaves the visit history the user has accumulated against that alias intact.
func TestImportSSHConfigRefreshesWithoutLosingFrecency(t *testing.T) {
	s := newStore(t)
	importConfig(t, s, "Host web\n  HostName old.example.com\n  User old\n")
	for range 3 {
		if err := s.Touch("web"); err != nil {
			t.Fatalf("Touch: %v", err)
		}
	}

	if n := importConfig(t, s, "Host web\n  HostName new.example.com\n  User new\n  Port 2222\n"); n != 1 {
		t.Fatalf("re-import count = %d, want 1", n)
	}
	h := findHost(t, s, "web")
	if h.HostName != "new.example.com" || h.User != "new" || h.Port != 2222 {
		t.Fatalf("re-import did not refresh: %+v", *h)
	}
	if h.Visits != 3 {
		t.Fatalf("visits = %d, want the 3 touches to survive the re-import", h.Visits)
	}
}

// A missing config file is an error the caller reports; an unreadable one must not look
// like "nothing to import".
func TestImportSSHConfigMissingFile(t *testing.T) {
	s := newStore(t)
	n, err := s.ImportSSHConfig(filepath.Join(t.TempDir(), "no-such-config"))
	if err == nil {
		t.Fatalf("ImportSSHConfig of a missing file = %d, nil; want an error", n)
	}
	if n != 0 {
		t.Fatalf("count = %d, want 0", n)
	}
}

// parseSSHForward accepts OpenSSH's TCP shapes and only those: the socket and dynamic
// forms belong to ssh itself and would be misrepresented as TCP definitions here.
func TestParseSSHForward(t *testing.T) {
	cases := []struct {
		name  string
		value string
		ok    bool
		want  Forward
	}{
		{"port only binds loopback", "8080 target:80", true,
			Forward{Kind: ForwardLocal, BindHost: "127.0.0.1", BindPort: 8080, TargetHost: "target", TargetPort: 80}},
		{"explicit bind host", "0.0.0.0:8080 target:80", true,
			Forward{Kind: ForwardLocal, BindHost: "0.0.0.0", BindPort: 8080, TargetHost: "target", TargetPort: 80}},
		{"bracketed IPv6 bind", "[::1]:8080 target:80", true,
			Forward{Kind: ForwardLocal, BindHost: "::1", BindPort: 8080, TargetHost: "target", TargetPort: 80}},
		{"bracketed IPv6 target", "8080 [2001:db8::1]:80", true,
			Forward{Kind: ForwardLocal, BindHost: "127.0.0.1", BindPort: 8080, TargetHost: "2001:db8::1", TargetPort: 80}},
		{"extra whitespace", "  8080    target:80  ", true,
			Forward{Kind: ForwardLocal, BindHost: "127.0.0.1", BindPort: 8080, TargetHost: "target", TargetPort: 80}},
		{"one field", "8080", false, Forward{}},
		{"three fields", "127.0.0.1 8080 target:80", false, Forward{}},
		{"unix socket target", "8080 /var/run/docker.sock", false, Forward{}},
		{"unix socket bind", "/tmp/sock target:80", false, Forward{}},
		{"target without a port", "8080 target", false, Forward{}},
		{"target without a host", "8080 :80", false, Forward{}},
		{"non-numeric port", "8080 target:http", false, Forward{}},
		{"port out of range", "8080 target:70000", false, Forward{}},
		{"zero port", "0 target:80", false, Forward{}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := parseSSHForward(tc.value, ForwardLocal)
			if ok != tc.ok {
				t.Fatalf("parseSSHForward(%q) ok = %v, want %v (got %+v)", tc.value, ok, tc.ok, got)
			}
			if ok && got != tc.want {
				t.Fatalf("parseSSHForward(%q) = %+v, want %+v", tc.value, got, tc.want)
			}
		})
	}

	if got, _ := parseSSHForward("8080 target:80", ForwardRemote); got.Kind != ForwardRemote {
		t.Fatalf("kind = %q, want it carried through from the directive", got.Kind)
	}
}
