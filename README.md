<div align="center">
<p align="center">
  <img src="./assets/logo.png" alt="hop Logo" width="400">
</p>

<h1 align="center">hop</h1>

**Hop from server to server without ever leaving your terminal.**

One keypress and you're in a shell. One more and you're browsing its files. One more and
you're editing one *on the box*, in a tab, beside all the others. Then hop to the next
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

> [!NOTE]
> **hop is in early development.** Things may break, change, or behave in ways they
> shouldn't. If you hit a bug, have an idea, or something just feels off,
> [open an issue](https://github.com/p-arndt/hop/issues)! Feedback at this stage is
> genuinely the most useful thing you can contribute. 🙌

## 🎬 What it looks like

<p align="center">
  <img src="./assets/demo.gif" alt="hop: filter the fleet, open a shell, open a second one, browse the files, edit one on the server" width="900">
</p>

<sub>Keys are shown bottom-right as they're pressed. `●` connected · `◐` connecting ·
`○` idle · `×2` two shells open · `▤` SFTP browser open · `⇄2` two tunnels running</sub>

The header always tells you **where your keystrokes are going**. That's the single most
disorienting thing about a TUI that embeds other people's programs, so it gets permanent
screen space.

<table>
<tr>
<td width="50%" valign="top">
  <img src="./assets/screens/hosts.png" alt="The host list with the details card for the host under the cursor" width="100%"><br>
  <sub><b>The host list.</b> Status dot, group, and what <code>enter</code> would do to the host under the cursor.</sub>
</td>
<td width="50%" valign="top">
  <img src="./assets/screens/shell.png" alt="A live remote shell in a hop pane" width="100%"><br>
  <sub><b>A shell.</b> A real terminal in the pane; the footer shows the three ways back out.</sub>
</td>
</tr>
<tr>
<td width="50%" valign="top">
  <img src="./assets/screens/sftp.png" alt="The SFTP file browser" width="100%"><br>
  <sub><b>The SFTP browser.</b> <code>f</code>, over the connection that's already open.</sub>
</td>
<td width="50%" valign="top">
  <img src="./assets/screens/editor.png" alt="A file open in a remote editor tab inside hop" width="100%"><br>
  <sub><b>A remote editor tab.</b> <code>enter</code> on a file runs the editor <i>on the server</i>, so <code>:w</code> writes the real file.</sub>
</td>
</tr>
</table>

<details>
<summary><b>More stills</b>: two shells on one connection, settings, the keys card</summary>
<br>

<table>
<tr>
<td width="50%" valign="top">
  <img src="./assets/screens/shells.png" alt="Two shells on one host, shown as a tab strip" width="100%"><br>
  <sub><b>Two shells, one connection.</b> <code>S</code> (or <code>ctrl+o</code> <code>0</code>) opens another channel, no second handshake.</sub>
</td>
<td width="50%" valign="top">
  <img src="./assets/screens/settings.png" alt="The settings popover" width="100%"><br>
  <sub><b>Settings.</b> <code>,</code>. The accent is a swatch strip that recolours hop as you walk it.</sub>
</td>
</tr>
<tr>
<td width="50%" valign="top">
  <img src="./assets/screens/keys.png" alt="The keys card listing every binding" width="100%"><br>
  <sub><b>Every key hop binds.</b> <code>?</code>. It lists the keyboard you actually have, with vim motions included only if you turned them on.</sub>
</td>
<td width="50%"></td>
</tr>
</table>

</details>

<sub>Every host, file and command in the recording is invented. It runs against a
throwaway SSH server (`tools/demoserver`) with a HOME of its own, so `just demo`
re-records it on any machine without exposing anything. See
[demo/hop.tape](demo/hop.tape).</sub>

## ✨ Features

|  | |
| --- | --- |
| 🖥️ **Embedded SSH shells** | Real terminals in a pane: a pure-Go SSH client (`x/crypto/ssh`) feeding a real VT emulator (`x/vt`). Agent *or* private-key auth, resize, cursor, the lot. |
| 🔑 **2FA and passwords** | A host that wants a verification code (`pam_google_authenticator`) or a password gets a card, right when it asks. The dial waits in the handshake rather than restarting — a one-time code is only good once. Nothing is stored; one prompt per host, since shells, SFTP, editors and tunnels ride the same connection. |
| 🗂️ **Multiple shells per host** | `S` (or `ctrl+o` `0`) opens another shell on a host you're already on. It's a second *channel*, so no new handshake and no second auth. Tabs across the top, `shift+←/→` to switch. |
| 📁 **SFTP file browser** | `f` browses the remote filesystem over the connection you already have. Download with `d`, open locally with `o`. |
| ⇄ **Local & remote tunnels** | Define TCP forwards per host with `T`; `t` starts or stops the set over the connection hop already holds. The dashboard shows saved and live state, SSH-config forwards import with the host, and reconnect puts running tunnels back. |
| ✎ **Remote editor tabs** | `enter` on a file runs `$EDITOR` **on the server** on a second channel and renders it in a tab. No download, no copy, and `:w` writes the real file. |
| 📂 **VS Code where you are** | `ctrl+o` `ctrl+o` in a shell (or `o` in the list) opens VS Code Remote on the directory the shell is standing in, not the one you log in to. hop tracks it over OSC 7, installing the prompt hook into bash/zsh itself and wiping the line it typed, so the pane looks untouched. |
| 🏠 **A directory to land in** | Give a host a default directory in the add/edit card and every session on it starts there: the shell `cd`s in on the way past — on the same line that installs the OSC 7 hook, and erased along with it, so the pane looks like it logged in there — and `f` opens the file browser in it. A directory that has since been renamed away says so rather than failing quietly. |
| ⇅ **Scrollback** | `shift+↑` pauses a shell into its history with vim-ish paging. Declines politely when a full-screen program owns the screen. |
| 🔁 **Reconnect after a drop** | A dropped link (suspended laptop, dead VPN, rebooted box) is *noticed* — keepalive probes, not silence — and the pane keeps the last screen the host drew instead of quietly freezing. `r` dials again and puts the session's shape back: the same shell tabs, browser directory and running tunnels. |
| 🔐 **Honest host keys** | An unknown key **aborts the dial** and shows a fingerprint card. `y` trusts it and appends; `n` trusts nothing. A *mismatch* is always a hard error. |
| 📥 **SSH config import** | `i` (or `hop import`) upserts every host and its TCP `LocalForward` / `RemoteForward` entries from `~/.ssh/config`. It's a *sync*, not a one-shot: re-import refreshes, and hosts you added by hand are left alone. |
| 🔎 **Fuzzy find** | `/` filters as you type, with the matched characters picked out so a surprising hit explains itself. |
| 🖱️ **Mouse** | Wheel and click in the list, the browser and the panes: the wheel scrolls a shell's history, a double-click connects, a click on a tab switches to it. Every gesture is a key you already have, and a remote program that asked for the mouse (vim's `set mouse=a`, `htop`) gets the pointer verbatim. Selecting still works, because hop does it: drag across a pane and it highlights, let go and it is on your clipboard. `ctrl+g` lends the pointer to your terminal for the selections that span hop's own furniture; `,` → *Mouse* → off hands it back for good. |
| 📋 **Copy and paste** | Paste with your terminal's own paste — hop marks it as a paste (bracketed paste) so vim inserts it verbatim instead of indenting every line into a staircase. On Windows, where the console delivers a paste as synthesised keystrokes with no marker at all, hop recognises the burst and does it anyway. The other direction too: a drag over a pane copies what it covers, and a yank on the remote host (OSC 52) lands on your local clipboard, and `,` → *Remote clipboard* → off closes that door. |
| ⚙️ **Live settings** | `,` opens a popover for editor, download dir, accent colour (a swatch strip you *see*, not a number you look up), open-with, vim keys, mouse and remote clipboard. Applied on the spot. |
| 🎯 **Frecency ordering** | The hosts you actually use float to the top. |
| 🪟 **Cross-platform** | Static, dependency-free binaries for Windows, macOS and Linux (amd64 + arm64). No cgo, no libc, no runtime. |
| ⌨️ **Opt-in vim keys** | Off by default, because `h`/`l` meaning "out of" and "into" a host is a surprise to anyone who didn't ask for it. Flip one switch and the motions appear everywhere at once. |

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

Every release ships a `hop_<version>_checksums.txt`, so you can verify with `sha256sum -c`.

### Staying current

```bash
hop check-update   # is there a newer release?
hop self-update    # download it, verify its checksum, swap this binary
```

`self-update` fetches the archive for *your* platform from the latest GitHub
release, checks its SHA-256 against that release's `checksums.txt`, and replaces
the running binary atomically. On Windows the old `hop.exe` is renamed aside and
swept up the next time hop starts. Source builds (`version = dev`) are refused:
there's nothing to compare them against.

hop also checks once a day in the background and mentions a newer version in the
footer and on the CLI. `HOP_NO_UPDATE_CHECK=1` turns that off; the two commands
above still work.

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

On a first run with no hosts, hop offers to import `~/.ssh/config` for you: one
`enter` and your list is full. There's a CLI too, for when you're already in a shell:

```bash
hop import              # sync hosts from ~/.ssh/config
hop import path/to/cfg  # …or from somewhere else
hop add web1 deploy@10.0.0.4:2222
hop list                # alias  user@host:port
hop check-update        # is a newer release out?
hop self-update         # upgrade this binary in place
hop version
```

Then, in the TUI: `↑`/`↓` to move, `enter` to connect, `ctrl+o` to come back out.
That's the whole model.

### Where things live

| | Path |
| --- | --- |
| Host database | `<config dir>/hop/hop.db` (SQLite) |
| Settings | `<config dir>/hop/config.json` (plain JSON, hand-editable) |
| Update check cache | `<config dir>/hop/update-check.json` (last check + latest version seen) |
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
| `t` / `T` | start or stop all tunnels / manage their definitions |
| `o` | open in VS Code Remote, in the directory this host's shell is in |
| `d` | disconnect |
| `r` | reconnect a session whose connection dropped |
| `a` `e` `x` | add / edit / delete a host |
| `i` | import from an OpenSSH config |
| `/` | fuzzy filter |
| `,` `?` | settings / keys card |
| `ctrl+b` | hide / show the sidebar |
| `ctrl+g` | hand the mouse to your terminal (and take it back) |
| `q` `ctrl+c` | quit |

</details>

<details>
<summary><b>Inside a shell</b></summary>

| Key | Action |
| --- | --- |
| `ctrl+o` | the **leader** — opens hop's menu, does nothing on its own |
| `ctrl+o` `o` | out, back to hop |
| `esc` `esc` | back to hop (within 400 ms) |
| `shift+←` `shift+→` | switch shells |
| `ctrl+o` `1…9` | straight to that shell, without leaving the pane |
| `ctrl+o` `0` | another shell on this host, without leaving the pane |
| `ctrl+o` `c` | open **this directory** in VS Code Remote |
| `shift+↑` / `shift+pgup` | into scrollback |
| `ctrl+b` | hide the sidebar — the shell takes the whole window |
| `ctrl+g` | hand the mouse to your terminal (and take it back) |
| *everything else* | goes to the remote shell |

`ctrl+o` `ctrl+o` opens VS Code Remote on the directory the shell is standing in —
`cd` somewhere, `ctrl+o` out of the pane, `ctrl+o` again. (Only a chord because the
remote shell owns every plain key, and a control byte is the one thing every terminal
sends without configuration.) hop learns that directory over **OSC 7**, the escape
sequence terminals use for it, installing the prompt hook itself for **bash** and
**zsh**: one line typed at the first prompt and then wiped from the pane, so the
session looks untouched. Nothing is typed into a shell that already emits OSC 7, or
while a full-screen program owns the screen (vim, tmux, a `ForceCommand`), and the
erase is declined if anything but hop's own line is on the rows. Anywhere hop cannot
learn a directory the key still opens the host in its default directory, and says so.
See [KEYBINDINGS.md](KEYBINDINGS.md).

</details>

<details>
<summary><b>The file browser & editor tabs</b></summary>

| Key | Action |
| --- | --- |
| `enter` `→` | enter a directory, or open a file in a remote editor tab |
| `o` / `d` | open in the local desktop app / download |
| `←` `backspace` | up one directory |
| `r` | refresh |
| `ctrl+b` | hide / show the sidebar |
| `shift+←` `shift+→` | switch editor tabs |
| `ctrl+o` `o` | back to the browser |
| `:q` | close an editor tab |
| `ctrl+o` | back one level |

</details>

<details>
<summary><b>When a connection drops</b></summary>

| Key | Action |
| --- | --- |
| `r` `enter` | reconnect and reopen what was open |
| `d` `x` | drop the session — the host goes back to idle |
| `ctrl+o` `esc` `q` | back to the host list, pane left on screen |

The pane keeps the last screen the host drew, under a banner saying what happened.
Nothing is forwarded to the far end, because there is no far end.

</details>

📖 **[KEYBINDINGS.md](KEYBINDINGS.md)** is the full reference, including the vim
motions, the settings popover, and *why* each reserved chord is reserved.

## 🛠️ Development

```bash
just            # list recipes
just run list   # go run . list
just build      # dev binary
just test       # go test ./...
just test-e2e   # + the Docker 2FA end-to-end tests (needs Docker)
just vet
just fmt        # gofmt -w .
just ci         # fmt-check + vet + test (what CI runs)
just demo       # re-record assets/demo.gif + the stills (needs vhs)
```

The `justfile` is deliberately universal: recipe bodies are plain commands that run
under both `sh` and PowerShell, and the two that need real shell logic (`fmt-check`,
`clean`) are split with `[unix]` / `[windows]` attributes.

**The demo.** `just demo` records the GIF and the stills above. `scripts/demo.mjs`
builds hop, points `HOME` at a throwaway directory with a seeded host database, and
starts `tools/demoserver`, a loopback-only SSH server that invents everything on
screen: a fake shell with a table of canned command output, an in-memory filesystem
over SFTP, and a fake vi. The keypress overlay in the corner is hop's own, compiled
in only under `-tags hopdemo` (`internal/tui/keycast.go`), so a released binary
doesn't carry it.

**Testing.** Headless tests drive the real Bubble Tea model with real keystrokes
against in-process Go SSH/SFTP servers and temp-file stores. See
`internal/tui/hostmgmt_test.go`, `TestEmbeddedRoundTrip`, `TestSFTPRoundTrip`.

**The 2FA end-to-end tests.** An in-process Go SSH server answers whatever you
tell it to, which is no way to find out whether hop can log into a box with
two-factor authentication. So `internal/dockerenv` brings up an Ubuntu container
running the real `openssh-server` and the real `libpam-google-authenticator`,
configured the way the guides say, listening four times: the code alone, the
hardened `publickey,keyboard-interactive`, password-then-code, and both methods
offered as alternatives. The tests compute TOTP codes the way a phone does and
log in — `internal/sshx` through the SSH engine, `internal/tui` by typing into
the actual card. Negative controls (a wrong code, a ten-minute-old code) are part
of the suite, because a container that accepted anything would make every other
test here pass while proving nothing. Opt in with `just test-e2e`; without
`HOP_DOCKER_E2E=1` they skip, so CI and a laptop without Docker are unaffected.
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
Linux runner, with checksums and a git-cliff changelog. Windows gets a `.zip`,
everything else a `.tar.gz` so the exec bit survives.

## 🗺️ Roadmap

Next up:

- 💓 **Health panel**: per-host reachability, latency, uptime and disk
- ⬆️ **Uploads & file ops** in the browser (`u`, `x`, `R`, `m`) with async transfer progress
- 🏷️ **Groups & tags** in the list: section by group, filter by tag, pin favourites
- 📐 Narrow-terminal layouts and cursor-style fidelity

The living version, with far more detail on each item, is [TODO.md](TODO.md).

## 🙏 Built with

[Bubble Tea](https://github.com/charmbracelet/bubbletea) · [Lip Gloss](https://github.com/charmbracelet/lipgloss) ·
[x/vt](https://github.com/charmbracelet/x) · [x/crypto/ssh](https://pkg.go.dev/golang.org/x/crypto/ssh) ·
[pkg/sftp](https://github.com/pkg/sftp) · [modernc.org/sqlite](https://gitlab.com/cznic/sqlite) ·
[sahilm/fuzzy](https://github.com/sahilm/fuzzy) · [skeema/knownhosts](https://github.com/skeema/knownhosts)

<div align="center">
<sub>Made for people with too many servers and not enough terminals.</sub>
</div>
