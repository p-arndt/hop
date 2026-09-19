# hop — task runner
#
# Install `just`:  winget install Casey.Just   (or  brew install just)
# List recipes:    just
#
# Shared recipes (build, test, fmt, ci, release, …) live in .just/, copied from
# ~/coding/just-common. Edit them there and run `just sync-common`; this file
# only holds what is specific to hop.

import '.just/common.just'
import '.just/go.just'
import '.just/release.just'

BIN_NAME := "hop"
BUILDINFO_PKG := "hop/internal/buildinfo"

# Build this checkout and put it on the PATH: /usr/local/bin when writable and
# ~/.local/bin otherwise (or $HOP_INSTALL_DIR) on unix, %LOCALAPPDATA%\Programs\hop
# on Windows, where the installer also adds that directory to the user PATH. The
# same scripts, run without --from-source, install a released binary instead.

# Install hop from source onto your PATH.
[unix]
install:
    sh scripts/install.sh --from-source

[windows]
install:
    pwsh.exe -NoLogo -NoProfile -File scripts/install.ps1 -FromSource

# Run the test suite including the Docker-backed end-to-end tests: a real Ubuntu
# sshd with a real pam_google_authenticator, dialled by hop's own SSH engine and
# by its authentication card (see internal/dockerenv). Needs a running Docker;
# the image takes about a minute to build the first time.
test-e2e:
    HOP_DOCKER_E2E=1 go test ./... -count=1

# Regenerate index.html, README.md and KEYBINDINGS.md from docs/*.md. The
# markdown under docs/ is the only source for all three; `go test ./...` fails
# if a generated file has drifted from it (tools/docsgen).

# Render the docs.
docs:
    go run ./tools/docsgen

# Fail if a generated doc is out of date.
docs-check:
    go run ./tools/docsgen -check

# Re-records assets/demo.gif and assets/screens/*.png from demo/hop.tape. Needs
# `vhs` (brew install vhs). Nothing real is recorded: the script points hop at a
# throwaway HOME and a fake SSH server (tools/demoserver) that invents the hosts,
# the files and the command output, so anyone can re-record it safely.

# Record the README demo.
demo:
    node scripts/demo.mjs
