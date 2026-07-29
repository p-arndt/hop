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
- [x] **Vim motions + back/forward keys:** `gg`/`G`, `H`/`M`/`L`, `ctrl+d`/`ctrl+u`, `ctrl+f` in the browser; the host list keeps only `hjkl` + the page keys (see the keymap-scope entry under UX polish). `enter`/`l`/`right` descends, `h`/`left` backs out; `left` at the browser top pops back to hop. Panes reserve `ctrl+o` and a 400 ms double-`esc`; every other key — `left` included — goes to the remote shell.

## 📁 SFTP browser

- [x] **Preview without download** — done better: `enter` opens the file in a remote editor tab.
- [ ] **Upload** (`u`): needs a text-input for the local path (or a local file picker).
- [ ] In-browser file ops: delete (`x`), rename (`R`), mkdir (`m`) — `sftpx` supports these, no UI/keys yet.
- [ ] Transfer progress + async transfers (currently synchronous → large files stall the UI).
- [ ] Sort toggle (name/size/mtime) and show mtime column.
- [ ] Confirm before overwriting an existing local download.
- [x] Browser look verified end-to-end and screenshotted — `assets/screens/sftp.png`, recorded against the demo server (`just demo`). A live *real* host is still Patrick's side.

## 🖥️ Host management

- [x] **Add/edit host in the TUI:** `a` adds, `e` edits the host under the cursor via a centered modal form (`internal/tui/hostform.go`). `store.Add` (INSERT that fails on a taken alias) prevents silent overwrites; alias rename via `store.Rename` preserves visit/frecency history.
- [x] **Delete host:** `x` deletes behind a confirmation modal (`internal/tui/confirm.go`); a live session is torn down first.
- [ ] **Groups/tags UI:** the form sets `Group`/`Tags`, but the list doesn't section by group or filter by tag yet.
- [ ] Pin host to the top of the list.
- [ ] Local file picker for the identity-file field (currently free-text; needs a local-fs adapter — `filebrowser` is remote-oriented).
- [x] **Re-import / sync `~/.ssh/config` from within the TUI:** `i` opens a one-field import card (`internal/tui/importer.go`) pre-filled with `~/.ssh/config`; `enter` upserts every non-wildcard host, `esc` closes. A first run (no hosts + a config on disk) opens it automatically, so nobody has to leave the TUI for `hop import`.

## 🔐 Connection & auth

- [x] **known_hosts:** an unknown key aborts the dial (`sshx.UnknownHostKeyError`, nothing appended) and the TUI shows a modal "NEW HOST KEY" card (`internal/tui/hostkey.go`) with the fingerprint; `y` retries via `sshx.ConnectTrusting` (appends only if the presented key matches the approved fingerprint), `n`/`esc` trusts nothing. A known-host mismatch stays a hard error.
- [x] **Private-key auth (agent no longer required):** `internal/sshx/keys.go` loads the host's `IdentityFile` (with `~` expansion) or the default `~/.ssh/id_{ed25519,ecdsa,rsa,dsa}`, and `authMethods` merges those signers with the agent's into a **single** publickey method — the SSH client tries each method name only once, so a separate agent method would swallow the attempt. Fixes connecting on macOS, where launchd always exports `$SSH_AUTH_SOCK` but the agent holds no identities until `ssh-add`. Passphrase-protected and missing-`IdentityFile` keys are reported in the "no usable authentication" error.
- [ ] Reconnect handling: detect a dropped session, mark it dead, offer reconnect (a dead pane just stops updating today).
- [ ] Deferred (not needed for current setup): password auth, 2FA/OTP passthrough, ProxyJump/bastion, PuTTY session import.

## ⌨️ Terminal / UX polish

- [x] Cursor visible (reverse-video overlay at `CursorPosition`); typing lag removed (event-driven redraw); visual pass (keycap pills, `HOSTS` section, 3-state status dots incl. yellow `◐` connecting, accent selection bar, status badges).
- [x] **Settings popover:** `,` opens a floating card (`internal/tui/overlay.go` composites over the finished screen via `x/ansi`) over `internal/config` — editor, download dir, accent, open-with. Stored as JSON at `<UserConfigDir>/hop/config.json` (`%AppData%\hop\` on Windows, `~/Library/Application Support/hop/` on macOS), applied live on save.
- [ ] Cursor: respect hidden state and cursor style (block/bar/underline); optional blink.
- [x] Scrollback UI: `shift+↑` (one line) or `shift+pgup` (a page) pauses a focused shell into its history; `↑`/`↓` or `j`/`k` move a line, `pgup`/`pgdn` (or `ctrl+f`) a page, `ctrl+u`/`ctrl+d` a half, `g`/`home` to the top, `G`/`end` back to live. `esc`/`q`/`ctrl+o`/`enter`/`←` (or scrolling back to the bottom) return to the live shell. Off on the alt screen and when there's no scrollback.
- [x] **Collapsible sidebar:** `ctrl+b` hides the host list and gives the whole window to the pane, `ctrl+b` again brings it back (`model.sidebarHidden` → `listWidth() == 0`, everything else derives from it; every shell/browser/editor is resized on the toggle). Bound in every mode below the modal cards — the point of it is a focused shell — and answered once in `handleKey` rather than in four handlers. Session-only, no setting: hop opens on its host list. The costs, taken deliberately: a remote tmux never sees its `ctrl+b` prefix through hop, and `ctrl+b` is no longer a page-up motion anywhere (`pgup` is).
- [x] **Host list keymap trimmed to the step keys:** `keymap.Scope` splits the shared motion table — the SFTP browser keeps all of it (`gg`/`G`/`H`/`M`/`L`/`ctrl+d`/`ctrl+u`/`ctrl+f`), the host list keeps `hjkl`, the arrows, `pgup`/`pgdn` and `enter`. The list doesn't scroll, so the jumps landed a `j` or two from the cursor while holding nine keys hostage in the one view with commands to spend them on. Same meanings in both views, just fewer of them in the list.
- [ ] Mouse support (scroll/click) in panes and lists.
- [ ] Narrow-terminal handling: header/footer truncation and min-size behavior.
- [ ] Copy/paste into the remote shell.

## ⚙️ Config & distribution

- [ ] Config file: keybindings and default user (accent/download dir/editor/open-with are done — see the settings popover).
- [x] **Demo recording:** `just demo` (`scripts/demo.mjs`) records `assets/demo.gif` + `assets/screens/*.png` from `demo/hop.tape` with VHS. `tools/demoserver` is a loopback-only fake SSH host (canned shell, in-memory SFTP tree, fake vi, seeded host db, generated client/host keys) so the recording contains nothing real and anyone can re-run it. The keypress overlay is hop's own, behind `-tags hopdemo` (`internal/tui/keycast.go`), absent from released binaries.
- [x] **README:** `README.md` — hero + ASCII screenshot of the two-pane layout, feature table, install (release archives + from source), quick start incl. the CLI subcommands, where `hop.db`/`config.json`/`known_hosts` live, a collapsed key reference per mode linking to [KEYBINDINGS.md](KEYBINDINGS.md), an architecture map of `internal/*`, the `just` recipes and release flow, and a roadmap pointing back here.
- [ ] Put `hop` on PATH; ship a build/install script.
- [x] **Self-update:** `hop self-update` / `hop check-update` (`internal/update`) — resolves the latest GitHub release, downloads the archive for this platform over https (GitHub hosts only, redirects included), verifies its SHA-256 against the release's `checksums.txt`, and swaps the running binary atomically (Windows renames the old `.exe` aside; `CleanupLeftovers` sweeps it on the next start). A passive check runs at most once a day, cached in `<config dir>/hop/update-check.json`: CLI commands print a one-line hint on stderr and the TUI footer shows one in navigation mode. `HOP_NO_UPDATE_CHECK=1` disables it; dev builds never check.
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
- `x/vt` is an untagged dep (pinned to a pseudo-version) — watch for breaking changes on update.
- SFTP ops are synchronous (acceptable for MVP; see async item above).
