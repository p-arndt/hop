---
id: "20260816-232706-close-the-four-open-testing-todos"
title: "Close the four open testing TODOs"
status: "completed"
updated: "2026-08-16T23:27:17+02:00"
base_commit: "6203bed51576956a07193d889bb2dd50ca220e8e"
branch: "main"
agent: null
tags: ["coverage", "tests"]
files:
  - "TODO.md"
  - "internal/action/action.go"
  - "internal/action/action_test.go"
  - "internal/filebrowser/render_test.go"
  - "internal/store/frecency_test.go"
  - "internal/store/import_test.go"
  - "internal/terminal/keys_test.go"
---

# Close the four open testing TODOs

## Goal

Cover store import parsing + frecency ordering, the action package, terminal's keyToBytes mapping table, and filebrowser rendering.

## Scope

## Discoveries

- keyToBytes' modifier-encoded and alt-prefixed cases were already covered in terminal/cursor_test.go; the new keys_test.go covers only the base table (plain keys, the full ctrl+letter alphabet, ctrl symbols, unmapped keys -> nil), so the two files split by encoding rather than by key.

- filebrowser rendering tests pin the clock through the package's now var (sort.go) so the mtime column is stable, and strip SGR with a regexp before asserting layout — the styles render differently depending on the color profile the environment reports.

## Decisions

- **Decision:** Extract vscodeArgs from OpenVSCodeRemote instead of leaving the argv construction inline.
  - **Reason:** OpenVSCodeRemote ends in exec.Command(...).Start(), so nothing about the argument list was assertable without spawning a process; the alias can come from an imported ssh config, which is exactly what the argv-not-shell shape protects.
  - **Trade-off:** One more unexported helper in a 49-line package.

## Failures

- **Approach:** First cut of the column-dropping test used w=32 and expected the time column gone.
  - **Evidence:** w=32: time column present = true, want false
  - **Lesson:** renderRow's drop threshold is room(w-2) - len(tail) - 1 >= 12; at w=32 the full 'size time' tail still leaves the name exactly 12 cells, so the boundary case is w=30.

## Validation

- go test ./... — passed; go vet ./internal/... — clean; gofmt -l — clean

## Remaining risks

- humanizeBytes can never print a T: its loop stops at i < len(units)-1, so a terabyte-sized file reads as 5120.0G. The doc comment says B/K/M/G, so the test pins the observed behaviour rather than the unreachable unit.

## Handoff

- If humanizeBytes should really reach T, fix the loop bound in filebrowser.go and update TestHumanizeBytes with it.
