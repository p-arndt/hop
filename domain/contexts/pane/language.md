---
type: language
context: pane
status: draft
code:
  - internal/terminal/**
---

# Ubiquitous language — pane

### Pane

**Is:** one embedded terminal — a VT emulator bound to a remote shell channel, drawn
in a rectangle of hop's screen.

**Is not:** the rectangle itself (that is [[workspace]]'s *rect* / *frame*), and not
the tab that names it.

**Lifecycle:** opened on a session → live → scrolled back / live → closed. Closing a
pane never closes the [[connection]] under it.

**In code:** `internal/terminal/terminal.go` — `terminal.Pane`.

**Not to be confused with:** `keys.Pane`, which is a keyboard *layer* — the set of
bindings that apply while a pane holds the keyboard.

### Emulator

**Is:** the VT state machine that turns the server's byte stream into a screen.

**In code:** `vt.SafeEmulator`, held by `Pane.emu`.

### Alternate screen

**Is:** the full-screen mode a program like vim or htop switches into. While it is on,
there is no scrollback to speak of and hop types nothing of its own.

**Rule:** leaving the alternate screen drops the mouse mode with it.

### Scrollback

**Is:** the history above the live screen, and the offset the user has lifted the view
by. Offset 0 is live.

**Is not:** anything the remote knows about. Scrolling back sends nothing.

**In code:** `Pane.scrollOffset`.

### Mouse tracking

**Is:** the level of mouse reporting the *remote program* has asked for. hop forwards
only what was asked for.

**Lifecycle:** off → a tracking level requested by the remote → off again on full
reset or on leaving the alternate screen.

**In code:** `internal/terminal/mouse.go` — `mouseState`, `trackingLevel`.

### Bracketed paste

**Is:** wrapping pasted text in markers so the far end knows it was pasted rather than
typed, when the program has asked for that.

**In code:** `internal/terminal/paste.go` — `pasteState`.

### Application cursor keys

**Is:** DECCKM (DECSET 1). A full-screen program asks for its cursor keys as SS3
(`ESC O A`) rather than CSI (`ESC [ A`); vim, less and mc all do. A *modified* cursor key
is unaffected — xterm sends `CSI 1;<mod>A` either way.

**Rule:** the mode belongs to the program that asked, so leaving the alternate screen drops
it, like [[mouse-tracking]] and [[bracketed-paste]].

**In code:** `internal/terminal/cursorkeys.go` — `cursorKeysState`, read by `keyBytes`.

### OSC 7 / cwd

**Is:** the remote shell reporting its working directory, and the directory hop
therefore believes the user is in.

**Is why:** `ctrl+o ctrl+o` can open VS Code in the right place and the browser can
start in the right directory.

**In code:** `internal/terminal/cwd.go`, `oscScanner`; read under `cwdMu`.

### Shell integration hook

**Is:** the one line hop submits at pane startup so the shell will report its cwd.

**Rule:** it must be **invisible** — its echo is erased, and the erase is *declined*
rather than attempted when the geometry is untrustworthy or the host has printed into
the span.

**In code:** `startupLine`, `eraseEcho`, `trackCwd`.

### OSC 52 / clipboard write

**Is:** a remote program asking for text to be put on the *local* clipboard.

**Rule:** size-capped and dropped when oversized; writes are serialised; arriving in
pieces across chunks is normal and must be reassembled.

**In code:** `internal/terminal/clipboard.go`, `clipSink`, `clipQueue`.

### Selection

**Is:** a region of the pane's screen the user has marked with the mouse, to copy
locally.

**Is not:** the remote program's own selection. hop's selection is drawn over the
emulator's screen and never sent.

**In code:** `internal/terminal/selection.go`.

### Send / refused write

**Is:** handing bytes to the far end. It can be refused — the input queue is capped,
and only a far end that stopped reading fills it.

**Rule:** a refusal is reported to the user. Dropped keystrokes must never be silent.

**In code:** `Pane.send`, `inChunk`, `inQueue`; `model.reportInput` on the other side.
