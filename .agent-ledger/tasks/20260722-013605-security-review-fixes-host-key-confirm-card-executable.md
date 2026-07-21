---
id: "20260722-013605-security-review-fixes-host-key-confirm-card-executable"
title: "Security review fixes: host-key confirm card, executable open guard, escape stripping"
status: "completed"
updated: "2026-07-22T01:36:06+02:00"
base_commit: "acc46728f8db3a15dbb512ebc149d87e6a986f78"
branch: "main"
agent: null
tags: ["filebrowser", "security", "ssh", "tui"]
files:
  - "TODO.md"
  - "internal/filebrowser/filebrowser.go"
  - "internal/filebrowser/filebrowser_test.go"
  - "internal/sshx/sshx.go"
  - "internal/sshx/sshx_test.go"
  - "internal/tui/commands.go"
  - "internal/tui/details.go"
  - "internal/tui/hostkey.go"
  - "internal/tui/hostkey_test.go"
  - "internal/tui/keys.go"
  - "internal/tui/list.go"
  - "internal/tui/list_render_test.go"
  - "internal/tui/model.go"
  - "internal/tui/msgs.go"
  - "internal/tui/session.go"
  - "internal/tui/view.go"
---

# Security review fixes: host-key confirm card, executable open guard, escape stripping

## Goal

Fix the top 3 findings of the defensive review: explicit trust for first-contact host keys, no ShellExecute of server-named executables, no terminal escapes from host fields.

## Scope

- This entry recreates two ledger files that were deleted from disk untracked (20260722-013134 and 20260722-013339), cause unknown — a concurrent session had committed around the same time.

## Discoveries

- errors.As unwraps *sshx.UnknownHostKeyError through ssh.Dial's and ConnectAddr's %w wrapping. The 'extra' shell intent (S/alt+0 vs first shell) rides on connectedMsg so the card's retry replays the right action. Editors reuse an existing connection and never dial, so they never prompt.

- openInApp guard applies only when OpenWith is empty (OS default handler = ShellExecute); an explicit OpenWith command gets the file as an argument, so it stays allowed. Download 'd' is deliberately unguarded. Windows strips trailing dots/spaces in names, so extension and CON/PRN checks TrimRight '. ' first.

- Fuzzy-highlight offsets are byte offsets into the original alias, so highlight() skips control runes inside its loop instead of pre-stripping, keeping offsets aligned.

## Decisions

- **Decision:** Abort the dial on an unknown host key and hand the trust decision to a modal card; retry via ConnectTrusting(h, fingerprint) which appends to known_hosts only if the presented key matches the approved fingerprint.
  - **Reason:** Silent TOFU waves a first-contact MITM through with no chance to decline.
  - **Trade-off:** First contact now takes two dials (probe + trusted retry).

- **Decision:** Strip control chars at render time (list.go/details.go) rather than sanitizing on import/save.
  - **Reason:** Covers all entry paths (import, CLI, form paste) without a data migration.
  - **Trade-off:** hop list CLI output still prints raw; any future render site must strip on its own.

## Failures

## Validation

- go build ./... && go vet ./... && go test ./internal/... — all packages pass (new sshx callback tests, tui hostkey/list_render tests, filebrowser guard tests)

## Remaining risks

- hop list (main.go cmdList) still prints host fields raw to stdout

## Handoff

- Unfixed review findings: silent download overwrite, unused IdentityFile field, port/IPv6 validation
