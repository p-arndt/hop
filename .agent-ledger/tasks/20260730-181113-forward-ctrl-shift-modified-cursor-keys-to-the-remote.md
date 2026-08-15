---
id: "20260730-181113-forward-ctrl-shift-modified-cursor-keys-to-the-remote"
title: "Forward ctrl/shift-modified cursor keys to the remote shell"
status: "completed"
updated: "2026-07-30T18:11:45+02:00"
base_commit: "4580597c1d1e50cf763edb47d6febb71b1f7dbf3"
branch: "7-ctrl-arrow-keys-for-word-navigation-not-working"
agent: null
tags: ["keys", "terminal"]
files:
  - "internal/terminal/cursor_test.go"
  - "internal/terminal/terminal.go"
---

# Forward ctrl/shift-modified cursor keys to the remote shell

## Goal

ctrl+left/right (word-wise motion) did nothing inside a connected pane; encode modified cursor keys as their xterm CSI sequences.

## Scope

- Nothing in the TUI intercepts ctrl+arrow — handleKey/handleShellKey/handleEditorKey pass it straight to Pane.SendKey — so the whole bug lived in internal/terminal/terminal.go's encoder.

## Discoveries

- Bubble Tea v1 gives modified cursor keys their OWN key types (tea.KeyCtrlLeft, KeyShiftLeft, KeyCtrlShiftLeft, KeyCtrlHome/End, KeyCtrlPgUp/PgDown) — they never arrive as tea.KeyLeft with a modifier flag. keyBytes only matched KeyLeft and a 'ctrl+<one letter>' string form, so 'ctrl+left' matched neither, fell off the end of the function and returned nil: nothing at all was written to the SSH stdin.

- Review follow-up: tea.KeyShiftTab and tea.KeyInsert dropped to nil for the same reason ctrl+left did — their String() ("shift+tab", "insert") matches no branch and they carry no runes. Added ESC[Z (back-tab, zsh menu-complete backwards) and ESC[2~. Any tea.Key* type with a multi-word String() and no runes fails this way, and keyBytes' fallback returns nil rather than erroring, so a dropped key is always silent.

## Decisions

- **Decision:** Encode the modifier inside the sequence (CSI 1;<1+bitmask><final>, bitmask shift 1 / alt 2 / ctrl 4) in a new modifiedKeyBytes, called from keyToBytes BEFORE the meta-ESC prefix.
  - **Reason:** xterm puts the modifier in the CSI parameter; readline and every editor bind ESC[1;5D, not an ESC-prefixed ESC[D. Running it before the Alt branch also makes ctrl+alt+left the correct ESC[1;7D instead of a doubled ESC.
  - **Trade-off:** Plain alt+arrow keeps the older ESC-prefix form (asserted by TestKeyToBytesPrefixesAltWithEsc) — the two encodings now sit side by side, deliberately.

## Failures

## Validation

- go test ./... — passed (new TestKeyToBytesModifiedCursorKeys in internal/terminal/cursor_test.go covers ctrl/shift/ctrl+shift arrows, ctrl+home/end, ctrl+pgup/pgdown, ctrl+alt+left, and pins the unmodified forms)

## Remaining risks

- Arrows are always sent in normal-cursor form (ESC[A), never the DECCKM application form (ESC OA), whatever mode the remote program set. Pre-existing and out of scope here; readline and vim accept both.

- On macOS, ctrl+←/→ is taken by Mission Control ('Move left/right a space') by default and never reaches any terminal app — the encoder is right but the key may still look dead until that system shortcut is turned off. See the hop-keybindings-on-macOS memory.

## Handoff

- Live-check on a real host that ctrl+left/right move by word in bash/zsh readline; headless tests only assert the bytes.
