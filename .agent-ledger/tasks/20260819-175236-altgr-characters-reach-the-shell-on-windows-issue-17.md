---
id: "20260819-175236-altgr-characters-reach-the-shell-on-windows-issue-17"
title: "AltGr characters reach the shell on Windows (issue #17)"
status: "completed"
updated: "2026-08-19T17:53:01+02:00"
base_commit: "ad7b8c98dace9234b7d3c3bb0659d855beb08dd6"
branch: "17-alt-gr-and-ctrlalt-key-not-working-on-remote-shell"
agent: null
tags: ["input", "keys", "windows"]
files:
  - "internal/tui/altgr.go"
  - "internal/tui/altgr_test.go"
  - "internal/tui/model.go"
---

# AltGr characters reach the shell on Windows (issue #17)

## Goal

Typing @ (AltGr+q) and other third-level characters into a connected shell on a non-US Windows layout must produce the character, not an alt chord.

## Scope

- Gated by altGrKeyboard = runtime.GOOS == "windows" (a var so tests can flip it). On macOS/Linux the same shape means a genuine ESC-prefixed meta chord and must not be rewritten.

## Discoveries

- Bubble Tea v1.3.10 reads the Windows console as key records (key_windows.go). Its KeyMsg.Alt is ControlKeyState.Contains(LEFT_ALT|RIGHT_ALT), and coninput's Contains is an any-bit test, so an AltGr press (LEFT_CTRL+RIGHT_ALT) arrives as an alt-modified rune carrying the composed character. keyType() deliberately returns KeyRunes for AltGr, so the character is there — only the modifier is wrong.

- Two things then ate the character: handleShellKey/handleEditorKey read alt+1..9 as tab jumps (AltGr+7/8/9 are { [ ] on a German layout), and terminal.keyToBytes prefixes any alt key with ESC, so the shell got a meta chord instead of the character.

- Pressing ctrl+alt+<key> by hand composes the same character but takes a different branch of Bubble Tea's keyType: with ctrl set and no RIGHT_ALT it switches on the character, and '@' alone maps to KeyCtrlAt. Hop binds no ctrl+alt or ctrl+@ chord (checked internal/keys), so KeyCtrlAt+Alt is safe to rewrite to '@'.

## Decisions

- **Decision:** Normalize the key once in model.Update (internal/tui/altgr.go, normalizeAltGr) rather than at the pane or per handler.
  - **Reason:** Every mode reads msg.String(); a fix at the pane would still let alt+1..9 and the leader swallow the character, and hop's own fields (host form, filter) need @ too.
  - **Trade-off:** The rune is told from a chord by shape, not by the real control-key state, which KeyMsg does not carry: an alt-modified rune that is printable and not ASCII alphanumeric is treated as a composition. Alt chords hop uses (alt+1..9, alt+b/f) stay chords; a hypothetical alt+. binding on Windows would not.

## Failures

## Validation

- go test ./... — passed (new internal/tui/altgr_test.go covers composed characters, real alt chords, the ctrl+@ case, non-Windows, and end to end that @ reaches a pane's stdin as "@")

- go build ./... and GOOS=windows go build ./... — passed; go vet ./... clean

## Remaining risks

- Unverified on a real Windows console: the premise that a genuine alt+letter arrives with no character (so its runes are not printable) comes from reading key_windows.go, not from a live test. If some terminal does deliver alt+a as a printable 'a', the ASCII-alphanumeric exclusion still keeps it a chord.

## Handoff

- If a user reports a third-level character that is ASCII alphanumeric on their layout, the shape test in composedRunes is where it fails; the real fix would be reading ControlKeyState, which needs Bubble Tea v2 or hop's own console reader.
