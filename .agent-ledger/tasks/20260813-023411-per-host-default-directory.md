---
id: "20260813-023411-per-host-default-directory"
title: "Per-host default directory"
status: "completed"
updated: "2026-08-13T02:34:34+02:00"
base_commit: "96aee42ac2b396a0d9cfa898c26eb1d477b35c2c"
branch: "main"
agent: null
tags: ["hosts", "sftp", "terminal", "tui"]
files:
  - "README.md"
  - "TODO.md"
  - "internal/filebrowser/filebrowser.go"
  - "internal/filebrowser/filebrowser_test.go"
  - "internal/store/store.go"
  - "internal/store/store_test.go"
  - "internal/terminal/cwd.go"
  - "internal/terminal/cwd_test.go"
  - "internal/tui/commands.go"
  - "internal/tui/hostform.go"
  - "internal/tui/hostmgmt_test.go"
  - "internal/tui/reconnect.go"
  - "internal/tui/session.go"
  - "internal/tui/startdir_docker_test.go"
  - "internal/tui/vscode_docker_test.go"
---

# Per-host default directory

## Goal

Let a host carry a remote directory that new sessions start in: shells cd there on connect, and the SFTP browser opens there. Includes the fix the eighth form field forced — the add/edit card no longer runs off the bottom of a short terminal.

## Scope

- The alt-screen guard must stay *after* the 300 ms reportsCwd grace, not before it: the grace is 300 ms of the session's first moments, and a full-screen program that takes the screen inside that window has to be seen before anything is typed.

## Discoveries

- The OSC 7 prompt hook is typed into the pane's own pty and then erased from the emulator (terminal/cwd.go), not sent over a side channel — so the cd rides that same line rather than needing a second mechanism.

- filebrowser.New already took a startDir (reconnect uses it to restore where a dropped browser was standing); the fresh-open call sites in session.go were the only ones passing an empty one.

- The settings popover had already solved the short-window problem twice over — settingsFullH/PackedH/MinH plus settingsWindow, a cursor-centred run of fields. The host form mirrors it rather than inventing tabs or a scrollbar, since the two cards are meant to read as one family.

- injectHook used a cwd report as its 'the line ran' signal. On a shell with no hook (fish) nothing ever reports, so the erase would never fire and the cd would stay visible — hence the new expectReport argument, which falls back to waitPromptBelow alone.

## Decisions

- **Decision:** Fold the cd into the same line that installs the OSC 7 hook, cd first, joined with ';'.
  - **Reason:** One line means one echo and one erase, and putting the cd first makes the hook's trailing hop_cwd call report the directory the session actually starts in rather than the login directory.
  - **Trade-off:** '&&' cannot be used: a function definition is not a valid right-hand side of && in bash. With ';' a failed cd still leaves a shell that reports where it ended up.

- **Decision:** Do not silence a failing cd (no 2>/dev/null).
  - **Reason:** The shell's own 'no such file or directory' is how the user learns the setting is stale; and because it puts text hop did not type into the echoed span, holdsEchoOnly declines the erase, so the typed line stays on screen next to the reason.
  - **Trade-off:** A wrong default directory leaves the raw hook line visible in the pane. That is the intended trade: visible beats swallowed.

- **Decision:** A start directory the browser cannot list falls back to the remote home with the error as its status.
  - **Reason:** A default directory renamed on the server should not make 'f' refuse to open anything. Failing to list the home as well is still a hard error, since there is nothing left to show.
  - **Trade-off:** filebrowser.New now calls Home() lazily rather than up front.

- **Decision:** A pending reconnect's browser directory outranks the host default (model.browserStartDir).
  - **Reason:** Where the user was standing when the connection dropped is a stronger claim on 'where I meant' than a setting made once.

- **Decision:** On a short window the host form packs the air out first, then scrolls the fields inside the card; it never grows past the window.
  - **Reason:** What has to survive a short window is the shape of the card — a field, and the key hints at its foot. Cutting the bottom off hides the hints and the field you just tabbed to.
  - **Trade-off:** Below hostFormMinH (14 rows) the overlay still drops the bottom lines; the honest answer there is a taller window.

- **Decision:** Show an 'n/8' counter in the hint line, but only while the card is scrolling.
  - **Reason:** A window of fields cannot say for itself how many there are; on a card showing all of them it would be noise. This is the one place the host form diverges from the settings popover.

## Failures

## Validation

- go test ./... — passed

- HOP_DOCKER_E2E=1 go test ./internal/tui/ -run StartDirE2E — passed against real bash, zsh and fish (TestStartDirE2ELandsInTheDefaultDirectory/{bashy,zshy}, WorksOnAnUnhookableShell, ShowsAMissingDirectory)

- HOP_DOCKER_E2E=1 go test ./internal/tui/ -run VSCodeE2E — passed, so the hook-only path is unregressed

- go test ./internal/tui/ — passed, including TestHostFormFitsTheWindow (every height from the floor up, every cursor position), TestHostFormWindowHoldsTheCursor and TestHostFormCounterOnlyWhenScrolled

## Remaining risks

- The default directory is not read from or written to ~/.ssh/config; an import will not carry one, by design.

- hostFormMinH is 14 rows and is pinned by a test at 24; a ninth field cannot break the standard terminal, but the floor rises if hostFormMinFields ever does.

- shellQuotePath leaves a leading ~ unquoted and quotes everything else, so $VAR in a default directory stays literal. Undocumented in the UI beyond the field's 'home' placeholder.

## Handoff

- The pack/window logic is now duplicated between renderSettings and renderHostForm; if the two cards diverge further it wants extracting.

- If the identity-file local picker lands, the same picker would suit this field — but it is a *remote* path, so it needs the SFTP browser, not a local-fs adapter.
