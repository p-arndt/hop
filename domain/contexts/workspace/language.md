---
type: language
context: workspace
status: draft
code:
  - internal/tui/**
---

# Ubiquitous language — workspace

### Session

**Is:** everything hop is holding open for **one host** — its shell tabs, its editor
tabs, its file browser, its tunnels, and whether the content area is split.

**Is not:** [[connection]]'s `sshx.Session`, which is one shell channel. This session
*contains* several of those. **This is the sharpest collision in the codebase.**

**Lifecycle:** opened on first connect → lives across every view change → survives
being left → closed only by disconnecting the host or emptying it.

**In code:** `internal/tui/session.go` — the unexported `session` struct, keyed by alias.

**Not to be confused with:** `sshx.Session`. See the glossary.

### Mode

**Is:** where the keystrokes go — list, shell, scrollback, browser, editor.

**Is not:** what is drawn. A mode says nothing about layout. And it is not
[[keyboard]]'s *layer*, though each mode selects one.

**In code:** `paneMode` in `internal/tui/model.go`.

### Focus

**Is:** which column, half and tab currently holds the keyboard, plus what the pointer
is holding.

**In code:** the embedded `focus` struct in `model`.

### Layout

**Is:** the arrangement facts — total width, the sidebar, the list column, the tree
column, the content area, the split.

**Rule:** only a resize or a column toggle writes it.

**In code:** the embedded `layout` struct; `recomputeLayout`, `relayout`.

### Rect / frame

**Is:** a rectangle of screen (`rect`) and the set of rectangles one host's content is
drawn into (`frame`), including its two halves when split.

**In code:** `internal/tui/layout.go`.

### Column

**Is:** one vertical region of the screen: the sidebar, the host list, the file tree,
the content area.

**Rule:** a column that does not fit is not drawn. The browser falls back to a full
pane below 96 columns.

### Split

**Is:** the content area divided into two halves, each with its own tab strip, so two
files are readable side by side.

**Rule:** closing the split **keeps the file you were reading**, not the half that
happened to be focused.

**In code:** `session.split` / `splitRight`, `openSplit`, `collapseSplit`.

### Tab

**Is:** one named thing inside a session's content area — a shell tab or an editor tab.

**In code:** `shellTab`, `editorTab`; the strip is rendered by `tabs.go`.

### Shell tab

**Is:** another shell on an already-connected host — a new channel, no new handshake.

### Editor tab

**Is:** `${EDITOR:-vi}` running on a remote pty against one remote file. Nothing was
downloaded to open it.

**Is not:** a local editor. That is the VS Code action, which is a different thing.

### Status bar

**Is:** the permanent line above the footer naming the host, the mode, the directory or
file, and the machine behind the alias.

**Rule:** it is never optional and never scrolls away — it is the answer to "where are
my keystrokes going".

**In code:** `internal/tui/status.go`.

### Status (line) / generation

**Is:** a transient message with a kind (so its colour is never sniffed back out of the
text) and a generation stamp.

**Rule:** clearing bumps the generation, so an expiry timer in flight can never fire
against a newer message. Remote-derived text is stripped of control characters first.

**In code:** `statusKind`, `setStatus`, `clearStatus`.

### Card

**Is:** a modal overlay that takes the keyboard — help, settings, the palette, the
menu, a confirmation, the importer, the tunnel manager, the auth prompt, the host-key
prompt.

**In code:** `overlay.go` and one file per card.

### Sidebar

**Is:** the narrow column hop shows when there is room for it, toggled by a key that is
reserved in **every** mode.

**In code:** `layout.sidebarOn`, `toggleSidebar`.
