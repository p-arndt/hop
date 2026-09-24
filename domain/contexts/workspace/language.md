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

**Is:** where the keystrokes go — the sidebar, a shell, scrollback, the browser, an editor,
the terminal panel.

**Is not:** what is drawn. A mode says nothing about layout. And it is not
[[keyboard]]'s *layer*, though each mode selects one.

**In code:** `paneMode` in `internal/tui/model.go`.

### Focus

**Is:** which column, half and tab currently holds the keyboard, plus what the pointer
is holding.

**In code:** the embedded `focus` struct in `model`.

### Layout

**Is:** the arrangement facts — total width, the [[Sidebar]] and its two boxes, the
[[Content area]], the terminal panel, the split.

**Rule:** a resize, the dock or tree toggle, or a change of [[Tab in front]] writes it. Pane
sizes depend on the window, the session and those two toggles alone, never on which host is
in front or where the keyboard is.

**In code:** the embedded `layout` struct; `recomputeLayout`, `relayout`, `syncView`.

### Tab in front

**Is:** the tab of the host in front that the [[Content area]] shows: a [[Shell tab]], the
[[Files tab]] or an [[Editor tab]]. The keyboard decides it where it can — a shell's keys
mean its shell tab — and the sidebar having the keyboard changes nothing.

**Is not:** a [[Mode]]. The mode says where the keys go; the tab in front says what is drawn.

**In code:** `session.front`, `frontOf`, `front()`, `frontTarget`.

### Terminal panel

**Is:** a shell under the files tab or an editor tab, the way an IDE keeps one under its
editor. One per session, started on demand in the directory under the tree's cursor.

**Is not:** a [[Shell tab]]. It is its own shell, so the shell tabs are never resized
by it and it is never resized by them. Hiding it keeps it running.

**Rule:** its height is a share of the body the user sets (drag the edge, `ctrl+o +`/`-`),
held between its floor and the files' floor.

**In code:** `internal/tui/drawer.go` — `session.drawer`, `modeDrawer`, `frame.drawer`.

### Rect / frame

**Is:** a rectangle of screen (`rect`) and the set of rectangles the body is drawn into
(`frame`): the hosts box, the tree box, the content area and its two halves when split, the
terminal panel, and whether the hosts box floats.

**Rule:** one frame feeds both the drawing and the pointer; a test holds them together.

**In code:** `internal/tui/layout.go`.

### Column

**Is:** one vertical region of the screen: the [[Sidebar]] or the [[Content area]]. There
are never more than two.

**Rule:** a column that does not fit is not drawn. The sidebar is a column from 94 columns
up (32 for it, 62 for the content), unless the user hid it.

### Split

**Is:** the content area divided into two halves, each with its file's path above it, so two
files are readable side by side.

**Rule:** closing the split **keeps the file you were reading**, not the half that
happened to be focused.

**In code:** `session.split` / `splitRight`, `openSplit`, `collapseSplit`.

### Tab

**Is:** one named thing a host holds that the content area can show — a shell tab, the files
tab, an editor tab. Listed under its host in the sidebar in the order it was opened, which
the leader's digits count.

**In code:** `shellTab`, `editorTab`, the browser; `hostTabs`, `session.order`.

### Shell tab

**Is:** another shell on an already-connected host — a new channel, no new handshake.

### Start dir

**Is:** the directory a new shell tab or browser is started in. By default the host's
[[fleet]] default dir; a reconnect's restored directory; the shell's cwd for `ctrl+o f`;
the browser's cursor directory for `S`.

**Rule:** a shell tab is started in one the same way whichever of those chose it — as the
default dir, typed as the startup line's `cd`. There is no second mechanism.

**In code:** the `startDir` of `shellCmd` / `openBrowserCmd`; `openShellIn`, `openBrowserAt`.

### Files tab

**Is:** the host's SFTP browser as a tab. Its tree is in the sidebar's [[Tree box]] and the
content area shows a [[Preview]]; with no docked sidebar the tree takes the content area.

### Preview

**Is:** the first 64 KiB of the file under the tree's cursor, read when the cursor stops on
it, off the UI's goroutine. A directory, a binary, a device or anything larger is described
instead of read.

**In code:** `internal/filebrowser/preview.go`, read through `sftpx.Client.ReadHead`.

### Editor tab

**Is:** `${EDITOR:-vi}` running on a remote pty against one remote file. Nothing was
downloaded to open it.

**Is not:** a local editor. That is the VS Code action, which is a different thing.

### Crumb

**Is:** the footer's left side, naming the host, the tab and the directory or file the
keyboard is at — or, in the sidebar, the host and tab under the cursor.

**Rule:** it is never optional and never scrolls away — it is the answer to "where are
my keystrokes going". A transient status borrows its place while it lasts; crumbs that do
not fit go whole from the end.

**In code:** `footerLeft`, `crumbs` in `internal/tui/status.go`.

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

**Is:** the card over every target on every connected host, then every host — called
**go to** on screen. With no query it is a tree: each open host and what is open on it,
numbered as the leader's digits count them, then the hosts with nothing open. Typing
flattens it into one list, most recently used first, narrowed over alias, path and name.
`enter` lands exactly on the row. With no query the cursor starts on the place used last
that is not where the keyboard is, so opening it and pressing `enter` goes back.

**Is not:** the host list's filter, which narrows the column in place; nor the palette,
which lists actions, not places.

**In code:** `hostSwitchUI`, `openHostSwitch`, `switchRows` in `internal/tui/hostswitch.go`.

### Last host

**Is:** the host that was in front before the current one. Going back to it swaps the two,
alt-tab style, landing on its [[Last place]].

**Rule:** it changes only when the host in front changes, never on a change of mode.

**In code:** `focus.last` / `focus.shown`, kept by `noteHost`; `backToLastHost`.

### Sidebar

**Is:** the left column: the host list in its [[Hosts box]] and, under a files tab or an
editor tab, the [[Tree box]]. Every host is a row; the host in front is **opened out** into
its tabs and its tunnels, and any other can be opened out with `→`.

**Rule:** **docked** — a column beside the content — whenever the window has 94 columns and
the user has not hidden it (`ctrl+o b`). Otherwise it **floats** over the content while it
has the keyboard, and is off screen the rest of the time. Its width never depends on where
the keyboard is.

**In code:** `docked`, `listWidth`, `sidebarFloats`, `sidebarOn`, `toggleSidebar`; the rows
in `buildRows` and `renderList`.

### Hosts box

**Is:** the sidebar's box of host and tab rows. Alone it takes the body; with the tree box
under it, it takes what its rows need up to 40% of the body.

**In code:** `frame.list`, `hostsBoxH`.

### Tree box

**Is:** the sidebar's box under the hosts that holds the [[Files tab]]'s tree, on the files
tab and on an editor tab. `ctrl+o t` hides it and brings it back.

**In code:** `frame.tree`, `treeInSidebar`, `treeBoxOn`.

### Content area

**Is:** the column beside the sidebar: the [[Tab in front]], or with no host in front the
[[Recent places]] and the details of the host under the cursor.

**In code:** `frame.content`, `renderRight`.

### Recent places

**Is:** the places used last across every host, offered in the content area while no host
is in front. The cursor reaches them by going up from the first host.

**In code:** `recentPlaces`, `model.recent`, `recentAt`, `renderStart`.
