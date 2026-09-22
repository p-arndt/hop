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

**Is:** the arrangement facts — total width, the list column, the tree column, the
content area, the split.

**Rule:** a resize, a column toggle or a change of [[View]] writes it. Pane sizes depend
on the window and the session alone, never on which host is in front or where the keyboard is.

**In code:** the embedded `layout` struct; `recomputeLayout`, `relayout`, `syncView`.

### View

**Is:** what a host in front shows: the **shell view** (one shell at full width) or the
**files view** (the tree beside the open files, or the browser alone at full width).

**Is not:** a [[Mode]]. The mode says where the keys go; the view says what is drawn. In the
host list the view is the one the host was last in.

**In code:** `model.filesView`, `session.filesView`.

### Terminal panel

**Is:** a shell under the files in the files view, the way an IDE keeps one under its
editor. One per session, started on demand in the directory under the tree's cursor.

**Is not:** a [[Shell tab]]. It is its own shell, so the shell view's tabs are never resized
by it and it is never resized by them. Hiding it keeps it running.

**Rule:** its height is a share of the body the user sets (drag the edge, `ctrl+o +`/`-`),
held between its floor and the files' floor.

**In code:** `internal/tui/drawer.go` — `session.drawer`, `modeDrawer`, `frame.drawer`.

### Rect / frame

**Is:** a rectangle of screen (`rect`) and the set of rectangles one host's content is
drawn into (`frame`), including its two halves when split.

**In code:** `internal/tui/layout.go`.

### Column

**Is:** one vertical region of the screen: the sidebar, the host list, the file tree,
the content area.

**Rule:** a column that does not fit is not drawn. The tree is a column only beside open
files and only from 92 columns up (a quarter of the window, 30 to 44 wide, plus 62 for the files).

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

### Start dir

**Is:** the directory a new shell tab or browser is started in. By default the host's
[[fleet]] default dir; a reconnect's restored directory; the shell's cwd for `ctrl+o f`;
the browser's cursor directory for `S`.

**Rule:** a shell tab is started in one the same way whichever of those chose it — as the
default dir, typed as the startup line's `cd`. There is no second mechanism.

**In code:** the `startDir` of `shellCmd` / `openBrowserCmd`; `openShellIn`, `openBrowserAt`.

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

**Is:** a modal overlay that takes the keyboard — help, settings, the palette, the host
switcher, the menu, a confirmation, the importer, the tunnel manager, the auth prompt, the host-key
prompt.

**In code:** `overlay.go` and one file per card.

### Target

**Is:** one place on a connected host the keyboard can be put back into — a shell tab, the
browser, an editor tab, the terminal panel — or, as a row of the [[Switcher]], the host
itself or its tunnels. Named by alias, kind and the tab's stable id, never its index.

**Is not:** a [[Tab]]. The browser and the panel are targets and not tabs.

**Rule:** a dead session has no targets; reaching it goes through its host, which reconnects.

**In code:** `target`, `openTargets`, `jumpTo` in `internal/tui/targets.go`.

### Last place

**Is:** the target on a host that last had the keyboard — or, on a host never used yet, its
shell, browser or editor, in that order. Entering a host lands there.

**Rule:** every target is stamped when the keyboard moves into it, after every message; the
last place is the one stamped last that is still open.

**In code:** `focus.used` / `useSeq`, kept by `noteTarget`; `lastPlace`, `enterHost`.

### Switcher

**Is:** the card over every target on every connected host, most recently used first, then
every host — connected ones first — narrowed by typing over alias, path and name. `enter`
lands exactly on the row. With no query the cursor starts on the row after where the
keyboard is, so opening it and pressing `enter` goes back.

**Is not:** the host list's filter, which narrows the column in place; nor the palette,
which lists actions, not places.

**In code:** `hostSwitchUI`, `openHostSwitch`, `switchRows` in `internal/tui/hostswitch.go`.

### Session bar

**Is:** the header row: the host in front and a chip per target open on it, the keyboard's
highlighted, then the other connected hosts. With no host in front, every connected host.
Every chip is a way to its target.

**Rule:** laid out once (`sessionBar`) and read by both the renderer and the pointer, so what
is clicked is what is drawn. The far end gives way to a "+N" that opens the [[Switcher]]; the
keyboard's chip gives way last. A transient status takes the right side while it lasts.

**In code:** `internal/tui/sessionbar.go`.

### Last host

**Is:** the host that was in front before the current one. Going back to it swaps the two,
alt-tab style, landing on its [[Last place]].

**Rule:** it changes only when the host in front changes, never on a change of mode.

**In code:** `focus.last` / `focus.shown`, kept by `noteHost`; `backToLastHost`.

### Sidebar

**Is:** the host list: a column while no host is in front, drawn **over** the host's view
while one is. On screen exactly while it has the keyboard.

**Is not:** toggled by a key any more; [[Switcher]] is the way to hop without it.

**In code:** `sidebarOn`, `sidebarFloats`, `listWidth`.
