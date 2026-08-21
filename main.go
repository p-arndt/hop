// Command hop is a Windows-first TUI SSH connection manager with embedded terminal panes.
package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"hop/internal/buildinfo"
	"hop/internal/sshx"
	"hop/internal/store"
	"hop/internal/tui"
	"hop/internal/update"
)

func main() {
	args := os.Args[1:]

	// A running .exe can be renamed but not deleted, so a Windows self-update leaves the old one behind.
	update.CleanupLeftovers()

	if len(args) > 0 {
		switch args[0] {
		case "version", "--version", "-v":
			fmt.Println("hop", buildinfo.String())
			return
		case "self-update":
			cmdUpdate(false)
			return
		case "check-update":
			cmdUpdate(true)
			return
		}
	}

	st, err := store.Open()
	if err != nil {
		fmt.Fprintln(os.Stderr, "hop: open store:", err)
		os.Exit(1)
	}
	defer st.Close()

	// A ProxyJump target may name a store alias, not just a bare hostname.
	sshx.SetJumpResolver(func(name string) (store.Host, bool) {
		h, ok, err := st.HostByAlias(name)
		if err != nil || !ok {
			return store.Host{}, false
		}
		return h, true
	})

	if len(args) == 0 {
		if err := tui.Run(st); err != nil {
			fmt.Fprintln(os.Stderr, "hop:", err)
			os.Exit(1)
		}
		return
	}

	switch args[0] {
	case "import":
		cmdImport(st, args[1:])
	case "add":
		cmdAdd(st, args[1:])
	case "list", "hosts":
		cmdList(st)
	default:
		usage()
	}

	// stderr, never stdout, so `hop list` stays pipeable.
	update.NotifyIfAvailable(os.Stderr, buildinfo.Version)
}

// cmdUpdate backs `hop self-update` and, with checkOnly, `hop check-update`.
func cmdUpdate(checkOnly bool) {
	current := buildinfo.Version
	ctx, cancel := context.WithTimeout(context.Background(), update.UpdateTimeout)
	defer cancel()

	if !checkOnly {
		fmt.Printf("Current version: %s. Checking for updates…\n", current)
	}
	res, err := update.SelfUpdate(ctx, current, checkOnly)
	if err != nil {
		fmt.Fprintln(os.Stderr, "hop:", err)
		os.Exit(1)
	}

	switch {
	case !update.IsNewer(res.Latest, res.Current):
		fmt.Printf("You're on the latest version (%s).\n", res.Current)
	case checkOnly:
		fmt.Printf("A newer version is available: %s (you have %s). Run `hop self-update` to upgrade.\n", res.Latest, res.Current)
	default:
		fmt.Printf("Updated hop %s → %s.\n", res.Current, res.Latest)
	}
}

// cmdImport imports hosts from an OpenSSH config file, ~/.ssh/config by default.
func cmdImport(st *store.Store, args []string) {
	var path string
	if len(args) > 0 && args[0] != "" {
		path = args[0]
	} else {
		home, err := os.UserHomeDir()
		if err != nil {
			fmt.Fprintln(os.Stderr, "hop: locate home dir:", err)
			os.Exit(1)
		}
		path = filepath.Join(home, ".ssh", "config")
	}

	n, err := st.ImportSSHConfig(path)
	if err != nil {
		fmt.Fprintln(os.Stderr, "hop: import:", err)
		os.Exit(1)
	}
	fmt.Printf("imported %d hosts\n", n)
}

// cmdAdd adds (or updates) a host: hop add <alias> <user@host[:port]> [port].
func cmdAdd(st *store.Store, args []string) {
	if len(args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: hop add <alias> <user@host[:port]> [port]")
		os.Exit(1)
	}

	alias := args[0]
	target := args[1]

	user := ""
	hostPort := target
	if at := strings.LastIndex(target, "@"); at >= 0 {
		user = target[:at]
		hostPort = target[at+1:]
	}

	hostName := hostPort
	port := 0
	if colon := strings.LastIndex(hostPort, ":"); colon >= 0 {
		hostName = hostPort[:colon]
		if p, err := strconv.Atoi(hostPort[colon+1:]); err == nil {
			port = p
		}
	}

	// An explicit trailing port argument wins over any :port in the target.
	if len(args) >= 3 {
		if p, err := strconv.Atoi(args[2]); err == nil {
			port = p
		}
	}
	if port == 0 {
		port = 22
	}

	if _, err := st.Upsert(store.Host{
		Alias:    alias,
		HostName: hostName,
		User:     user,
		Port:     port,
	}); err != nil {
		fmt.Fprintln(os.Stderr, "hop: add:", err)
		os.Exit(1)
	}

	who := hostName
	if user != "" {
		who = user + "@" + hostName
	}
	fmt.Printf("added %s -> %s:%d\n", alias, who, port)
}

// cmdList prints every stored host as "alias\tuser@hostname:port".
func cmdList(st *store.Store) {
	hosts, err := st.Hosts()
	if err != nil {
		fmt.Fprintln(os.Stderr, "hop: list:", err)
		os.Exit(1)
	}
	for _, h := range hosts {
		port := h.Port
		if port == 0 {
			port = 22
		}
		who := h.HostName
		if h.User != "" {
			who = h.User + "@" + h.HostName
		}
		fmt.Printf("%s\t%s:%d\n", h.Alias, who, port)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, `hop — SSH/server connection manager with embedded terminal panes

usage:
  hop                          launch the interactive TUI
  hop import [path]            import hosts from an OpenSSH config (default ~/.ssh/config)
  hop add <alias> <user@host[:port]> [port]
                               add or update a host
  hop list                     list stored hosts (alias, user@host:port)
  hop hosts                    alias for list
  hop check-update             report whether a newer release is available
  hop self-update              replace this binary with the latest release
  hop version                  print the version and exit

Set HOP_NO_UPDATE_CHECK=1 to silence the passive "newer version" notice.`)
}
