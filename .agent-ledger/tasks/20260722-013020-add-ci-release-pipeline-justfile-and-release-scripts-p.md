---
id: "20260722-013020-add-ci-release-pipeline-justfile-and-release-scripts-p"
title: "Add CI/release pipeline, justfile, and release scripts (ported from shenv)"
status: "completed"
updated: "2026-07-22T01:30:44+02:00"
base_commit: "199c1da544828097321f14e51899ed6e6bfb6541"
branch: "main"
agent: null
tags: ["build", "ci", "release", "tooling"]
files:
  - ".github/dependabot.yml"
  - ".github/workflows/ci.yml"
  - ".github/workflows/release.yml"
  - ".gitignore"
  - "VERSION"
  - "cliff.toml"
  - "internal/buildinfo/buildinfo.go"
  - "internal/buildinfo/buildinfo_test.go"
  - "justfile"
  - "main.go"
  - "scripts/release.mjs"
  - "scripts/set-version.mjs"
---

# Add CI/release pipeline, justfile, and release scripts (ported from shenv)

## Goal

Give hop a just task runner, GitHub Actions CI + release workflow, dependabot, git-cliff changelog, and node release scripts modeled on the shenv repo, adapted to hop's layout.

## Scope

- Unlike shenv, hop has NO self-updater / signing (no cmd/release-sign, no embedded pubkey), so all of shenv's release-signing steps and RELEASE_SIGNING_KEY were dropped from the release workflow and .gitignore.

## Discoveries

- hop is Windows-only: internal/sshx/sshx.go calls winio.DialPipe unconditionally, so CGO-free cross-compile to linux/darwin fails. Verified windows/amd64 and windows/arm64 both build. CI runs on windows-latest only; release builds only those two Windows arches (zip archives).

- hop's entry point is the repo-root package (go build .), module 'hop', binary hop.exe — not shenv's cmd/shenv. justfile ldflags target hop/internal/buildinfo. Added internal/buildinfo (ported) + wired 'hop version' / --version / -v into main.go. VERSION file is single source of truth, starts at 0.1.0.

## Decisions

- **Decision:** Release workflow split into two jobs: build on windows-latest, publish on ubuntu-latest.
  - **Reason:** orhun/git-cliff-action is a Docker container action that only runs on Linux runners, but the Windows-only binaries must be built on windows-latest. Build job uploads dist/ as an artifact; release job downloads it, generates the changelog, and creates the GitHub release.
  - **Trade-off:** Adds artifact upload/download hop between jobs; version metadata is passed via job outputs.

## Failures

## Validation

- go test ./internal/buildinfo — passed; scripts/set-version.mjs resolve patch/minor/major — verified (0.1.0->0.1.1/0.2.0/1.0.0); windows/amd64 + windows/arm64 'go build .' — both OK; gofmt clean on all newly added files.

## Remaining risks

- Pre-existing WIP already in the working tree (NOT part of this task): internal/tui (hostkey.go untracked, model.go etc.) does not compile, and internal/filebrowser_test.go is gofmt-dirty. This makes the whole repo fail 'go build ./...' / 'go vet ./...' right now, so the new CI + release 'Test' steps will go red until that tui work is finished. Not introduced here.

- Repo currently does not compile due to unrelated in-progress internal/tui work, so CI/release Test steps fail until that is fixed. Release workflow is untested against real GitHub Actions (no push).

## Handoff

- Finish the internal/tui WIP so 'go vet ./...' passes, then push to trigger CI. Optionally add a README.md so it can be bundled into release archives (currently zip contains only hop.exe).
