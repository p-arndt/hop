---
type: context
name: pane
title: pane
subdomain: core
status: draft
owner: p-arndt
code:
  - internal/terminal/**
  - internal/tui/paste.go
  - internal/tui/selection.go
  - internal/tui/cursor.go
  - internal/tui/clipboard.go
  - internal/tui/mouse.go
relationships:
  - context: connection
    role: downstream
    pattern: CF
    via: sshx.Session
  - context: keyboard
    role: downstream
    pattern: CF
    via: keys.Layer Pane / Scrollback / Editor / DeadPane
---

# pane

## Purpose

A pane is **somebody else's program, running on a server, drawn inside hop**. Vim,
htop, a login shell, `less` — they behave as they would in a real terminal, including
the parts that are easy to get wrong: the alternate screen, mouse reporting,
bracketed paste, OSC clipboard writes, cursor shape.

Being an *embedded* terminal adds a second job: hop must know things about the program
that a real terminal never needs to know — most importantly **which directory the user
is in**, so the rest of hop can act on it.

## Strategic classification

| Dimension | Value | Why |
|---|---|---|
| Domain type | core | Fidelity is the whole illusion; a pane that mangles vim makes hop unusable |
| Business model role | engagement | The demo is a person editing a file on a box, inside a tab |
| Evolution | custom-built | `x/vt` for emulation, everything above it is hop's |

The consequence: **invest, and test against real programs.** This is where an
almost-right implementation is worse than none.

## Domain roles

**Execution context** (bytes go out, screens come back) and **translation layer**
(hop's key and mouse events into the wire's escape sequences, and the wire's escape
sequences back into facts hop can use).

## Ubiquitous language

The terms live in [`language.md`](language.md).

## Inbound communication

| Message | Type | From | Relationship | Note |
|---|---|---|---|---|
| SendKey / SendMouse | command | workspace | customer/supplier | may be refused; a refusal must be surfaced, never silent |
| Paste(text) | command | workspace | customer/supplier | bracketed if the remote asked for it |
| Resize(w,h) | command | workspace | customer/supplier | propagates to the remote pty |
| Scroll / scrollback motions | command | workspace | customer/supplier | lifts the view off the live bottom |

## Outbound communication

| Message | Type | To | Relationship | Note |
|---|---|---|---|---|
| View (rendered screen) | query result | workspace | open host service | one string per frame |
| cwd changed | event | workspace, files | published language | from OSC 7 |
| clipboard write | event | workspace → local clipboard | published language | from OSC 52, size-capped |
| onOutput | event | workspace | published language | wakes a redraw |
| alt-screen / mouse-mode changed | event | workspace | published language | changes what keys mean |

## Business decisions

- **A dropped keystroke is never silent.** If a write is refused, the user is told.
- **The remote decides whether the mouse is live.** hop forwards mouse events only at
  the tracking level the program asked for, and forgets the mouse on a full reset or
  when the program leaves the alternate screen.
- **Shell integration must not be visible.** The cwd hook is one submitted line at
  startup; its echo is erased from the screen — and *declined* if the geometry is
  untrustworthy or the host printed into the span. Better no erase than a corrupted
  screen.
- **Nothing is typed onto the alternate screen.** A pane running vim never receives
  hop's own keystrokes.
- **Clipboard payloads are capped.** A generous cap, but a remote may not fill local
  memory through OSC 52. Oversized payloads are dropped, not truncated.
- **Everything the remote sends is untrusted text.** Control characters are stripped
  before any remote-derived string reaches a status line or a label.
- **Writes are serialised through one goroutine.** An SSH write blocks when the remote
  window fills; the UI update loop must never block on it.
- **Scrollback is a view, not a mode of the remote.** Scrolling back never sends
  anything to the far end.

## Aggregates

| Aggregate | Protects | Doc |
|---|---|---|
| Pane | emulator/session pairing, the input queue's ordering, cwd and clipboard concurrency invariants | _not yet written_ |

## Assumptions

- The TUI-side selection, paste, cursor and mouse files are anchored here rather than
  in [[workspace]] because they speak the terminal's language, not the layout's. They
  are also where the split is least obvious.
- Editor tabs are modelled as panes (a pty running `$EDITOR`), with only their tab
  identity owned by [[workspace]].

## Verification metrics

- Bug reports of the form "X behaves differently in hop than in my terminal" → the
  emulation model has a gap; each one names a term that should exist here.
- Growth of terminal-shaped logic under `internal/tui/` → the boundary is leaking
  upward.

## Open questions

- `sshx.Session.Resize` sits in [[connection]] but is only ever meaningful to a pane.
  Should the pty move here?
- Is scrollback its own small model (it has a keyboard layer of its own) or part of
  the pane?
