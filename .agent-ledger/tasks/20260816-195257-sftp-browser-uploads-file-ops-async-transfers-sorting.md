---
id: "20260816-195257-sftp-browser-uploads-file-ops-async-transfers-sorting"
title: "SFTP browser: uploads, file ops, async transfers, sorting"
status: "completed"
updated: "2026-08-16T20:10:00+02:00"
base_commit: "03935df3543c96572cd997ea43b9370863f9eca3"
branch: "feat/sftp-browser"
agent: null
tags: ["async", "filebrowser", "sftp", "sftpx", "tui"]
files: [".gitignore", "KEYBINDINGS.md", "README.md", "TODO.md", "docs/30-browser.md", "docs/62-roadmap.md", "index.html", "internal/filebrowser/filebrowser.go", "internal/filebrowser/filebrowser_test.go", "internal/filebrowser/ops.go", "internal/filebrowser/ops_test.go", "internal/filebrowser/prompt.go", "internal/filebrowser/sort.go", "internal/filebrowser/sort_test.go", "internal/filebrowser/transfer.go", "internal/filebrowser/transfer_test.go", "internal/sftpx/sftpx.go", "internal/sftpx/sftpx_test.go", "internal/tui/actions.go", "internal/tui/help.go", "internal/tui/keys.go", "internal/tui/model.go", "internal/tui/mouse_test.go", "internal/tui/reconnect_test.go", "internal/tui/view.go"]
---

# SFTP browser: uploads, file ops, async transfers, sorting

## Goal

Close the five open SFTP-browser TODOs: upload (u), delete/rename/mkdir (x/R/m), async transfers with progress, sort toggle plus an mtime column, and a confirm before overwriting a local download.

## Scope

- Sort order, prompt state and the in-flight transfer are all Browser fields, so they survive load() and a directory change. sortBy deliberately persists across navigation; b.xfer deliberately does not get cancelled by one.

- filebrowser.Client requires DownloadProgress/UploadProgress rather than Download/Upload. Five fakes implement it: fakeClient and pickyClient in filebrowser_test.go, liveClient in ops_test.go, mouseSFTP in tui/mouse_test.go, fakeSFTP in tui/reconnect_test.go.

## Discoveries

- All five TODOs lived in one 738-line filebrowser.go, so the work was made parallel-safe by splitting the package across files first: prompt.go (shared question overlay), sort.go, ops.go, transfer.go. filebrowser.go keeps only the seams — the Client interface, the Browser struct, Handle's dispatch, View's line ordering and renderRow's columns.

- handleBrowserKey in internal/tui/keys.go intercepts ',', '?', 'ctrl+k', 'esc' and 'ctrl+o' before forwarding to the browser. Any browser feature that reads typed text must therefore expose Prompting() and be checked there first, or a ',' typed into a filename opens the settings popover.

- sftpx.Download/Upload have callers outside filebrowser (tools/demoserver/demoserver_test.go, internal/sftpx/sftpx_test.go), so when progress was added they stayed as thin wrappers over DownloadProgress/UploadProgress rather than growing a parameter.

## Decisions

- **Decision:** Report transfer progress from inside the copy: sftpx copies through a countingWriter and hands the running total back through a callback, so both directions show a real percentage.
  - **Reason:** The first cut observed progress instead (a download statting the growing local file, an upload showing only elapsed time behind an indeterminate bar) because sftpx had no callback to hook. That left an upload with no true percentage, which was not worth keeping once the parallel slices had landed and widening the interface was free.
  - **Trade-off:** The counter sits on the writing side of io.Copy, not the reading side — a read io.Copy has buffered but not yet written is not progress the user should be shown.

- **Decision:** Publish the count through an atomic.Int64 on the transfer, and snapshot it once per tick into a plain field the view reads.
  - **Reason:** The callback fires on the copying goroutine while the UI goroutine draws the bar. io.Copy reports every 32 KiB, which on a fast link is far more often than a terminal can usefully repaint, so the tick decides the redraw rate rather than the network.
  - **Trade-off:** transfer is no longer copyable — go vet's copylocks would catch a value copy of it.

- **Decision:** One transfer at a time; a second d/u/o is refused on the status line rather than queued.
  - **Reason:** There is one progress line and one b.xfer. Concurrency would mean a scheduler and a transfer list for a case a single browser pane is not really for.
  - **Trade-off:** Blocks the reasonable 'start three downloads and walk away'. Recorded as the next roadmap item.

- **Decision:** Ask on the status line (prompt.go) rather than in a modal card, and give the open question the whole keyboard.
  - **Reason:** The listing behind the question is its context, and a card would cover the very row being renamed. Owning the keyboard is what makes a typed filename safe from hop's own single-key bindings.
  - **Trade-off:** One line of text only: no cursor movement inside the answer, no history. ctrl+u clears, esc cancels.

- **Decision:** No recursive delete and no recursive upload.
  - **Reason:** sftpx.Remove refuses a non-empty directory and that refusal is passed on with the reason. Walking a remote tree leaf-first behind one keystroke is a great deal of destruction, and a symlink met on the way would take it outside the directory.

## Failures

- **Approach:** Cutting the three agent worktrees before committing the seams meant none of them could compile against the real package.
  - **Lesson:** When fanning out onto files that share a package, commit the seams first and confirm the worktrees are cut from that commit — otherwise each agent has to reconstruct the seam in a scratch copy to verify anything.

- **Approach:** Six pre-existing tests asserted the synchronous transfer contract and went red the moment d/o became commands.
  - **Command:** `go test ./internal/filebrowser/`
  - **Evidence:** TestOpenInAppKey: o returned a tea.Cmd; the default-app launch must not suspend the TUI
  - **Lesson:** Making a key async inverts any test that asserted it returned no command. Drive the returned tea.Cmd and feed its messages back through Update — transfer_test.go's drive() helper is the shared way to do that.

- **Approach:** Two transfer tests then asserted the observed-progress contract and went red when progress became reported: an upload having no percentage, and a tick reading the growing local file via os.Stat.
  - **Command:** `go test ./internal/filebrowser/`
  - **Evidence:** upload progressLine = "...0B/1.0K 0%", want no percentage — the count is not knowable
  - **Lesson:** Both were correct assertions about a deliberate limitation. When the limitation is lifted the test has to be rewritten, not patched.

- **Approach:** TestTransferProgressReports left a 200 KiB file in internal/sftpx/, which reached a commit.
  - **Lesson:** The in-process SFTP server is rooted at the real filesystem and t.Cleanup runs *after* the function's defers, so cleanup through an already-closed client silently does nothing. Remove remote test files explicitly before the client closes.

## Validation

- go build ./... — passed

- go test ./... — passed (whole repo)

- go test -race ./internal/filebrowser/ ./internal/sftpx/ ./internal/tui/ — passed

- go vet ./... — passed

- just docs — regenerated; the drift test in go test ./... passes

- New progress tests: TestCountingWriter, and TestTransferProgressReports against the in-process SFTP server with a 200 KiB payload so several 32 KiB reports must fire, asserting monotonic totals ending on the size. TestReportedBytesReachTheProgressLine and TestUploadProgressIsReported cover the callback -> atomic -> tick -> rendered percentage path.

- Rendered the browser through throwaway tests and read the output: the listing with its mtime column, all three sort orders, a rename prompt mid-typing, a delete confirm, a 34-column pane, and progressLine at 0/38/100% plus an unknown total. Both tests were deleted afterwards.

## Remaining risks

- Browser transfer messages are broadcast to every open browser from model.go's Update, not routed by alias. Safe only because transferTickMsg/transferDoneMsg carry the *transfer itself and handleTransferMsg compares pointer identity against b.xfer — a browser that did not start the copy matches nothing. Adding a message type without that identity check would cause cross-talk between two open browsers.

- A transfer cannot be cancelled once started: sftpx copies take no context.Context. Closing the browser mid-transfer leaves the goroutine to finish into a Browser nobody is rendering.

- Deleting a non-empty directory reports the server's refusal; there is no way to remove a tree from the browser at all.

## Handoff

- Closes the silent-download-overwrite handoff left by the 20260722 security-review entry; d now confirms before overwriting in the download directory, and o's scratch fetch still quarantines (now on the transfer goroutine).

- For recursive/concurrent transfers, start at transfer.go's begin/handleTransferMsg: pointer identity on *transfer is what makes one-at-a-time safe, so a transfer list needs an explicit id and model.go's broadcast routing revisited.

- fakeClient.steps (byteSteps) scripts what a copy reports; empty means a copy that reports nothing, which is what most tests want.

- filebrowser_test.go's fakeClient.List returns a fixed slice and cannot show a mutation; ops_test.go's liveClient wraps it to fix that. Prefer promoting liveClient into filebrowser_test.go if a third file needs it.
