---
id: "20260826-231536-function-keys-and-decckm-in-the-pane-encoder"
title: "Function keys and DECCKM in the pane encoder"
status: "completed"
updated: "2026-08-26T23:15:37+02:00"
base_commit: "10c6a991d3627be05a26097170a5899836fc60e2"
branch: "main"
agent: null
tags: ["keyboard", "pane", "terminal"]
files:
  - "TODO.md"
  - "docs/dev/bubbletea-v2-migration.md"
  - "domain/contexts/pane/README.md"
  - "domain/contexts/pane/language.md"
  - "internal/terminal/cursor_test.go"
  - "internal/terminal/cursorkeys.go"
  - "internal/terminal/cursorkeys_test.go"
  - "internal/terminal/keys_test.go"
  - "internal/terminal/terminal.go"
---

# Function keys and DECCKM in the pane encoder

## Goal

Close the two encoder gaps the v2 migration made visible: F1-F12 reached no remote program at all, and hop ignored application cursor mode.

## Scope

## Discoveries

- DECCKM needed no new plumbing: x/vt already reports mode changes through the EnableMode/DisableMode callbacks hop wires in terminal.go, which is how bracketed paste (pasteState) and mouse tracking already work. cursorKeysState is the same shape, and rides the existing AltScreen and RIS teardown so vim exiting cannot leave the shell behind it receiving SS3 arrows.

- F1-F4 fell out for free: they are CSI 1;<mod>P/Q/R/S when modified, which is the cursor-key form, so adding them to cursorFinal gave modified function keys with no extra code. F5-F12 are tilde keys with xterm's gapped numbering (15,17,18,19,20,21,23,24 - no 16 or 22).

- The wheel improves as a side effect: wheelKeys sends KeyUp/KeyDown through SendKeys, which now reads the pane's DECCKM state, so scrolling inside vim or less sends the arrows those programs actually expect.

## Decisions

- **Decision:** Modified cursor keys stay CSI under DECCKM.
  - **Reason:** xterm applies the SS3 introducer only to the unmodified form; ctrl+left is CSI 1;5D in both modes.

## Failures

## Validation

- TestKeyToBytesUnmappedKeysProduceNothing asserted F1 and F12 produce no bytes - it was the gap written down as an expectation. Replaced by TestKeyToBytesFunctionKeys; the remaining case is an empty key.

- just ci green. New: TestKeyToBytesFunctionKeys (12 keys plus ctrl+F1 and shift+F5), TestCursorKeysFollowTheRemote and TestLeavingTheAltScreenDropsApplicationCursorKeys, both end-to-end through the emulator like the bracketed-paste tests.

## Remaining risks

- Neither is verified against a real remote program; the manual list in docs/dev/bubbletea-v2-migration.md now carries a line for arrows in vim/less and F-keys in mc/htop.

## Handoff

- F13 and up stay unmapped: v2 reports shift+F1 as {KeyF1, ModShift}, so those codes only arrive from terminals that send them outright.
