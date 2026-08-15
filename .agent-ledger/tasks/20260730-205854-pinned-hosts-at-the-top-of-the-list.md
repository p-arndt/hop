---
id: "20260730-205854-pinned-hosts-at-the-top-of-the-list"
title: "Pinned hosts at the top of the list"
status: "completed"
updated: "2026-07-30T20:59:27+02:00"
base_commit: "d778aca08eacb5c19918bd6d4da719b70db5cb73"
branch: "main"
agent: null
tags: ["store", "tui", "ui"]
files:
  - "KEYBINDINGS.md"
  - "TODO.md"
  - "internal/store/store.go"
  - "internal/store/store_test.go"
  - "internal/tui/help.go"
  - "internal/tui/keys.go"
  - "internal/tui/layout.go"
  - "internal/tui/list.go"
  - "internal/tui/model.go"
  - "internal/tui/mouse.go"
  - "internal/tui/mouse_test.go"
  - "internal/tui/pin.go"
  - "internal/tui/pin_test.go"
  - "internal/tui/view.go"
---

# Pinned hosts at the top of the list

## Goal

Let a host be pinned above the frecency order into a PINNED section, in an order the user sets by hand.

## Scope

- Out of scope: pinning from the host form/edit modal, a hop CLI pin command, mouse drag-to-reorder, pinning groups/tags.

## Discoveries

- store.OpenAt only ran CREATE TABLE IF NOT EXISTS, so any column added to the schema constant was invisible to every existing install. New columns now go through store.migrate, which reads PRAGMA table_info(hosts) and ALTERs what is missing — asking the pragma rather than swallowing the driver's duplicate-column error string.

- The sidebar's cursor is an index into m.filtered, but with section headings the drawn rows and the hosts are no longer the same sequence. m.rows (internal/tui/list.go listRow) is now the drawn-row space, built in applyFilter, and listStart/scrollbarCell/listRowAt/listRows all measure in it. Anything added to the sidebar later must go through buildRows or the mouse mapping silently drifts from the renderer.

- Test models built by hand (mouse_test.go newMouseModel) set m.filtered directly; they must call applyFilter or m.rows stays empty and every click maps to nothing. applyFilter is the single place filtered and rows are kept in step.

- keymap has no binding for uppercase J/K in either scope, so shift+j/k were free for reordering without touching internal/keymap or the Vim-keys setting.

- Review caught two bugs in the first cut, both from mixing the two index spaces. (1) pinnedFirst partitioned *stably*, so under a filter the PINNED section drew in fuzzy-score order while store.MovePin moved in stored pin order: shift+k on the visually-second pin asked the store to move index 0 and silently did nothing. It now sorts the pinned slice by PinOrder. (2) move()'s page motions did cursor += listRows(), adding a row count (headings included) to a host index, so pgdn stepped over one host per heading on screen. Paging goes through keys.go pageCursor, which works in row space and backs off a landing heading toward the cursor. Anything that mixes m.cursor (host space) with a row count is the bug to look for here.

- keys_test.go newNavModel and mouse_test.go newMouseModel set m.filtered directly and now have to call applyFilter, or m.rows stays empty and every click and every page is a no-op.

## Decisions

- **Decision:** A separate PINNED section with an explicit pin_order column, rather than a boolean that only re-sorts within frecency.
  - **Reason:** Requested: the pinned order is the user's, not a second frecency. pin_order is 1..n and renumbered densely on every pin/unpin/move/delete (renumberPins), so a hole can never confuse MovePin's index arithmetic.
  - **Trade-off:** Every sidebar row calculation had to move into a row space that includes headings; the fixed HOSTS title is dropped once sections exist, so the sidebar has two layouts to keep working.

- **Decision:** A new pin appends to the bottom of the PINNED section.
  - **Reason:** Pinning is 'keep this where I can find it', not 'make this first'; appending never reshuffles an order the user set by hand.
  - **Trade-off:** Pinning a host you want at the top costs a few shift+k presses.

- **Decision:** A pin outranks the fuzzy match score while filtering (pinnedFirst), and a section with no matches drops its heading.
  - **Reason:** A filter narrows the list; it does not dissolve the sections. Empty section blocks would waste rows on a narrow sidebar.
  - **Trade-off:** The PINNED section is sorted by PinOrder even under a filter — a match score has nothing to say about an order the user set by hand, and drawing it by score would desync the section from what shift+j/k move in (see the review fixes below). Only the unpinned tail keeps the ranking.

## Failures

## Validation

- go test ./... — passed (new: internal/store pin ordering, dense pin_order, MovePin edges, old-schema migration; internal/tui pin_test.go sections, filtering, reorder keys, click mapping)

- go vet ./... — clean

- After the review fixes: go test ./... and go vet ./... — passed, with new tests for the filtered pin order (TestFilterKeepsPinOrder) and for paging in row space (TestPageStepsBySectionRows). internal/tui TestRemoteYankReachesTheClipboard is timing-flaky in a full-suite run and passes on its own; it predates this work.

## Remaining risks

- MovePin reorders across *all* pinned hosts, so under a filter shift+j/k can move a host past a pin the filter is hiding: the persisted order changes while the visible order looks unmoved. Deliberate — the alternative is a reorder that means something different depending on the filter.

- Not yet driven in a live terminal; the section layout is covered by renderList/listRowAt tests only.

## Handoff

- If a pin marker per row is ever wanted, renderRow is where it goes — the section heading is currently the only signal. A local identity-file picker and the groups/tags UI are the neighbouring TODO items that would also section the list.
