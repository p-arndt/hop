---
id: "20260819-012257-make-a-dropped-keystroke-visible-instead-of-silent"
title: "Make a dropped keystroke visible instead of silent"
status: "completed"
updated: "2026-08-19T01:22:57+02:00"
base_commit: "512b0d23019d6e492e1b3a04ae9e398a45c6095d"
branch: "main"
agent: null
tags: ["input", "review", "ux"]
files:
  - "internal/terminal/cwd.go"
  - "internal/terminal/input_test.go"
  - "internal/terminal/paste.go"
  - "internal/terminal/terminal.go"
  - "internal/tui/input_test.go"
  - "internal/tui/keys.go"
  - "internal/tui/model.go"
  - "internal/tui/paste.go"
---

# Make a dropped keystroke visible instead of silent

## Goal

Close the one review finding left open: a full input queue discarded input with nothing said about it.

## Scope

- Supersedes the decision in 20260819-011831 ("Keep the silent drop when the queue is full"): the drop stays — blocking is the freeze the queue removed, and an unbounded queue holds input that is meaningless by the time it lands — but it is no longer silent.

## Discoveries

## Decisions

- **Decision:** Pane.send reports whether the bytes were taken, and SendKey/SendKeys/SendPaste pass that up; the TUI turns a refusal into a warning status naming the host (model.reportInput).
  - **Reason:** The drop happens on the UI goroutine, in the same call the key came in on, so the refusal needs no plumbing out of the pane — the caller that would have shown the keystroke is the one that learns it did not go. A truncated command line the user never saw coming is the failure mode worth spending a status line on.
  - **Trade-off:** An empty write and a closed pane report success: neither is a drop, and a dead pane already has its own banner. writeString (the cwd hook) ignores the result deliberately — a dropped hook only leaves Cwd empty.

## Failures

## Validation

- go vet ./... , go test ./... — passed

- go test -race -count=2 ./internal/terminal ./internal/tui — passed

- New tests: TestAFullQueueRefusesInput (the queue is bounded, refuses past that, and everything it took still lands in order), TestDroppedInputIsReported (the warning reaches the model, as a warning, naming the host)

## Remaining risks

## Handoff

- Nothing outstanding from the review. The whole input path is still unverified against a real Windows console and a real host.
