---
id: "20260820-234053-sftp-browser-becomes-a-file-explorer-tree-column-marks"
title: "SFTP browser becomes a file explorer: tree column, marks, remote copy/move, split panes"
status: "completed"
updated: "2026-08-21T01:30:00+02:00"
base_commit: "bf54d9089f27232a0b6f80639edf145d2cf40f85"
branch: "main"
agent: null
tags: ["filebrowser", "layout", "sftp", "tui"]
files:
  - "KEYBINDINGS.md"
  - "README.md"
  - "TODO.md"
  - "docs/30-browser.md"
  - "docs/31-editor.md"
  - "index.html"
  - "internal/filebrowser/copymove.go"
  - "internal/filebrowser/fbtest/fbtest.go"
  - "internal/filebrowser/filebrowser.go"
  - "internal/filebrowser/filebrowser_test.go"
  - "internal/filebrowser/marks.go"
  - "internal/filebrowser/mouse_test.go"
  - "internal/filebrowser/ops.go"
  - "internal/filebrowser/ops_test.go"
  - "internal/filebrowser/prompt.go"
  - "internal/filebrowser/prompt_test.go"
  - "internal/filebrowser/render_test.go"
  - "internal/filebrowser/sort.go"
  - "internal/filebrowser/sort_test.go"
  - "internal/filebrowser/transfer.go"
  - "internal/filebrowser/transfer_test.go"
  - "internal/filebrowser/tree.go"
  - "internal/filebrowser/tree_test.go"
  - "internal/keys/keys.go"
  - "internal/sftpx/sftpx.go"
  - "internal/sftpx/sftpx_test.go"
  - "internal/tui/actions.go"
  - "internal/tui/actions_test.go"
  - "internal/tui/editor_test.go"
  - "internal/tui/help.go"
  - "internal/tui/keys.go"
  - "internal/tui/keys_test.go"
  - "internal/tui/landing.go"
  - "internal/tui/layout.go"
  - "internal/tui/mode_test.go"
  - "internal/tui/model.go"
  - "internal/tui/mouse.go"
  - "internal/tui/mouse_test.go"
  - "internal/tui/reconnect_test.go"
  - "internal/tui/session.go"
  - "internal/tui/sidebar_test.go"
  - "internal/tui/status.go"
  - "internal/tui/tabs.go"
  - "internal/tui/theme.go"
  - "internal/tui/view.go"
  - "internal/tui/view_test.go"
---

# SFTP browser becomes a file explorer: tree column, marks, remote copy/move, split panes

## Goal

Replace VSCode Remote Explorer for a colleague who moves files daily: a permanently visible tree beside the file being read, plural file operations, and copy/move that never types a path. Covers the two review rounds (/code-review, /security-review) and the fixes they produced — one change, one commit.

## Scope

- Work was split across three agents by Go package (sftpx / filebrowser / tui). The shared seam — internal/keys actions and bindings, plus compiling sftpx stubs — was written first by hand, which is what let the three build independently.

## Discoveries

- pkg/sftp v1.13.11 has no copy-data@openssh.com and no way to send an arbitrary extended packet (HasExtension only reports). A remote-to-remote copy therefore streams through this process: every byte crosses the link twice, so "c" costs double what downloading the same file costs. Verified by grepping the module cache, not assumed.

- The tree kept the flat-row-index invariant on purpose: View/RowAt/Select/Scroll and every motion still address b.rows, the flattened visible list. That is why the whole pointer layer in filebrowser survived the tree unchanged, and it is the first thing to preserve in any further tree work.

- m.mode changed meaning, not shape: modeBrowser/modeEditor now say where the keyboard is, not what fills the right pane. hasTree()/treeInline()/splitOn()/session.editor() carry what the modes used to imply.

- The card, the palette and the footer are hand-written allowlists, not generated from the key registry, so every key added to internal/keys has to be added to all three by hand or it is invisible. TestEveryBrowserActionIsDiscoverable now walks the Browser layer and fails on any bound action missing from both the card and the palette; browserHelpActions was extracted from help.go so the test can read it.

- transferTickMsg carries the *transfer and the comment calls pointer identity a generation counter, but that identity is per-JOB, not per-ITEM. finish() advancing a batch must not schedule a new tick: the running chain already re-arms across the item boundary, and a second one per item multiplies the repaint rate.

- overlay.view stripped the typed text but not the label, and the label carries remote filenames (delete/overwrite/collision). A filename holding OSC or ANSI reached the terminal verbatim, one keystroke from being answered. Pre-existing sink, widened by the new copy/move prompts, now stripped in both branches.

## Decisions

- **Decision:** Build a persistent tree column plus content panes, not a two-pane Midnight Commander.
  - **Reason:** The second MC pane exists to answer "where does this go" in a UI without a tree. With a tree and a pinned target both on screen, that job is already done, and two listings cost ~40 columns each in the place the user wants to read text.
  - **Trade-off:** No symmetric source/destination model: the target is one pinned directory, so a swap-panes workflow is not expressible.

- **Decision:** Marks are global across the tree, keyed by absolute remote path.
  - **Reason:** Several directories are open at once and their rows interleave, so "the marks of the current directory" is not a set the user can see.
  - **Trade-off:** A mark inside a collapsed directory still counts; the footer must always show the total or it becomes a trap.

- **Decision:** Move goes through the transfer machinery, not inline.
  - **Reason:** A rename is instant, but sftpx.Move falls back to a full copy across a filesystem boundary. Inline, that freezes the UI goroutine for the length of a copy.
  - **Trade-off:** A bar that appears and vanishes in one frame for the common rename case.

- **Decision:** sftpx.Move refuses a destination name that already exists, and the browser reports the collision from the keystroke.
  - **Reason:** The copy fallback truncates what is there and charges a whole copy to do it; a silent remote overwrite is the worst outcome of the three.
  - **Trade-off:** The fallback became unreachable from Move in tests, so it was extracted as moveByCopy and is tested directly — a mount boundary cannot be staged in-process.

- **Decision:** Copy asks before overwriting, and the question names the colliding files (up to three, then "+N more").
  - **Reason:** sftpx.Copy writes through Create, which truncates, and every other direction already asks. One "y" covers the whole batch, so "overwrite 3 entries?" would ask for consent to something the user cannot see.
  - **Trade-off:** Past three names the list is trimmed, so a very large batch still hides some of what it overwrites. A per-file y/n/a/q flow has no room in a one-line status prompt.

- **Decision:** destFor skips entries already in the target and counts them, instead of refusing the batch.
  - **Reason:** Refusing everything because one file was already there breaks the mark-a-screenful workflow that marking exists for. download() already sets the precedent by skipping directories and naming the count.
  - **Trade-off:** A directory into its own subtree is still a hard refusal — that one recurses into what it is writing, so it is not merely nothing to do.

- **Decision:** Split does not arm on a directory at all, instead of being spent afterwards.
  - **Reason:** The flag was cleared only in doBrowser and openFile, so the pointer path never spent it and a double-click landed in an unasked-for split. Fixing the arming removes the class rather than one route out of it.
  - **Trade-off:** filebrowser gained an exported CursorOnFile() purely for the tui to ask.

- **Decision:** pruneTarget only drops a target whose parent has actually been listed.
  - **Reason:** A target inside a directory nobody has opened is unread, not absent; forgetting it there would silently lose an aim the user set.

## Failures

- **Approach:** The whole selection/target feature shipped invisible: bound, working, and in no menu.
  - **Lesson:** The package split that made the work parallel is what caused it — the filebrowser agent added the keys, the tui agent added only its own three to the shared surfaces, and neither owned the other's. Any split along a package boundary needs the shared discovery surfaces assigned to someone explicitly, or verified afterwards. The guard test is the durable half of the fix.

- **Approach:** Marks were cleared when a batch job was built, not as each item completed.
  - **Evidence:** TestMoveStopsAtTheFirstFailure: marks = map[], want the entry that did not move still marked
  - **Lesson:** batchError promises "still marked, ready for the same keystroke"; clearing up front broke that for copy and download too, and only the move test caught it because the copy tests never asserted marks after a failure. The mark is now spent in finish(), per completed item. A test that asserts the happy path does not assert the promise the error message makes.

- **Approach:** Recursive copy followed symlinks via Stat, so a tree containing a link to a directory was uncopyable.
  - **Lesson:** sftp Open refuses a directory, so the whole copy aborted mid-tree with no rollback. Links are now recreated with ReadLink/Symlink and the root is Lstat'ed. A link pointing elsewhere would otherwise also have silently inflated the copy into a second full tree.

- **Approach:** A subagent documented remote copy as server-side: "the bytes never cross the link".
  - **Lesson:** It was a plausible assumption about SFTP that the sibling agent had already disproved by reading the library. Comments asserting the behaviour of a dependency are worth verifying like code — this one would have made the progress bar look broken and sent the next reader hunting for a bug that was not there.

- **Approach:** Two split tests broke on the arming fix, and one of them was my error, not the fix's.
  - **Evidence:** TestSplitOpenAndClose: the split key did not mark the open in flight
  - **Lesson:** The listing sorts directories first, so the fixture's row 0 was the directory and row 1 the file — the reverse of the order they were written in. The other test's premise (a split armed on a directory is spent by the next key) became unreachable and had to be replaced rather than repaired: a fix that removes a state makes the test for that state obsolete, not failing.

## Validation

- go build ./... , go vet ./... and go test ./... all clean, including tools/docsgen (README.md, KEYBINDINGS.md and index.html regenerated from docs/ via "go run ./tools/docsgen"). 8 Docker E2E tests self-skip with no daemon.

- A security pass over the whole change found nothing exploitable. Verified independently rather than taken on trust: download() checks every batch name through checkLocalName before a byte moves, and openInApp checks and opens the same immutable node, so the plural refactor did not open a check-name/use-name gap. checkNotIntoItself and descends both test with a separator.

- The discoverability guard was verified non-vacuous by removing two keys from the card and the palette and watching it fail with exactly those two names.

## Remaining risks

- Never run against a real SSH host. The three-column layout in the 96-128 column band, where the narrow fallback switches, has only ever been drawn by unit tests. This is the largest untested surface in the change.

- A remote-to-remote copy has no cancel and no rollback: an aborted recursive copy leaves what it wrote. "Cancel a transfer in flight" is still open in TODO.md and needs context.Context in sftpx.

- Still one transfer at a time — the batch is a queue inside a single *transfer, and pointer identity on it is what makes that safe (see the 20260816 entry). A concurrent transfer list needs an explicit id and model.go's broadcast routing revisited.

- collisions() returns "" when the target directory has not been listed yet, so an unopened target skips the overwrite question entirely and falls through to the server's behaviour — Create truncates, Rename refuses.

## Handoff

- Run it against a real host before trusting the widths: the narrow fallback at 96 columns after the sidebar, and ctrl+t while the keyboard is in the tree.

- filebrowser.Path() now follows the cursor rather than a navigated cwd. internal/tui/reconnect.go restores a browser to it, so a reconnect reopens where the cursor was, not where the browser was rooted.

- New layout keys (tab, ctrl+t, backslash) are resolved in tui/keys.go handleBrowserKey, so they sit on hop's side of the browser's question. The Prompting() early return is what keeps them from escaping it — TestLayoutKeysDoNotEscapeAnOpenQuestion guards it.

- The remote-to-remote copy still streams through the client because pkg/sftp has no copy-data extension. A "cp -R" fast path over the existing SSH connection would keep the bytes on the server — sftpx.Open already receives the *ssh.Client and would only need to keep it. It builds a shell command from partly server-supplied paths, so the quoting is the part that has to be right.
