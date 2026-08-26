---
id: "20260826-200440-altgr-no-longer-corrupts-remote-password-prompts"
title: "AltGr no longer corrupts remote password prompts"
status: "completed"
updated: "2026-08-26T20:05:15+02:00"
base_commit: "65d4d899f3d553f3ba24d904be11ae61c434d033"
branch: "main"
agent: null
tags: ["auth", "keyboard", "windows"]
files:
  - "domain/contexts/keyboard/README.md"
  - "domain/contexts/keyboard/language.md"
  - "internal/tui/altgr.go"
  - "internal/tui/altgr_test.go"
  - "internal/tui/model.go"
---

# AltGr no longer corrupts remote password prompts

## Goal

A key hop sends must be a key the user pressed: the Windows console's modifier key-downs must not reach the remote program, where a password prompt reads them as part of the secret.

## Scope

## Discoveries

- Bubble Tea's Windows console reader (key_windows.go) filters only VK_SHIFT key-downs, so the VK_CONTROL and VK_MENU records of an AltGr press arrive as key events of their own. Their Char is 0, keyType() returns KeyRunes for them, and one AltGr+q therefore delivers three KeyMsgs: NUL, alt+NUL, alt+'@'. Same for every plain ctrl or alt chord: a NUL precedes it.

- terminal.keyToBytes turns those into 0x00 and ESC 0x00 on the wire. A shell's line editor discards both, which is why issue #17's fix looked complete; sudo/ssh/passwd read every byte, so the secret became "\x00\x1b\x00@..." and only password entry appeared broken.

- hop's own AUTHENTICATION card had the same corruption (two NUL runes appended per AltGr character), invisible because the field is masked with one bullet per rune.

## Decisions

- **Decision:** Drop the phantom key events in model.Update, ahead of normalizeAltGr, by their shape: a KeyRunes message whose runes are all NUL.
  - **Reason:** Bubble Tea v1 does not expose the virtual key code or ControlKeyState, so the modifier record and a real ctrl+space are indistinguishable in the KeyMsg; dropping is the only fix available without hop's own console reader.
  - **Trade-off:** ctrl+space (tmux prefix) can no longer be sent to the remote program on Windows. It was already broken there — the modifier record made hop send two NULs for it — and hop binds no ctrl+space of its own.

## Failures

## Validation

- Behaviour before the fix, captured by stashing model.go: the full AltGr+q sequence reached the pane as "\x00\x1b\x00@".

- go test ./... — all packages pass; go build ./... and go vet ./... clean. TestAltGrSendsOnlyTheCharacterToTheRemoteProgram and TestAltGrTypesOneCharacterIntoTheAuthCard drive the real three-event AltGr sequence end to end; both fail without the guard in model.Update.

## Remaining risks

- Not yet confirmed on the user's real console: the three-event sequence is derived from bubbletea's key_windows.go, not captured live. A dumper is built at %TEMP%\keydump.exe to verify the event shape.

- ctrl+space no longer reaches the remote program on Windows; a tmux user with that prefix will notice.

## Handoff

- If a NUL-carrying key must be sendable again, the fix is hop reading the console itself (or Bubble Tea v2) so ControlKeyState and the virtual key code survive into the KeyMsg — the same lead the issue #17 entry left.
