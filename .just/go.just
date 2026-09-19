# go.just — Go CLI recipes. Synced from github.com/p-arndt/just-common.
#
# The importing justfile sets:
#   BIN_NAME      := "hop"                        (required) binary name
#   BUILDINFO_PKG := "hop/internal/buildinfo"     (required) package with Version/Commit/Date
# and may override (needs `set allow-duplicate-variables`):
#   MAIN          := "./cmd/hop"                  main package, default "."
#
# Requires just >= 1.39 for read().

MAIN := "."

# Static, libc-free binaries, the same as the release workflow ships. Exported
# because there is no portable way to set an env var inline in a recipe.
export CGO_ENABLED := "0"

_BIN := BIN_NAME + if os_family() == "windows" { ".exe" } else { "" }

# just built-ins instead of shell commands, so these resolve identically on every
# platform. `git` is the same invocation in sh and PowerShell.
_VERSION := trim(read("VERSION"))
_COMMIT := `git rev-parse --short HEAD`
_DATE := datetime_utc('%Y-%m-%dT%H:%M:%SZ')
_LDFLAGS := "-s -w" + \
    " -X " + BUILDINFO_PKG + ".Version=" + _VERSION + \
    " -X " + BUILDINFO_PKG + ".Commit=" + _COMMIT + \
    " -X " + BUILDINFO_PKG + ".Date=" + _DATE

# Run from source, passing args through:  just run list
run *ARGS:
    go run {{MAIN}} {{ARGS}}

# Build a dev binary (version reports as "dev").
build:
    go build -o {{_BIN}} {{MAIN}}

# Build a stripped, static binary stamped with VERSION.
build-release:
    go build -trimpath -ldflags "{{_LDFLAGS}}" -o {{_BIN}} {{MAIN}}

# Run the test suite.
test:
    go test ./...

# Vet for suspicious constructs.
vet:
    go vet ./...

# Format all Go code.
fmt:
    gofmt -w .

# gofmt exits 0 either way, so the check is on its output: real shell logic,
# hence one recipe per platform.

# Fail if any Go file is unformatted.
[unix]
fmt-check:
    @out="$(gofmt -l .)"; if [ -n "$out" ]; then echo "$out"; echo "unformatted files (run: just fmt)" >&2; exit 1; fi

[windows]
fmt-check:
    @$out = gofmt -l .; if ($out) { $out; Write-Error "unformatted files (run: just fmt)"; exit 1 }

# Lint with golangci-lint (uses .golangci.yml when present).
lint:
    golangci-lint run

# Every check CI runs. Override with `set allow-duplicate-recipes` to add lint etc.
ci: fmt-check vet test

# Remove build artifacts.
[unix]
clean:
    rm -rf {{_BIN}} dist build

[windows]
clean:
    -Remove-Item -Force {{_BIN}} -ErrorAction SilentlyContinue
    -Remove-Item -Recurse -Force dist, build -ErrorAction SilentlyContinue
