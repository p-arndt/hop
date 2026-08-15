---
id: "20260815-032335-replace-internal-update-with-the-selfupdate-library"
title: "Replace internal/update with the selfupdate library"
status: "completed"
updated: "2026-08-15T03:23:46+02:00"
base_commit: "60b5b759d790e4f2aee7db29e05a2f149c84e326"
branch: "main"
agent: null
tags: ["dependency", "refactor", "update"]
files:
  - "go.mod"
  - "go.sum"
  - "internal/update/hardening_test.go"
  - "internal/update/notice.go"
  - "internal/update/notice_test.go"
  - "internal/update/update.go"
  - "internal/update/update_test.go"
  - "main.go"
---

# Replace internal/update with the selfupdate library

## Goal

Swap hop's hand-rolled updater for github.com/p-arndt/selfupdate, keeping every user-visible string, command and file location identical.

## Scope

## Discoveries

- The library is the extraction of this very code: hop's release.yml already emits exactly the default layout.Archive names (hop_<v>_<goos>_<goarch>.tar.gz/.zip + hop_<v>_checksums.txt), and the derived defaults for the opt-out env (HOP_NO_UPDATE_CHECK), the cache path (<UserConfigDir>/hop/update-check.json) and the user agent (hop-updater) all match what hop used. UpdateCmd is the single divergence — the library derives 'hop update', hop's subcommand is 'hop self-update'.

- Supplying a non-nil Config.HTTP does NOT lose the https-only redirect hardening: ghrelease.NewHTTPClient only fills in CheckRedirect when it is nil, so hop's 60s download client keeps the policy.

## Decisions

- **Decision:** Keep internal/update as a thin shim around the library instead of importing selfupdate at the call sites.
  - **Reason:** main.go and internal/tui/commands.go stay unchanged apart from one call, the Owner/Repo/UpdateCmd config lives in one place, and a pre-v1 API break is absorbed in one file. The shim also takes a Config so tests can pass the APIBase/StatePath/ExecutablePath seams while production passes the zero value.
  - **Trade-off:** One extra indirection layer over a library that is already small.

- **Decision:** cmdUpdate keeps calling SelfUpdate rather than the library's Run().
  - **Reason:** Run() prints its own wording and returns an error on dev builds even in check-only mode; SelfUpdate preserves hop's existing output byte-for-byte.

## Failures

## Validation

- go vet ./... — passed; go test ./... — passed; ./hop version and ./hop check-update on a dev build print the unchanged refusal; a 0.0.1-stamped build ran check-update against the real GitHub API and reported 0.7.0.

## Remaining risks

- github.com/p-arndt/selfupdate has no tags at all, so go.mod pins a pseudo-version (v0.0.0-20260815010710-47f21e81fa4c). The library is pre-v1 and documents that its API may move.

- internal/update/update_test.go's assetNames() duplicates release.yml's naming rule and is the only tripwire left for asset-name drift — the old TestArchiveAndBinaryNames is gone with the hand-rolled code.

- The Windows rename-aside/.old sweep now lives in the library and is exercised only by its CI, not by hop's tests.

## Handoff

- No release signing, again by explicit user call ('ohne signing'). The library supports it as a one-field Verifier, and adding it later is non-breaking as long as the successor key is ADDED to the list rather than swapped in — swapping the sole key permanently bricks every binary already in the field. Supersedes nothing in the 2026-07-25 self-update entry; that entry's handoff (port shenv's sign.go) is now obsolete — the library replaces it.

- If the library tags a release, replace the pseudo-version pin in go.mod with it.
