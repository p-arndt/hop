---
type: context
name: files
title: files
subdomain: supporting
status: draft
owner: p-arndt
code:
  - internal/filebrowser/**
  - internal/sftpx/**
  - internal/pathx/**
relationships:
  - context: connection
    role: downstream
    pattern: ACL
    via: sftpx.Open(*ssh.Client), then only the filebrowser.Client port
  - context: keyboard
    role: downstream
    pattern: CF
    via: keys.Layer Browser
---

# files

## Purpose

Browsing and moving files **on the server**, without downloading them to look at them.
The user walks a remote tree, marks a set, and copies, moves, renames, deletes or
transfers them — and when they want to *read* one, hop opens an editor on the box
rather than fetching a copy.

The pressure this context is under is that every name on screen came from a machine
hop does not control, and every operation is destructive if it is wrong.

## Strategic classification

| Dimension | Value | Why |
|---|---|---|
| Domain type | supporting | Necessary and well-shaped, but SFTP browsing is a solved problem others also do |
| Business model role | engagement | "browse its files, edit one on the box" is half the demo |
| Evolution | product | `pkg/sftp` underneath; hop's value is the tree, the marks and the guard rails |

The consequence: **simple, careful code.** The interesting part is the safety rules,
not the model depth.

## Domain roles

**Execution context** with a strong **quarantine** role: it is the place remote-derived
names are sanitised before they reach anything else.

## Ubiquitous language

The terms live in [`language.md`](language.md).

## Inbound communication

| Message | Type | From | Relationship | Note |
|---|---|---|---|---|
| key event | command | workspace | customer/supplier | forwarded verbatim; the browser resolves its own layer |
| Options replaced | command | workspace | customer/supplier | settings edits replace them wholesale |
| Resize | command | workspace | customer/supplier | |

## Outbound communication

| Message | Type | To | Relationship | Note |
|---|---|---|---|---|
| OpenFileMsg | command | workspace → pane | published language | absolute remote path, plus `Beside` |
| Msg{Alias, Body} | event | workspace | published language | every browser message is tagged with its alias |
| View | query result | workspace | open host service | |
| transfer progress | event | workspace | published language | real byte counts, sampled per tick |

## Business decisions

- **Never download to preview.** `enter` on a file opens an editor on the remote box.
  The only local copies are an explicit download (`d`) or the scratch fetch behind
  "open with a local app" (`o`).
- **Remote names are hostile input.** Names that are unsafe, normalise to a reserved
  name, or carry control characters are refused or stripped before display.
- **An executable is not opened locally** without an explicit `OpenWith` override.
  Downloading one is allowed; running one by accident is not.
- **One typed-name guard before anything destructive**, and **no recursive delete**.
- **A move onto an existing name is refused. A copy overwrites.** The asymmetry is
  deliberate and must not be "tidied up".
- **Overwrites are confirmed in both directions** — upload and download.
- **Marks are global across the tree**, keyed by absolute path, and survive a refresh.
  Every operation acts on the marked set, or on the cursor entry when nothing is marked.
- **A batch stops at the first failure**, names what got through, and leaves the rest
  marked so the same keystroke resumes.
- **One transfer at a time**, with real byte counts from the client's callbacks — not
  a spinner pretending to be progress.
- **Copy and move are recursive, and symlinks are recreated, never followed.**
- **Directories open in place**, lazily listed and cached; the whole browser still
  speaks flat row indices so scrolling and mouse hit-testing stay simple.

## Aggregates

| Aggregate | Protects | Doc |
|---|---|---|
| Browser | cursor-within-rows, mark set consistency across refresh, one in-flight transfer | _not yet written_ |
| Tree | lazy listing, cache coherence, flat row projection | _not yet written_ |

## Assumptions

- **`sftpx` and `filebrowser` are one context, not two.** They share one language
  (entry, path, transfer) and `filebrowser.Client` is a port `sftpx` happens to
  satisfy. If a second implementation of that port ever appears, this should split.
- `internal/pathx` (tilde expansion) is anchored here because that is where local
  paths enter, but it is a generic utility with no domain meaning.

## Verification metrics

- A second implementation of `filebrowser.Client` appearing → the port is real and the
  context should split into a browser and a transport.
- Any file operation added without a matching guard-rail rule above → the safety model
  is being outgrown.

## Open questions

- Should the local-side concerns (download dir, `OpenWith`, the scratch dir,
  macOS quarantine) be a separate small context? They are the only place files
  cross onto the user's own machine.
