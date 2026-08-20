---
id: "20260821-015135-parallel-refactor-of-the-three-column-tui-characterize"
title: "Parallel refactor of the three-column TUI: characterize, then unpick the coupling"
status: "completed"
updated: "2026-08-21T01:52:18+02:00"
base_commit: "fb0e1345fdb546ab6013d9c2d1ebfcf9b16330ea"
branch: "refactor/tui-layout-phases"
agent: null
tags: ["architecture", "layout", "refactor", "tui"]
files: []
---

# Parallel refactor of the three-column TUI: characterize, then unpick the coupling

## Goal

The SFTP column and split content area had rippled through most of internal/tui. Pin the layout behaviour down with characterization tests, then remove the three couplings that caused the ripple, without changing what the user sees.

## Scope

- Real base commit is 01243a8, not the one 'start' captured — the four commits were already on refactor/tui-layout-phases when this entry was opened. Use 'git diff 01243a8..' for the full change.

## Discoveries

- Geometry is derived independently in four places and nothing enforces agreement: layout.go (recomputeLayout/treeWidth/splitHalf), view.go (JoinHorizontal composition), mouse.go (zoneAt, treeLocal, contentLocal, clampToPane), and now selection.go (selectionW). The offsets differ by starting point — contentLocal reaches the right half with -base-w-3, clampToPane with base+=w+2 — so both are correct only relative to their own origin. This is the cause of the file-count ripple, and it is NOT fixed by this task.

- The frame is wider than the terminal below 28 columns: listWidth's floor of 16 and recomputeLayout's floor of 10 for paneW are independent of each other and of m.width, so a 20-column window renders 28 cells per body line. Pinned as-is by TestFrameOverrunsVeryNarrowWindows (layout_test.go) rather than fixed, because the test agent did not own production code.

- The strict frame contract holds today and is now asserted: every rendered body line is EXACTLY m.width, not merely no wider, and the line count is exactly m.height. The odd column an odd-width split leaves over is a real padded blank cell (JoinVertical), not a short line. Any layout change must keep this.

- contentW(s) answers for the SESSION, so it cannot be used as 'the width of what is on screen'. s.split stays true while the keyboard is in a shell of that same session, and renderShellPane still draws at m.paneW. selectionW therefore mirrors renderContent's switch instead. Both directions are regression-tested.

## Decisions

- **Decision:** Characterize first, refactor second — the golden tests were written and proven against the OLD code before anything moved.
  - **Reason:** Both refactors here are 'must look identical afterwards' changes. A test written after the fact only proves the new code is self-consistent.
  - **Trade-off:** Cost roughly half the total effort. The footer goldens (37 states) are regenerable via HOP_FOOTER_DUMP=1.

- **Decision:** Carry the split intent on OpenFileMsg (Beside) and stamp it only after the directory branch has returned.
  - **Reason:** The two 'clear the stale flag' sites existed solely because a split armed on a directory never comes back as a file. Making the marked message unproducible for a directory removes the reason rather than the symptom.
  - **Trade-off:** A second exported verb (ActivateBeside) rather than an option parameter — chosen because every other exported browser verb is zero-arg, and a variadic Activate would make the double-click site read as if it had a choice.

- **Decision:** footerArm tables live in view.go, not beside actions.go's spec tables.
  - **Reason:** A spec describes something runnable, resolved out of the keys registry and dispatched by layer. Several footer hints are literal strings for keys a card handles itself ('enter submit', ':q close') with no registry row, so they could never be specs.
  - **Trade-off:** Two table vocabularies in the package instead of one.

- **Decision:** Split the work by FILE OWNERSHIP across four agents in separate git worktrees, not by feature.
  - **Reason:** Three of the four slices edit package tui. In a shared checkout one agent's half-finished edit breaks another's go test. Disjoint file sets plus worktrees made the merge a straight copy with zero conflicts.
  - **Trade-off:** Cross-slice fixes had to be deferred to the merge — the three browserSize sites in session.go and the dead CursorOnFile in tree.go.

## Failures

- **Approach:** The one-line fix specified for the selection-width bug, m.contentW(m.sessions[m.active]), was wrong and would have introduced a new defect.
  - **Evidence:** TestSelectionInAShellIsMeasuredAtTheFullWidth: clipboard = <82 chars>, want <120 chars> — the shell's row was cut off at half the content area
  - **Lesson:** A helper named for a session ('is this session split?') is not a helper for the screen ('how wide is the box this was drawn in?'). Before reusing an existing accessor as a width, check every caller path, not just the one the bug was reported on. Writing the failing test for BOTH directions is what caught it.

## Validation

- go build ./... — clean; go vet ./... — clean; go test -count=1 ./... — all packages ok (tui 3.6s, filebrowser 6.7s). Both bug fixes proven by regression tests that were watched FAILING against the old code first. Footer goldens (37 states) passed against the old switch before the refactor.

## Remaining risks

- Never run against a real SSH host — the whole three-column layout is still only ever drawn by unit tests, as the parent entry already flagged. The split halves and the 96-128 column fallback band are the untested surfaces.

- Geometry is still derived in four independent places; selection.go's selectionW added a fourth copy of the render switch. Until the frame/rect work lands, any layout change must be made in all four.

- The frame overruns terminals narrower than 28 columns. TestFrameOverrunsVeryNarrowWindows asserts the BROKEN behaviour on purpose — flip it when fixing, do not delete it.

## Handoff

- Phase 1, the one thing deliberately left undone: a single frame of rects computed once in recomputeLayout, consumed by view.go and mouse.go alike. layout_test.go was written specifically as its safety net — its round-trip assertion (zoneAt/treeLocal/contentLocal agree cell-for-cell with the boxes View drew) is what makes the change verifiable. Fix the sub-28-column overrun in the same pass.

- No key collapses a split; collapseSplit is only reached by closing files. Decide whether that is intended before binding anything — it touches the same code as the split work.
