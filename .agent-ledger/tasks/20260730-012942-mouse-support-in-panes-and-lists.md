---
id: "20260730-012942-mouse-support-in-panes-and-lists"
title: "Mouse support in panes and lists"
status: "completed"
updated: "2026-07-30T01:30:29+02:00"
base_commit: "4bc227cbbac70e3d8aac64ffec9fca2849196b17"
branch: "main"
agent: null
tags: ["mouse", "terminal", "tui"]
files:
  - "KEYBINDINGS.md"
  - "README.md"
  - "TODO.md"
  - "internal/config/config.go"
  - "internal/config/config_test.go"
  - "internal/filebrowser/filebrowser.go"
  - "internal/filebrowser/mouse_test.go"
  - "internal/terminal/cwd.go"
  - "internal/terminal/mouse.go"
  - "internal/terminal/mouse_test.go"
  - "internal/terminal/terminal.go"
  - "internal/tui/help.go"
  - "internal/tui/list.go"
  - "internal/tui/model.go"
  - "internal/tui/mouse.go"
  - "internal/tui/mouse_test.go"
  - "internal/tui/settings.go"
  - "internal/tui/settings_test.go"
  - "internal/tui/tabs.go"
---

# Mouse support in panes and lists

## Goal

Give hop the wheel and clicks — in the host list, the SFTP browser and the terminal panes — without letting the pointer do anything the keyboard cannot, and forward the pointer to a remote program that has asked for it.

## Scope

- Two row-mappings have to run a renderer backwards, and both now share the arithmetic with the renderer instead of duplicating it: model.listStart (extracted from renderRows) for the sidebar, model.tabAt/tabPills/tabStart (extracted from renderTabStrip) for the tab strips. Browser.RowAt lives in filebrowser because the browser drew those rows.

## Discoveries

- charmbracelet/x/vt has SendMouse on *Emulator, but it cannot be used: it reads e.modes without the SafeEmulator mutex (the output pump writes that map from its own goroutine), and isModeSet is unexported so a caller cannot ask whether an event would have gone anywhere — which is exactly the question the TUI must answer to decide between forwarding and hop's own scrollback. hop tracks the modes itself through emu.SetCallbacks(vt.Callbacks{EnableMode,DisableMode}) — the callbacks fire for every DEC mode, from inside Write — and encodes the report with x/ansi (EncodeMouseButton + MouseSgr/MouseX10), writing it to sess.Stdin under the same mutex SendKey uses.

- tea.MouseMsg is 'type MouseMsg MouseEvent', so MouseEvent's methods (IsWheel) are NOT available on it — a local isWheel helper is needed. Bubble Tea's and x/ansi's MouseButton enumerations happen to agree in order but are separate types, so the mapping is written out rather than cast.

- **vt's fullReset (RIS, ESC c — what `reset` / `tput reset` send) rewrites the whole mode map through resetModes() and fires NO callbacks** (vt/esc.go:24, vt/mode.go:6, registered at vt/handlers.go:423). Anything shadowing the emulator's modes therefore goes stale on a reset, and there is no reset callback to hook. hop notices it in the byte stream instead: oscScanner gained a `ris` flag set when 'c' follows an ESC outside an OSC payload, read (and cleared) by tookReset() in the output pump, which then calls mouseState.clear(). It is the only ANSI-aware pass hop makes over the stream besides the emulator's own, which is why the OSC 7 scanner is where a non-OSC sequence ended up.

- **ansi.MouseX10 (x/ansi v0.11.7) cannot be used for the report:** it builds the bytes with string(byte(x)+33), a *rune* conversion, so every coordinate from 95 up is UTF-8 encoded into two bytes and the report arrives malformed — and a pane is normally far wider than 95 columns. hop writes the three bytes itself; the 222-cell ceiling that X10 really has is then the only limit. Only reachable via a program that asks for tracking *without* DECSET 1006 (older less, mc), which is exactly the case the fallback exists for.

- SetCallbacks was previously unused in hop: cwd tracking scans the byte stream itself (internal/terminal/cwd.go, oscScanner) rather than using the WorkingDirectory callback, so the callback struct was free to claim. It is wired in terminal.New before the pumps start, which is what makes assigning it race-free.

## Decisions

- **Decision:** The pointer is only ever a shorthand for an existing binding; nothing is mouse-only.
  - **Reason:** A link or terminal that eats mouse reports must lose no capability, and a second, parallel input vocabulary would double the surface every mode has to answer for. So: wheel over the list = up/down, wheel over a shell = shift+up, click in the sidebar = ctrl+o, double-click = enter, click on a tab = alt+1..9.
  - **Trade-off:** No drag-to-select-text of hop's own, and no right-click menus — both would be mouse-only ideas.

- **Decision:** The wheel over a focused shell goes to the remote program when it has asked for the mouse (DECSET 9/1000/1001/1002/1003), and to hop's scrollback when it has not.
  - **Reason:** It is the same contract a real terminal honours, and it is what makes vim's 'set mouse=a' and htop usable in a pane while an ordinary shell still scrolls its history.
  - **Trade-off:** A program that enables tracking and then ignores wheel reports leaves the wheel doing nothing there; hop cannot tell the difference.

- **Decision:** Modal cards swallow every mouse event.
  - **Reason:** handleKey's whole ordering exists so a card cannot leak input to the list behind it; a click that fell through would be that trap with a pointer. The cards are small and name their keys along their foot.
  - **Trade-off:** No clicking a settings row or a swatch — the popover stays keyboard-driven.

- **Decision:** The Mouse setting defaults to on, which makes it the one config field whose zero value is not its default.
  - **Reason:** config.Load starts from Default() and unmarshals over it, so an existing config.json (or any file omitting the key) still comes back with the mouse on; only "mouse": false switches it off. The alternative — an inverted NoMouse field — would read backwards in the settings card.
  - **Trade-off:** A config.Config{} literal in code has the mouse off; tests that care must use config.Default().

## Failures

- **Keying the double-click chord on the screen row connected to the wrong host.** Both scrolling lists re-scroll around the cursor a click has just moved — model.listStart pins the cursor to the *bottom* row of a scrolled window (start = cursor-h+1), so selecting a host from the top row snaps the window back and moves that host down. A second click on the same row was then accepted as a double and opened whatever had slid under the pointer. Found by review, reproduced with 30 hosts and listRows()==15: clicking row 3 twice selected host 6 then host 0 and called openShell on host 0. **Lesson:** a double-click chord must be keyed on the identity of the thing clicked (the filtered index, the entry index), never on the cell it was drawn in. Pinned by TestDoubleClickConnects/"the same row, a different host".

## Validation

- just ci (gofmt -l, go vet ./..., go test ./...) — passed

- go test -race ./internal/tui ./internal/terminal ./internal/filebrowser ./internal/config — passed

- Live in a real pty (python3 pty, 100x30, SGR reports written to hop's stdin, HOME pointed at a throwaway store): hop emits ?1002h/?1006h at startup; a click selects the pointed-at host; the wheel steps the selection and clamps at both ends; a double-click connects (spinner, then the expected dial failure for a fake hostname)

- Live against tools/demoserver: a double-click connected and focused the pane; the wheel over the pane entered scrollback and moved three lines a notch (chip read 3/79 then 6/79); wheeling back to the live bottom returned the keyboard to the shell

## Remaining risks

- While hop reports the mouse the terminal's own click-and-drag selection belongs to hop, so copying out of a pane needs the terminal's bypass modifier (shift, alt on macOS). That is the reason the setting exists, and it applies live (tea.EnableMouseCellMotion / tea.DisableMouse via applyMouse, which only speaks when the state it last asked for changed). Cell motion, not all motion: drag is reported for a remote program's visual select, a bare pointer crossing the window is not.

- Adding a sixth settings field pushed the popover past an 80x24 window, which would have cut its own hint line. renderSettings now drops the blank line between fields when m.height < settingsFullH() (29 lines full, settingsMinH() == 23 packed), and packed is as small as it goes: below settingsMinH the overlay drops the bottom rows, hints included. TestSettingsCardFitsTheWindow asserts every height from settingsMinH up and fails outright if the packed floor ever rises above 24, so a seventh field cannot quietly break the standard terminal.

- The host list's scroll window is derived from the cursor alone (listStart), so while the list is scrolled the cursor is glued to its bottom row and *any* selection — keyboard or click — re-scrolls the list under you. Pre-existing, and the mouse only makes it visible: clicking a row on a scrolled list lands on the right host but jumps the list. A real scroll offset held in the model would fix both; it was deliberately left out of this change.

- The forward-to-a-remote-program path is covered by unit tests (mode tracking through a real emulator, plus the exact SGR bytes) but was not exercised against a live mouse-hungry program — the demo server has no htop and its fake vi does not ask for the mouse.

## Handoff

- The review round is folded into this entry rather than a second one: the four fixes are the same task. Its other two findings were left alone deliberately — the list-scroll jump above (out of scope), and cwd.go's eraseEcho writing into the emulator from the integration goroutine mid-stream (pre-existing, predates this change, worth its own entry).

- Try it against a real host with vim 'set mouse=a' and htop. If a remote program ever wants the mouse while hop is paused in scrollback, note that scrollback deliberately wins (mouseShell checks m.scrolling first).
