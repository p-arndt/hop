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
  - internal/tui/tabs.go
  - internal/tui/keys.go
  - internal/tui/status.go
  - internal/tui/commands.go
  - internal/tui/msgs.go
  - internal/tui/menu.go
  - internal/tui/palette.go
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
sees at once: which host is in front, what is beside it, which tab has the keyboard,
and the status line that answers that question permanently.

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
- **The user always knows where their keystrokes go.** The status bar is permanent
  screen space, directly above the keys that act on it, naming the host, the mode, the
  file or directory, and the machine behind the alias.
- **Mode says where keystrokes go, and only that** — not what is drawn. Layout and
  focus are separate facts.
- **Layout degrades, it does not break.** Below the width a column needs, the browser
  falls back to a full pane; the sidebar hides; the split refuses to open. hop never
  renders a broken screen because the terminal is small.
- **A column that is not on screen does not take keys.** The host list off screen —
  collapsed, or the window too narrow — holds no selection, so its keys go quiet; and
  anything that hands the keyboard back to it reveals it first when the window allows.
- **The cursor rides its entry** across a sort, a refresh or a tree collapse. The user's
  place is not lost by hop's own bookkeeping.
- **Every remote-derived string is stripped of control characters** before it reaches a
  status line, a tab or a card.
- **A status has a generation.** A timer in flight can never fire against a newer
  status — clearing bumps the generation too.
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
