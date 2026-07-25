---
id: "20260725-205613-self-update-and-check-update-from-github-releases"
title: "Self-update: `hop self-update` / `hop check-update` (ported from shenv)"
status: "completed"
updated: "2026-07-25T20:56:13+02:00"
base_commit: "af2a75f88abc9b11e8b5b0068a9a4c6e47047229"
branch: "main"
agent: null
tags: ["cli", "distribution", "release", "security", "tui", "update"]
files:
  - "README.md"
  - "TODO.md"
  - "internal/tui/commands.go"
  - "internal/tui/model.go"
  - "internal/tui/msgs.go"
  - "internal/tui/update_test.go"
  - "internal/tui/view.go"
  - "internal/update/hardening_test.go"
  - "internal/update/notice.go"
  - "internal/update/notice_test.go"
  - "internal/update/update.go"
  - "internal/update/update_test.go"
  - "main.go"
---

# Self-update: `hop self-update` / `hop check-update` (ported from shenv)

## Goal

Give hop the updater shenv already has: a command that replaces the running binary with the latest GitHub release, a command that only reports whether one exists, and a passive once-a-day "newer version available" hint.

## Scope

- New `internal/update`, ported from `~/coding/shenv/internal/update` and re-branded: repo `p-arndt/hop`, binary `hop`/`hop.exe`, assets `hop_<v>_<goos>_<goarch>.{tar.gz,zip}` + `hop_<v>_checksums.txt` — exactly what `.github/workflows/release.yml` already publishes, so the workflow needed no change.
- `Client.SelfUpdate(ctx, current, checkOnly)`: resolve `releases/latest` → find this platform's archive → download archive + checksums → verify SHA-256 → extract the binary → swap it atomically. `checkOnly` stops after the version comparison.
- `replaceExecutable` writes a temp file in the target's own directory (same filesystem → atomic rename) and, on Windows, renames the running `.exe` aside to `<exe>.old` first, restoring it if the second rename fails. `CleanupLeftovers()` runs at the top of `main` and sweeps that leftover.
- `notice.go`: cache at `<UserConfigDir>/hop/update-check.json` (`{last_check, latest}`, 0600), refreshed at most once per 24 h with a 1.5 s timeout. `NotifyIfAvailable(w, current)` prints the CLI hint; `Refresh(current)` is the blocking-but-bounded variant the TUI calls off the UI thread.
- CLI: `hop self-update`, `hop check-update` (handled before the store is opened, since neither touches it), usage text, and a hint on **stderr** after `import`/`add`/`list`.
- TUI: `Init` batches a one-shot `updateCheckCmd`; `updateAvailableMsg` sets `m.updateLatest`; `renderFooter` prepends `⬆ hop <v> available · hop self-update` in navigation mode only.
- `HOP_NO_UPDATE_CHECK=1` disables all passive checking; dev builds never check and `self-update` refuses them outright.
- Tests: version comparison, asset naming (pinned against what release.yml emits), checksum verify, tar.gz/zip extraction, executable replacement, end-to-end update + check-only + already-latest + checksum-mismatch against an `httptest` GitHub; notice cache behaviour incl. stale refresh and window-claiming on failure; hardening regressions (hostile tag, URL allowlist, redirect downgrade, oversize payloads); four TUI footer tests.

## Discoveries

- shenv's release workflow and hop's already emit identically-shaped asset names, so the port is drop-in — the only naming divergence was shenv shipping `README.md` inside the archive, which doesn't affect extraction (it matches on base name).
- shenv caches under `~/.shenv/`; hop keeps everything in `os.UserConfigDir()/hop/` (db, config, known-hosts pointer), so the cache moved there and its test isolates all three platform variables (`AppData`, `XDG_CONFIG_HOME`, `HOME`) the way `internal/config`'s tests do.

## Decisions

- **Decision:** No Ed25519 release signing — the checksums file is the only authenticator.
  - **Reason:** Explicit user call ("i just want the update code"). Signing would add a mandatory key-generation + `RELEASE_SIGNING_KEY` secret step, and a release published without it fails hard.
  - **Trade-off:** Anyone who can edit release assets (compromised GitHub account/token) can regenerate archive *and* checksums and the update still verifies. The transport hardening (https-only, GitHub-host allowlist across redirects) is what remains. Adding signing later is additive: port `sign.go` + `cmd/release-sign` and make the `.sig` asset required.
- **Decision:** The TUI hint is a footer line naming the *command*, not a key binding.
  - **Reason:** Updating swaps the running binary mid-session; that should not be one keystroke away in a pane-heavy TUI.
- **Decision:** The hint leads the footer legend rather than trailing it, and only in navigation mode.
  - **Reason:** The legend is truncated to the window width, so a trailing hint would vanish on exactly the narrow terminals it still matters on. In a focused pane the footer is the shell's key legend and news has no business competing with it.
- **Decision:** TUI uses `Refresh` (refresh-then-report) while the CLI uses `NotifyIfAvailable` (report-cached, then refresh).
  - **Reason:** The TUI call already runs off the UI thread in a `tea.Cmd`, so it can afford the bounded wait and converge on the first launch instead of the second; a CLI command must not add latency to `hop list`.

## Failures

## Validation

- `go build ./...`, `go vet ./...`, `gofmt -l .`, `go test ./...` — all clean.
- Live end-to-end against the real repo: a binary stamped `0.1.0` ran `hop check-update` ("A newer version is available: 0.2.1") and then `hop self-update`, which downloaded the real v0.2.1 darwin/arm64 archive, verified its checksum, swapped itself, and reported `hop 0.2.1 (af2a75f, …)`. A `0.2.1`-stamped build reports "You're on the latest version"; a dev build is refused.

## Remaining risks

- The Windows rename-aside path is exercised only by `TestReplaceExecutable` on a Windows runner; the `.old` sweep has never been observed on a real Windows install.
- The updater is pinned to `p-arndt/hop` and to release.yml's asset naming in two places (`ArchiveName`/`ChecksumsName` vs. the workflow's `hop_${VERSION}_${GOOS}_${GOARCH}` + `sha256sum` line). Renaming assets in the workflow silently breaks every shipped updater; `TestArchiveAndBinaryNames` is the tripwire.

## Handoff

- If release signing is wanted later, port `internal/update/sign.go` + `cmd/release-sign` from shenv and add the sign/selfcheck steps to release.yml — see shenv `docs/release-signing.md` for the rotation procedure.
