---
id: "20260821-154535-group-model-into-layout-and-focus-structs"
title: "Group model into layout and focus structs"
status: "completed"
updated: "2026-08-21T15:45:36+02:00"
base_commit: "c9cdb311551f6981e43a8c87b1827b5f0e574289"
branch: "main"
agent: null
tags: ["refactor", "tui"]
files:
  - "TODO.md"
  - "internal/tui/cursor_test.go"
  - "internal/tui/details.go"
  - "internal/tui/editor_test.go"
  - "internal/tui/hostmgmt_test.go"
  - "internal/tui/input_test.go"
  - "internal/tui/keys_test.go"
  - "internal/tui/layout.go"
  - "internal/tui/layout_test.go"
  - "internal/tui/list.go"
  - "internal/tui/list_render_test.go"
  - "internal/tui/model.go"
  - "internal/tui/mouse.go"
  - "internal/tui/mouse_test.go"
  - "internal/tui/paste_test.go"
  - "internal/tui/reconnect_test.go"
  - "internal/tui/selection_test.go"
  - "internal/tui/settings_test.go"
  - "internal/tui/shells_test.go"
  - "internal/tui/view.go"
  - "internal/tui/view_test.go"
  - "internal/tui/vscode_test.go"
---

# Group model into layout and focus structs

## Goal

Phase 4, the last item of the refactor plan: give the 60-field model some structure without a rename storm.

## Scope

## Discoveries

- Go 1.27 allows promoted fields in composite literals; go.mod here says 1.26.4 while the toolchain is 1.27.0. Deliberately did NOT bump go.mod — a refactor must not raise the project's minimum Go version for its own convenience. Cost was rewriting 18 &model{...} literals in tests to layout:layout{...}/focus:focus{...}. If the minimum is ever raised to 1.27, those could be flattened back.

- Embedding is what kept this cheap: 279 non-test references to the moved fields (active 88, mode 38, sel 33, width 32, paneW 20, frame 16, height 16, chords 14 ...) all still resolve by promotion. Only composite literals break, and only in tests. Methods moved onto the embedded types are promoted too, so m.focused() and m.listWidth() are unchanged at every call site.

- Renamed the spinner counter model.frame to spinFrame, which freed the name for the layout box set — that field had to be called 'fr' when the frame landed because 'frame' was taken. m.frame.tree now reads as it should.

## Decisions

- **Decision:** Move only the methods that need no session, and pass the rest their answer.
  - **Reason:** buildFrame was the only interesting case: everything in it is arithmetic on widths except whether the content is halved, which is s.split. Taking that as a bool parameter let the whole function move to *layout instead of stranding it on *model.
  - **Trade-off:** One more parameter at the single call site, in exchange for a layout type that can be built and tested without a model.

## Failures

## Validation

- go build ./... clean; go vet ./... clean; go test -count=1 ./... all packages ok; go run ./tools/docsgen -check clean. The 23 layout characterization cases passed unchanged, which is what makes a refactor of this shape checkable at all.

## Remaining risks

- Embedded fields are promoted, so a future field added to layout or focus that shares a name with a model field would shadow silently rather than fail to compile. There is no collision today (checked field names against every *model method name as well).

## Handoff

- internal/tui is now the only obvious target left, and the honest answer is to leave it: package extraction is worth it per component that owns its state, the way internal/filebrowser does. importer, guidance and hostform qualify on measured coupling; tunnels, settings, help, palette and menu do not.
