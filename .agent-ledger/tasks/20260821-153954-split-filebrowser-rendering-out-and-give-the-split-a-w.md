---
id: "20260821-153954-split-filebrowser-rendering-out-and-give-the-split-a-w"
title: "Split filebrowser rendering out, and give the split a way to close"
status: "completed"
updated: "2026-08-21T15:40:44+02:00"
base_commit: "be6a69e0929831288d4b2222fd80e744861faf8e"
branch: "main"
agent: null
tags: ["filebrowser", "keybindings", "tui"]
files:
  - "KEYBINDINGS.md"
  - "README.md"
  - "TODO.md"
  - "docs/30-browser.md"
  - "docs/31-editor.md"
  - "index.html"
  - "internal/keys/keys.go"
  - "internal/tui/actions.go"
  - "internal/tui/actions_test.go"
  - "internal/tui/editor_test.go"
  - "internal/tui/help.go"
  - "internal/tui/keys.go"
  - "internal/tui/keys_test.go"
  - "internal/tui/view.go"
---

# Split filebrowser rendering out, and give the split a way to close

## Goal

Finish the two items left over from the layout refactor: extract filebrowser's drawing into render.go, and bind a key to collapse a split content area, which until now could only be left by closing files.

## Scope

## Discoveries

- The keybinding agent's own instructions described view.go's footer as two ordered tables; it found a switch instead and said so rather than forcing the instruction. That mismatch was the tell that the base was stale, ahead of any git command.

- ctrl+backslash is effectively unclaimed by terminal editors, which is what makes it safe in a layer that must forward everything it does not handle. ctrl+w, ctrl+s, ctrl+e/y/n/p/r were all rejected for that reason — the Editor layer forwards to a real remote editor and must not steal its keys.

- filebrowser's render split was decided per symbol by caller evidence, and half the obvious candidates failed it: windowRows is motion (only move() calls it), humanizeBytes and truncateText are shared with transfer.go and prompt.go, and the accent palette is package-wide because marks.go rebuilds markGlyph from it. filebrowser.go 967 -> 762 (762 not 745 because Phase 3's Beside work is also in it), render.go 246.

## Decisions

- **Decision:** Keep the editor arm's extras as a method (editorExtras) rather than a literal in the footer table.
  - **Reason:** The unsplit hint is conditional on there being a split, and the table's hints field is already a func for exactly this case — a legend offering to close a split on a single-box screen names a key that would decline.
  - **Trade-off:** One more named method on *model, which Phase 4 will have to move if the footer tables ever leave view.go.

## Failures

- **Approach:** Both agent worktrees were created from origin/main (01243a8), NOT from the current branch head (8616405), so neither could see any of the five refactor phases that had already landed.
  - **Evidence:** The keybinding agent reported: 'this worktree's view.go is still the footerHints switch — there is no footerCardArms/footerModeArms here; the worktree base predates that refactor'
  - **Lesson:** NEVER merge a worktree agent's output by copying files — that silently reverts everything committed since the worktree's base. Check the base first with 'git worktree list', then merge with 'git diff <base>' piped into 'git apply -3'. Here that turned a would-be silent revert of all five phases into exactly one real conflict (the footer hint, which had to be re-expressed against the new table). The agent flagging its own stale base in its report is what caught it — worth asking agents to state what they found rather than only what they changed.

## Validation

- go build ./... clean; go test -count=1 ./... all packages ok; go run ./tools/docsgen -check exits 0, so KEYBINDINGS.md, README.md and index.html match docs/.

- The filebrowser move was verified pure: deleted text diffed byte-for-byte against inserted text over all 201 non-blank lines, and no test file was modified.

## Remaining risks

- Still never run against a real SSH host. ctrl+backslash in particular is a key whose terminal encoding varies; it is bound and unit-tested but has never been pressed in a real terminal.

## Handoff

- Phase 4 is the only thing left from the plan: internal/tui/model.go has 60 fields. Measured call sites for the two candidate structs — layout{width 32, height 16, ready 2, sidebarHidden 5, treeHidden 4, paneW 20, paneH 8, fr 16} and focus{active 88, mode 38, sel 33, dragGen 3, chords 14}, ~279 in non-test code. Go's embedded-struct field promotion makes this nearly churn-free: embedding layout and focus anonymously keeps m.width and m.active resolving, so the value is in moving the pure methods onto the structs, not in renaming call sites. Verified no name collisions: no existing type or method is called layout or focus, and no model field shares a name with a *model method.

- Splitting internal/tui into packages: only worth it per component that owns its state, the way internal/filebrowser already does. Measured coupling says importer, guidance and hostform qualify; tunnels and settings do not (session machinery / touches everything); help, palette and menu are views over the action registry rather than components. Reassess after Phase 4.
