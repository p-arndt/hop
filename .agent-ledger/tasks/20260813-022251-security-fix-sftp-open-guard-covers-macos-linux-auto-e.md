---
id: "20260813-022251-security-fix-sftp-open-guard-covers-macos-linux-auto-e"
title: "Security fix: SFTP open guard covers macOS/Linux auto-execute types"
status: "completed"
updated: "2026-08-13T02:23:28+02:00"
base_commit: "8ca6f40785d44a70ebc698dc35dc58eb098f3572"
branch: "main"
agent: null
tags: ["filebrowser", "security", "sftp"]
files:
  - "TODO.md"
  - "internal/filebrowser/filebrowser.go"
  - "internal/filebrowser/filebrowser_test.go"
  - "internal/filebrowser/quarantine_darwin.go"
  - "internal/filebrowser/quarantine_darwin_test.go"
  - "internal/filebrowser/quarantine_other.go"
---

# Security fix: SFTP open guard covers macOS/Linux auto-execute types

## Goal

Stop a hostile SSH server from getting local code execution when the user presses o on a planted file: the executable-extension refusal list only covered Windows ShellExecute types while open/xdg-open run on macOS/Linux too.

## Scope

## Discoveries

- Found by /security-review of the whole repo; the finder and an adversarial validator both confirmed it (validator confidence 7/10). The codebase's own comment at internal/filebrowser/filebrowser.go openInApp showed the attack was in the threat model but the executableExts blocklist listed only Windows ShellExecute types, while openCmd also runs open (darwin) and xdg-open (linux).

- Strongest vectors need no execute bit: .terminal (Terminal.app profile executes its CommandString on open), .fileloc/.inetloc, and Linux .desktop Exec= lines. os.Create's 0666 mode kills .command/.tool but not those.

## Decisions

- **Decision:** Keep executableExts one unconditional set checked on every GOOS instead of per-platform lists.
  - **Reason:** Refusing a .desktop file on macOS costs nothing; missing an extension on the right platform costs code execution. Also keeps the existing guard/test structure.
  - **Trade-off:** Slightly over-blocks: e.g. .js was already refused everywhere despite being Windows-specific.

- **Decision:** Add com.apple.quarantine xattr (flags 0083;hex-mtime;hop;) to scratch copies in fetch() on darwin, failing closed if Setxattr errors.
  - **Reason:** Defense in depth: Gatekeeper then adjudicates file types the extension list does not know about. hop's plain os.Create download never picks up quarantine on its own, unlike browser downloads.
  - **Trade-off:** Only the o/open scratch path is quarantined; explicit d downloads to DownloadDir are not.

## Failures

## Validation

- go test ./... — all packages pass (darwin)

- go vet ./internal/filebrowser/ — clean

- GOOS=linux and GOOS=windows go build ./... — quarantine no-op stub compiles

## Remaining risks

- Blocklist model remains: an auto-execute extension not on the list still reaches the default handler; macOS quarantine mitigates that only on darwin.

## Handoff

- fakeClient.Download in filebrowser_test.go must actually create the local file (os.WriteFile) now: fetch() quarantines the copy after download and Setxattr needs a real file on darwin.

- Consider quarantining explicit d downloads too, and/or inverting the guard to an allowlist of known-inert viewer types.
