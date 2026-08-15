---
id: "20260815-044248-status-bar-a-footer-that-fits-the-window-and-a-context"
title: "Status bar, a footer that fits the window, and a context-aware key card"
status: "completed"
updated: "2026-08-15T04:43:33+02:00"
base_commit: "2c0e425aaff91b6f88d1b65af5ee30ab96bac527"
branch: "main"
agent: null
tags: ["docs", "keybindings", "tui", "ux"]
files:
  - "KEYBINDINGS.md"
  - "README.md"
  - "TODO.md"
  - "docs/01-what.md"
  - "docs/10-modes.md"
  - "docs/11-hostlist.md"
  - "docs/21-leader.md"
  - "docs/22-scrollback.md"
  - "docs/30-browser.md"
  - "docs/41-reconnect.md"
  - "docs/47-cards.md"
  - "index.html"
  - "internal/tui/footer_test.go"
  - "internal/tui/help.go"
  - "internal/tui/help_test.go"
  - "internal/tui/keys.go"
  - "internal/tui/layout.go"
  - "internal/tui/reconnect.go"
  - "internal/tui/status.go"
  - "internal/tui/status_test.go"
  - "internal/tui/theme.go"
  - "internal/tui/view.go"
  - "internal/tui/vscode_test.go"
---

# Status bar, a footer that fits the window, and a context-aware key card

## Goal

Give hop a permanent 'where am I' row and cut the footer to what a mode cannot be worked without, with the full table moved into a key card that opens on the mode you are in. Picks up the handoff from the leader-key task.

## Scope

- chromeRows in internal/tui/layout.go is the single number every pane size derives from. A third chrome row is one edit there; nothing else measures the body.

- The remote cwd was already tracked (terminal.Pane.Cwd() over OSC 7, reached via m.shellCwd) for the VS Code binding — the status bar reuses it and needed no new plumbing.

## Discoveries

- '?' was bound in the host list ONLY (handleNavKey). In a shell or editor the key reaches the remote as text; in the browser, scrollback and a dead pane it was simply unbound. So a footer that names '?' everywhere — the premise the whole trim rests on — could not be built without new bindings. This was the one place the agreed scope ('no new keybindings') had to give.

- The place was already on screen, just split and far from the keys: breadcrumb() at the far top-left of the header, modeChip() at the top-right, footer at the bottom. The work was moving and joining them, not inventing them.

## Decisions

- **Decision:** The place gets its own row between the body and the footer, rather than staying in the header or moving into the footer.
  - **Reason:** You read where-you-are and what-to-press together, so they belong adjacent. Putting both in one row makes them compete for the same columns at 80 wide and both get cut.
  - **Trade-off:** Costs one body row (chromeRows 2 -> 3).

- **Decision:** '?' opens the card from every mode hop owns the keyboard in (list, browser, scrollback, dead pane); in a shell or editor it is the leader chord ctrl+o ?.
  - **Reason:** A legend must never name a key that does nothing, and in a forwarding pane a bare '?' is a question mark the remote is owed. While filtering there is no card key at all — every printable key is filter text.
  - **Trade-off:** New bindings, which the interview had put out of scope; taken because the alternative was a footer promising a dead key.

- **Decision:** The footer is a per-mode core (3-4 keys) plus a priority-ordered extra list that fills whatever room the window has.
  - **Reason:** The user asked for more keys on wide terminals after the first cut. A fixed short list wastes 200 columns; a fixed long list is unreadable at 80.
  - **Trade-off:** Two lists per mode to keep in order rather than one.

- **Decision:** A hint that does not fit is dropped whole; only the update hint and the card key are protected from the cut.
  - **Reason:** A legend ending in 'shift+<-...' names no key, so the room is spent on something unreadable.
  - **Trade-off:** On a very narrow window a mode may show only its way out.

## Failures

- **Approach:** Counted the footer's keys by looking for keycapStyle's opening ANSI sequence in the rendered row.
  - **Command:** `go test ./internal/tui/ -run TestFooterKeepsToFourKeys`
  - **Evidence:** the browser legend names 20 keys, want at most 4
  - **Lesson:** lipgloss renders styles bare in a test binary (no TTY), so keycapStyle.Render("x") is just " x " and the 'opening sequence' is a space. Never assert on styling in these tests — assert on the data. footerHints() was split out of renderFooter for exactly this, and it is the seam to use for any future footer rule.

- **Approach:** Chose the footer's card key from m.focused(), which is true for both modeShell and modeScrollback.
  - **Command:** `go test ./internal/tui/ -run TestTheCardOpensFromEveryMode`
  - **Evidence:** ? did not open the card from scrollback
  - **Lesson:** focused() spans a mode that forwards keys and one that does not. The property that decides whether a key can be spent on hop is 'does this mode forward to the remote' — (m.editing() || m.mode == modeShell) && !m.activeDead() — not focused(). Same trap awaits any future 'is this key free here' check.

- **Approach:** Moved the update hint into the shared footer tail, so it rendered in every mode.
  - **Command:** `go test ./internal/tui/ -run TestNoUpdateHintWhileFocused`
  - **Evidence:** focused footer should stay a key legend
  - **Lesson:** An existing test was carrying a deliberate rule that the code did not otherwise state: release news must not compete with a legend while keystrokes go to another machine. Worth re-reading a failing old test as a spec before assuming it is stale.

## Validation

- just ci — go vet, fmt-check and go test ./... all passed, including the docsgen drift test after just docs regenerated index.html, README.md and KEYBINDINGS.md

- New: internal/tui/status_test.go (13 subtests — place per mode, cwd tracking, target spelling, tab chips, one-row fit at 20-200 columns, left-elision), footer_test.go (key ceiling, card reachable from every mode, adaptive width, whole-hint dropping, esc esc fast and slow), help_test.go (section ordering, no duplicates, card names its own key)

- Mutation-checked the status tests: inverting elideLeft to cut from the right fails TestStatusElidesAPathFromTheLeft, so they are not merely passing by construction

## Remaining risks

- Only the DROPPED SESSION help section is owner-less among the pane modes, so opening the card from a dead pane leads with SHELL. paneMode does not encode deadness; fixing it needs either a mode or a second lookup key.

- The status bar is not verified against a real host — the cwd crumb, the elision and the fill were checked in tests and by printing the two rows, not by eye in a live session.

## Handoff

- Run hop against a real host and look at the two bottom rows: whether the filled status bar reads well against the pane border, and whether the extras that appear at 120+ columns are the right ones per mode (they are ordered by hand in footerHints).

- TODO.md still lists the remaining SFTP work (upload, file ops, async transfers) as the next natural item; the browser footer now has room reserved for those keys in its extra list.
