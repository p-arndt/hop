# hop — TODO

A Windows-first TUI SSH/server manager with embedded terminal panes.
Stack: Go, Bubble Tea/Lip Gloss, pure-Go SSH (`x/crypto/ssh`), `x/vt` emulator, SQLite (`modernc`), `pkg/sftp`.

See also: [KEYBINDINGS.md](KEYBINDINGS.md).

---

## 🚀 Core / phases

- [x] **Phase 1 — MVP:** SQLite host store, `~/.ssh/config` import, embedded SSH terminal panes (agent auth, VT emulator, keystrokes, resize), multi-session with `ctrl+o` to leave a pane, VS Code Remote open (`o`), fuzzy search (`/`).
- [x] **Phase 2 — SFTP browser:** `internal/sftpx` (List/Download/Upload/Mkdir/Remove/Rename over the same SSH conn) + `internal/filebrowser` TUI. `f` opens it; navigate, `o` opens locally, `d` downloads → `~/Downloads`, `h`/`backspace` up, `r` refresh.
- [ ] **Phase 3 — Tunnels / port-forwarding:** define local/remote forwards per host, start/stop with a key, show status in the dashboard. (`ssh.Client` supports `Dial`/`Listen`; reuse the existing connection.)
- [ ] **Phase 4 — Status/health panel:** per-host reachability + latency, `uptime`/disk via a background `session.Run`, shown like VS Code's connection status.

## 🪟 Panes & tabs

- [x] **Shell tabs:** `S` in the host list — or `alt+0` from a focused pane — opens another shell on an already-connected host (second channel, no re-handshake), shown as a tab strip. `alt+←/→` cycle, `alt+1..9` jump. `exit` closes a tab; the last one ends the session unless a browser/editor still holds the connection.
- [x] **Editor tabs:** `enter` on a file opens it in a *remote* editor (`${EDITOR:-vi}` over a second SSH channel on a pty), rendered in a hop pane with a tab strip. `alt+←/→` cycle, `alt+1..9` jump, `:q` closes a tab, `ctrl+o` back to the browser. No download — `:w` writes the real remote file. `sshx.Command` (pty + exec) is the primitive.
- [x] **Vim motions + back/forward keys:** `gg`/`G`, `H`/`M`/`L`, `ctrl+d`/`ctrl+u`, `ctrl+f`/`ctrl+b` in host list and browser. `enter`/`l`/`right` descends, `h`/`left` backs out; `left` at the browser top pops back to hop. Panes reserve `ctrl+o`, a 400 ms double-`esc`, and `left` *at a bare prompt only*; every other key goes to the remote shell.

## 📁 SFTP browser

- [x] **Preview without download** — done better: `enter` opens the file in a remote editor tab.
- [ ] **Upload** (`u`): needs a text-input for the local path (or a local file picker).
- [ ] In-browser file ops: delete (`x`), rename (`R`), mkdir (`m`) — `sftpx` supports these, no UI/keys yet.
- [ ] Transfer progress + async transfers (currently synchronous → large files stall the UI).
- [ ] Sort toggle (name/size/mtime) and show mtime column.
- [ ] Confirm before overwriting an existing local download.
- [ ] Live-verify the browser look with a real host (not yet screenshotted).

## 🖥️ Host management

- [x] **Add/edit host in the TUI:** `a` adds, `e` edits the host under the cursor via a centered modal form (`internal/tui/hostform.go`). `store.Add` (INSERT that fails on a taken alias) prevents silent overwrites; alias rename via `store.Rename` preserves visit/frecency history.
- [x] **Delete host:** `x` deletes behind a confirmation modal (`internal/tui/confirm.go`); a live session is torn down first.
- [ ] **Groups/tags UI:** the form sets `Group`/`Tags`, but the list doesn't section by group or filter by tag yet.
- [ ] Pin host to the top of the list.
- [ ] Local file picker for the identity-file field (currently free-text; needs a local-fs adapter — `filebrowser` is remote-oriented).
- [x] **Re-import / sync `~/.ssh/config` from within the TUI:** `i` opens a one-field import card (`internal/tui/importer.go`) pre-filled with `~/.ssh/config`; `enter` upserts every non-wildcard host, `esc` closes. A first run (no hosts + a config on disk) opens it automatically, so nobody has to leave the TUI for `hop import`.

## 🔐 Connection & auth

- [x] **known_hosts:** an unknown key aborts the dial (`sshx.UnknownHostKeyError`, nothing appended) and the TUI shows a modal "NEW HOST KEY" card (`internal/tui/hostkey.go`) with the fingerprint; `y` retries via `sshx.ConnectTrusting` (appends only if the presented key matches the approved fingerprint), `n`/`esc` trusts nothing. A known-host mismatch stays a hard error.
- [ ] Reconnect handling: detect a dropped session, mark it dead, offer reconnect (a dead pane just stops updating today).
- [ ] Deferred (not needed for current setup): password auth, 2FA/OTP passthrough, ProxyJump/bastion, PuTTY session import.

## ⌨️ Terminal / UX polish

- [x] Cursor visible (reverse-video overlay at `CursorPosition`); typing lag removed (event-driven redraw); visual pass (keycap pills, `HOSTS` section, 3-state status dots incl. yellow `◐` connecting, accent selection bar, status badges).
- [x] **Settings popover:** `,` opens a floating card (`internal/tui/overlay.go` composites over the finished screen via `x/ansi`) over `internal/config` — editor, download dir, accent, open-with. Stored as JSON at `<UserConfigDir>/hop/config.json` (`%AppData%\hop\` on Windows, `~/Library/Application Support/hop/` on macOS), applied live on save.
- [ ] Cursor: respect hidden state and cursor style (block/bar/underline); optional blink.
- [x] Scrollback UI: `shift+↑` (one line) or `shift+pgup` (a page) pauses a focused shell into its history; `↑`/`↓` or `j`/`k` move a line, `pgup`/`pgdn` (or `ctrl+b`/`ctrl+f`) a page, `ctrl+u`/`ctrl+d` a half, `g`/`home` to the top, `G`/`end` back to live. `esc`/`q`/`ctrl+o`/`enter`/`←` (or scrolling back to the bottom) return to the live shell. Off on the alt screen and when there's no scrollback.
- [ ] Mouse support (scroll/click) in panes and lists.
- [ ] Narrow-terminal handling: header/footer truncation and min-size behavior.
- [ ] Copy/paste into the remote shell.

## ⚙️ Config & distribution

- [ ] Config file: keybindings and default user (accent/download dir/editor/open-with are done — see the settings popover).
- [ ] Put `hop` on PATH; ship a build/install script; write a README.
- [x] **Cross-platform build (macOS/Linux):** the agent transport is now behind a build-tagged `dialAgent` — `internal/sshx/agent_windows.go` (named pipe) and `agent_unix.go` (`$SSH_AUTH_SOCK` unix socket). `go build`/`vet`/`test` are green on darwin; `GOOS=windows`/`linux` cross-build clean. Config/known_hosts/download paths already used `os.UserConfigDir`/`UserHomeDir`.
- [x] **Release/CI for all platforms:** CI tests on a windows/ubuntu/macos matrix; the release workflow gates on that matrix, then cross-compiles all six targets (windows/linux/darwin × amd64/arm64) from one Linux runner — `.zip` for Windows, `.tar.gz` elsewhere so the exec bit survives.
- [x] **Universal `justfile`:** recipe bodies are plain commands that run under both `sh` and PowerShell; the binary suffix, VERSION read and build timestamp come from just's built-ins (`os_family()`, `read()`, `datetime_utc()`) instead of shell commands, and the two recipes that need real shell logic (`fmt-check`, `clean`) are split with `[unix]`/`[windows]` attributes. Needs just >= 1.39.
- [ ] Cross-platform follow-ups: `action.NewTab` (wt.exe/pwsh) is Windows-only and currently unused; `filebrowser`'s executable-open guard is Windows-shaped and does nothing useful on Unix; `ci.yml` runs vet + test but not `just ci`, so `fmt-check` is never enforced.

## 🧪 Testing

- [x] `tui` host management (`hostmgmt_test.go`): add/edit/delete via real keystrokes against a temp-file store — validation, rename-preserves-history, duplicate/empty/bad-port rejection, modal key-swallowing.
- [x] `filebrowser` navigation (`filebrowser_test.go`, via `filebrowser.Client` interface + a fake): motions, up-a-directory keys, cursor-stays-visible invariant.
- [x] `tui` navigation-mode keys (`keys_test.go`).
- [x] `store` upsert/touch/delete/add/rename (`store_test.go`).
- [x] Round-trip: `TestEmbeddedRoundTrip` (terminal), `TestSFTPRoundTrip` (sftp). `go build`/`vet`/`test` green.
- [ ] `store` import parsing + frecency ordering (still untested; `store.OpenAt` takes a path, so point a test at a temp db).
- [ ] `action` package.
- [ ] `keyToBytes` mapping table test in `terminal`.
- [ ] `filebrowser` rendering.
- [ ] Mode *switches* (navigation ↔ terminal ↔ browsing) — need a fake pane/browser.

## 📝 Known limitations / notes

- Auto-tracked "recent directories" in the sidebar was built then removed — the sidebar is a host list; dirs shouldn't appear implicitly. **Don't reintroduce implicit tracking.**
- Local `sshd` can't be started here without admin → live verification of real remote sessions is manual (Patrick's side); headless tests use in-process Go SSH/SFTP servers.
- `x/vt` is an untagged dep (pinned to a pseudo-version) — watch for breaking changes on update.
- SFTP ops are synchronous (acceptable for MVP; see async item above).
