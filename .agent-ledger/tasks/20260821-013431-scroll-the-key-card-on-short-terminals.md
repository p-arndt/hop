---
id: "20260821-013431-scroll-the-key-card-on-short-terminals"
title: "Scroll the key card on short terminals"
status: "completed"
updated: "2026-08-21T01:34:49+02:00"
base_commit: "53d78748be330cbf17a5016f159b328f4d8ca635"
branch: "main"
agent: null
tags: ["help", "tui", "ux"]
files:
  - "internal/tui/help.go"
  - "internal/tui/help_test.go"
  - "internal/tui/keys.go"
  - "internal/tui/keys_test.go"
  - "internal/tui/model.go"
  - "internal/tui/mouse.go"
---

# Scroll the key card on short terminals

## Goal

Make the help card reachable in full on a window too short to hold it, instead of cutting the tail off with an ellipsis.

## Scope

- handleHelpKey takes up/down/j/k, pgup/pgdown/space, home/g and end/G; end sets math.MaxInt32 and lets fitHelp pull it back to the last page. The wheel is routed to the card in routeMouse, ahead of the capturing() gate that otherwise swallows the pointer while a modal is up.

## Discoveries

- The card's body is rendered before its height is known, and its length depends on which bindings the user has left bound, so the scroll offset can only be clamped where the body is drawn — fitHelp clamps m.helpScroll and renderHelp keeps a pointer receiver for that reason.

- At >=88 columns the card is two columns joined side by side, so scrolling moves both at once and the last section (EDITOR) is not the last body line — the left column is the longer one. Tests that assert 'the end is reachable' must not key on a named section.

## Decisions

- **Decision:** Scroll the card rather than shrink or paginate it by section.
  - **Reason:** The card is one table; a window-and-offset keeps every row in the place the reader learned it, and the previous behaviour already cut lines — it just gave no way to reach them.
  - **Trade-off:** The scroll offset is remembered only while the card is open (openHelp resets it).

## Failures

## Validation

- go test ./... — passed (incl. new TestHelpFitsAShortWindow, TestHelpScrollsToWhatDoesNotFit, TestHelpBodyWindows, TestHelpScrollStaysOnTheCard); go vet ./... — clean; card printed at 100x24 by eye, top and second page both bounded

## Remaining risks

- The scroll hint sits on the footer line next to 'esc close'; on a window under ~30 columns those two hints can outrun the card's width.

## Handoff

- The card gives no sense of position — a scrollbar or an 'n more' count on the footer is the natural follow-on if the hint proves too quiet.
