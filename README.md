<div align="center">
<p align="center">
  <img src="./assets/logo.png" alt="hop Logo" width="400">
</p>

<h1 align="center">hop</h1>

**Hop from server to server without ever leaving your terminal.**

One keypress and you're in a shell. One more and you're browsing its files. One more and
you're editing one *on the box* — in a tab, beside all the others. Then hop to the next
host and everything you left behind is still exactly where you left it.

*No new windows. No re-authenticating. No hunting for that one shell you had open.*

[![CI](https://github.com/p-arndt/hop/actions/workflows/ci.yml/badge.svg)](https://github.com/p-arndt/hop/actions/workflows/ci.yml)
[![Release](https://github.com/p-arndt/hop/actions/workflows/release.yml/badge.svg)](https://github.com/p-arndt/hop/actions/workflows/release.yml)
[![Go](https://img.shields.io/badge/go-1.26-00ADD8?logo=go&logoColor=white)](go.mod)
[![Platforms](https://img.shields.io/badge/platforms-windows%20%7C%20macOS%20%7C%20linux-informational)](#-install)
[![Built with Bubble Tea](https://img.shields.io/badge/built%20with-Bubble%20Tea-ff69b4)](https://github.com/charmbracelet/bubbletea)

[Install](#-install) · [Quick start](#-quick-start) · [Keys](#-keys) · [Development](#-development) · [Roadmap](#-roadmap)

</div>

---

## 🎬 What it looks like

```text
 hop  ssh manager › prod-web-1                      ● prod-web-1  shell 2/2   2 sessions   ✓ connected

╭─────────────────────────╮╭──────────────────────────────────────────────────────────────────────────╮
│ HOSTS  6                ││ shell 1 │ shell 2                                                        │
│                         ││                                                                          │
│   ● prod-web-1  ×2 ▤    ││ deploy@prod-web-1:~$ systemctl status caddy                              │
│ ▌ ● prod-db     [infra] ││ ● caddy.service - Caddy web server                                       │
│   ◐ staging             ││      Loaded: loaded (/lib/systemd/system/caddy.service; enabled)         │
│   ○ build-box   #ci     ││      Active: active (running) since Fri 2026-07-24 09:12:03 UTC          │
│   ○ nas         [home]  ││    Main PID: 812 (caddy)                                                 │
│   ○ router              ││                                                                          │
│                         ││ deploy@prod-web-1:~$ █                                                   │
╰─────────────────────────╯╰──────────────────────────────────────────────────────────────────────────╯
 ↑↓ move   ⏎ connect   S shell   f files   o code   / filter   a add   e edit   , settings   ? keys
```

`●` connected · `◐` connecting · `○` idle · `×2` two shells open · `▤` SFTP browser open

The header always tells you **where your keystrokes are going** — the single most
disorienting thing about a TUI that embeds other people's programs.

## ✨ Features

|  | |
| --- | --- |
| 🖥️ **Embedded SSH shells** | Real terminals in a pane — a pure-Go SSH client (`x/crypto/ssh`) feeding a real VT emulator (`x/vt`). Agent *or* private-key auth, resize, cursor, the lot. |
| 🗂️ **Multiple shells per host** | `S` (or `alt+0`) opens another shell on a host you're already on — a second *channel*, no new handshake, no second auth. Tabs across the top, `alt+1…9` to jump. |
| 📁 **SFTP file browser** | `f` browses the remote filesystem over the connection you already have. Download with `d`, open locally with `o`. |
| ✎ **Remote editor tabs** | `enter` on a file runs `$EDITOR` **on the server** on a second channel and renders it in a tab. No download, no copy — `:w` writes the real file. |
| ⇅ **Scrollback** | `shift+↑` pauses a shell into its history with vim-ish paging. Declines politely when a full-screen program owns the screen. |
| 🔐 **Honest host keys** | An unknown key **aborts the dial** and shows a fingerprint card. `y` trusts it and appends; `n` trusts nothing. A *mismatch* is always a hard error. |
| 📥 **SSH config import** | `i` (or `hop import`) upserts every host from `~/.ssh/config`. It's a *sync*, not a one-shot: re-import refreshes, and hosts you added by hand are left alone. |
| 🔎 **Fuzzy find** | `/` filters as you type, with the matched characters picked out so a surprising hit explains itself. |
| ⚙️ **Live settings** | `,` opens a popover — editor, download dir, accent colour (a swatch strip you *see*, not a number you look up), open-with, vim keys. Applied on the spot. |
| 🎯 **Frecency ordering** | The hosts you actually use float to the top. |
| 🪟 **Cross-platform** | Static, dependency-free binaries for Windows, macOS and Linux (amd64 + arm64). No cgo, no libc, no runtime. |
| ⌨️ **Opt-in vim keys** | Off by default — because `h`/`l` meaning "out of" and "into" a host is a surprise to anyone who didn't ask for it. Flip one switch and the motions appear everywhere at once. |

## 📦 Install

### Download a binary

Grab the archive for your platform from the [latest release](https://github.com/p-arndt/hop/releases/latest)
and put `hop` somewhere on your `PATH`.

```bash
# macOS / Linux
tar -xzf hop_*_darwin_arm64.tar.gz
sudo mv hop /usr/local/bin/
```

```powershell
# Windows
Expand-Archive hop_*_windows_amd64.zip -DestinationPath .
# then move hop.exe onto your PATH
```

Every release ships a `hop_<version>_checksums.txt` — verify with `sha256sum -c`.

### From source

Needs [Go 1.26+](https://go.dev/dl/) (and optionally [`just`](https://github.com/casey/just) ≥ 1.39).

```bash
git clone https://github.com/p-arndt/hop.git && cd hop
just build          # -> ./hop   (or: go build -o hop .)
just build-release  # stripped + version-stamped
```

## 🚀 Quick start

```bash
hop            # launch the TUI
```

On a first run with no hosts, hop offers to import `~/.ssh/config` for you — one
`enter` and your list is full. There's a CLI too, for when you're already in a shell:

```bash
hop import              # sync hosts from ~/.ssh/config
hop import path/to/cfg  # …or from somewhere else
hop add web1 deploy@10.0.0.4:2222
hop list                # alias  user@host:port
hop version
```

Then, in the TUI: `↑`/`↓` to move, `enter` to connect, `ctrl+o` to come back out.
That's the whole model.

### Where things live

| | Path |
| --- | --- |
| Host database | `<config dir>/hop/hop.db` — SQLite |
| Settings | `<config dir>/hop/config.json` — plain JSON, hand-editable |
| Known hosts | your usual `~/.ssh/known_hosts` |

`<config dir>` is `%AppData%\hop\` on Windows, `~/Library/Application Support/hop/`
on macOS, `~/.config/hop/` on Linux. A missing or malformed config file starts hop
on defaults rather than refusing to start.

## ⌨️ Keys

hop has three modes, and the footer always shows the keys for the one you're in.
Every mode returns to the host list with **`ctrl+o`**.

| Mode | You're here when | Who owns your keystrokes |
| --- | --- | --- |
| **Navigation** | the host list is focused (the default) | hop |
| **Browsing** | you opened the SFTP browser with `f` | hop |
| **Terminal** | you connected with `enter` or `s` | the **remote shell** |

<details>
<summary><b>The host list</b></summary>

| Key | Action |
| --- | --- |
| `↑` `↓` / `pgup` `pgdn` | move / page |
| `enter` `→` | connect, or focus the shell already open |
| `s` / `S` | focus this host's session / open **another** shell on it |
| `f` | SFTP browser |
| `o` | open in VS Code Remote |
| `d` | disconnect |
| `a` `e` `x` | add / edit / delete a host |
| `i` | import from an OpenSSH config |
| `/` | fuzzy filter |
| `,` `?` | settings / keys card |
| `q` `ctrl+c` | quit |

</details>

<details>
<summary><b>Inside a shell</b></summary>

| Key | Action |
| --- | --- |
| `ctrl+o` | back to hop |
| `esc` `esc` | back to hop (within 400 ms) |
| `←` | back to hop — **only at a bare prompt**, where the shell has no use for the key |
| `alt+0` | another shell on this host |
| `alt+←` `alt+→` / `alt+1…9` | switch shells |
| `shift+↑` / `shift+pgup` | into scrollback |
| *everything else* | goes to the remote shell |

</details>

<details>
<summary><b>The file browser & editor tabs</b></summary>

| Key | Action |
| --- | --- |
| `enter` `→` | enter a directory, or open a file in a remote editor tab |
| `o` / `d` | open in the local desktop app / download |
| `←` `backspace` | up one directory |
| `r` | refresh |
| `alt+←` `alt+→` / `alt+1…9` | switch editor tabs |
| `:q` | close an editor tab |
| `ctrl+o` | back one level |

</details>

📖 **[KEYBINDINGS.md](KEYBINDINGS.md)** is the full reference — including the vim
motions, the settings popover, and *why* each reserved chord is reserved.

## 🛠️ Development

```bash
just            # list recipes
just run list   # go run . list
just build      # dev binary
just test       # go test ./...
just vet
just fmt        # gofmt -w .
just ci         # fmt-check + vet + test — what CI should run
```

The `justfile` is deliberately universal: recipe bodies are plain commands that run
under both `sh` and PowerShell, and the two that need real shell logic (`fmt-check`,
`clean`) are split with `[unix]` / `[windows]` attributes.

**Testing.** Headless tests drive the real Bubble Tea model with real keystrokes
against in-process Go SSH/SFTP servers and temp-file stores — see
`internal/tui/hostmgmt_test.go`, `TestEmbeddedRoundTrip`, `TestSFTPRoundTrip`.
CI runs vet + test + build on a Windows / Linux / macOS matrix, because the agent
transport and the local-open handler are per-platform: a single-OS run can't tell
whether the others still compile.

**Releasing.**

```bash
just release          # patch bump: stamps VERSION, commits, tags, pushes
just release minor    # or major, or an explicit 1.0.0
```

The tag push triggers the release workflow: it gates on the three-OS test matrix,
then cross-compiles all six targets (windows/linux/darwin × amd64/arm64) from one
Linux runner — `.zip` for Windows, `.tar.gz` elsewhere so the exec bit survives —
with checksums and a git-cliff changelog.

## 🗺️ Roadmap

Done: embedded shells · multi-shell tabs · SFTP browser · remote editor tabs ·
scrollback · host management · host-key confirmation · SSH config import ·
live settings · cross-platform releases.

Next up:

- 🔌 **Tunnels / port forwarding** — local and remote forwards per host, on the connection hop already holds
- 💓 **Health panel** — per-host reachability, latency, uptime and disk
- ⬆️ **Uploads & file ops** in the browser (`u`, `x`, `R`, `m`) with async transfer progress
- 🏷️ **Groups & tags** in the list — section by group, filter by tag, pin favourites
- 🔁 **Reconnect handling** — detect a dropped session and offer to redial
- 🖱️ **Mouse support**, narrow-terminal layouts, copy/paste into panes

The living version, with far more detail on each item, is [TODO.md](TODO.md).

## 🙏 Built with

[Bubble Tea](https://github.com/charmbracelet/bubbletea) · [Lip Gloss](https://github.com/charmbracelet/lipgloss) ·
[x/vt](https://github.com/charmbracelet/x) · [x/crypto/ssh](https://pkg.go.dev/golang.org/x/crypto/ssh) ·
[pkg/sftp](https://github.com/pkg/sftp) · [modernc.org/sqlite](https://gitlab.com/cznic/sqlite) ·
[sahilm/fuzzy](https://github.com/sahilm/fuzzy) · [skeema/knownhosts](https://github.com/skeema/knownhosts)

<div align="center">
<sub>Made for people with too many servers and not enough terminals.</sub>
</div>
