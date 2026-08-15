---
id: "20260731-195141-hotfix-lone-enter-misread-as-a-paste-remote-terminal-s"
title: "Hotfix: lone Enter misread as a paste, remote terminal showed no output"
status: "completed"
updated: "2026-07-31T19:52:03+02:00"
base_commit: "30d88a83c776296009d0cf1b1ddd8b1a8050d7ac"
branch: "9-050-not-showing-output-on-remote-terminal"
agent: null
tags: ["hotfix", "paste", "terminal", "windows"]
files:
  - "internal/tui/paste.go"
  - "internal/tui/paste_test.go"
---

# Hotfix: lone Enter misread as a paste, remote terminal showed no output

## Goal

Fix the v0.5.0 regression where no command output appears in remote shells on Windows: a typed Enter was coalesced into a one-newline bracketed paste, which bash inserts instead of executing.

## Scope

## Discoveries

- Root cause in internal/tui/paste.go (shipped in 8c4696a, part of v0.5.0): the Windows keystroke-burst coalescer holds pane-bound keys for pasteGap (8ms). A typed Enter almost always arrives alone in its burst, and looksPasted() treated any KeyEnter as proof of a paste — so a lone Enter went out as ESC[200~ CR ESC[201~. Bash with bracketed paste on (readline default since 5.1) INSERTS bracketed text instead of executing it: the command never ran, and every further Enter did the same. Symptom looked like 'terminal shows no output' though input echo still worked (letters were replayed as keystrokes).

- Diagnosis detour worth remembering: the alt-screen/mouse/OSC-scanner/clipboard paths were all checked and are innocent — the scanner only observes bytes, copyOut is a non-blocking one-worker mailbox, and no lock is held across emu calls (no pipe deadlock). The output PUMP was fine; the bug was on the INPUT side, which is why echo still rendered.

## Decisions

- **Decision:** looksPasted() now refuses any burst of fewer than 2 keys; a single key is always replayed as the keystroke it was.
  - **Reason:** A genuine paste is a burst of many synthesised keystrokes; a burst of one is just the key that was pressed. A rare real paste of a bare newline degrades to an Enter keystroke, which is what non-bracketed terminals send anyway.
  - **Trade-off:** A typed Enter within 8ms of the previous character still joins that burst and is sent bracketed — humanly near-impossible, and identical to pasting the same text.

## Failures

## Validation

- go test ./... — all packages pass, incl. new TestALoneEnterIsAKeystrokeNotAPaste (fails on the old code); go vet + go build clean; user confirmed the remote shell executes commands again

## Remaining risks

## Handoff

- If any input-shape misclassification recurs, looksPasted in internal/tui/paste.go is the single decision point; pasteGap=8ms and pasteRun=4 are the tuning constants.
