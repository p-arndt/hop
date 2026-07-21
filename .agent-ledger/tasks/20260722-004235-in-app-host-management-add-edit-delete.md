---
id: "20260722-004235-in-app-host-management-add-edit-delete"
title: "In-app host management (add/edit/delete)"
status: "completed"
updated: "2026-07-22T00:42:55+02:00"
base_commit: "e2b0f7c49b687e50a90c28afe588ce2f8b9c83e8"
branch: "main"
agent: null
tags: ["hosts", "store", "tui"]
files:
  - "internal/store/store.go"
  - "internal/store/store_test.go"
  - "internal/tui/confirm.go"
  - "internal/tui/help.go"
  - "internal/tui/hostform.go"
  - "internal/tui/hostmgmt_test.go"
  - "internal/tui/keys.go"
  - "internal/tui/model.go"
  - "internal/tui/view.go"
---

# In-app host management (add/edit/delete)

## Goal

Let users create, edit, and delete hosts from inside the TUI instead of only via the hop CLI.

## Scope

- New modals live in their own files, mirroring settings.go: internal/tui/hostform.go (add/edit form) and internal/tui/confirm.go (delete). Integration seams are in model.go (hostForm/confirm state fields), keys.go (handleKey routing + a/e/x nav binds), view.go (modalCard + footer), help.go.

- Identity file is a plain text field in v1 (user deferred a local file picker to a follow-up). The identity picker is the deferred scope.

## Discoveries

- Key choice: a=add, e=edit, x=delete. 'x' (not 'd') for delete because 'd' already means disconnect in the host list. Modals are swallow-all like the existing settings/help cards.

- store.Upsert's ON CONFLICT(alias) clause intentionally does NOT update visits/last_connect, so editing non-alias fields preserves history automatically. Only an alias change needed special handling (Rename).

## Decisions

- **Decision:** Alias rename on edit goes through new store.Rename(old,new) rather than delete+re-upsert.
  - **Reason:** A plain Upsert of a new alias creates a fresh row with visits/last_connect zeroed; Rename does an UPDATE ... SET alias=? so frecency history survives.
  - **Trade-off:** Rename rejects a target alias that already exists and is the authoritative guard.

- **Decision (code-review follow-up):** New hosts go in through new store.Add(Host), an INSERT that fails on a taken alias (backed by the `alias UNIQUE` constraint), NOT through Upsert.
  - **Reason:** The add path originally guarded duplicates only with the in-memory m.hostByAlias check, but Upsert does ON CONFLICT DO UPDATE — a stale m.hosts (e.g. a concurrent `hop add` from another terminal) could silently overwrite an existing host. Add cannot overwrite.
  - **Note:** submitHostForm no longer pre-checks m.hostByAlias for either add or edit; it relies on Add / Rename returning a readable error and keeping the form open. This obsoletes the earlier "form pre-checks via m.hostByAlias" note.
  - Also from the review: reloadHosts was split into reloadHostsSelecting(alias) so the save path parks the cursor on the new/renamed host without a duplicated loop; a redundant clampCursor in confirmDelete was dropped; and `,` settings was restored to the default list footer.

## Failures

## Validation

- go build ./... , go vet ./... , gofmt -l (clean) , go test ./... all pass. New: internal/store/store_test.go (9 tests incl. Rename + Add's no-overwrite guarantee), internal/tui/hostmgmt_test.go (12 e2e tests driving handleKey against a real temp-file SQLite store).
- Re-run after the code-review fixes (store.Add, reloadHostsSelecting, footer): build/vet/gofmt/test all still pass.

## Remaining risks

- Deleting a host with a live session calls m.disconnect(alias) first (session.go) to tear down panes cleanly before store.Delete.

- Identity field is free-text only; local file picker not yet built.

## Handoff

- For the identity picker follow-up: the existing internal/filebrowser is SFTP/remote-oriented, so a local picker needs a small local-fs adapter, not a reuse of that browser.
