---
id: "20260815-051620-action-registry-context-menu-command-palette-and-guida"
title: "Action registry, context menu, command palette and guidance profiles"
status: "completed"
updated: "2026-08-15T05:17:11+02:00"
base_commit: "809ee67311a4faa17b634df0c39875b78144faf0"
branch: "main"
agent: null
tags: ["discoverability", "tui", "ux"]
files:
  - "KEYBINDINGS.md"
  - "README.md"
  - "TODO.md"
  - "docs/10-modes.md"
  - "docs/11-hostlist.md"
  - "docs/21-leader.md"
  - "docs/30-browser.md"
  - "docs/39-actions.md"
  - "docs/43-settings.md"
  - "docs/47-cards.md"
  - "docs/_keybindings.md"
  - "docs/_readme.md"
  - "index.html"
  - "internal/config/config.go"
  - "internal/config/config_test.go"
  - "internal/tui/actions.go"
  - "internal/tui/actions_test.go"
  - "internal/tui/details.go"
  - "internal/tui/guidance.go"
  - "internal/tui/guidance_test.go"
  - "internal/tui/help.go"
  - "internal/tui/keys.go"
  - "internal/tui/keys_test.go"
  - "internal/tui/menu.go"
  - "internal/tui/model.go"
  - "internal/tui/mouse.go"
  - "internal/tui/palette.go"
  - "internal/tui/paste.go"
  - "internal/tui/settings.go"
  - "internal/tui/theme.go"
  - "internal/tui/view.go"
  - "internal/tui/vscode.go"
---

# Action registry, context menu, command palette and guidance profiles

## Goal

Make hop usable without memorising its keyboard: one registry of actions rendered as a per-host context menu, a per-mode searchable palette, and a three-way guidance profile that changes only how much is on screen.

## Scope

- Interview agreement fixed the shape: palette + cursor-anchored context menu (rejected: a permanent action column, and profiles that change bindings). Profiles change visibility only; first run asks once, default hybrid; existing installs are never asked.

## Discoveries

- internal/tui already had a package-level identifier 'action' — the import of hop/internal/action, used once in vscode.go for openVSCode. The registry type won the name and that import is now aliased actionpkg.

- renderDetails carried a second, hand-written copy of the host keys (actionGrid, with its own 'focus shell'/'disconnect' state rules). It is now built from the same registry, so the card cannot disagree with the menu.

- listRowAt's row arithmetic (screen header + sidebar border + heading + filter prompt) is now listFirstRow() in mouse.go: the mouse runs it backwards, the context menu forwards to anchor itself.

## Decisions

- **Decision:** An action is a key: running one replays its key(s) through handleKey rather than calling a handler directly; a chord is written 'ctrl+o o' and replayed as two keystrokes.
  - **Reason:** The menu, the palette and the details card then cannot drift from the keyboard, and a binding that grows a condition grows it once. It also makes the pane's leader chords offerable without duplicating leader state.
  - **Trade-off:** Only keys hop itself binds can be actions (a remote editor's ':q' cannot), and availability has to be a predicate on the model rather than falling out of the handler.

- **Decision:** ctrl+k opens the palette in the host list and the browser, but in a pane it is behind the leader (ctrl+o ctrl+k).
  - **Reason:** ctrl+k kills a line at a readline prompt — a bare ctrl+k in a shell is a keystroke the remote is owed. Outside a pane nothing competes for it. Same rule the '?' card follows.

- **Decision:** space, and a right-click, open the context menu anchored under the host's row.
  - **Reason:** space was unbound in the list, reads as 'act on this one' rather than as a command, and arrives on every terminal — unlike alt chords on macOS.

- **Decision:** Guidance is a word in config.json, normalised to hybrid when unknown; the first-run question is gated on config.Exists() rather than on the field being empty.
  - **Reason:** An existing config file means these settings were once decided. Gating on an empty field would re-open the question for everyone who upgrades into a new setting.
  - **Trade-off:** Every future first-run question needs a marker of its own; Exists() only answers 'has hop ever saved anything'.

## Failures

- **Approach:** First cut of the menu clamped only against the window bottom, so a tall menu was pushed up over the row it belonged to — the anchor, which is the whole point of it.
  - **Lesson:** menuPlace now picks the side with more room (below the row, else above it) and shortens the list to fit; the row it names always stays visible, and the status bar and footer are off limits (menuBottom = height-2).

- **Approach:** guided first rendered hop's own keys as a second 'HOP' block under ACTIONS in the details card; on a normal-height pane the block and the hint line below it were the first things cut off.
  - **Lesson:** One grid, host actions then hop's — two columns fill more evenly and truncation eats the least important rows instead of a heading.

## Validation

- just ci (fmt-check, vet, docs-check, go test ./...) — passed. New tests: internal/tui/actions_test.go (availability rules, menu anchoring and swallowing, right-click, palette filter/run, chord replay through the leader), internal/tui/guidance_test.go (first-run answer persists, esc still answers, profile changes only what is shown), internal/config/config_test.go (guidance normalisation, Exists).

## Remaining risks

- The palette in a live shell/editor pane was exercised against a session struct with no real pane; a chord replayed against a genuine remote program has not been tried by hand.

- The guidance profile is read in exactly two places (footerHints via guidedHints, renderDetails). A third reader added later must keep the rule that a profile never changes what a key does.

## Handoff

- Run hop against a real host: right-click the sidebar, ctrl+o ctrl+k inside a shell, and check the 'keys' profile does not feel stripped. Not built and deliberately left: a context menu for files in the SFTP browser (space there belongs to the browser), and any auto-opening of the menu in guided.
