---
id: install
title: Install
group: Start
---

Grab the archive for your platform from the
[latest release](https://github.com/p-arndt/hop/releases/latest) and put `hop` somewhere on
your `PATH`.

```bash
# macOS / Linux
tar -xzf hop_*_darwin_arm64.tar.gz
sudo mv hop /usr/local/bin/
```

```powershell
# Windows
Expand-Archive hop_*_windows_amd64.zip -DestinationPath .
# then move hop.exe onto your PATH
```

Every release ships a `hop_<version>_checksums.txt`, so you can verify with `sha256sum -c`.

## From source

Needs [Go 1.26+](https://go.dev/dl/) and optionally [`just`](https://github.com/casey/just) ≥ 1.39.

```bash
git clone https://github.com/p-arndt/hop.git && cd hop
just build          # -> ./hop   (or: go build -o hop .)
just build-release  # stripped + version-stamped
```
