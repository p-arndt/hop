---
id: "20260826-233307-host-list-keys-go-quiet-when-the-list-is-off-screen"
title: "Host list keys go quiet when the list is off screen"
status: "completed"
updated: "2026-08-26T23:33:24+02:00"
base_commit: "207832087feca1c194bdc710db9e78c67119c1cc"
branch: "main"
agent: null
tags: ["keys", "sidebar", "tui"]
files:
  - "TODO.md"
  - "domain/contexts/workspace/README.md"
  - "domain/contexts/workspace/language.md"
  - "internal/tui/keys.go"
  - "internal/tui/keys_test.go"
  - "internal/tui/layout.go"
  - "internal/tui/shells_test.go"
  - "internal/tui/sidebar_test.go"
  - "internal/tui/vscode_test.go"
---

# Host list keys go quiet when the list is off screen

## Goal

Stop modeList acting on a selection nobody can see, and reveal a collapsed sidebar when the keyboard is handed back to it.

## Scope

## Discoveries

- Two ways out of a pane exist: leavePane/leaveBrowser/leaveEditor/leaveAll (keys.go) all land in modeList. The reported trap — esc esc or ctrl+o o out of a pane with the sidebar collapsed — was all four of them, not one.

- revealSidebar is deliberately silent when !sidebarFits(): a window too narrow has nothing to reveal and the user's collapse preference must survive the resize (same reason toggleSidebar declines).

## Decisions

- **Decision:** Gate the list keys in doList, and reveal the sidebar on every path that hands the keyboard back to the list.
  - **Reason:** Gating alone leaves the user in modeList with nothing on screen and only ctrl+b out; revealing alone leaves the collapsed-in-list case (ctrl+b pressed while already in modeList) acting on an invisible cursor.
  - **Trade-off:** Quit and Help are exempt from the gate — ctrl+c lives in the List layer, so a blanket gate would make a collapsed list unquittable.

## Failures

## Validation

- go build ./... — passed

- go test ./... — passed (new: TestCollapsedListIgnoresSelectionKeys, TestLeavingAPaneRevealsTheSidebar, TestNarrowWindowLeavesTheSidebarOff)

- go vet ./... — passed

## Remaining risks

- Not driven in a live terminal; the collapse/reveal path is covered by model-level tests only.

## Handoff

- Several TUI tests built models with layout{height: N} and no width, so sidebarOn() was false and the new gate silenced them. Any test that presses a host-list key now needs a width.

- The mouse path (mouse.go listHasFocus) was left alone — an off-screen list cannot be clicked, but if a future layout draws the list elsewhere that assumption breaks.
