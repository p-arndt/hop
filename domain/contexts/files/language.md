---
type: language
context: files
status: draft
code:
  - internal/filebrowser/**
  - internal/sftpx/**
---

# Ubiquitous language — files

### Browser

**Is:** one remote directory tree the user is walking, on one host — its cursor, its
marks, its sort order and whatever operation is in flight.

**Is not:** a window or a column. Where it is drawn is [[workspace]]'s business.

**In code:** `internal/filebrowser/filebrowser.go` — `filebrowser.Browser`.

### Entry

**Is:** one thing in a remote directory — its name, whether it is a directory, its
size, its modification time, whether it is a symlink.

**In code:** `sftpx.Entry`.

### Client (the port)

**Is:** the ten operations a browser needs from a remote filesystem — list, home,
mkdir, remove, rename, copy, move, upload, download, close.

**Is not:** [[connection]]'s `sshx.Client` (the transport) and not `sftpx.Client`
(the implementation). It is the browser's own vocabulary for "a filesystem I can
reach", and it is the seam a fake slots into for tests.

**In code:** `filebrowser.Client`; satisfied by `sftpx.Client`.

### Tree / row

**Is:** the browser's view of the filesystem — directories opened **in place**, lazily
listed and cached. A **row** is one visible line of that tree.

**Rule:** every index in this context — cursor, scroll, hit-testing — is a **row
index**, not an entry index. That is what keeps mouse handling simple.

**In code:** `internal/filebrowser/tree.go` — `node`, `rows`.

### Mark

**Is:** a file or directory the user has selected with `space`/`a`, keyed by absolute
remote path, global across the whole tree and surviving a refresh.

**In code:** `Browser.marks`, `internal/filebrowser/marks.go`.

### Targets

**Is:** what an operation acts on — the marked set, or the cursor entry alone when
nothing is marked. Every destructive key resolves through it.

**In code:** `Browser.targets()`.

### Transfer

**Is:** one upload or download in flight, with real byte counts reported by the
client's progress callbacks and sampled per UI tick.

**Rule:** one at a time. The fast `io.ReaderFrom` path must stay intact for uploads.

**In code:** `internal/filebrowser/transfer.go`, `sftpx` counting reader/writer.

### Batch

**Is:** one keystroke acting on many targets, shown as one progress line (`3/7 · name`).

**Rule:** stop at the first failure, name what got through, leave the rest marked.

### Copy / Move

**Is:** server-side recursive duplication or relocation of the targets into a directory.

**Rule, deliberately asymmetric:** **copy overwrites a taken name; move refuses it.**
Symlinks are **recreated, not followed**. Self-destructive cases (into itself, onto
itself) are refused.

**In code:** `sftpx.Copy` / `sftpx.Move`, `internal/filebrowser/copymove.go`.

### Overlay

**Is:** the prompt that owns the keyboard while a name is being typed or a destructive
action confirmed.

**Rule:** its label is stripped of control characters — it usually contains a remote
name.

**In code:** `internal/filebrowser/prompt.go` — `overlay`, `overlayKind`.

### Unsafe name

**Is:** a remote name hop refuses to act on locally — path-escaping, control
characters, or something that normalises to a reserved device name.

**In code:** the name guards in `filebrowser.go`; `quarantine_darwin.go` for the
macOS attribute on downloaded files.

### Open with / download dir

**Is:** the two ways a remote file reaches the local machine — the local application
`o` hands a scratch copy to, and the directory `d` puts a real download in.

**Rule:** an executable is not opened locally without an explicit `OpenWith` override.

**In code:** `filebrowser.Options`.

### Open file (beside)

**Is:** the request that a remote file be opened in an editor **on the server** —
as another tab, or `Beside` the one already open.

**Is not:** a download. Nothing is fetched.

**In code:** `filebrowser.OpenFileMsg`.
