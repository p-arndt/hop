---
id: "20260826-235144-install-scripts-put-hop-on-the-path"
title: "Install scripts put hop on the PATH"
status: "completed"
updated: "2026-08-26T23:52:04+02:00"
base_commit: "4d117b28b713aa0e330d62fe5026143cb95c2221"
branch: "main"
agent: null
tags: ["distribution", "install", "scripts"]
files:
  - "README.md"
  - "TODO.md"
  - "docs/03-install.md"
  - "docs/60-dev.md"
  - "index.html"
  - "justfile"
  - "scripts/install.ps1"
  - "scripts/install.sh"
  - "scripts/install_test.go"
---

# Install scripts put hop on the PATH

## Goal

Ship a one-line installer per platform (download or build from source) that places hop on the PATH.

## Scope

- release.yml names archives hop_<version>_<os>_<arch>.zip|.tar.gz plus one hop_<version>_checksums.txt; the installers rebuild those names client-side, so a rename in the workflow silently breaks them. scripts/install_test.go asserts both sides.

- The installers hard-code p-arndt/hop, the same owner/repo internal/update passes to selfupdate; the test fails if update.go stops naming it.

- Windows installs by renaming any existing hop.exe to hop.exe.old before the move — a running .exe can be renamed but not overwritten, and main.go's update.CleanupLeftovers() already deletes that file on the next start.

- install.ps1 rejects -FromSource when $PSScriptRoot is empty: that is precisely the irm|iex case, where there is no checkout to build.

## Discoveries

## Decisions

- **Decision:** One script per platform does both jobs: download a release, or build the checkout with --from-source/-FromSource.
  - **Reason:** just install and the curl|sh one-liner then share every code path — the install directory, the PATH handling, the version stamping — instead of drifting apart.
  - **Trade-off:** The ldflags string is now spelled in three places: justfile, release.yml and both installers.

- **Decision:** A missing sha256 tool or checksums file warns and continues; a mismatch or an unlisted archive is fatal.
  - **Reason:** Refusing to install because busybox has no shasum is worse than an unverified install the user was told about.

## Failures

## Validation

- install.ps1 -FromSource and install.sh --from-source both built and installed 0.11.0 into temp dirs; install.ps1 downloaded hop_0.11.0_windows_amd64.zip from the real release and the checksum verified

- gofmt -l . clean; go vet ./...; go test ./... — all passed (includes the new hop/scripts package)

- go run ./tools/docsgen -check — README.md and index.html in sync with docs/

## Remaining risks

- The unix download path could not be exercised from this Windows machine: uname -s there is MINGW64, so download_release() exits early. Only the from-source half of install.sh ran end to end.

## Handoff

- If the release workflow ever renames an archive, scripts/install_test.go fails first — fix the installers, not the test.
