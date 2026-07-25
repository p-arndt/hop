// Command hop is a Windows-first TUI SSH/server connection manager with
// embedded terminal panes. Run with no arguments to launch the TUI; the
// subcommands below manage the host store from the command line.
package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"hop/internal/buildinfo"
	"hop/internal/store"
	"hop/internal/tui"
	"hop/internal/update"
)

func main() {
	args := os.Args[1:]

	// A previous self-update on Windows leaves the old binary beside the new one
	// (a running .exe can be renamed but not deleted). Sweep it up now that it is
	// no longer running.
	update.CleanupLeftovers()

	// Handle commands that don't need the store first.
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

	// A one-line hint on stderr — never stdout, so `hop list` stays pipeable.
	// It reports the previous check and refreshes the cache for the next run;
	// the TUI shows the same thing in its footer.
	update.NotifyIfAvailable(os.Stderr, buildinfo.Version)
}

// updateTimeout bounds the whole check-download-verify-install cycle. Generous
// compared to the passive notice, since the user explicitly asked for it.
const updateTimeout = 60 * time.Second

// cmdUpdate backs `hop self-update` and, with checkOnly, `hop check-update`:
// the first replaces the running binary with the latest release once its
// checksum verifies, the second only reports whether a newer one exists.
func cmdUpdate(checkOnly bool) {
	current := buildinfo.Version
	client := update.NewClient(&http.Client{Timeout: updateTimeout})
	ctx, cancel := context.WithTimeout(context.Background(), updateTimeout)
	defer cancel()

	if !checkOnly {
		fmt.Printf("Current version: %s. Checking for updates…\n", current)
	}
	res, err := client.SelfUpdate(ctx, current, checkOnly)
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

// cmdImport imports hosts from an OpenSSH config file. The path defaults to
// ~/.ssh/config but may be overridden by the first argument.
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
