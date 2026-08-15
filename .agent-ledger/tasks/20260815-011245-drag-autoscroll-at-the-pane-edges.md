---
id: "20260815-011245-drag-autoscroll-at-the-pane-edges"
title: "Drag autoscroll at the pane edges"
status: "completed"
updated: "2026-08-15T01:13:05+02:00"
base_commit: "07d4d5c15a6a031b8ac658dd581392d9c5e28e19"
branch: "main"
agent: null
tags: ["mouse", "scrollback", "selection", "tui"]
files:
  - "TODO.md"
  - "internal/tui/commands.go"
  - "internal/tui/model.go"
  - "internal/tui/mouse.go"
  - "internal/tui/msgs.go"
  - "internal/tui/selection.go"
  - "internal/tui/selection_test.go"
---

# Drag autoscroll at the pane edges

## Goal

A selection drag held against a pane's top or bottom row must keep going — scrolling the view under the pointer — so a selection is not limited to the screenful the button went down on.

## Scope

- Autoscroll is shell-panes only: editor panes keep no hop-side scrollback (mouseEditor has nothing to scroll), and the live screen has nothing below it, so the bottom edge only scrolls while already paused in scrollback.

## Discoveries

- A pointer held still against an edge sends no further mouse reports (hop enables cell-motion tracking, 1002 — motion is reported only when the cell changes). So edge autoscroll cannot be motion-driven: it needs its own tea.Tick chain (dragScrollMsg / dragScrollCmd, 60ms) that re-arms itself while sel.edge is non-zero.

- The selection is stored in the pane's *view* coordinates, so scrolling the view moves the text under a fixed anchor. dragScrollStep therefore adjusts sel.anchor.Y by the actual offset delta (ScrollUp/Down clamp, so the delta can be 0 = nothing left to scroll = stop the chain).

## Decisions

- **Decision:** A drag in progress owns every mouse event, whatever region the pointer is over.
  - **Reason:** routeMouse used to dispatch by zone, so wandering into the sidebar hit mouseList and cleared the selection mid-gesture; and paneLocal dropped anything outside the pane, which is exactly where a drag past the bottom edge lands. Now routeMouse short-circuits to mousePane while sel.dragging and mousePane clamps the cell back onto the pane (clampToPane).
  - **Trade-off:** A release over the sidebar now extends the selection to the clamped edge cell instead of keeping the last in-pane head — TestDragReleasedOutsideThePaneEnds was updated to that rule.

## Failures

## Validation

- go build ./... , go vet ./internal/tui , go test ./... — all passed; five new tests in internal/tui/selection_test.go cover the top edge, the tick repeat (plus stale-tick drop and release), the bottom edge back toward live, the no-op at the bottom of a live screen, and a drag crossing the sidebar.

## Remaining risks

- Never tried against a real host: the tick rate (60ms/line) is a feel judgement, and a drag held at the top of a long history scrolls one line per tick until the button comes up or history runs out.

## Handoff

- If the pointer is dragged over a pane that a remote program has claimed (MouseEnabled), hop never starts a selection there, so autoscroll cannot fire — unchanged, but worth remembering before extending this to editor panes.
