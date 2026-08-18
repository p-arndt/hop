---
id: "20260819-011831-review-follow-ups-on-the-pane-input-queue-and-the-burs"
title: "Review follow-ups on the pane input queue and the burst gate"
status: "completed"
updated: "2026-08-19T01:18:32+02:00"
base_commit: "0eedc46d5ebfb128b5b928603aae468a3f5e6f3f"
branch: "main"
agent: null
tags: ["input", "latency", "review"]
files:
  - "internal/terminal/cwd.go"
  - "internal/terminal/input_test.go"
  - "internal/terminal/mouse.go"
  - "internal/terminal/mouse_test.go"
  - "internal/terminal/paste_test.go"
  - "internal/terminal/terminal.go"
  - "internal/tui/model.go"
  - "internal/tui/paste.go"
  - "internal/tui/paste_test.go"
---

# Review follow-ups on the pane input queue and the burst gate

## Goal

Close the code-review findings on the input queue and the Windows burst gate.

## Scope

## Discoveries

- Findings fixed: (1) the drain goroutine quit on the first failed session write, which left a live pane silently swallowing input and Flush hanging — it now keeps draining and only ends when the pane closes; (2) Flush counted in-flight chunks with a sync.WaitGroup while send could Add from the UI and from the emulator's response pump, which is the WaitGroup misuse that panics — it now queues a marker chunk and waits for that; (3) burstGap went 10ms -> 20ms, because the gap is measured where a key reaches Update, so what sits between two keys of a paste is hop's own repaint (~1.6ms) — 10ms left only ~6x margin before a paste would split into keystrokes; (4) Enter no longer takes the immediate path, so a clipboard beginning with a newline cannot submit the line it was pasted at; (5) a burst replayed as keystrokes goes out as one queue item (Pane.SendKeys) instead of up to pasteMax separate ones, so a stalled link cannot drop half a command line; (6) the burst tests now drive the test clock instead of relying on two handleKey calls landing inside burstGap on a loaded CI runner.

## Decisions

- **Decision:** Keep the silent drop when the queue is full, rather than blocking or signalling.
  - **Reason:** The queue only fills behind a far end that has stopped reading entirely, and the alternatives are worse: blocking is the freeze this whole change removed, and an unbounded queue holds input that is meaningless by the time it lands. SendKeys also took the realistic pressure off — a replayed burst is one item now, not thousands.
  - **Trade-off:** Nothing tells the user input was dropped. If that is ever reported, terminal.Pane.send is the place, and a status line would have to be plumbed out of the pane.

## Failures

## Validation

- go vet ./... and go test ./... — passed

- go test -race -count=3 ./internal/terminal ./internal/tui — passed (and -count=2 after the SendKeys change)

- New tests: TestAFailedWriteDoesNotStopTheQueue (internal/terminal/input_test.go), TestEnterIsBufferedEvenAfterAPause (internal/tui/paste_test.go)

## Remaining risks

- burstGap now sits between hop's per-key repaint cost and Windows' fastest key repeat (~32ms); both walls are estimates rather than measurements on the reporting machine.

## Handoff

- Type and paste on a real Windows box against a real host — that is the only thing none of this has been checked against.
