---
id: "20260730-203536-mouse-text-selection-and-two-defects-the-mouse-work-le"
title: "Mouse text selection, and two defects the mouse work left behind"
status: "completed"
updated: "2026-07-30T20:37:30+02:00"
base_commit: "8d9b662a1a501a5d6cbbb2d1fda0f0d757406c23"
branch: "main"
agent: null
tags: ["clipboard","mouse", "rendering", "selection"]
files:
  - "KEYBINDINGS.md"
  - "README.md"
  - "TODO.md"
  - "internal/config/config.go"
  - "internal/terminal/mouse.go"
  - "internal/terminal/mouse_test.go"
  - "internal/terminal/paste.go"
  - "internal/terminal/paste_test.go"
  - "internal/terminal/selection.go"
  - "internal/terminal/selection_test.go"
  - "internal/terminal/terminal.go"
  - "internal/tui/editor_test.go"
  - "internal/tui/help.go"
  - "internal/tui/keys.go"
  - "internal/tui/model.go"
  - "internal/tui/mouse.go"
  - "internal/tui/selection.go"
  - "internal/tui/selection_test.go"
  - "internal/tui/settings.go"
  - "internal/tui/view.go"
  - "internal/tui/view_test.go"
---

# Mouse text selection, and two defects the mouse work left behind

## Goal

Give click-and-drag selection back (hop does the selecting, and copies on release), stop an over-wide pane line from scrolling hop's frame off the terminal, and close the paths by which hop could write raw non-character bytes into a remote shell.

## Scope

## Discoveries

- lipgloss grows a bordered box to fit its content in BOTH directions: a line wider than the box is wrapped onto another row, so one over-wide row makes the pane a row taller and the whole screen a row taller than the window — at which point the user's terminal scrolls hop's own header and box tops off the top of itself (what the reported 'sidebar scrolls weirdly' screenshot actually is). fitLines only ever cut height; renderRight now clampLines()es width too. The lines get over-wide in the ordinary course of things: Pane.ViewScrollback renders scrollback lines via sb.Line(i).Render(), and a scrollback line keeps the width the pane had when it was pushed — so collapsing the sidebar (ctrl+b) or resizing the window leaves history wider than the pane it is read back in.

- The only place hop can write a byte with its top bit set into a remote shell is the X10 mouse encoding (byte(x)+33 etc.) — every other write path is UTF-8 text or ASCII escapes. Such a byte is not a character on a UTF-8 pty; a shell left in mouse mode by a program that exited without switching it off swallows the ESC[M and takes the rest as input, which is how a raw 0x9f ends up as a command name (Ubuntu's python3 command-not-found then prints it back as the literal text \udc9f). This is the leading explanation for the reported garbage line.

- vt does not report the mode changes an alt-screen restore makes, the same way it does not report a RIS (which oscScanner.ris already existed for). A full-screen program that is killed — or that restores the screen without switching its modes off — therefore leaves hop's mouse/paste shadow state set over the shell underneath it, and hop then encodes every drag over that shell into it as input.

## Decisions

- **Decision:** hop implements text selection itself rather than leaving it to the terminal.
  - **Reason:** Reporting the mouse and having the terminal's own click-and-drag selection are mutually exclusive at the protocol level — the drag never reaches the terminal. Every TUI that takes the mouse (tmux, editors) selects for itself, and it is what makes the mouse setting cost no capability. Drag anchors/moves/copies in internal/tui/selection.go; the two primitives (Highlight, PlainText) are in internal/terminal/selection.go and work on the *rendered* view string, so the live screen, the scrollback window and an editor pane are all covered by the same code.
  - **Trade-off:** Selection is per-pane: it cannot span the sidebar and a pane, or reach the header/footer. ctrl+g (a third reserved chord, after ctrl+o and ctrl+b) hands the pointer back to the terminal for those.

- **Decision:** X10 mouse reports are capped at column/row 93 instead of xterm's 222.
  - **Reason:** The byte would otherwise have its top bit set, which is not a character on a UTF-8 pty and lands as undecodable input if the far end mis-parses the report. Everything that asks for the mouse today also sets SGR (1006), which hop prefers and which has no such ceiling.
  - **Trade-off:** A program that asks for 1000 without 1006 gets no reports past column 93 — xterm would have sent them.

## Failures

## Validation

- go test ./... — passed; go test -race ./internal/terminal ./internal/tui — passed; go vet ./... — clean

- The frame-overflow fix is pinned by a test that was watched failing without it: TestPaneContentWiderThanTheBoxDoesNotGrowTheScreen renders 22 lines into a 20-row window with the width clamp removed.

## Remaining risks

- The reported garbage line (a raw 0x9f typed into the remote shell) was never reproduced locally — the fixes close every path by which hop could write such a byte (X10 cap, alt-screen mode drop, ToValidUTF8 on pastes), but the diagnosis rests on the fact that the X10 encoder is the only place in hop that writes a non-ASCII byte that is not part of a UTF-8 character. If it recurs, look for a fourth path rather than assuming these covered it.

- Selection is drawn by re-rendering the pane's view each frame with reverse-video spliced in; a very large drag is O(rows x escape-walk) per repaint. Fine at terminal sizes, not free.

## Handoff

- Try a drag against a real host — live screen and scrollback — and vim with 'set mouse=a' to confirm the remote still owns the pointer. Selection deliberately does not span the sidebar and a pane; ctrl+g is the answer for that.
