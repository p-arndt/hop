---
id: install
title: Install
group: Start
---

One line, either platform. The script picks the archive for your OS and CPU, verifies its
checksum, drops `hop` on your `PATH` and prints where it went.

```bash
# macOS / Linux -> /usr/local/bin, or ~/.local/bin when that is not writable
curl -fsSL https://raw.githubusercontent.com/p-arndt/hop/main/scripts/install.sh | sh
```

```powershell
# Windows -> %LOCALAPPDATA%\Programs\hop, added to your user PATH
irm https://raw.githubusercontent.com/p-arndt/hop/main/scripts/install.ps1 | iex
```

Both take the same options — a specific release, a directory of your own:

```bash
sh scripts/install.sh --version 0.11.0 --dir ~/bin
```

```powershell
.\scripts\install.ps1 -Version 0.11.0 -Dir C:\tools\bin -NoModifyPath
```

`$HOP_INSTALL_DIR` / `$env:HOP_INSTALL_DIR` sets the directory too. On unix the script never
edits a shell profile: if the directory is not on your `PATH` it prints the one line to add.
On Windows it appends to the user `PATH` in the registry unless you pass `-NoModifyPath`, so
a new terminal has `hop`.

Prefer to do it by hand? Grab the archive from the
[latest release](https://github.com/p-arndt/hop/releases/latest), unpack it and move the
binary onto your `PATH`. Every release ships a `hop_<version>_checksums.txt` for
`sha256sum -c`.

## From source

Needs [Go 1.26+](https://go.dev/dl/) and optionally [`just`](https://github.com/casey/just) ≥ 1.39.

```bash
git clone https://github.com/p-arndt/hop.git && cd hop
just build          # -> ./hop   (or: go build -o hop .)
just build-release  # stripped + version-stamped
just install        # build it, then put it on your PATH
```

`just install` is the same installer with `--from-source` / `-FromSource`: it builds the
checkout, stamps it with `VERSION` and the current commit, and installs it exactly where the
downloading path would have.

## Updating

`hop self-update` replaces the binary in place, wherever the installer put it — see
[Update](#update).
