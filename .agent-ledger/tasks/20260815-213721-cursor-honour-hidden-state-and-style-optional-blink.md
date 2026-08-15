---
id: "20260815-213721-cursor-honour-hidden-state-and-style-optional-blink"
title: "Cursor: honour hidden state and style, optional blink"
status: "completed"
updated: "2026-08-15T21:37:52+02:00"
base_commit: "5b73c4359180b8f83885d4982b57c9a5a2df2432"
branch: "main"
agent: null
tags: ["config", "cursor", "terminal", "tui"]
files:
  - "KEYBINDINGS.md"
  - "README.md"
  - "TODO.md"
  - "docs/20-terminal.md"
  - "docs/43-settings.md"
  - "index.html"
  - "internal/config/config.go"
  - "internal/terminal/cursor.go"
  - "internal/terminal/cursor_test.go"
  - "internal/terminal/terminal.go"
  - "internal/tui/commands.go"
  - "internal/tui/cursor.go"
  - "internal/tui/cursor_test.go"
  - "internal/tui/model.go"
  - "internal/tui/msgs.go"
  - "internal/tui/settings.go"
---

# Cursor: honour hidden state and style, optional blink

## Goal

Draw the cursor the remote asked for — hidden state (DECTCEM) and shape (DECSCUSR block/underline/bar) — instead of an unconditional reverse-video block, plus an opt-in blink.

## Scope

## Discoveries

- vt exposes the cursor through Callbacks.CursorVisibility and Callbacks.CursorStyle, not through getters: SafeEmulator has CursorPosition() but no CursorHidden()/CursorStyle(), so the pane has to shadow the state (cursorState in internal/terminal/cursor.go), the same way mouseState/pasteState shadow their modes.

- vt's CursorStyle callback second argument is named 'blink' but screen.go calls it with !blink — it is *steady*. DECSCUSR Ps: odd (and 0) blink, style = Ps/2 rounded down, so 1/2 block, 3/4 underline, 5/6 bar.

- vt keeps a cursor per screen: entering the alt screen copies the primary's cursor into the alt one, leaving it just switches back, and it re-emits CursorVisibility (never CursorStyle) on the switch. So a style set by an alt-screen program is not withdrawn when it dies — hop clears the style in its own AltScreen(false) callback, next to mouse.clear()/paste.clear(), and lets vt's visibility callback land after it.

- RIS (ESC c) rewrites vt state with no callback at all; the pane already watches for it through oscScanner.tookReset() for the modes, and cursor.clear() joins that path.

## Decisions

- **Decision:** A bar cursor is drawn as U+258F in place of the cell's character, and as a reverse-video block when that character is wide.
  - **Reason:** A cell grid has no left edge for a bar to stand in. Replacing the glyph keeps the row's cell count, and the character under an insertion point is the one about to be typed over — usually a blank at the end of a line.
  - **Trade-off:** The character under a bar cursor is not visible. A one-cell bar over a two-cell character would slide the rest of the row left, hence the block fallback.

- **Decision:** Blinking is a setting (config.CursorBlink, off by default) driven by one model-level clock that phases every pane, not the focused one.
  - **Reason:** A drawn cursor cannot blink by itself, so hop must repaint twice a second — until now the connect spinner was the only clock hop ran, so it is asked for rather than assumed. Phasing every pane means switching tab or dropping to the host list can never land on a pane left mid-blink.
  - **Trade-off:** While the setting is on the clock runs even with no session open; the gen/blinking pair in model.go keeps a setting walked twice from starting a second chain.

## Failures

## Validation

- go build ./... && go vet ./... — passed

- go test ./... — passed (new: TestMarkAtColumnPerStyle, TestMarkForStyle, TestPaneHonoursHiddenCursor, TestPaneHonoursCursorStyle, TestCursorResetOnRIS, TestCursorResetLeavingAltScreen, TestCursorBlinkPhase in internal/terminal; TestCursorBlink*/TestSettingsCursorBlinkToggle in internal/tui)

- go test -race ./internal/terminal/ ./internal/tui/ — passed (the cursor shadow is written by the output pump, read by the UI goroutine)

- go run ./tools/docsgen — README.md, KEYBINDINGS.md and index.html regenerated for the new setting and the cursor section in docs/20-terminal.md

## Remaining risks

- Unrun against a real host: worth checking vim's insert/normal switch (bar vs block), a program that hides the cursor while painting, and how the bar reads over text on a real font.

- The ninth settings field: TestSettingsCardFitsTheWindow still passes, so the packed card stays inside 80x24, but the margin is what it was measured at.

## Handoff

- Ownership: internal/terminal/cursor.go = the shadowed state, the marks and the row painting (overlayCursor/markAtColumn, moved out of terminal.go, which kept runeWidth; markAtColumn now walks with selection.go's walkRow instead of its own escape scanner). internal/tui/cursor.go = the blink clock and eachPane. The setting is a plain toggle row in internal/tui/settings.go and applies through applySettings.

- If the blink clock ever needs to stop while no pane is open, gate applyCursorBlink on eachPane finding one and restart it where a pane lands (shellLanded/editorLanded) — today it runs whenever the setting is on.
