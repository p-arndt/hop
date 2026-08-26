---
id: "20260826-225204-migrate-hop-to-bubble-tea-v2"
title: "Migrate hop to Bubble Tea v2"
status: "completed"
updated: "2026-08-26T22:52:16+02:00"
base_commit: "6f17735bf5a3beae114e88ec0f1404a65dbdbdf7"
branch: "refactor/bubble-tea-to-v2"
agent: null
tags: ["keyboard", "migration", "windows"]
files:
  - "TODO.md"
  - "docs/dev/bubbletea-v2-migration.md"
  - "domain/contexts/keyboard/README.md"
  - "domain/contexts/keyboard/language.md"
  - "go.mod"
  - "go.sum"
  - "internal/filebrowser/copymove.go"
  - "internal/filebrowser/filebrowser.go"
  - "internal/filebrowser/filebrowser_test.go"
  - "internal/filebrowser/ops.go"
  - "internal/filebrowser/ops_test.go"
  - "internal/filebrowser/prompt.go"
  - "internal/filebrowser/render_test.go"
  - "internal/filebrowser/transfer.go"
  - "internal/filebrowser/transfer_test.go"
  - "internal/keys/keys.go"
  - "internal/terminal/cursor_test.go"
  - "internal/terminal/input_test.go"
  - "internal/terminal/keys_test.go"
  - "internal/terminal/mouse.go"
  - "internal/terminal/mouse_test.go"
  - "internal/terminal/pane_test.go"
  - "internal/terminal/terminal.go"
  - "internal/tui/actions.go"
  - "internal/tui/actions_test.go"
  - "internal/tui/altgr.go"
  - "internal/tui/altgr_test.go"
  - "internal/tui/authprompt.go"
  - "internal/tui/commands.go"
  - "internal/tui/composed_test.go"
  - "internal/tui/confirm.go"
  - "internal/tui/cursor.go"
  - "internal/tui/editor_test.go"
  - "internal/tui/guidance.go"
  - "internal/tui/hostform.go"
  - "internal/tui/hostkey.go"
  - "internal/tui/importer.go"
  - "internal/tui/input_test.go"
  - "internal/tui/keycast_test.go"
  - "internal/tui/keys.go"
  - "internal/tui/keys_test.go"
  - "internal/tui/landing.go"
  - "internal/tui/layout_test.go"
  - "internal/tui/menu.go"
  - "internal/tui/mode_test.go"
  - "internal/tui/model.go"
  - "internal/tui/mouse.go"
  - "internal/tui/mouse_test.go"
  - "internal/tui/palette.go"
  - "internal/tui/paste.go"
  - "internal/tui/paste_test.go"
  - "internal/tui/reconnect.go"
  - "internal/tui/reconnect_test.go"
  - "internal/tui/selection_test.go"
  - "internal/tui/session.go"
  - "internal/tui/settings.go"
  - "internal/tui/sidebar_test.go"
  - "internal/tui/tabkeys_test.go"
  - "internal/tui/tunnels.go"
  - "internal/tui/tunnels_test.go"
  - "internal/tui/twofactor_docker_test.go"
  - "internal/tui/view.go"
  - "internal/tui/view_test.go"
  - "internal/tui/vscode_docker_test.go"
---

# Migrate hop to Bubble Tea v2

## Goal

Delete hop's two Windows keyboard hacks at the root by moving to an input layer that does not throw the console's key information away, and get the NUL byte back.

## Scope

## Discoveries

- The module path moved: v2 declares itself charm.land/bubbletea/v2, not github.com/charmbracelet/bubbletea/v2. go mod tidy fails with 'module declares its path as' until the import path is changed everywhere.

- The migration doc's premise was wrong and the correction matters for anyone reading ultraviolet: parseWin32InputKeyEvent (decoder.go:1837-2054), with its named modifier keys and explicit AltGr detection, is DEAD CODE under bubbletea v2. cancelreader_windows.go:52-58 sets ENABLE_VIRTUAL_TERMINAL_INPUT, so terminal_reader_windows.go:89-107 forwards only each key-down's character and the win32-record branch never runs. The phantom NULs disappear because conhost emits nothing for a modifier press, not because ultraviolet names them.

- ctrl+space and ctrl+2 remain indistinguishable in v2 (both 0x00 -> {Code: KeySpace, Mod: ModCtrl}, decoder.go:1128-1132). Irrelevant for hop: both mean NUL, and NUL is sendable again. The v1 tradeoff is what is gone, not the ambiguity.

- Compile-error surface, measured with -gcflags=-e (go stops at 10 errors per package otherwise, and internal/tui is not type-checked until internal/terminal compiles): 58 in the leaf packages, then 296 in internal/tui, then 827 across the tests. Bottom-up is the only order that gives feedback.

- 331 of the test call sites go through one helper, key(t, name) in internal/tui/keys_test.go. Rewriting it generically (modifier-prefix parsing + a name->Code map) took the tui test errors from ~500 to 277 in one edit. Same for click/wheel/motion/dragEvents/altKey/pasted: five helpers, ~90 errors.

## Decisions

- **Decision:** Keep hop's own key encoder and port it, rather than adopting x/vt's Emulator.SendKey.
  - **Reason:** x/vt writes into its own pty writer rather than returning bytes, and has no modifier-parameterized sequences except shift+tab: ctrl+up falls to default and emits NOTHING (x/vt key.go:293). hop's modifiedKeyBytes is ahead of upstream.
  - **Trade-off:** hop keeps owning an encoder no library maintains for it.

- **Decision:** Flatten v2's four mouse message types back into one value (mouseEvt) at the edge of internal/tui, and give internal/terminal its own MouseEvent.
  - **Reason:** The routing tree re-inspects button and action across ~15 functions; type-switching at every step would have rewritten all of them. The encoder in internal/terminal should not name a UI framework's message type at all.

- **Decision:** A bare alt keeps the v1 ESC prefix instead of xterm's CSI 1;3D.
  - **Reason:** v2 makes the CSI form trivial to emit, but changing what remote programs receive is not what a migration is for.
  - **Trade-off:** hop is now deliberately non-xterm for alt+arrow; revisit separately.

## Failures

- **Approach:** Writing Go source containing backslash escapes through a bash heredoc into python.
  - **Evidence:** newline in rune literal / newline in string
  - **Lesson:** The escape layer eats one level: '\r' arrived as a real CR and '\\' as a single backslash. Write such blocks with the Write/Edit tool, or splice them in from a file.

## Validation

- just ci green: fmt-check, vet, 14 packages. Four new regression tests in internal/tui/composed_test.go, of which TestCtrlSpaceReachesTheRemoteAsNUL asserts the capability the v1 fix had to give up.

- just ci green (fmt-check, vet, go test across 14 packages). Whole-module build clean. New: internal/tui/composed_test.go.

## Remaining risks

- NOT run against a real terminal. lipgloss v1.1.0 with bubbletea v2 is an undocumented, unsupported combination — two colour-profile detectors, the pessimistic one wins.

- View.Content is parsed by ultraviolet into cells (cursed_renderer.go:301,345), not blitted. hop's frame is ANSI-heavy x/vt pane content inside lipgloss boxes, so it is now interpreted; nothing in the suite covers how that renders.

- The paste burst heuristic had to stay: v2 only emits PasteMsg where the host brackets the paste, and ultraviolet has no fallback for legacy conhost right-click paste.

## Handoff

- Work the manual checklist in docs/dev/bubbletea-v2-migration.md, starting with 'does the frame render at all' and the sudo password prompt. If colours are wrong, force tea.WithColorProfile(TrueColor) and pin lipgloss to match.

- Separate changes now visible: F-keys reach no remote program (no KeyF1..F12 case in keyBytes, true in v1 too), and hop ignores DECCKM so arrows are always ESC[A.
