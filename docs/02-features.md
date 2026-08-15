---
id: features
title: Features
group: Start
---

:::cards
### 🖥️ Embedded SSH shells
Real terminals in a pane: a pure-Go SSH client (`x/crypto/ssh`) feeding a real VT emulator (`x/vt`). Agent or private-key auth, resize, cursor, the lot.

### 🔑 2FA and passwords
A card appears the moment the host asks. Nothing is stored, and one prompt per host covers everything riding that connection.

### 🗂️ Multiple shells per host
[[S]] opens a second channel on the connection you already have — no new handshake, no second auth.

### 📁 SFTP file browser
[[f]] browses the remote filesystem over that same connection. Download with [[d]], open locally with [[o]].

### ⇄ Local & remote tunnels
Define forwards per host with [[T]], start and stop them with [[t]]. Imported from your SSH config, restored on reconnect.

### 🛰️ ProxyCommand & ProxyJump
Bastions and brokers (`aws ssm`, `cloudflared`, `gcloud`) work exactly as your SSH config describes them.

### ✎ Remote editor tabs
[[enter]] on a file runs `$EDITOR` *on the server* in a tab. No download — `:w` writes the real file.

### 📂 VS Code where you are
[[ctrl+o]] [[c]] opens VS Code Remote in the directory the shell is standing in, tracked over OSC 7.

### 🏠 A directory to land in
Give a host a default directory and every session on it starts there.

### ⇅ Scrollback
[[shift+↑]] pauses a shell into its history, with vim-ish paging.

### 🔁 Reconnect after a drop
Drops are noticed, the last screen is kept, and [[r]] puts the shells, browser and tunnels back.

### 🔐 Honest host keys
An unknown key aborts the dial and shows a fingerprint card. A mismatch is always a hard error.

### 📥 SSH config import
[[i]] syncs hosts from `~/.ssh/config` — a re-import refreshes, hand-added hosts are left alone.

### 🔎 Fuzzy find
[[/]] filters as you type, with the matched characters picked out so a surprising hit explains itself.

### 🖱️ Mouse
Wheel, click and drag-to-copy everywhere; remote programs that want the pointer get it verbatim.

### 📋 Copy and paste
Bracketed paste (even on Windows), drag to copy, and remote yanks (OSC 52) land on your clipboard.

### ⚙️ Live settings
[[,]] — editor, download dir, accent colour, vim keys, mouse, remote clipboard. Applied on the spot.

### 🎯 Frecency ordering
The hosts you actually use float to the top of the list.

### 🪟 Cross-platform
Static, dependency-free binaries for Windows, macOS and Linux (amd64 + arm64). No cgo, no libc, no runtime.

### ⌨️ Opt-in vim keys
Off by default. Flip one switch and the motions appear everywhere at once.
:::

:::details The longer version of a few of these
**2FA.** The dial waits inside the handshake rather than restarting — a one-time code is
only good once. One prompt per host, since shells, SFTP, editors and tunnels ride the same
connection.

**ProxyCommand.** The command runs *without a shell*: a line needing one is refused with a
clear error, so an imported config cannot smuggle in `sh -c`. `%h` `%p` `%r` `%n` are
expanded. A `ProxyJump` may name another hop host by alias and borrows its user, port and key.

**OSC 7.** hop installs the prompt hook into bash/zsh itself — one line typed at the first
prompt, then wiped from the pane, so the session looks untouched. Nothing is typed into a
shell that already emits OSC 7, or while a full-screen program owns the screen. Where no
directory can be learned, the key still opens the host in its default directory and says so.

**Reconnect.** Drops are found with keepalive probes rather than silence, so a suspended
laptop or a dead VPN does not leave a pane quietly frozen.
:::
