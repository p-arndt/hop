---
id: "20260724-195133-make-hop-build-and-run-on-macos-agent-transport-abstraction"
title: "Make hop build and run on macOS: agent transport, multi-platform release, universal justfile"
status: "completed"
updated: "2026-07-24T20:10:00+02:00"
base_commit: "6c95cf4d3d60f0555a53300b3b63b6ea3f7099a3"
branch: "main"
agent: null
tags: ["build", "ci", "cross-platform", "macos", "release", "ssh", "tooling"]
files:
  - ".github/workflows/ci.yml"
  - ".github/workflows/release.yml"
  - "TODO.md"
  - "justfile"
  - "internal/config/config_test.go"
  - "internal/filebrowser/filebrowser_test.go"
  - "internal/sshx/agent_unix.go"
  - "internal/sshx/agent_unix_test.go"
  - "internal/sshx/agent_windows.go"
  - "internal/sshx/sshx.go"
  - "internal/tui/settings_test.go"
---

# Make hop build and run on macOS: agent transport, multi-platform release, universal justfile

## Goal

hop did not compile on macOS. Get build/vet/test green on darwin without regressing Windows, ship darwin/linux release artifacts, and make the justfile run on all three platforms.

## Discoveries

- The only compile error was `winio.DialPipe` being `//go:build windows` — the module builds everywhere, so `go.mod` looked fine. Everything else was already portable (`os.UserConfigDir`, `os.UserHomeDir`, a `runtime.GOOS` switch for local-open).
- `config`/`tui` test isolation set `%AppData%` and `$XDG_CONFIG_HOME` only. darwin ignores XDG and reads `$HOME/Library/Application Support`, so the suite failed *and* wrote a real `~/Library/Application Support/hop/config.json`.
- Every target cross-compiles under `CGO_ENABLED=0`, so the release no longer needs a per-OS build matrix.
- just has built-ins that remove most of the justfile's need for a shell: `os_family()`, `read()` (1.39+), `datetime_utc()` (1.30+), and `export VAR := …`. `[unix]`/`[windows]` recipes may share a name with no extra setting (verified on just 1.57).
- `just ci` had apparently never been run: `filebrowser_test.go` was unformatted, so `fmt-check` failed on a clean tree.

## Decisions

- **Decision:** Split the dial into `agent_windows.go` (named pipe) / `agent_unix.go` (`$SSH_AUTH_SOCK`), both exposing `dialAgent()`; `AgentAuth` stays platform-agnostic.
  - **Reason:** Build tags keep winio off non-Windows; the agent protocol above the transport is identical.
  - **Trade-off:** Error text varies per platform, so callers must not match on it (none do).
- **Decision:** An unset `$SSH_AUTH_SOCK` is its own error, not a dial against a guessed path.
  - **Reason:** No well-known fallback exists — the socket lives in a per-session temp dir.
- **Decision:** Test isolation also sets `HOME`, and `isolate()` returns `filepath.Dir(Path())` instead of assuming `<tmp>/hop`.
  - **Reason:** Deriving the dir from the code under test is correct on all three platforms.
- **Decision:** Release builds all six targets from one Linux runner, gated on a windows/ubuntu/macos test matrix; `.tar.gz` for unix, `.zip` for Windows.
  - **Reason:** One runner means no archive drift; the tarball preserves the exec bit a zip would drop.
- **Decision:** Make justfile recipe bodies plain command invocations valid in both `sh` and PowerShell, push the platform-varying values into just built-ins, and split only `fmt-check`/`clean` with `[unix]`/`[windows]`.
  - **Reason:** Keeps one recipe per task instead of two, so the platforms cannot drift apart.
  - **Trade-off:** Requires just >= 1.39 for `read()`; `CGO_ENABLED=0` is exported globally (there is no portable inline env syntax), so dev builds are static too.
- **Decision:** Keep the deprecated `set windows-shell` rather than the recommended `[windows]` attribute on `set shell`.
  - **Reason:** Setting-level attributes are much newer than the recipe-level ones this file already needs; the deprecated form parses on every version that supports the rest.

## Failures

- First draft used `[ "$GOOS" = windows ] && bin=hop.exe` in the build script — under `set -e` that exits the job on every non-Windows target. Replaced with `if/then`.
- Considered `#!/usr/bin/env node` shebang recipes for the shell-dependent tasks; just does not translate `/usr/bin/env` on Windows (casey/just#1549), so they would be unix-only. Used OS attributes instead.
- First justfile pass broke `just --list`: just takes the *last* comment line as a recipe's doc string, so multi-line explanations surfaced as fragments. Explanations moved above a blank line, one-line summaries directly above each recipe.

## Validation

- `go build`/`vet`/`test` green on darwin/arm64 (go1.26.5); `GOOS=windows` and `GOOS=linux` build + vet clean.
- New build-tagged `agent_unix_test.go`: unset sock, dead socket, live `net.Listen("unix")` happy path, `AgentAuth` error surfacing.
- Release build script dry-run locally: 6 archives + checksums; extracted darwin binary is `rwxr-xr-x` and reports its stamped version. Both workflow YAMLs parse.
- `hop list` opens the SQLite store on macOS (exit 0).
- Every justfile recipe exercised on macOS with just 1.57: `--list`, `version`, `run`, `build`, `build-release` (binary reports the stamped version/commit/date), `fmt-check` (correctly failed, then passed), `ci`, `clean`.

## Remaining risks

- The pre-fix test run left `~/Library/Application Support/hop/config.json` (a test artifact, `"editor": "helix"`) on the dev machine; needs deleting by hand.
- No live SSH session verified on macOS — the local agent has no identities loaded.
- The workflow changes are unexercised until a release actually runs.
- The `[windows]` halves of `fmt-check` and `clean` are untested — no PowerShell on the dev machine. `clean` is carried over verbatim from the previous justfile; `fmt-check` is new PowerShell.

## Handoff

- `action.NewTab` (wt.exe + pwsh) is Windows-only and unused — delete it or build-tag it before anything calls it.
- `ci.yml` runs `go vet`/`go test` directly, not `just ci`, so `fmt-check` is still not enforced anywhere.
