package store

// Reading and writing the OpenSSH config file that holds hop's hosts. hop rewrites its own
// file whole, and never touches ~/.ssh/config beyond adding one Include line.

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/kevinburke/ssh_config"
)

const managedHeader = `# This file is managed by hop (https://github.com/p-arndt/hop).
#
# hop rewrites it whenever you add, edit or remove a host, so hand-written comments and
# formatting inside it are not preserved. Editing values by hand is fine — hop reads them
# back. Hosts you want hop to leave alone belong in ~/.ssh/config instead.
#
# Tags, groups, pins and connection counts are hop's own and live in hop's config
# directory, not here.
`

// writeHosts renders hosts as an OpenSSH config file and replaces path atomically.
func writeHosts(path string, hosts []Host) error {
	var b strings.Builder
	b.WriteString(managedHeader)
	for _, h := range hosts {
		b.WriteString("\nHost ")
		b.WriteString(h.Alias)
		b.WriteString("\n")
		writeDirective(&b, "HostName", h.HostName)
		writeDirective(&b, "User", h.User)
		if h.Port > 0 && h.Port != 22 {
			writeDirective(&b, "Port", strconv.Itoa(h.Port))
		}
		writeDirective(&b, "IdentityFile", h.IdentityFile)
		writeDirective(&b, "ProxyCommand", h.ProxyCommand)
		writeDirective(&b, "ProxyJump", h.ProxyJump)

		forwards := append([]Forward(nil), h.Forwards...)
		// Deterministic order keeps the rewritten file from churning.
		sort.SliceStable(forwards, func(i, j int) bool {
			if forwards[i].Kind != forwards[j].Kind {
				return forwards[i].Kind == ForwardLocal
			}
			if forwards[i].BindHost != forwards[j].BindHost {
				return forwards[i].BindHost < forwards[j].BindHost
			}
			return forwards[i].BindPort < forwards[j].BindPort
		})
		for _, f := range forwards {
			key := "LocalForward"
			if f.Kind == ForwardRemote {
				key = "RemoteForward"
			}
			writeDirective(&b, key, fmt.Sprintf("%s %s",
				joinHostPort(f.BindHost, f.BindPort), joinHostPort(f.TargetHost, f.TargetPort)))
		}
	}
	return writeFileAtomic(path, []byte(b.String()), 0o600)
}

// writeDirective emits one indented "Key value" line, skipping blanks, which OpenSSH
// would reject as an empty directive.
func writeDirective(b *strings.Builder, key, value string) {
	value = strings.TrimSpace(value)
	if value == "" {
		return
	}
	fmt.Fprintf(b, "    %s %s\n", key, value)
}

// joinHostPort spells an endpoint the way OpenSSH does, bracketing IPv6 literals.
func joinHostPort(host string, port int) string {
	if strings.Contains(host, ":") {
		return fmt.Sprintf("[%s]:%d", host, port)
	}
	return fmt.Sprintf("%s:%d", host, port)
}

// readHosts parses the config file in file order; a missing file is the first run.
func readHosts(path string) ([]Host, error) {
	f, err := os.Open(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return decodeHosts(f)
}

// decodeHosts parses OpenSSH config syntax, skipping wildcard patterns, which set defaults
// rather than naming a host.
func decodeHosts(r io.Reader) ([]Host, error) {
	cfg, err := ssh_config.Decode(r)
	if err != nil {
		return nil, err
	}
	var hosts []Host
	seen := map[string]bool{}
	for _, block := range cfg.Hosts {
		for _, pattern := range block.Patterns {
			alias := pattern.String()
			if alias == "" || strings.ContainsAny(alias, "*?") || seen[alias] {
				continue
			}
			seen[alias] = true
			hosts = append(hosts, hostFromConfig(cfg, alias))
		}
	}
	return hosts, nil
}

func hostFromConfig(cfg *ssh_config.Config, alias string) Host {
	get := func(key string) string {
		v, _ := cfg.Get(alias, key)
		return strings.TrimSpace(v)
	}
	h := Host{
		Alias:        alias,
		HostName:     get("HostName"),
		User:         get("User"),
		Port:         22,
		IdentityFile: get("IdentityFile"),
		ProxyCommand: normalizeProxyCommand(get("ProxyCommand")),
		ProxyJump:    get("ProxyJump"),
	}
	if h.HostName == "" {
		h.HostName = alias
	}
	if p, err := strconv.Atoi(get("Port")); err == nil && p > 0 && p <= 65535 {
		h.Port = p
	}
	for _, directive := range []struct {
		key  string
		kind ForwardKind
	}{{"LocalForward", ForwardLocal}, {"RemoteForward", ForwardRemote}} {
		values, _ := cfg.GetAll(alias, directive.key)
		for _, value := range values {
			f, ok := parseSSHForward(value, directive.kind)
			if !ok {
				continue // dynamic and Unix-socket forwarding are not TCP tunnels
			}
			h.Forwards = append(h.Forwards, f)
		}
	}
	return h
}

const includeLine = "Include hop.config"

// ensureInclude prepends the Include to ~/.ssh/config: OpenSSH takes the first value for
// most keywords, so an Include at the bottom would be shadowed by any earlier Host block.
func ensureInclude(configPath, hostsPath string) error {
	existing, err := os.ReadFile(configPath)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	line := includeLine
	// Only the default filename may be relative to ~/.ssh; anything else needs a full path.
	if filepath.Base(hostsPath) != "hop.config" || filepath.Dir(hostsPath) != filepath.Dir(configPath) {
		line = "Include " + hostsPath
	}
	if hasInclude(string(existing), hostsPath) {
		return nil
	}

	var b strings.Builder
	b.WriteString("# Added by hop: hop's own hosts live in the included file.\n")
	b.WriteString(line)
	b.WriteString("\n")
	if len(existing) > 0 {
		b.WriteString("\n")
		b.Write(existing)
	}
	if err := os.MkdirAll(filepath.Dir(configPath), 0o700); err != nil {
		return err
	}
	return writeFileAtomic(configPath, []byte(b.String()), 0o600)
}

// hasInclude matches any spelling that reaches hop's file: bare name, absolute or ~ path.
func hasInclude(config, hostsPath string) bool {
	base := filepath.Base(hostsPath)
	for _, raw := range strings.Split(config, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 || !strings.EqualFold(fields[0], "Include") {
			continue
		}
		for _, arg := range fields[1:] {
			if arg == hostsPath || filepath.Base(arg) == base {
				return true
			}
		}
	}
	return false
}
