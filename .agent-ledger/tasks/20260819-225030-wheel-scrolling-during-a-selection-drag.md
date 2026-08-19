---
id: "20260819-225030-wheel-scrolling-during-a-selection-drag"
title: "Wheel scrolling during a selection drag"
status: "completed"
updated: "2026-08-19T22:50:56+02:00"
base_commit: "41b6e9e8356611e85ff084e6a3bfe0f5108990fc"
branch: "main"
agent: null
tags: ["mouse", "scrollback", "selection", "tui"]
files:
  - "KEYBINDINGS.md"
  - "TODO.md"
  - "docs/22-scrollback.md"
  - "docs/44-mouse.md"
  - "index.html"
  - "internal/tui/mouse.go"
  - "internal/tui/mouse_test.go"
  - "internal/tui/selection.go"
  - "internal/tui/selection_test.go"
  - "internal/terminal/terminal.go"
---

# Wheel scrolling during a selection drag

## Goal

Make the wheel scroll a shell pane while a selection drag is live (the selection grows with it), keep a selection over the text it was made on while the view scrolls, and give the wheel something to do on the alt screen.

## Scope

## Discoveries

- mouseShell used to clear the selection on every wheel notch (both the scrollback branch and the live-screen entry branch), which is what made an edge-held drag the only way to select more than one screenful. Wheel handling now runs before the MouseEnabled/scrolling split, via wheelDir + wheelShell.

- Selection lives in the pane's VIEW coordinates, so any scroll of the view has to move it. scrollShellBy (internal/tui/mouse.go) is now the single place hop's shell view scrolls: it enters/leaves scrollback, moves the offset and calls shiftSelection(moved). dragScrollStep is a one-line wrapper over it, so the wheel and the edge autoscroll cannot drift apart.

- A selection is stored in view coordinates and was copied out of the *rendered screen*, so a selection grown past one screenful silently copied only the rows still visible. Pane.ViewRows(from, to) (internal/terminal/terminal.go) now renders any row range of the virtual scrollback+screen buffer — ViewScrollback is ViewRows(0, h-1) — and endSelection reads the copy out of the rows the span covers (shellSpanView/shiftSpan in tui/selection.go). The highlight is still painted on the screen only, which is all the screen can show.

- shiftSelection moves the anchor always but the head only when the drag is over: during a live drag the head belongs to the pointer, which did not move, so the caller re-places it at the pointer cell.

## Decisions

- **Decision:** A wheel notch on the alt screen is translated into three up/down arrow keys instead of doing nothing.
  - **Reason:** enterScrollback declines on the alt screen (a full-screen program keeps no scrollback in hop), so the wheel over less/vim/htop that never asked for the mouse was previously spent doing nothing at all. This is xterm's alternate-scroll, and it stays inside hop's rule that the pointer only does what the keyboard can.
  - **Trade-off:** A full-screen program that means something else by the arrow keys (a menu, a form) is moved by the wheel. Suppressed while a drag is live, where those keys would move the far end's cursor under a selection hop is still making.

- **Decision:** A selection now survives a scroll instead of being cleared by it.
  - **Reason:** The old contract (any scroll takes the highlight down) was written when the view could only scroll out from under a fixed highlight. With the selection riding the text, the highlight stays over the same words, which is what every terminal does. Keys still clear it (handleKey).

## Failures

## Validation

- go build ./... ; go vet ./... ; go test ./... — all packages pass (14 ok). New/changed tests: TestScrollCarriesTheSelection (replaces TestScrollClearsTheSelection, whose contract this work reverses), TestWheelDuringDragExtendsTheSelection, TestWheelOnAltScreenSendsArrowKeys. TestSelectionTallerThanThePaneCopiesEveryRow was confirmed to fail (15 lines copied of 21) with shellSpanView stubbed out, so it is not a vacuous pass. gofmt clean on the touched files. docsgen regenerated KEYBINDINGS.md + index.html from docs/22-scrollback.md and docs/44-mouse.md.

## Remaining risks

- Never tried against a real host: the alt-screen arrow translation is untested with a program that binds the arrows to something other than scrolling (a TUI menu), where a wheel notch now moves it three steps.

- A selection scrolled far enough carries its anchor off-screen. The copy now covers it (ViewRows), but the *highlight* still only paints the rows on screen — the visible part of a selection that runs off the window looks the same as one that stops at the edge.

- A drag pulled back further than scrollback holds selects blank rows: ViewRows renders a row above the oldest line as empty rather than dropping it, so the row numbering stays aligned. Those rows are trimmed to empty lines by PlainText, not skipped.

## Handoff

- Try a drag + wheel against a real host, and a wheel over less/vim without 'set mouse=a' to confirm the arrow translation feels right; the wheelStep of 3 is the only knob.
