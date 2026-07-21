# hop — task runner
#
# Install `just`:  winget install Casey.Just   (or  go install github.com/casey/just@latest)
# List recipes:    just            (or  just --list)
#
# Layout:
#   main.go            — the `hop` CLI/TUI entry point   (-> hop.exe)
#   internal/…         — store, tui, ssh, sftp, terminal, buildinfo
#   VERSION            — single source of truth for the version (stamped into the binary)
#
# Note: hop is Windows-only (it uses Windows named pipes), so the release
# builds target windows/amd64 and windows/arm64.

# Run recipes through PowerShell on Windows so multi-line bodies and env work.
set windows-shell := ["pwsh.exe", "-NoLogo", "-NoProfile", "-Command"]

# ldflags shared by the release builds: stamp version metadata + strip symbols.
_LDFLAGS := "-s -w -X hop/internal/buildinfo.Version=$(Get-Content VERSION -Raw).Trim() -X hop/internal/buildinfo.Commit=$(git rev-parse --short HEAD) -X hop/internal/buildinfo.Date=$(Get-Date -AsUTC -Format o)"

# Default: show the recipe list.
default:
    @just --list

# ---------------------------------------------------------------------------
# Dev
# ---------------------------------------------------------------------------

# Run the CLI/TUI from source, passing through any args:  just run list
run *ARGS:
    go run . {{ARGS}}

# Build a plain dev binary -> hop.exe (version reports as "dev").
build:
    go build -o hop.exe .

# Build a stripped, statically-linked release binary for the host platform,
# stamped with the current VERSION -> hop.exe.
build-release:
    $env:CGO_ENABLED = "0"; go build -trimpath -ldflags "{{_LDFLAGS}}" -o hop.exe .

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

# Verify formatting without writing changes (fails if anything is unformatted).
fmt-check:
    @if (gofmt -l .) { Write-Error "unformatted files (run: just fmt)"; exit 1 }

# Run every check the way CI should.
ci: fmt-check vet test

# ---------------------------------------------------------------------------
# Release
# ---------------------------------------------------------------------------

# Print the current version (read from the VERSION file).
version:
    @(Get-Content VERSION -Raw).Trim()

# Stamp a version into the VERSION file without committing. Accepts a bump
# keyword or an explicit version. Examples:
#   just set-version patch        just set-version 0.2.0
set-version BUMP="patch":
    node scripts/set-version.mjs {{BUMP}}

# Cut a release: bump the version (patch|minor|major, or an explicit x.y.z),
# stamp VERSION, commit, tag, and push -> triggers the release workflow which
# builds the static Windows binaries. Examples:
#   just release            just release minor            just release 1.0.0
release BUMP="patch":
    node scripts/release.mjs {{BUMP}}

# ---------------------------------------------------------------------------
# Housekeeping
# ---------------------------------------------------------------------------

# Remove build artifacts.
clean:
    -Remove-Item -Force hop.exe -ErrorAction SilentlyContinue
    -Remove-Item -Recurse -Force dist, build -ErrorAction SilentlyContinue
