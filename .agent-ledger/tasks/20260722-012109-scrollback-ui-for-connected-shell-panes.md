---
id: "20260722-012109-scrollback-ui-for-connected-shell-panes"
title: "Scrollback UI for connected shell panes"
status: "completed"
updated: "2026-07-22T01:21:33+02:00"
base_commit: "0741200a3f0aaa43f77486e9a8d0c9cc45e08df4"
branch: "main"
agent: null
tags: ["terminal", "tui", "ux"]
files:
  - "KEYBINDINGS.md"
  - "TODO.md"
  - "internal/terminal/scrollback_test.go"
  - "internal/terminal/terminal.go"
  - "internal/tui/help.go"
  - "internal/tui/keys.go"
  - "internal/tui/keys_test.go"
  - "internal/tui/model.go"
  - "internal/tui/scrollback_test.go"
  - "internal/tui/view.go"
---

# Scrollback UI for connected shell panes

## Goal

Let a focused shell pane scroll up through the emulator's scrollback history, since the vt emulator kept scrollback but no key exposed it.

## Scope

- terminal.Pane owns the scroll offset (scrollOffset int, lines-from-live-bottom, 0==live). It needs no mutex: it is touched only on Bubble Tea's UI goroutine (Update+View), never the output pump, and reads the emulator through the concurrency-safe SafeEmulator. ViewScrollback() renders exactly emu.Height() lines with no cursor overlay, so it slots into the layout identically to View().

## Discoveries

- charmbracelet/x/vt's SafeEmulator already exposes the whole scrollback API (ScrollbackLen, Scrollback().Line(i).Render(), IsAltScreen) and enables a 10000-line main-screen scrollback by default — no SetScrollbackSize call needed. Alt-screen programs (vim/htop/less) keep no scrollback there, so scrollback mode is main-screen only.

## Decisions

- **Decision:** Scrollback is a distinct focused sub-mode (model.scrolling bool), entered from a live shell only by shift+↑ / shift+pgup.
  - **Reason:** The shell owns nearly every key in hop; a dedicated chord a bare shell never uses avoids stealing keys, and entering declines (falls through to the shell) when there is no scrollback or a full-screen program owns the alt screen.
  - **Trade-off:** Entry keys are shift-modified and only fire at a shell with history; discoverability rests on the footer hint (shift+↑) shown only when ScrollbackLen>0.

- **Decision:** In scrollback mode, scrolling down to the live bottom (or G/end) auto-exits back to the live shell; ctrl+o exits scrollback only (a second ctrl+o then leaves the pane).
  - **Reason:** Arriving at the tail means you are done looking; ctrl+o stays the consistent 'back one level' key hop keeps everywhere.

## Failures

## Validation

- go build ./... — ok; go vet ./... — ok; go test ./... -count=1 — all packages pass; go test -race ./internal/terminal/ ./internal/tui/ — clean.

## Remaining risks

- Scroll offset is lines-from-live-bottom, so while scrolled up new output arriving pushes the viewport (offset is re-clamped each frame but not anchored to an absolute scrollback line). Acceptable for MVP (users scroll at an idle prompt); anchor to an absolute position if drift becomes a problem.

## Handoff

- Live-verify against a real host (headless tests cover engine math + mode routing only). A copy/select-and-yank mode and a visible scroll position gutter are natural follow-ons.
