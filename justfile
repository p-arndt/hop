# hop — task runner
#
# Install `just`:  winget install Casey.Just   (or  brew install just,
#                  or  go install github.com/casey/just@latest)
# List recipes:    just            (or  just --list)
#
# Requires just >= 1.39 (for the `read()` function used to stamp VERSION).
#
# Layout:
#   main.go            — the `hop` CLI/TUI entry point   (-> hop / hop.exe)
#   internal/…         — store, tui, ssh, sftp, terminal, buildinfo
#   VERSION            — single source of truth for the version (stamped into the binary)
#
# Portability: recipe bodies are plain command invocations with no shell syntax,
# so the same line runs under both `sh` and PowerShell. Where a task genuinely
# needs shell logic (fmt-check, clean) it is split into `[unix]` and `[windows]`
# recipes of the same name — just picks the right one per platform. Everything
# else that would otherwise need a shell (reading VERSION, the timestamp, the
# binary suffix) uses just's built-in functions instead.
#
# Shebang recipes are deliberately not used: just does not translate
# `#!/usr/bin/env <interp>` on Windows (casey/just#1549), so such a recipe would
# only run on unix.

# Windows has no `sh`, which is just's default shell, so point it at PowerShell
# there. (`set windows-shell` is deprecated in favour of `[windows]` on `set
# shell`, but it parses on every just version that has the OS attributes below,
# and the replacement does not.)
set windows-shell := ["pwsh.exe", "-NoLogo", "-NoProfile", "-Command"]

# Static, libc-free binaries — the same thing the release workflow ships, so a
# local build behaves like a released one. Exported, because there is no
# portable way to set an environment variable inline in a recipe body.
export CGO_ENABLED := "0"

# The output name, which is the one thing about a Go build that is not already
# platform-independent.
BIN := if os_family() == "windows" { "hop.exe" } else { "hop" }

# Version metadata stamped into internal/buildinfo. All three come from just's
# built-ins rather than from shell commands, so they resolve identically on
# every platform. `git` is the exception — it is the same invocation in sh and
# PowerShell, so a backtick is safe here.
_VERSION := trim(read("VERSION"))
_COMMIT := `git rev-parse --short HEAD`
_DATE := datetime_utc('%Y-%m-%dT%H:%M:%SZ')

_LDFLAGS := "-s -w" + \
    " -X hop/internal/buildinfo.Version=" + _VERSION + \
    " -X hop/internal/buildinfo.Commit=" + _COMMIT + \
    " -X hop/internal/buildinfo.Date=" + _DATE

# Default: show the recipe list.
default:
    @just --list

# ---------------------------------------------------------------------------
# Dev
# ---------------------------------------------------------------------------

# Run the CLI/TUI from source, passing through any args:  just run list
run *ARGS:
    go run . {{ARGS}}

# Build a plain dev binary -> hop (hop.exe on Windows); version reports as "dev".
build:
    go build -o {{BIN}} .

# Build a stripped, static release binary stamped with the current VERSION.
build-release:
    go build -trimpath -ldflags "{{_LDFLAGS}}" -o {{BIN}} .

# ---------------------------------------------------------------------------
# Quality
# ---------------------------------------------------------------------------

# Run the test suite.
test:
    go test ./...

# Vet for suspicious constructs.
vet:
    go vet ./...

# Format all Go code.
fmt:
    gofmt -w .

# gofmt exits 0 whether or not anything is unformatted, so the check has to be on
# its output — the one task here that needs real shell logic, hence a recipe per
# platform.

# Fail if any Go file is unformatted.
[unix]
fmt-check:
    @out="$(gofmt -l .)"; if [ -n "$out" ]; then echo "$out"; echo "unformatted files (run: just fmt)" >&2; exit 1; fi

[windows]
fmt-check:
    @$out = gofmt -l .; if ($out) { $out; Write-Error "unformatted files (run: just fmt)"; exit 1 }

# Run every check the way CI should.
ci: fmt-check vet test

# ---------------------------------------------------------------------------
# Release
# ---------------------------------------------------------------------------

# Print the current version (read from the VERSION file).
version:
    @echo "{{_VERSION}}"

# Accepts a bump keyword or an explicit version:
#   just set-version patch        just set-version 0.2.0

# Stamp a version into the VERSION file without committing.
set-version BUMP="patch":
    node scripts/set-version.mjs {{BUMP}}

# Bumps the version (patch|minor|major, or an explicit x.y.z), stamps VERSION,
# commits, tags and pushes -> triggers the release workflow, which builds static
# binaries for windows, linux and darwin (amd64 + arm64). Examples:
#   just release            just release minor            just release 1.0.0

# Cut a release.
release BUMP="patch":
    node scripts/release.mjs {{BUMP}}

# ---------------------------------------------------------------------------
# Docs
# ---------------------------------------------------------------------------

# Re-records assets/demo.gif and assets/screens/*.png from demo/hop.tape. Needs
# `vhs` (brew install vhs). Nothing real is recorded: the script points hop at a
# throwaway HOME and a fake SSH server (tools/demoserver) that invents the hosts,
# the files and the command output, so anyone can re-record it safely.

# Record the README demo.
demo:
    node scripts/demo.mjs

# ---------------------------------------------------------------------------
# Housekeeping
# ---------------------------------------------------------------------------

# Remove build artifacts.
[unix]
clean:
    rm -rf hop dist build

[windows]
clean:
    -Remove-Item -Force hop.exe -ErrorAction SilentlyContinue
    -Remove-Item -Recurse -Force dist, build -ErrorAction SilentlyContinue
