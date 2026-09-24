---
type: context
name: workspace
title: workspace
subdomain: core
status: draft
owner: p-arndt
code:
  - internal/tui/model.go
  - internal/tui/layout.go
  - internal/tui/view.go
  - internal/tui/session.go
  - internal/tui/list.go
  - internal/tui/keys.go
  - internal/tui/status.go
  - internal/tui/commands.go
  - internal/tui/msgs.go
  - internal/tui/menu.go
  - internal/tui/palette.go
  - internal/tui/hostswitch.go
  - internal/tui/targets.go
  - internal/tui/help.go
  - internal/tui/settings.go
  - internal/tui/confirm.go
  - internal/tui/overlay.go
  - internal/tui/guidance.go
  - internal/tui/landing.go
  - internal/tui/theme.go
  - internal/tui/actions.go
  - internal/tui/vscode.go
relationships:
  - context: fleet
    role: downstream
    pattern: CF
    via: store.Host, ordered Hosts()
  - context: connection
    role: downstream
    pattern: ACL
    via: connect / lost / reconnect messages, and the Prompter port it implements
  - context: pane
    role: downstream
    pattern: CF
    via: View / SendKey / Resize
  - context: files
    role: downstream
    pattern: ACL
    via: filebrowser.Msg, tagged by alias
  - context: keyboard
    role: downstream
    pattern: CF
    via: keys.Map, one Layer per mode
---

# workspace

## Purpose

**Where am I, and where are my keystrokes going?** workspace owns everything the user
sees at once: which host is in front, which of its tabs the content area shows, whether
the keyboard is there or in the sidebar, and the crumb that answers that question
permanently.

It is also the context that makes hop's other promise true — **leaving a pane never
tears down what is inside it.** A session is what hop holds open for one host: its
shells, its editors, its browser, its tunnels. Hopping away and back finds all of it
exactly where it was.

## Strategic classification

| Dimension | Value | Why |
|---|---|---|
| Domain type | core | "everything you left behind is still exactly where you left it" is the product |
| Business model role | engagement | Disorientation is the failure mode of a TUI that embeds other programs |
| Evolution | custom-built | Bubble Tea underneath; the session/layout/focus model is hop's own |

The consequence: **invest — and split it before it collapses.** This is the largest
context by far and the one most at risk of becoming "everything else".

## Domain roles

**Coordinator.** It holds no remote state of its own: it owns *arrangement*,
*focus* and *continuity*, and delegates everything else.

## Ubiquitous language

The terms live in [`language.md`](language.md).

## Inbound communication

| Message | Type | From | Relationship | Note |
|---|---|---|---|---|
| key / mouse / resize | command | the terminal hop runs in | conformist | Bubble Tea's messages |
| Hosts() | query result | fleet | conformist | already ordered |
| connect / lost / Challenge / NewHostKey | event | connection | anticorruption layer | turned into cards and status |
| filebrowser.Msg | event | files | anticorruption layer | routed by alias |
| cwd changed, onOutput | event | pane | conformist | drives the status bar and redraws |

## Outbound communication

| Message | Type | To | Relationship | Note |
|---|---|---|---|---|
| Connect / Disconnect | command | connection | customer/supplier | |
| Touch(alias) | command | fleet | customer/supplier | after a successful connect |
| SendKey / Resize / Paste | command | pane | customer/supplier | |
| key event | command | files | customer/supplier | forwarded to the focused browser |
| open VS Code / new local tab | command | (generic) | conformist | `internal/action` |

## Business decisions

- **A session outlives the view of it.** Leaving a pane, collapsing a column or hopping
  to another host never closes a shell, an editor, a browser or a tunnel.
- **Closing is explicit and narrow.** `q` in the browser closes the browser and nothing
  else: its editor tabs are channels of their own and may hold unsaved work. The
  connection goes only when that leaves the session empty. `x` on a tab row of the sidebar
  closes that one tab the same way; an editor asks first, since unsaved work would go with it.
- **A hop is three keys.** A double `esc` from any pane, the tree or the panel puts the
  keyboard in the sidebar on the tab it was in; `↑`/`↓` and `enter` (or `→`) land anywhere. For
  hopping without the sidebar, go to is one chord away (`ctrl+o space`), the last host
  another (`ctrl+o tab`), and the neighbouring open hosts `ctrl+o ←`/`→`. In a pane only
  the leader and the double `esc` reach them: a bare key belongs to the remote program.
- **`esc` never quits hop.** In the sidebar it gives the keyboard back to where it was with
  nothing changed, a second one straight after is the same `esc` rather than a stray key for
  the pane, and with nowhere to go it does nothing. Quitting is `q` or `ctrl+c` there.
- **Everything open is one move away.** Go to lists every target on every connected host
  and lands exactly on the one chosen — that shell tab, that file, the panel. With no query
  it is a tree of the open hosts and what is under each, its cursor on the place before
  this one, so `enter` goes back there; typing flattens it into one ranked list.
- **Entering a host lands on its last place.** The sidebar's `enter` on a host, a host in go
  to, a click on an open host, and `ctrl+o tab` all go back to the target last used there —
  or, if that has closed, the one used before it, then its shell, browser or editor. Only an
  explicit new-shell action opens another shell.
- **The last host is the one in front before the current one**, changing only when the host
  in front does. With no last host, or none with a session left, hop says so and stays put;
  one whose connection dropped is reconnected.
- **The sidebar shows what is open, and every row is a way there.** The host in front is
  opened out into its tabs, in the order they were opened; the other open hosts are folded
  up with a count. The rows are built once (`buildRows`) and read by both the renderer and
  the pointer, so what is clicked is what is drawn.
- **The user always knows where their keystrokes go.** The footer's crumb is permanent
  screen space, beside the keys that act on it, naming the host, the tab and the file or
  directory; a transient status borrows it while it lasts. The accent marks only where the
  keyboard is: the focused box's border, the sidebar's cursor, and the marker on the tab in
  front while the keyboard is in it. Everything else is grey.
- **At most two columns, and a pane keeps one size.** The sidebar and the content area, and
  nothing between them: the tree lives in the sidebar, under the hosts. The sidebar's width
  is decided by the window and by the user's own dock toggle, never by where the keyboard
  is; where it is not docked it floats over the content while it has the keyboard, so going
  to the sidebar and back never reflows a remote program.
- **The terminal panel belongs to the files.** It needs a browser or an open file to sit
  under, goes when the browser closes with no file left, and moving it with `S` sends a
  `cd` to the running shell rather than starting another — never onto the alternate screen.
- **Mode says where keystrokes go, and only that** — not what is drawn. Layout and
  focus are separate facts: the keyboard going to the sidebar leaves the content area
  showing exactly what it showed.
- **Layout degrades, it does not break.** Below the width a docked sidebar needs, it floats
  and the tree takes the content area; below what even a floating one needs, it gives way;
  the split refuses to open. hop never renders a broken screen because the terminal is small.
- **A column that is not on screen does not take keys.** The sidebar off screen — a window
  too narrow even to float it — holds no selection, so its keys go quiet; and anything that
  hands the keyboard back to it reveals it first when the window allows.
- **The cursor rides its entry** across a sort, a refresh or a tree collapse. The user's
  place is not lost by hop's own bookkeeping.
- **Every remote-derived string is stripped of control characters** before it reaches a
  status line, a tab or a card.
- **A status has a generation.** A timer in flight can never fire against a newer
  status — clearing bumps the generation too.
- **A shell and the browser on one host follow each other.** `ctrl+o f` shows the host's
  browser at the shell's cwd — the open browser moves there rather than a second one
  opening — and `S` in the browser opens a shell tab in the cursor directory. Either way
  the keyboard goes with it. With no reported cwd the browser still comes up, where it
  was or at the default dir, and the status line says why it is not where the shell is.
- **Closing the split keeps the file you were reading**, rather than closing the half
  that happened to be focused.
- **hop reserves the fewest keys it can**, and the ones it reserves work in every mode.

## Aggregates

| Aggregate | Protects | Doc |
|---|---|---|
| Session | one per alias; shells, editors, browser and tunnels live and die together with the host, not the view | _not yet written_ |
| Layout | rect/frame consistency, minimum widths, the split invariant | _not yet written_ |

## Assumptions

- **This context is too big.** `internal/tui` is 9k lines and was touched by 567 of the
  file-changes in history — by far the most coupled package in the repo. It has been
  carved here into workspace plus the files claimed by [[fleet]], [[connection]],
  [[pane]] and [[keyboard]], on the grounds of *which language each file speaks*. That
  carve is the model's biggest bet and the first thing to take to a human.
- The cards (help, settings, palette, menu, confirm, guidance) are treated as
  workspace's, though several of them are really the UI of another context.
- `internal/action` (VS Code, new local tab) is treated as generic and not modelled.

## Verification metrics

- `internal/tui` continuing to appear in > 60% of commits touching any other package →
  the carve above is fiction and the boundary has not actually moved.
- New files under `internal/tui/` that speak another context's language (host, entry,
  challenge, binding) → that context's UI is being written in the wrong place.
- Terms defined here that duplicate a term defined elsewhere → run `ddd terms`.

## Open questions

- Should the per-host **session** be its own context? It is the one aggregate that
  every other context touches, and it is the reason `internal/tui` couples to
  everything.
- Should the **cards** (settings, help, palette, importer, tunnel manager, auth prompt,
  host key) be a presentation context of their own, or should each move to the context
  whose language it speaks?
