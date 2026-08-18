---
id: "20260819-001131-take-pane-input-off-the-ui-goroutine-and-raise-the-fra"
title: "Take pane input off the UI goroutine, and raise the frame ceiling"
status: "completed"
updated: "2026-08-19T00:11:42+02:00"
base_commit: "0eedc46d5ebfb128b5b928603aae468a3f5e6f3f"
branch: "main"
agent: null
tags: ["input", "latency", "rendering", "ssh"]
files:
  - "internal/terminal/cwd.go"
  - "internal/terminal/input_test.go"
  - "internal/terminal/mouse.go"
  - "internal/terminal/mouse_test.go"
  - "internal/terminal/paste_test.go"
  - "internal/terminal/terminal.go"
  - "internal/tui/model.go"
  - "internal/tui/paste_test.go"
---

# Take pane input off the UI goroutine, and raise the frame ceiling

## Goal

Remove the two remaining sources of hop's own input lag: a blocking SSH write on Bubble Tea's update goroutine, and the 60fps frame gap before an echo is painted.

## Scope

## Discoveries

- Every write to a pane's session used to happen on the caller's goroutine under Pane.mu — SendKey and SendMouse from Bubble Tea's update loop, writeString from the shell-integration goroutine, and the emulator's auto-response io.Copy through lockedWriter. An SSH channel Write blocks once the remote's window is exhausted or the link stalls, so a stalled far end froze the whole TUI (no repaint, no other key), and a large SendPaste could park it outright.

## Decisions

- **Decision:** One input queue per pane (Pane.in, 1024 chunks) drained by a single goroutine; Pane.mu and lockedWriter are gone.
  - **Reason:** The queue gives back exactly what the mutex was for — nothing interleaves halfway through a sequence — while no caller waits on the wire. A single drainer keeps SendKey, writeString, SendPaste and the emulator's auto-responses in the order they were made.
  - **Trade-off:** A full queue drops input rather than blocking or growing without bound; that only happens with a far end that has stopped reading entirely. Tests can no longer read the session the instant after a key: Pane.Flush exists for them (hop itself never waits on the wire), and internal/tui tests go through the flushPanes helper.

- **Decision:** tea.WithFPS(120) instead of Bubble Tea's default 60.
  - **Reason:** The renderer only paints when a frame is due, so the default added up to 16ms between an echo arriving and the screen showing it. A full 200x50 paint measures ~1.6ms, so the frames are nowhere near the cost of the ceiling.
  - **Trade-off:** 120 is Bubble Tea v1's maximum; there is nothing further to gain there.

## Failures

## Validation

- go build ./... , go vet ./... , go test ./... — all passed

- go test -race ./internal/terminal ./internal/tui — passed

- New tests: TestSendKeyDoesNotWaitForTheWire (50 keys against a writer that never returns, SendKey still returns and the input lands in order once the link comes back), TestQueuedInputKeepsItsOrder, TestFlushReturnsOnAClosedPane — internal/terminal/input_test.go

## Remaining risks

- Input is dropped, silently, if 1024 chunks ever queue up. Only a far end that has stopped reading gets there, and hop shows nothing when it happens — if dropped keystrokes are ever reported, this is the place.

## Handoff

- Ownership after this: internal/terminal/terminal.go owns the queue (Pane.in, send, Flush, the drain goroutine in New). Every other write path — mouse.go SendMouse, cwd.go writeString, paste.go SendPaste — goes through send and nothing touches sess.Stdin directly. Adding a new one means calling send, not writing to the session.

- Unmeasured on a real remote: the three fixes together take hop's own added latency from ~8-25ms to ~0-8ms, but the wire is untouched. Worth typing on a real box before assuming anything is left to fix.
