# hop — TODO

A Windows-first TUI SSH/server manager with embedded terminal panes.
Stack: Go, Bubble Tea/Lip Gloss, pure-Go SSH (`x/crypto/ssh`), `x/vt` emulator, `pkg/sftp`.

See also: [KEYBINDINGS.md](KEYBINDINGS.md). Rationale lives in the package it belongs to — this
file tracks *what*, not *why*.

---

## 🚀 Core / phases

- [x] **Phase 1 — MVP:** host store, `~/.ssh/config` import, embedded SSH panes, multi-session, VS Code Remote, fuzzy search.
- [x] **Phase 2 — SFTP browser:** `internal/sftpx` + `internal/filebrowser`.
- [x] **Working directory → VS Code:** panes track remote cwd over OSC 7; `ctrl+o ctrl+o` / `o` opens VS Code there.
- [x] **Phase 3 — Tunnels:** `T` per-host manager, local/remote forwards, SSH-config import, restored on reconnect.
- [x] **SQLite removed:** hosts live in `~/.ssh/hop.config` (included from `~/.ssh/config`), hop-only metadata under the `hosts` key of `config.json`. A legacy `hop.db` migrates once, via a dependency-free reader, and is kept as `hop.db.bak`.
- [ ] **Phase 4 — Status/health panel:** per-host reachability, latency, `uptime`/disk in the background.

## 🪟 Panes & tabs

- [x] **Shell tabs** (`S`): another shell on a connected host, no re-handshake.
- [x] **Editor tabs:** `enter` opens `${EDITOR:-vi}` on a remote pty — no download.
- [x] **Vim motions + back/forward** in the browser; panes reserve `ctrl+o` and double-`esc`.

## 📁 SFTP browser

- [x] Preview without download — instead: `enter` opens a remote editor tab.
- [x] Look verified end-to-end and screenshotted (`assets/screens/sftp.png`).
- [x] **Upload** (`u`): typed local path, `~` expanded. A local file picker was deliberately not built.
- [x] **File ops:** delete (`x`, confirmed), rename (`R`), mkdir (`m`) — one typed-name guard, no recursive delete.
- [x] **Async transfers + progress** (`transfer.go`): real byte counts from `sftpx` callbacks, sampled per tick; `io.ReaderFrom` kept intact for fast uploads. One transfer at a time.
- [x] **Sort toggle** (`s`): name / size / modified, dirs first, cursor rides its entry.
- [x] **Confirm before overwriting** — both directions.
- [x] **Tree listing** (`tree.go`): directories open in place, lazily listed and cached. `View`/`RowAt`/`Select`/`Scroll` still speak flat row indices, which is what left the mouse handling untouched.
- [x] **Multi-selection** (`space`, `a`): marks are global across the tree, keyed by absolute path, and survive a refresh. Every op acts on `targets()` — the marked set, or the cursor entry alone.
- [x] **Batch ops with one progress line** (`3/7 · name.txt`): stop at the first failure, name what got through, leave the rest marked for the same keystroke.
- [x] **Copy/move to a target** (`t`, `c`, `v`) via `sftpx.Copy`/`Move`, recursive, symlinks recreated rather than followed. A move onto an existing name is refused, not silently overwritten.
- [x] **The browser is a column** (`internal/tui/layout.go`): tree and file on screen together, `tab`/`alt+t` for the keyboard, `ctrl+t` to collapse, full-pane fallback below 96 columns.
- [x] **Two files side by side** (`\`): the content area splits into two halves with their own tab strips.
- [ ] Recursive upload/download of a *local* directory tree; more than one transfer at a time.
- [ ] Cancel a transfer in flight (needs `context.Context` in `sftpx`).
- [ ] Server-side copy: `pkg/sftp` has no `copy-data@openssh.com`, so `c` pays double the wire cost of a download. Needs the extension, or `ssh cp` as a fast path.

## 🖥️ Host management

- [x] **Add/edit host** (`a`/`e`); rename preserves frecency history.
- [x] **Delete host** (`x`) behind a confirm.
- [x] **Pin host** (`p`) into a PINNED section; `shift+k`/`shift+j` reorder.
- [x] **Per-host default directory:** applied to shells and the browser; E2E in Docker.
- [x] **Re-import `~/.ssh/config`** (`i`); opens automatically on an empty first run.
- [ ] **Groups/tags UI:** the form sets them, the list doesn't section or filter by them.
- [ ] Local file picker for the identity-file field (needs a local-fs adapter).

## 🔐 Connection & auth

- [x] **known_hosts:** unknown key → fingerprint card, `y` trusts and retries; a mismatch is a hard error.
- [x] **Private-key auth** (`sshx/keys.go`): identity files + agent signers in one publickey method.
- [x] **Interactive auth (2FA/OTP, passwords):** `sshx.Prompter` asks the UI mid-handshake; `esc` cancels stickily.
- [x] **E2E 2FA against a real server:** four auth shapes in Docker, `HOP_DOCKER_E2E=1` / `just test-e2e`.
- [x] **Reconnect:** keepalives detect a dead link; `r` restores the session's shape (not editor tabs).
- [x] **ProxyCommand / ProxyJump** (#13): `sshx/proxy.go`, imported and editable; no shell, argv only.
- [ ] Deferred: PuTTY session import.

## ⌨️ Terminal / UX polish

- [x] Cursor visible; event-driven redraw; visual pass (keycap pills, status dots, accent bar, badges).
- [x] **Settings popover** (`,`) over `internal/config`, applied live on save.
- [x] **Scrollback UI:** `shift+↑` / `shift+pgup` with vim + page motions; `esc`/`q`/`ctrl+o` return to live.
- [x] **Collapsible sidebar** (`ctrl+b`), session-only. Costs a remote tmux its prefix.
- [x] **Host list keymap trimmed** to step keys via `keymap.Scope`.
- [x] **Mouse support** routed by region (`tui/mouse.go`); a remote asking for the mouse gets it verbatim.
- [x] **Mouse text selection** over shells, scrollback and editors → clipboard on release; `ctrl+g` hands reporting back.
- [x] **Selection past one screenful:** the wheel scrolls under a live drag, a drag held at a pane edge autoscrolls, and a selection rides the text it was made on. On the alt screen the wheel is sent on as `↑`/`↓`.
- [x] **Drag autoscroll** past a pane's top/bottom row, into scrollback and back.
- [x] **Copy/paste:** paste as your terminal pastes (shape-detected on Windows); copy via OSC 52. A remote *read* is never answered.
- [x] **Cursor is the remote's** (`terminal/cursor.go`): DECSCUSR shape, DECTCEM hiding, block restored on reset. Blinking is a setting, off by default.
- [x] **One mode enum:** `model.mode` is a single `paneMode`; old flag names survive as predicates.
- [x] **One action registry** (`tui/actions.go`): context menu, palette (`ctrl+k`) and details grid all render from it; running a row replays its key.
- [x] **Guidance profiles** (`keys`/`hybrid`/`guided`): visibility only, asked once, editable at `,`.
- [x] **Status bar + fitting footer:** host › mode › location, tab chips right; per-mode footer core plus extras, dropping whole hints. `?` opens the card on your section.
- [x] **`esc` `esc` quits from the host list**; a single `esc` still just drops the selection.
- [ ] Narrow-terminal handling: header/footer truncation and min-size behavior.

## ⚙️ Config & distribution

- [x] **Demo recording:** `just demo` → `assets/demo.gif` via VHS against `tools/demoserver`; overlay behind `-tags hopdemo`.
- [x] **README:** hero, features, install, quick start, keys, architecture, roadmap.
- [x] **Docs generated from one source:** `docs/*.md` → `index.html` + `README.md` + `KEYBINDINGS.md` (`just docs`); tests fail on drift.
- [x] **Site published:** GitHub Pages at <https://p-arndt.github.io/hop/>, with offline search.
- [x] **Self-update:** `hop self-update` / `check-update`, SHA-256 verified, atomic swap; daily passive check.
- [x] **Cross-platform build:** agent transport behind a build-tagged `dialAgent`.
- [x] **Release/CI:** windows/ubuntu/macos matrix, six cross-compiled targets.
- [x] **Universal `justfile`:** runs under `sh` and PowerShell (just >= 1.39).
- [ ] Config file: keybindings and default user (the rest is done).
- [ ] Put `hop` on PATH; ship a build/install script.
- [ ] Cross-platform follow-ups: `action.NewTab` is Windows-only and unused; `filebrowser`'s executable-open guard is Windows-shaped; `ci.yml` skips `fmt-check`.

## 🏗️ Architecture / debt

- [x] **The split intent rides on the message** (`OpenFileMsg.Beside`), not a `splitPending` flag on the session. A directory cannot produce a marked message, so the two "clear the stale flag" sites are gone.
- [x] **`footerHints` is a table** (`footerCardArms`/`footerModeArms`), not a 242-line switch. First-match-wins; the two order dependencies are written down.
- [x] **One source for geometry** (`frame`/`rect` in `layout.go`): `recomputeLayout` places every box of the body once, in outer coordinates. `rect.contains` answers the zone hit-testing, `rect.inner` the panes, `rect.clamp` a drag off the edge. `view.go`, `mouse.go` and `selection.go` name no width and no offset — every `listWidth`/`treeWidth`/`splitHalf` reference outside `layout.go` is gone. The selection now keeps the box the drag began in, so it survives a resize mid-drag.
- [x] **The frame no longer overruns narrow terminals.** `listWidth` yields the sidebar entirely rather than honour a floor the window cannot pay for, on the same terms as the tree column, and the content area has no floor left to break. `TestVeryNarrowWindowsStillFitTheirTerminal` asserts every width from 3 to 40.
- [x] **`model` is grouped, not flat**: `layout` (window size, derived sizes, the frame, the two collapse flags) and `focus` (active session, mode, selection, drag chain, chords) are embedded structs, so `m.width` and `m.active` still read as they did. 60 named fields down to 47. Ten methods that never needed a model moved onto them — `bodyHeight`, `listWidth`, `splitHalf`, `splitFits`, `buildFrame` on `*layout`; the five mode predicates on `*focus` — which is the part that makes the grouping more than cosmetic. `buildFrame` takes `split bool` because whether the content is halved is the session's answer, not the layout's.
- [x] **`ctrl+\` closes the split**, keeping whichever file the focused half was showing. With nothing split it falls through to the remote editor rather than being swallowed. Chosen as `\` plus a modifier so open-beside and close-the-split read as one gesture, and ctrl-based because `alt+…` is only ever an alias here.
- [ ] **The host list answers keys it cannot show.** Below the sidebar threshold `sidebarOn()` is false and no list is drawn, but `enter` in `modeList` still acts on the invisible selection. The gating belongs in `keys.go`/`list.go` and is now a one-line question to ask.

## 🧪 Testing

- [x] `tui` host management (`hostmgmt_test.go`) and navigation keys (`keys_test.go`).
- [x] `filebrowser` navigation (`filebrowser_test.go`), sorting, file ops, transfers (async asserted through `Update`).
- [x] `store` upsert/touch/delete/add/rename (`store_test.go`).
- [x] Round-trips: `TestEmbeddedRoundTrip`, `TestSFTPRoundTrip`.
- [x] Mode switches (`mode_test.go`), every transition against a real shell pane.
- [x] `store` import parsing + frecency ordering.
- [x] `store` config-file round-trip, `Include` handling, and migration from checked-in legacy `hop.db` fixtures.
- [x] `action` package.
- [x] `keyToBytes` mapping table test in `terminal`.
- [x] `filebrowser` rendering.
- [x] **Every bound action is discoverable** (`TestEveryActionIsDiscoverable`): each Browser and Editor action must appear in the key card and the palette, with an explicit exempt set. Guards the failure mode that once shipped marks and copy/move bound but invisible.
- [x] **Layout characterization** (`tui/layout_test.go`): 23 window/column/split cases, each asserting that every rendered line is *exactly* the window width and that `zoneAt`/`treeLocal`/`contentLocal` agree cell-for-cell with the boxes `View` actually drew.
- [x] **Footer legend** (`tui/footer_test.go`): 37 states pinned as goldens, `core`/`extra`/`help` kept apart so a hint moving between them fails instead of cancelling out.

## 📝 Known limitations / notes

- The browser asks on the status line, not in a card — the listing is the question's context. While a prompt is open it owns the keyboard.
- Auto-tracked "recent directories" was built then removed. **Don't reintroduce implicit tracking.**
- `x/vt` is untagged (pinned to a pseudo-version) — watch for breaking changes.
- SFTP listing and file ops stay synchronous; only transfers are async.
- `alt+…` chords need "Option as Meta" on macOS, so they are only ever aliases — every binding must be reachable with `ctrl`/`shift`.
