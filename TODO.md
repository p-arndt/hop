# hop — TODO

A Windows-first TUI SSH/server manager with embedded terminal panes.
Stack: Go, Bubble Tea/Lip Gloss, pure-Go SSH (`x/crypto/ssh`), `x/vt` emulator, SQLite (`modernc`), `pkg/sftp`.

See also: [KEYBINDINGS.md](KEYBINDINGS.md). Rationale for how something works lives in
the package it works in — this file tracks *what*, not *why*.

---

## 🚀 Core / phases

- [x] **Phase 1 — MVP:** SQLite host store, `~/.ssh/config` import, embedded SSH terminal panes, multi-session, VS Code Remote open, fuzzy search.
- [x] **Phase 2 — SFTP browser:** `internal/sftpx` over the same SSH conn + `internal/filebrowser` TUI.
- [x] **Working directory → VS Code:** shell panes track the remote cwd over OSC 7 (`internal/terminal/cwd.go`); `ctrl+o` `ctrl+o` in a pane, or `o` in the host list, opens VS Code Remote there. E2E against bash/zsh/fish in Docker.
- [x] **Phase 3 — Tunnels / port-forwarding:** `T` opens the per-host manager; local/remote TCP forwards in a `forwards` table, imported from SSH-config `LocalForward`/`RemoteForward`. `sshx.Tunnel` owns listener and copies; reconnect restores the running set.
- [ ] **Phase 4 — Status/health panel:** per-host reachability + latency, `uptime`/disk via a background `session.Run`, shown like VS Code's connection status.

## 🪟 Panes & tabs

- [x] **Shell tabs:** `S` opens another shell on a connected host (second channel, no re-handshake), shown as a tab strip.
- [x] **Editor tabs:** `enter` on a file opens `${EDITOR:-vi}` on a remote pty in a hop pane. No download — `:w` writes the real remote file.
- [x] **Vim motions + back/forward keys** in the browser; the host list keeps `hjkl` + page keys. Panes reserve `ctrl+o` and a 400 ms double-`esc`.

## 📁 SFTP browser

- [x] Preview without download — done better: `enter` opens the file in a remote editor tab.
- [x] Browser look verified end-to-end and screenshotted (`assets/screens/sftp.png`).
- [ ] **Upload** (`u`): needs a text-input for the local path (or a local file picker).
- [ ] In-browser file ops: delete (`x`), rename (`R`), mkdir (`m`) — `sftpx` supports these, no UI/keys yet.
- [ ] Transfer progress + async transfers (currently synchronous → large files stall the UI).
- [ ] Sort toggle (name/size/mtime) and show mtime column.
- [ ] Confirm before overwriting an existing local download.

## 🖥️ Host management

- [x] **Add/edit host in the TUI** (`a`/`e`, `internal/tui/hostform.go`); alias rename preserves visit/frecency history.
- [x] **Delete host** (`x`) behind a confirmation modal.
- [x] **Pin host** (`p`) into a PINNED section above the frecency order; `shift+k`/`shift+j` reorder inside it.
- [x] **Per-host default directory:** a "Default dir" field saying where a session on this host starts — applied to shells (via `terminal.startupLine`) and to the SFTP browser. E2E in Docker (`TestStartDirE2E*`).
- [x] **Re-import / sync `~/.ssh/config`** from the TUI (`i`); opens automatically on a first run with no hosts.
- [ ] **Groups/tags UI:** the form sets `Group`/`Tags`, but the list doesn't section by group or filter by tag yet.
- [ ] Local file picker for the identity-file field (currently free-text; needs a local-fs adapter — `filebrowser` is remote-oriented).

## 🔐 Connection & auth

- [x] **known_hosts:** an unknown key aborts the dial and raises a modal fingerprint card; `y` trusts and retries. A known-host mismatch stays a hard error.
- [x] **Private-key auth** (`internal/sshx/keys.go`): `IdentityFile` or the default `~/.ssh/id_*`, merged with the agent's signers into a single publickey method. Agent no longer required.
- [x] **Interactive auth (2FA/OTP, passwords):** `sshx.Prompter` asks the UI from inside the handshake, so a retry does not spend a fresh TOTP code. `esc` cancels stickily.
- [x] **End-to-end 2FA tests against a real server:** `internal/dockerenv` runs openssh-server + `libpam-google-authenticator` in four auth shapes. Opt-in via `HOP_DOCKER_E2E=1` (`just test-e2e`).
- [x] **Reconnect handling:** keepalives detect a blackholed link, the dead session stays on screen with its last output, and `r` restores the shape of it (shell tabs, browser directory). Editor tabs are deliberately not reopened.
- [x] **ProxyCommand / ProxyJump** (issue #13): a host reachable only through a broker (AWS SSM, cloudflared, IAP) or a bastion. `internal/sshx/proxy.go` runs the proxy program as a `net.Conn` and stacks the jump over a first client; both imported from SSH-config and editable in the host form. ProxyCommand is split into argv without a shell — a line needing one is refused, not run.
- [ ] Deferred (not needed for current setup): PuTTY session import.

## ⌨️ Terminal / UX polish

- [x] Cursor visible; typing lag removed (event-driven redraw); visual pass (keycap pills, status dots, accent bar, badges).
- [x] **Settings popover** (`,`) over `internal/config`, stored as JSON under `<UserConfigDir>/hop/`, applied live on save.
- [x] **Scrollback UI:** `shift+↑` / `shift+pgup` pauses a focused shell into its history with vim + page motions; `esc`/`q`/`ctrl+o` return to live.
- [x] **Collapsible sidebar** (`ctrl+b`). Session-only, no setting. Costs: a remote tmux never sees its `ctrl+b` prefix, and `ctrl+b` is no longer a page-up motion.
- [x] **Host list keymap trimmed to the step keys** via `keymap.Scope`; the browser keeps the full motion table.
- [x] **Mouse support (scroll/click)** routed by region (`internal/tui/mouse.go`); every gesture is an existing binding reached by pointing. A remote program that asks for the mouse gets the pointer verbatim (`internal/terminal/mouse.go`). Cards are keyboard-only; `,` → Mouse → off gives the pointer back.
- [x] **Mouse text selection** (`internal/terminal/selection.go` + `internal/tui/selection.go`) over shells, scrollback and editor panes, landing on the clipboard at release. `ctrl+g` hands mouse reporting back to the terminal live.
- [x] **Drag autoscroll:** a selection drag held against a pane's top or bottom row scrolls the view under the pointer (into scrollback and back), so a selection is not limited to one screenful; a drag that wanders off the pane keeps its events.
- [x] **Copy/paste into the remote shell:** no key of its own — paste the way your terminal pastes and hop marks it as a paste (bracketed when the far end asked). Windows has no marked paste, so the burst is recognised by shape (`internal/tui/paste.go`). Copy is OSC 52 → `internal/clipboard`; a remote asking to *read* the clipboard is never answered. Setting: `,` → Remote clipboard.
- [ ] Cursor: respect hidden state and cursor style (block/bar/underline); optional blink.
- [ ] Narrow-terminal handling: header/footer truncation and min-size behavior.
- [x] **Mode flags are one enum:** `focused`/`browsing`/`editing`/`scrolling` are gone; `model.mode` is a single `paneMode` (`modeList`/`modeShell`/`modeScrollback`/`modeBrowser`/`modeEditor`), so the invalid combinations are unrepresentable and a call site can no longer forget to clear the other three. The old names live on as predicates derived from it (`m.focused()`, `m.scrolling()`, …), plus `m.inPane()`. Scrollback is its own mode rather than a flag on the shell.
- [x] **Status bar + a footer that fits:** a third chrome row (`internal/tui/status.go`) says where you are — host › mode › the directory, file or listing — with `user@host:port` and the tab chips at its right; the header kept only the title and session count. The footer (`footerHints`, `internal/tui/view.go`) is a per-mode core of 3-4 keys that fills a wider window from an `extra` list, dropping whole hints rather than cutting words. `?` opens the key card from every mode hop owns the keyboard in, `ctrl+o ?` where a bare `?` is the remote's, and the card opens on your mode's section (`helpFor`, `internal/tui/help.go`).
- [x] **`esc` `esc` quits from the host list** — one level out, the list being the last level. A single `esc` still only drops the selected host.

## ⚙️ Config & distribution

- [x] **Demo recording:** `just demo` records `assets/demo.gif` + screenshots from `demo/hop.tape` via VHS, against `tools/demoserver` (a loopback-only fake host, so nothing real is in the recording). Keypress overlay is behind `-tags hopdemo`, absent from releases.
- [x] **README:** hero, feature table, install, quick start, file locations, key reference, architecture map, roadmap.
- [x] **Docs are generated from one source:** `docs/*.md` renders to `index.html` (the website), `README.md` and
      `KEYBINDINGS.md` via `tools/docsgen` (`just docs`). The website has an offline search (ctrl+k) built from the
      rendered sections; `go test ./...` fails when a generated file has drifted.
- [x] **The site is published:** GitHub Pages serves `index.html` from `main` / root at <https://p-arndt.github.io/hop/>, linked from the README and KEYBINDINGS.
- [x] **Self-update:** `hop self-update` / `hop check-update` (`internal/update`) — GitHub-hosts-only download, SHA-256 verified against `checksums.txt`, atomic swap. Passive check once a day; `HOP_NO_UPDATE_CHECK=1` disables it.
- [x] **Cross-platform build (macOS/Linux):** agent transport behind a build-tagged `dialAgent`.
- [x] **Release/CI for all platforms:** windows/ubuntu/macos test matrix, then six cross-compiled targets from one Linux runner.
- [x] **Universal `justfile`:** recipe bodies run under both `sh` and PowerShell. Needs just >= 1.39.
- [ ] Config file: keybindings and default user (accent/download dir/editor/open-with are done).
- [ ] Put `hop` on PATH; ship a build/install script.
- [ ] Cross-platform follow-ups: `action.NewTab` is Windows-only and unused; `filebrowser`'s executable-open guard is Windows-shaped; `ci.yml` runs vet + test but not `just ci`, so `fmt-check` is never enforced.

## 🧪 Testing

- [x] `tui` host management (`hostmgmt_test.go`): add/edit/delete via real keystrokes against a temp-file store.
- [x] `tui` navigation-mode keys (`keys_test.go`).
- [x] `filebrowser` navigation (`filebrowser_test.go`, via a fake `filebrowser.Client`).
- [x] `store` upsert/touch/delete/add/rename (`store_test.go`).
- [x] Round-trip: `TestEmbeddedRoundTrip` (terminal), `TestSFTPRoundTrip` (sftp).
- [ ] `store` import parsing + frecency ordering (`store.OpenAt` takes a path, so point a test at a temp db).
- [ ] `action` package.
- [ ] `keyToBytes` mapping table test in `terminal`.
- [ ] `filebrowser` rendering.
- [x] Mode *switches* (navigation ↔ terminal ↔ browsing ↔ editing ↔ scrollback), `mode_test.go`: every transition asserted through the real helpers, against a shell pane with genuine scrollback.

## 📝 Known limitations / notes

- Auto-tracked "recent directories" in the sidebar was built then removed — the sidebar is a host list; dirs shouldn't appear implicitly. **Don't reintroduce implicit tracking.**
- `x/vt` is an untagged dep (pinned to a pseudo-version) — watch for breaking changes on update.
- SFTP ops are synchronous (acceptable for MVP; see async item above).
- `alt+…` chords need "Option as Meta" on macOS terminals, so they are only ever *aliases* — every hop binding must be reachable with `ctrl`/`shift`. See KEYBINDINGS.md.
