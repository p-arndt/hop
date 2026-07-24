---
id: "20260724-201458-in-tui-ssh-config-import-first-run-offer"
title: "In-TUI SSH config import with a first-run offer"
status: "completed"
updated: "2026-07-24T20:14:58+02:00"
base_commit: "f149c7c39c8ad6283f9b2e0c3e1435508338ccff"
branch: "main"
agent: null
tags: ["hosts", "import", "onboarding", "tui"]
files:
  - "KEYBINDINGS.md"
  - "TODO.md"
  - "internal/tui/details.go"
  - "internal/tui/help.go"
  - "internal/tui/importer.go"
  - "internal/tui/importer_test.go"
  - "internal/tui/keys.go"
  - "internal/tui/list.go"
  - "internal/tui/model.go"
  - "internal/tui/view.go"
---

# In-TUI SSH config import with a first-run offer

## Goal

A first-time user could only fill the host list by quitting the TUI and running `hop import`.
Bring the import inside: a modal card bound to `i`, opened automatically on a first run.

## Scope

- New `internal/tui/importer.go`: `importUI` state (open, path, first), `openImport`/`closeImport`,
  `handleImportKey`, `submitImport`, `renderImport`, plus `defaultSSHConfigPath`, `haveSSHConfig`,
  `expandHome` and a small `wrapDim` prose helper.
- Integration seams follow the existing modal pattern (hostform/confirm): `model.importer` field,
  `handleKey` routing, `modalCard` + footer in view.go, `helpLeft` in help.go.
- First-run auto-open lives in `tui.Run`: zero hosts **and** a real `~/.ssh/config` on disk.
- Empty-state copy in list.go and details.go now names the `i`/`a` keys instead of the shell commands.
- `hop import [path]` on the CLI is untouched; both call `store.ImportSSHConfig`.

## Discoveries

- `store.ImportSSHConfig` already upserts per host and skips wildcard patterns, so the same call
  serves as a re-import/sync — no new store code was needed for the "run it again" case.
- `store.Upsert`'s ON CONFLICT clause leaves visits/last_connect alone, so a re-import refreshes
  connection details without resetting frecency history.

## Decisions

- **Decision:** `i` opens the card from navigation mode and stays bound once hosts exist.
  - **Reason:** Import is a sync, not only an onboarding step; the TODO item asked for re-import too.
  - **Trade-off:** `i` is now taken in the host list; it collides with nothing (the vim keymap does
    not bind it, and the settings popover's `i` is its own mode).

- **Decision:** The first run opens the card only when `~/.ssh/config` exists (`haveSSHConfig`,
  which also rejects a directory of that name).
  - **Reason:** Offering an import with no file to read is a dead end for a brand-new user; the
    empty list already says `i` / `a`.

- **Decision:** The path field is pre-filled with the default config path, and a leading `~` is
  expanded on submit (`expandHome`).
  - **Reason:** Makes the common case one keystroke while still letting a custom path be typed the
    way it is spoken.

- **Decision:** A failed import keeps the card open with the typed path intact; `n == 0` reports a
  warning ("no hosts found in …") rather than a cheerful "imported 0 hosts".
  - **Reason:** Same shape as `submitHostForm` — a bad value gets fixed, not lost.

- **Decision:** Import runs synchronously in the update loop.
  - **Reason:** It is a local file parse plus a few SQLite upserts; no spinner/command plumbing
    is warranted. Would need revisiting only for pathologically large configs.

## Failures

## Validation

- `go build ./...`, `go vet ./...`, `gofmt -l .` (clean), `go test ./...` all pass.
- New `internal/tui/importer_test.go` (12 tests) drives `handleKey` against a real temp-file SQLite
  store: the happy path, bad path keeps the card open, wildcard-only config warns, empty path
  refused, esc skips, modality (keys do not leak to the list), re-import updates in place,
  backspace editing, `expandHome` table, `~` path import, default-path prefill, `haveSSHConfig`,
  and a render check that no card line exceeds the window width.

## Remaining risks

- The first-run offer keys off "zero hosts", so a user who deliberately deletes every host will be
  offered the import again on the next start. Cheap to dismiss with `esc`; no persisted "seen" flag.
- No preview of what will be imported (count/aliases) before the write — the upsert semantics make
  it recoverable, but a dry run would be a natural follow-up.

## Handoff

- If a "sync" that also *removes* hosts dropped from the config is ever wanted, it needs new store
  support (import currently only adds/updates) and should be a distinct, confirmed action.
