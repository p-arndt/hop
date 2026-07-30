---
id: "20260730-213245-review-fixes-alt-screen-mode-clearing-x10-range-abando"
title: "Review fixes: alt-screen mode clearing, X10 range, abandoned drags"
status: "completed"
updated: "2026-07-30T21:33:13+02:00"
base_commit: "d3d2c95d8b84ed975a425c9c786cbe78534c398b"
branch: "main"
agent: null
tags: ["mouse", "review", "terminal", "tui"]
files:
  - "TODO.md"
  - "internal/terminal/mouse.go"
  - "internal/terminal/mouse_test.go"
  - "internal/terminal/terminal.go"
  - "internal/tui/mouse.go"
  - "internal/tui/selection_test.go"
---

# Review fixes: alt-screen mode clearing, X10 range, abandoned drags

## Goal

Fix the three findings the code review raised against the committed mouse/clipboard work.

## Scope

## Discoveries

- vt.Callbacks has an AltScreen func(bool) that fires from inside setAltScreenMode, i.e. during emu.Write and in stream order — which is what the alt-screen mode clearing needed. The old code checked emu.IsAltScreen() after the whole chunk was parsed, so a read holding vim's teardown AND the shell's next prompt (the normal shape over SSH) cleared the ?2004h readline had just set. hop then pasted unbracketed into a shell that runs each line of what it is given. Any 'react to a mode/screen change' logic belongs in a callback, never in a post-Write check.

- A RIS still needs the separate osc.tookReset() path: the emulator resets its screen without firing AltScreen or the mode callbacks.

## Decisions

- **Decision:** X10 mouse reports go back to xterm's full byte range (x10Last 0xff, so columns to 222), instead of stopping at 0x7e.
  - **Reason:** The 0x7e ceiling silently dropped every click past column 94 for programs that ask for the mouse without SGR (older vim, mc, ncurses apps) — reachable on any wide pane, especially with the sidebar hidden. A program that is decoding the report reads raw bytes and wants exactly xterm's; the guarded-against case is a *stale* mouse mode typing junk on a command line, which is junk either way and is now prevented at its source by the in-band alt-screen clear.
  - **Trade-off:** A shell left in mouse mode by an inline program that was killed (no alt screen to leave) can still receive a report, and its trailing bytes are now undecodable rather than printable. A coordinate past 222 is still dropped rather than wrapped onto the wrong cell.

- **Decision:** A release that nothing claimed ends the drag, decided in handleMouse after routing rather than in each handler.
  - **Reason:** sel.dragging survived a release outside the pane (over the sidebar/footer/card, or forwarded to a remote program that took the mouse mid-gesture), and the next release over the pane — even one with no press behind it — then copied a span anchored where the abandoned drag started. Checking dragging after the route means whoever handled it has already cleared it, so there is no second predicate to keep in sync with mousePane's.

## Failures

## Validation

- go test ./... and go vet ./... — passed

- Both new regression tests were checked against the old code and fail there: TestAltScreenExitKeepsWhatFollowsInTheSameChunk (with the post-chunk clear patched back in) and TestDragReleasedOutsideThePaneEnds (with mouse.go stashed).

- internal/terminal TestMouseBytes updated for the restored X10 range: columns past 94 now encode, 222 is the last cell, 223 is dropped, and a wheel with every modifier set encodes its high button byte.

## Remaining risks

- The X10 change is a deliberate reversal of an earlier safety trade — see the decision above. If a stale inline mouse mode ever does put undecodable bytes on somebody's command line, x10Last is the one constant to move back.

## Handoff

- The remaining stale-mouse-mode case is an inline program (no alt screen) that is killed without resetting its modes; nothing clears those until the next RIS.
