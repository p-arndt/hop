---
id: "20260819-000400-stop-the-windows-paste-coalescer-delaying-every-keystr"
title: "Stop the Windows paste coalescer delaying every keystroke"
status: "completed"
updated: "2026-08-19T00:04:26+02:00"
base_commit: "9e4606c6ca67ae3a6e6ae58a28acfc98684f1a2d"
branch: "main"
agent: null
tags: ["input", "latency", "paste", "windows"]
files:
  - "internal/tui/model.go"
  - "internal/tui/paste.go"
  - "internal/tui/paste_test.go"
---

# Stop the Windows paste coalescer delaying every keystroke

## Goal

Take hop's own added latency off remote typing on Windows: no key a hand produces is held back, while a synthesised paste burst is still recognised.

## Scope

## Discoveries

- Reported symptom: keystrokes in a remote shell sometimes do not appear at once (Windows). Measured hop's own contribution rather than guessing. Culprit: takeKey (internal/tui/paste.go) buffered EVERY pastable key — runes, space, tab, enter — for pasteGap=8ms before it reached the wire, on every keystroke into a pane, Windows only. Verified the timer is not the loose part: a Go 8ms time.After on this box lands at avg 8.3ms / worst 8.7ms, so the delay was real and constant, not scheduler slop.

- Two other latency sources measured and ruled out as the main cause, worth not re-measuring: (1) rendering is cheap — vt Render+cursor overlay is 0.07ms at 80x24 and 0.33ms at 200x50, and model.View() is 1.3ms at 200x50; (2) Bubble Tea's standard renderer caps at 60fps, so up to ~16ms of frame jitter, unchanged by this task.

## Decisions

- **Decision:** Gate entry into a burst on the gap since the previous pastable key (burstGap=10ms) instead of buffering every key.
  - **Reason:** A synthesised paste arrives microseconds apart; a hand needs tens of milliseconds and a held key repeats no faster than ~30ms. So the first key of any burst — which is every key that is actually typed — can go out immediately, and only what follows too fast to be typed is buffered.
  - **Trade-off:** A paste's first character now travels as a keystroke ahead of the bracketed remainder: a full-screen program acts on that one character, and a paste beginning with a newline submits the line it was pasted at. burstGap must also stay well above hop's per-key work (Update + repaint, ~1-3ms) or a slow frame would split a paste into keystrokes.

## Failures

## Validation

- go test ./... — passed (full suite, incl. internal/tui and the docker-tagged files that were already skipping)

- go build ./... and go vet ./internal/tui — clean

## Remaining risks

- Unfixed and separate: Pane.SendKey/SendPaste/writeString (internal/terminal/terminal.go, cwd.go) write to the SSH channel synchronously from the Bubble Tea update goroutine, under p.mu. When the remote window is exhausted or the link stalls, that write blocks the whole UI — no repaint, no other key. A large SendPaste can freeze it outright. Fix would be a per-pane writer goroutine behind a buffered channel.

- Unverified on a real Windows paste: burstGap=10ms is derived from the console's microsecond spacing plus a margin for hop's per-key work, not measured against an actual paste on this machine. If a paste ever arrives as keystrokes instead, burstGap is the single constant to raise.

## Handoff

- model.clock (internal/tui/model.go) is nil in a running hop and set only by tests: a test types a whole burst inside a microsecond, so the gap gate cannot be exercised against the wall clock. newTestClock in paste_test.go drives it.

- Try a multi-line paste into remote vim and a held j on a real box; if typing still feels late, the next suspect is the blocking SSH write on the UI goroutine (see risks).
