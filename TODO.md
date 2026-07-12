# hop — TODO

A Windows-first TUI SSH/server manager with embedded terminal panes. Stack: Go, Bubble Tea/Lip Gloss, pure-Go SSH (`x/crypto/ssh`), `x/vt` emulator, SQLite (`modernc`), `pkg/sftp`.

## ✅ Done

- **Phase 1 — MVP:** SQLite host store, `~/.ssh/config` import, embedded SSH terminal panes (agent auth, VT emulator, keystrokes, resize), multi-session with `ctrl+o` to leave a pane, VS Code Remote open (`o`), fuzzy search (`/`).
- **Phase 2 — SFTP browser:** `internal/sftpx` (List/Download/Upload/Mkdir/Remove/Rename over the same SSH conn) + `internal/filebrowser` TUI component. `f` opens the browser; navigate, `enter` opens a file in the OS default app, `e` in `$EDITOR`, `d` downloads it → `~/Downloads`, `h`/`backspace` up, `r` refresh.
- **Vim motions + back/forward keys:** `gg`/`G`, `H`/`M`/`L`, `ctrl+d`/`ctrl+u`, `ctrl+f`/`ctrl+b` in both the host list and the browser. `enter`/`l`/`right` descends, `h`/`left` backs out; `left` at the browser's top directory pops back to hop. The terminal pane reserves only `ctrl+o` and a 400 ms double-`esc` — every other key belongs to the remote shell (a lone `esc` is still forwarded). See [KEYBINDINGS.md](KEYBINDINGS.md).
- **Polish/fixes:** cursor now visible (reverse-video overlay at `CursorPosition`), typing lag removed (event-driven redraw instead of 50ms ticker), visual pass (keycap pills, `HOSTS` section, 3-state status dots incl. yellow `◐` connecting, accent selection bar, status badges).
- **Verified headless:** `TestEmbeddedRoundTrip` (terminal), `TestSFTPRoundTrip` (sftp). `go build`/`vet`/`test` green.

## 🔜 Roadmap (next phases)

- [ ] **Phase 3 — Tunnels / port-forwarding:** define local/remote forwards per host, start/stop with a key, show status in the dashboard. (`ssh.Client` supports `Dial`/`Listen`; reuse the existing connection.)
- [ ] **Phase 4 — Status/health panel:** per-host reachability + latency, and `uptime`/disk via a background `session.Run`, shown like VS Code's connection status.

## 📁 SFTP browser — enhancements

- [ ] **Upload** (`u`): needs a small text-input for the local path (or a local file picker).
- [ ] File operations in-browser: delete (`x`), rename (`R`), mkdir (`m`) — `sftpx` already supports Remove/Rename/Mkdir, just no UI/keys.
- [ ] File preview/view (small text files) without downloading.
- [ ] Transfer progress + async transfers (currently synchronous → large files briefly stall the UI).
- [ ] Sort toggle (name/size/mtime) and show mtime column.
- [ ] Confirm before overwriting an existing local download.
- [ ] Live-verify the browser look with a real host (not yet screenshotted).

## 🖥️ Host management

- [ ] **Add/edit host inside the TUI** (form): currently only `hop add` (CLI) + `hop import`.
- [ ] **Groups/tags UI:** schema has `Group`/`Tags` but nothing sets them and the list doesn't section by group. Add grouping + tag filter.
- [ ] Delete/pin host from the list.
- [ ] Re-import / sync `~/.ssh/config` from within the TUI.

## 🔐 Connection & auth

- [ ] **known_hosts** currently uses silent TOFU (auto-accepts + appends unknown keys). Add a confirmation prompt on first connect / mismatch warning.
- [ ] Reconnect handling: detect a dropped session, mark it dead in the list, offer reconnect (currently a dead pane just stops updating).
- [ ] Deferred (not needed for current setup): password auth, 2FA/OTP passthrough, ProxyJump/bastion, PuTTY session import.

## ⌨️ Terminal / UX polish

- [ ] Cursor: respect hidden state (apps that hide the cursor still show a block) and cursor style (block/bar/underline); optional blink.
- [ ] Scrollback UI: the emulator keeps scrollback but there's no key to scroll up in a pane.
- [ ] Mouse support (scroll/click) in panes and lists.
- [ ] Narrow-terminal handling: header/footer truncation and min-size behavior.
- [ ] Copy/paste into the remote shell.

## ⚙️ Config & distribution

- [ ] Config file (theme/accent, download dir, keybindings, default user).
- [ ] Put `hop` on PATH; ship a build/install script; write a README.
- [ ] Cross-platform (macOS/Linux) — currently Windows-first (uses the Windows OpenSSH agent named pipe; agent access needs an OS abstraction).

## 🧪 Testing

- [ ] Unit tests for `store` (import parsing, frecency ordering, upsert/touch), `action`. `store.OpenAt` takes a path, so a test can point at a temp db.
- [x] `filebrowser` navigation: motions, up-a-directory keys, cursor-stays-visible invariant (`filebrowser_test.go`, via the new `filebrowser.Client` interface + a fake). Rendering still untested.
- [ ] `keyToBytes` mapping table test in `terminal`.
- [x] `tui` navigation-mode keys (`keys_test.go`). Mode *switches* (navigation ↔ terminal ↔ browsing) still untested — they need a fake pane/browser.

## 📝 Known limitations / notes

- Auto-tracked "recent directories" in the sidebar (the host at the cursor expanding into the dirs the SFTP browser had visited) was built and then removed — the sidebar is a host list, and dirs should not appear in it without being asked for. Don't reintroduce implicit tracking.
- Local `sshd` can't be started here without admin → live verification of real remote sessions is manual (Patrick's side); headless tests use in-process Go SSH/SFTP servers.
- `x/vt` is an untagged dep (pinned to a pseudo-version) — watch for breaking changes on update.
- SFTP ops are synchronous (acceptable for MVP; see async item above).
