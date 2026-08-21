package store

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Every field hop writes to the config file comes back out of it.
func TestHostsRoundTripThroughConfigFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "hop.config")
	want := []Host{{
		ID:           1,
		Alias:        "web",
		HostName:     "10.0.0.1",
		User:         "deploy",
		Port:         2222,
		IdentityFile: "~/.ssh/id_ed25519",
		ProxyJump:    "bastion",
		Forwards: []Forward{
			{Kind: ForwardLocal, BindHost: "127.0.0.1", BindPort: 8080, TargetHost: "app.internal", TargetPort: 80},
			{Kind: ForwardRemote, BindHost: "0.0.0.0", BindPort: 9000, TargetHost: "localhost", TargetPort: 9000},
		},
	}, {
		ID:           2,
		Alias:        "broker",
		HostName:     "internal.corp",
		Port:         22,
		ProxyCommand: "sh -c 'aws ssm start-session --target i-123'",
	}}

	if err := writeHosts(path, want); err != nil {
		t.Fatalf("writeHosts: %v", err)
	}
	got, err := readHosts(path)
	if err != nil {
		t.Fatalf("readHosts: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d hosts, want 2", len(got))
	}

	web := got[0]
	if web.Alias != "web" || web.HostName != "10.0.0.1" || web.User != "deploy" || web.Port != 2222 {
		t.Fatalf("web = %+v", web)
	}
	if web.IdentityFile != "~/.ssh/id_ed25519" || web.ProxyJump != "bastion" {
		t.Fatalf("web = %+v", web)
	}
	if len(web.Forwards) != 2 {
		t.Fatalf("web forwards = %+v, want 2", web.Forwards)
	}
	var local, remote *Forward
	for i := range web.Forwards {
		switch web.Forwards[i].Kind {
		case ForwardLocal:
			local = &web.Forwards[i]
		case ForwardRemote:
			remote = &web.Forwards[i]
		}
	}
	if local == nil || local.BindPort != 8080 || local.TargetHost != "app.internal" || local.TargetPort != 80 {
		t.Fatalf("local forward = %+v", local)
	}
	if remote == nil || remote.BindHost != "0.0.0.0" || remote.BindPort != 9000 {
		t.Fatalf("remote forward = %+v", remote)
	}

	if got[1].ProxyCommand != want[1].ProxyCommand {
		t.Fatalf("broker ProxyCommand = %q", got[1].ProxyCommand)
	}
}

// Real directives, with the default port left out.
func TestWriteHostsEmitsOpenSSHSyntax(t *testing.T) {
	path := filepath.Join(t.TempDir(), "hop.config")
	if err := writeHosts(path, []Host{{Alias: "plain", HostName: "plain.test", Port: 22}}); err != nil {
		t.Fatalf("writeHosts: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	if !strings.Contains(text, "Host plain\n") || !strings.Contains(text, "    HostName plain.test\n") {
		t.Fatalf("config file = %q", text)
	}
	if strings.Contains(text, "Port 22") {
		t.Fatalf("the default port should not be written: %q", text)
	}
	// Unset fields leave no empty directive behind, which ssh would reject.
	for _, key := range []string{"User", "IdentityFile", "ProxyCommand", "ProxyJump"} {
		if strings.Contains(text, key) {
			t.Fatalf("unset %s was written: %q", key, text)
		}
	}
}

func TestReadHostsSkipsWildcards(t *testing.T) {
	hosts, err := decodeHosts(strings.NewReader("Host *\n  User everyone\n\nHost real\n  HostName real.test\n"))
	if err != nil {
		t.Fatalf("decodeHosts: %v", err)
	}
	if len(hosts) != 1 || hosts[0].Alias != "real" {
		t.Fatalf("hosts = %+v, want just real", hosts)
	}
}

func TestReadHostsDefaultsHostNameToAlias(t *testing.T) {
	hosts, err := decodeHosts(strings.NewReader("Host bare\n  User me\n"))
	if err != nil {
		t.Fatalf("decodeHosts: %v", err)
	}
	if len(hosts) != 1 || hosts[0].HostName != "bare" {
		t.Fatalf("hosts = %+v, want HostName bare", hosts)
	}
}

// The Include must go first: OpenSSH takes the first value it finds for most keywords.
func TestEnsureIncludePrependsAndPreserves(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config")
	hostsPath := filepath.Join(dir, "hop.config")

	original := "# my own config\nHost mine\n  HostName mine.test\n"
	if err := os.WriteFile(configPath, []byte(original), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := ensureInclude(configPath, hostsPath); err != nil {
		t.Fatalf("ensureInclude: %v", err)
	}

	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	if !strings.Contains(text, original) {
		t.Fatalf("the user's config was not preserved: %q", text)
	}
	include := strings.Index(text, "Include hop.config")
	if include < 0 {
		t.Fatalf("no Include was added: %q", text)
	}
	if include > strings.Index(text, "Host mine") {
		t.Fatalf("the Include must come before the user's own blocks: %q", text)
	}

	if err := ensureInclude(configPath, hostsPath); err != nil {
		t.Fatalf("second ensureInclude: %v", err)
	}
	again, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(again) != text {
		t.Fatalf("a second run rewrote the file:\n%q\n%q", text, again)
	}
	if n := strings.Count(string(again), "Include"); n != 1 {
		t.Fatalf("Include appears %d times, want 1", n)
	}
}

func TestEnsureIncludeRecognisesExistingSpellings(t *testing.T) {
	dir := t.TempDir()
	hostsPath := filepath.Join(dir, "hop.config")
	for _, existing := range []string{
		"Include hop.config\n",
		"Include ~/.ssh/hop.config\n",
		"Include " + hostsPath + "\n",
		"include hop.config\n",
		"Include other.config hop.config\n",
	} {
		if !hasInclude(existing, hostsPath) {
			t.Errorf("hasInclude(%q) = false, want true", existing)
		}
	}
	for _, existing := range []string{
		"",
		"# Include hop.config\n",
		"Include work.config\n",
		"Host hop.config\n",
	} {
		if hasInclude(existing, hostsPath) {
			t.Errorf("hasInclude(%q) = true, want false", existing)
		}
	}
}

func TestEnsureIncludeCreatesMissingConfig(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config")
	if err := ensureInclude(configPath, filepath.Join(dir, "hop.config")); err != nil {
		t.Fatalf("ensureInclude: %v", err)
	}
	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("no config was created: %v", err)
	}
	if !strings.Contains(string(data), "Include hop.config") {
		t.Fatalf("config = %q", data)
	}
}

// A hosts file outside ~/.ssh needs its full path; a bare name resolves relative to ~/.ssh.
func TestEnsureIncludeUsesFullPathWhenElsewhere(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config")
	hostsPath := filepath.Join(t.TempDir(), "elsewhere.config")
	if err := ensureInclude(configPath, hostsPath); err != nil {
		t.Fatalf("ensureInclude: %v", err)
	}
	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "Include "+hostsPath) {
		t.Fatalf("config = %q, want the full path", data)
	}
}

func TestWriteFileAtomicLeavesNoTemp(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "hop.config")
	if err := writeFileAtomic(path, []byte("first"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := writeFileAtomic(path, []byte("second"), 0o600); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil || string(data) != "second" {
		t.Fatalf("content = %q, %v", data, err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("temp files left behind: %v", entries)
	}
}
