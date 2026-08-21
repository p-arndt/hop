---
id: "20260821-152126-collapse-the-tui-s-scattered-geometry-into-one-frame-o"
title: "Collapse the TUI's scattered geometry into one frame of rects"
status: "completed"
updated: "2026-08-21T15:22:01+02:00"
base_commit: "df0c33904cf6a7e9ad3a813e3a41f152065fa957"
branch: "main"
agent: null
tags: ["layout", "refactor", "tui"]
files:
  - "TODO.md"
---

# Collapse the TUI's scattered geometry into one frame of rects

## Goal

Phase 1, deliberately left out of the parallel fan-out because it touches layout, view and mouse at once: derive every box of the body in one place and fix the narrow-window frame overrun the characterization tests recorded.

## Scope

## Discoveries

- rect is in OUTER coordinates — x,y is the top-left cell and w,h include the border. That is deliberate: it is the unit listWidth and treeWidth already spoke in, so a rect is built from them with no adjustment nobody can see. inner() strips the border for the panes, which address themselves from their own top-left corner.

- m.fr is read by the pointer, and it holds the frame the LAST RENDER laid out rather than one derived live. That is more correct for hit-testing — the user clicked on what they saw — but it means anything moving the columns without a relayout now shows up as a stale frame. Two existing tests set m.sidebarHidden directly and had to gain a recomputeLayout; production always goes through toggleSidebar/relayout.

- The selection keeps the box the drag began in (selection.box), rather than reconstructing the width later. Which box a selection belongs to is a fact about where the pointer went down, not about the current screen — so this also survives a resize mid-drag, which the reconstruction could not. It deleted selectionW, the fourth copy of the render switch, added only a day earlier.

- The narrow-window overrun was two independent floors: listWidth's 16 and recomputeLayout's 10 for paneW, neither aware of m.width, summing to 28 outer columns on any window. Fixed by making listWidth yield the sidebar entirely below the threshold — the same rule the tree column already used — and dropping paneW's floor, whose job was then done. Only behaviour below 28 columns changed; all 23 characterization cases were unaffected.

## Decisions

- **Decision:** Cache the frame on the model rather than derive it per call.
  - **Reason:** Hit-testing should measure against the frame that was drawn, not one recomputed from state that may have moved since the paint. View already calls recomputeLayout before measuring anything.
  - **Trade-off:** A stale frame is now possible if something moves the columns without relayout. Made visible by tests rather than guarded against.

- **Decision:** Keep m.paneW and m.paneH alongside the frame.
  - **Reason:** They are SIZE questions (editorSize, shellSize, browserSize, tab strips), not POSITION questions. The frame owns positions; forcing sizes through it too would have been churn without a reader.
  - **Trade-off:** Two representations of the content area's inner width, both written only by recomputeLayout so they cannot drift.

## Failures

## Validation

- go build ./... clean; go vet ./... clean; go test -count=1 ./... all packages ok. The 23 layout characterization cases from the previous task passed unchanged throughout — they are what made this verifiable, in particular the round-trip assertion that zoneAt/treeLocal/contentLocal agree cell-for-cell with the boxes View drew.

- TestVeryNarrowWindowsStillFitTheirTerminal (was TestFrameOverrunsVeryNarrowWindows) now asserts every width from 3 to 40 renders exactly that many cells; TestTheSidebarYieldsBeforeTheFrameOverruns pins which column gives way at 27/28/40.

## Remaining risks

- Still never run against a real SSH host — the three-column layout and the split halves have only ever been drawn by unit tests. Unchanged from the parent task, and now the largest remaining unknown.

- m.fr is a cache. Anything that moves the columns without calling relayout will hit-test against the previous frame. Production paths all go through relayout; a new one that does not would be a silent bug.

## Handoff

- internal/tui/model.go still has 60 fields. Pulling out a layout struct (width, height, ready, sidebarHidden, treeHidden, paneW, paneH, fr) is now the obvious next cut — the frame gave those fields a natural home together.

- No key collapses a split; collapseSplit is only reached by closing files. Still open, still a product question rather than a technical one.
