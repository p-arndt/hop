---
id: "20260729-224812-reconnect-handling-for-dropped-ssh-sessions"
title: "Reconnect handling for dropped SSH sessions"
status: "completed"
updated: "2026-07-29T22:49:14+02:00"
base_commit: "80acf63b9145d94057f92a9a40d18262c8d9ec87"
branch: "main"
agent: null
tags: ["reconnect", "ssh", "tui"]
files:
  - "KEYBINDINGS.md"
  - "README.md"
  - "TODO.md"
  - "internal/sshx/lost_test.go"
  - "internal/sshx/sshx.go"
  - "internal/tui/commands.go"
  - "internal/tui/details.go"
  - "internal/tui/help.go"
  - "internal/tui/keys.go"
  - "internal/tui/list.go"
  - "internal/tui/model.go"
  - "internal/tui/msgs.go"
  - "internal/tui/reconnect.go"
  - "internal/tui/reconnect_test.go"
  - "internal/tui/session.go"
  - "internal/tui/theme.go"
  - "internal/tui/view.go"
  - "internal/tui/view_test.go"
---

# Reconnect handling for dropped SSH sessions

## Goal

Detect a dropped connection (including a blackholed one), mark the session dead while keeping its panes on screen, and offer a reconnect that restores what was open.

## Scope

- A zero sshx.Client (tests build &sshx.Client{}) has a nil lost channel: Lost() blocks forever, IsLost() is false. Deliberate — a client that never connected cannot be lost — and it is what lets the tui tests drive deadness through sessionLostMsg alone, with no fake client.

## Discoveries

- A dropped connection ends every channel on it, so each shell's sess.Wait() returns and each editor's too — arriving as shellExitedMsg/editorExitedMsg, previously indistinguishable from 'exit' or ':q'. Untreated, the last shell's exit closes the session (model.shellExited) and the reconnect offer disappears with it. Both handlers now check session.deadConnection() first.

- ssh.Client.Wait() only returns when the transport actually ends. A blackholed link (suspended laptop, dropped VPN, expired NAT entry) never ends it: hop writes nothing, so TCP never complains. Keepalive probes are what make the loss detectable at all — sshx.Client.keepalive sends keepalive@openssh.com every 15s, 10s per-probe timeout, 3 misses then Close(). A *failure* reply counts as answered; only a transport error or no reply is a miss.

- sshx.Client.waitErr is written by the Wait goroutine before close(lost); LostErr reads it only after observing that close via select, which is what makes it race-free (checked under -race).

## Decisions

- **Decision:** Keep the dead session in m.sessions with session.dead set, rather than closing it, and render its panes under a banner.
  - **Reason:** The last screen the host drew (the running command, the error on the way out) is the thing you want to read when a link goes down, and the session object is what 'r' reconnects.
  - **Trade-off:** Every exit path (shell exit, editor exit, disconnect, quit) has to know about the dead state; the panes and their emulators stay in memory until the user reconnects or drops the session.

- **Decision:** Identify a loss by the *connection* (sessionLostMsg.client compared against session.client), not by the alias.
  - **Reason:** The watcher fires for hop's own closes too — 'd', quit, the re-dial in reconnect — and by then the alias may hold a different connection or none. Comparing pointers is what keeps a reconnect from being marked dead by the loss of the connection it replaced.

- **Decision:** Restore the *shape* of a session (shell-tab count, browser + its directory), not editor tabs, and dial the half the user was in first.
  - **Reason:** An editor holds a buffer on the far end; reopening the file on a fresh pty would look like nothing had been lost. Dialing the browser first for someone who was browsing makes the primary landing the focused one, so no restore-mode bookkeeping is needed — restored pieces land with connectedMsg.restore/browserOpenedMsg.restore set and take neither the keyboard nor the status line.
  - **Trade-off:** Editor tabs are gone after a reconnect; the status names how many. A restored browser attaches behind a focused shell, not beside it.

- **Decision:** Park the reconnect plan in model.pending keyed by alias instead of threading it through the dial.
  - **Reason:** A dial can detour through the host-key card or the 2FA card, which replay it through their own retry paths; a plan waiting under the alias is picked up by whichever landing eventually arrives. Failure paths call dropPlan so a later ordinary connect does not restore tabs nobody asked for.

## Failures

## Validation

- go build ./... , gofmt -l . (clean), go vet ./... — clean

- go test -race ./... — all packages pass, including the new internal/sshx/lost_test.go (real loopback sshd: Lost fires on a server-side close and on our own close; ping answered live, refused dead) and internal/tui/reconnect_test.go (15 tests: dead marking, cut-off exits kept, superseded-loss ignored, dead-pane keyboard, plan capture incl. browser dir, landing restore + 'editor not reopened' status, quiet restores, failed reconnect drops the plan, list keys, on-screen banner/breadcrumb/footer)

- TestViewFitsTheWindow now covers a dropped session at 4 window sizes

## Remaining risks

- The 15s/3-miss keepalive means a blackholed link takes up to ~45s to be noticed. Not configurable — no setting was added.

- Not exercised against a real host being dropped by hand: the transport-loss half is covered by a loopback sshd, the UI half by model tests. No visual check of the dead pane in a live terminal.

- A dead session still counts in the header's 'N sessions' chip.

## Handoff

- If editor restore is ever wanted, reconnectPlan already carries the count — add paths/names and issue openEditorCmd from applyPlan, but decide first what to do about the buffer that was lost.

- Keepalive interval/count would fit the settings popover (internal/tui/settings.go's settingsFields table) if a user ever needs a longer or shorter grace period.
