---
id: "20260725-204236-remove-left-arrow-back-out-from-shell-panes"
title: "Remove the ← back-out binding from shell panes"
status: "completed"
updated: "2026-07-25T20:42:36+02:00"
base_commit: "6449146e21cbf9fd4f04c8e753610254eb000e5b"
branch: "main"
agent: null
tags: ["keys", "terminal", "tui", "ux"]
files:
  - "KEYBINDINGS.md"
  - "README.md"
  - "TODO.md"
  - "internal/terminal/altscreen_test.go"
  - "internal/terminal/terminal.go"
  - "internal/tui/help.go"
  - "internal/tui/keys.go"
  - "internal/tui/shells_test.go"
  - "internal/tui/view.go"
---

# Remove the ← back-out binding from shell panes

## Goal

← in a focused shell pane kicked the user back to the host list instead of reaching the remote shell. Give the key back to the shell unconditionally; ctrl+o and double-esc remain the ways out.

## Scope

- `handleShellKey` no longer has a `left` case — ← falls through to `pane.SendKey` like any other key.
- `backsOut` (tui/keys.go) and the conditional `← back` footer hint (tui/view.go) deleted; the SHELL section of the `?` card no longer lists ←.
- `Pane.LineEmpty`, `Pane.track`, and the `typed`/`lineMu` fields deleted from internal/terminal — they existed only to answer "is the prompt bare?" for this binding. `AltScreen` stays (scrollback entry still uses it).
- `internal/terminal/line_test.go` → `altscreen_test.go`: `TestLineEmpty` and its helpers gone, `TestAltScreen` + `waitFor` kept.
- `TestLeftAtABarePromptLeavesTheShell` / `TestLeftOverATypedLineGoesToTheShell` / `TestLeftAfterTheLineIsCleared` replaced by one table test, `TestLeftAlwaysGoesToTheShell`, asserting the pane stays focused at a bare prompt, mid-line, and after enter/ctrl+c/ctrl+u/backspace.
- Untouched: ← as back in the host list, ← as "up a directory" in the browser, ← as "leave scrollback", and alt+← as "previous shell/tab". Only the focused-live-shell binding is gone.

## Discoveries

- The bare-prompt heuristic was structurally unreliable, not merely buggy: hop counts only the keys it forwards and never reads the remote line buffer, so anything it cannot count (ctrl+w, tab-completion, a program printing onto the line, an inline arrow-reading program like `fzf --height` that never takes the alt screen) left `typed == 0` while the line was in fact occupied — and then ← ejected the user out of a line they were editing. The user hit exactly this while moving back over words.

## Decisions

- **Decision:** Remove the binding outright rather than tightening the heuristic.
  - **Reason:** ← is load-bearing for readline (and for the alt+b/alt+f word motions built on it) and for every full-screen program; a heuristic that is wrong even occasionally breaks editing on every server. Two unconditional exits (ctrl+o, esc esc) already exist.
  - **Trade-off:** Loses a one-key exit at an idle prompt; "← is back" no longer holds inside a pane (it still does in the list, the browser and scrollback).

- **Decision:** Delete the line-tracking machinery in internal/terminal instead of leaving it as unused API.
  - **Reason:** It had exactly one consumer and its whole contract ("as far as the keys hop forwarded can say") only made sense for that consumer; keeping it invites a future caller to trust a number that is known to over-count.

## Failures

## Validation

- `go build ./...` — ok; `go vet ./...` — ok; `gofmt -l .` — clean; `go test ./...` — all packages pass.

## Remaining risks

- Docs rewritten in three places (README shell table, KEYBINDINGS terminal table + the "### `left` at a bare prompt" section, now "### Why `left` is not a way out", TODO.md line 21). Any external notes or screenshots still advertising ← as back are now stale.

## Handoff

- Live-verify on a real host that ← now moves the readline cursor and that alt+b/alt+f word motions work end to end; headless tests only assert the pane keeps focus.
