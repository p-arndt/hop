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
- [ ] Re-import / sync `~/.ssh/config` from within the TUI.

## 🔐 Connection & auth

- [ ] **known_hosts:** currently silent TOFU (auto-accepts + appends). Add a confirmation prompt on first connect / mismatch warning.
- [ ] Reconnect handling: detect a dropped session, mark it dead, offer reconnect (a dead pane just stops updating today).
- [ ] Deferred (not needed for current setup): password auth, 2FA/OTP passthrough, ProxyJump/bastion, PuTTY session import.

## ⌨️ Terminal / UX polish

- [x] Cursor visible (reverse-video overlay at `CursorPosition`); typing lag removed (event-driven redraw); visual pass (keycap pills, `HOSTS` section, 3-state status dots incl. yellow `◐` connecting, accent selection bar, status badges).
- [x] **Settings popover:** `,` opens a floating card (`internal/tui/overlay.go` composites over the finished screen via `x/ansi`) over `internal/config` — editor, download dir, accent, open-with. Stored as JSON at `%AppData%\hop\config.json`, applied live on save.
- [ ] Cursor: respect hidden state and cursor style (block/bar/underline); optional blink.
- [x] Scrollback UI: `shift+↑` (one line) or `shift+pgup` (a page) pauses a focused shell into its history; `↑`/`↓` or `j`/`k` move a line, `pgup`/`pgdn` (or `ctrl+b`/`ctrl+f`) a page, `ctrl+u`/`ctrl+d` a half, `g`/`home` to the top, `G`/`end` back to live. `esc`/`q`/`ctrl+o`/`enter`/`←` (or scrolling back to the bottom) return to the live shell. Off on the alt screen and when there's no scrollback.
- [ ] Mouse support (scroll/click) in panes and lists.
- [ ] Narrow-terminal handling: header/footer truncation and min-size behavior.
- [ ] Copy/paste into the remote shell.

## ⚙️ Config & distribution

- [ ] Config file: keybindings and default user (accent/download dir/editor/open-with are done — see the settings popover).
- [ ] Put `hop` on PATH; ship a build/install script; write a README.
- [ ] Cross-platform (macOS/Linux) — currently Windows-first (Windows OpenSSH agent named pipe; agent access needs an OS abstraction).

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
