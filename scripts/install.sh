#!/bin/sh
# hop installer — macOS / Linux.
#
#   curl -fsSL https://raw.githubusercontent.com/p-arndt/hop/main/scripts/install.sh | sh
#   sh scripts/install.sh --version 0.11.0 --dir ~/bin
#   sh scripts/install.sh --from-source          # build this checkout instead
#
# POSIX sh on purpose: this has to run under dash, ash and busybox, not just bash.
set -eu

REPO="p-arndt/hop"
VERSION="latest"
DIR="${HOP_INSTALL_DIR:-}"
FROM_SOURCE=0

usage() {
    cat <<'USAGE'
usage: install.sh [--version <x.y.z>] [--dir <path>] [--from-source]

  --version   release to install (default: the latest one)
  --dir       where to put the binary (default: /usr/local/bin if writable,
              otherwise ~/.local/bin; or $HOP_INSTALL_DIR)
  --from-source
              build the checkout this script lives in instead of downloading
USAGE
}

die() { echo "install.sh: $*" >&2; exit 1; }

while [ $# -gt 0 ]; do
    case "$1" in
        --version) [ $# -ge 2 ] || die "--version needs a value"; VERSION="${2#v}"; shift 2 ;;
        --dir) [ $# -ge 2 ] || die "--dir needs a value"; DIR="$2"; shift 2 ;;
        --from-source) FROM_SOURCE=1; shift ;;
        -h|--help) usage; exit 0 ;;
        *) usage >&2; die "unknown option: $1" ;;
    esac
done

# --- where it goes -----------------------------------------------------------

if [ -z "$DIR" ]; then
    if [ -d /usr/local/bin ] && [ -w /usr/local/bin ]; then
        DIR="/usr/local/bin"
    else
        DIR="$HOME/.local/bin"
    fi
fi
# ~ only expands when the shell wrote it, not when it came out of an option.
case "$DIR" in "~/"*) DIR="$HOME/${DIR#\~/}" ;; esac

mkdir -p "$DIR" || die "cannot create $DIR"
[ -w "$DIR" ] || die "$DIR is not writable — re-run with --dir ~/.local/bin, or with sudo"

# --- fetch helpers -----------------------------------------------------------

if command -v curl >/dev/null 2>&1; then
    fetch() { curl -fsSL "$1" -o "$2"; }
    fetch_stdout() { curl -fsSL "$1"; }
elif command -v wget >/dev/null 2>&1; then
    fetch() { wget -qO "$2" "$1"; }
    fetch_stdout() { wget -qO- "$1"; }
else
    die "need curl or wget"
fi

# --- the two ways to get a binary -------------------------------------------

build_from_source() {
    command -v go >/dev/null 2>&1 || die "--from-source needs Go on the PATH (https://go.dev/dl/)"

    # The script lives in scripts/, so the module root is its parent.
    root="$(CDPATH='' cd -- "$(dirname -- "$0")/.." && pwd)"
    [ -f "$root/go.mod" ] || die "--from-source must run from a hop checkout"

    version="$(tr -d ' \t\r\n' < "$root/VERSION")"
    commit="$(git -C "$root" rev-parse --short HEAD 2>/dev/null || echo none)"
    date="$(date -u +%Y-%m-%dT%H:%M:%SZ)"

    echo "building hop $version from $root"
    CGO_ENABLED=0 go build -C "$root" -trimpath \
        -ldflags "-s -w -X hop/internal/buildinfo.Version=$version -X hop/internal/buildinfo.Commit=$commit -X hop/internal/buildinfo.Date=$date" \
        -o "$tmp/hop" .
}

download_release() {
    case "$(uname -s)" in
        Linux) os="linux" ;;
        Darwin) os="darwin" ;;
        *) die "unsupported OS: $(uname -s) — build from source with --from-source" ;;
    esac
    case "$(uname -m)" in
        x86_64|amd64) arch="amd64" ;;
        arm64|aarch64) arch="arm64" ;;
        *) die "unsupported architecture: $(uname -m) — build from source with --from-source" ;;
    esac

    if [ "$VERSION" = "latest" ]; then
        VERSION="$(fetch_stdout "https://api.github.com/repos/$REPO/releases/latest" \
            | sed -n 's/.*"tag_name"[[:space:]]*:[[:space:]]*"v\{0,1\}\([^"]*\)".*/\1/p' | head -n 1)"
        [ -n "$VERSION" ] || die "could not resolve the latest release — pass --version"
    fi

    archive="hop_${VERSION}_${os}_${arch}.tar.gz"
    base="https://github.com/$REPO/releases/download/v$VERSION"

    echo "downloading $archive"
    fetch "$base/$archive" "$tmp/$archive" || die "download failed: $base/$archive"

    verify_checksum "$archive"
    tar -xzf "$tmp/$archive" -C "$tmp" || die "could not extract $archive"
    [ -f "$tmp/hop" ] || die "$archive did not contain a hop binary"
}

# Every release ships one checksums file; a missing sha256 tool is a warning,
# not a failure — refusing to install would be worse than an unverified install
# the user was told about.
verify_checksum() {
    if command -v sha256sum >/dev/null 2>&1; then
        sum="$(sha256sum "$tmp/$1" | cut -d' ' -f1)"
    elif command -v shasum >/dev/null 2>&1; then
        sum="$(shasum -a 256 "$tmp/$1" | cut -d' ' -f1)"
    else
        echo "warning: no sha256sum/shasum — skipping checksum verification" >&2
        return 0
    fi

    if ! fetch "$base/hop_${VERSION}_checksums.txt" "$tmp/checksums.txt" 2>/dev/null; then
        echo "warning: no checksums file for v$VERSION — skipping verification" >&2
        return 0
    fi

    want="$(sed -n "s/^\([0-9a-f]\{64\}\) [ *]\{0,1\}$1\$/\1/p" "$tmp/checksums.txt" | head -n 1)"
    [ -n "$want" ] || die "$1 is not listed in the checksums file"
    [ "$sum" = "$want" ] || die "checksum mismatch for $1 (got $sum, want $want)"
    echo "checksum ok"
}

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT INT TERM

if [ "$FROM_SOURCE" -eq 1 ]; then
    build_from_source
else
    download_release
fi

chmod 0755 "$tmp/hop"
# Replace by rename so a running hop keeps its open inode instead of being
# rewritten under itself.
mv -f "$tmp/hop" "$DIR/hop" || die "could not install into $DIR"

echo "installed $("$DIR/hop" version) -> $DIR/hop"

# --- PATH --------------------------------------------------------------------

case ":${PATH:-}:" in
    *":$DIR:"*) exit 0 ;;
esac

if [ "${SHELL##*/}" = "fish" ]; then
    line="fish_add_path $DIR"
else
    case "${SHELL##*/}" in
        zsh) rc="~/.zshrc" ;;
        bash) rc="~/.bashrc" ;;
        *) rc="~/.profile" ;;
    esac
    line="echo 'export PATH=\"$DIR:\$PATH\"' >> $rc"
fi

cat >&2 <<EOM

$DIR is not on your PATH. Add it:

  $line
  exec \$SHELL -l
EOM
