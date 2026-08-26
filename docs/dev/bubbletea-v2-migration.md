# Bubble Tea v2 migration

**Status:** code migrated, `just ci` green (fmt, vet, 14 packages). **Not yet run against a
real terminal** — the manual list at the bottom is what remains.

Migrated from `bubbletea v1.3.10` to `charm.land/bubbletea/v2 v2.0.9` (note the module path
moved off `github.com/`). This file was written as a plan and is kept as the record; the
manual verification is the open half. It lives outside `docs/NN-id.md`, so `tools/docsgen`
does not read it and the published site is unaffected.

## Why, corrected

The plan said v2 keeps the Windows console's `ControlKeyState` and virtual key code, so
`ultraviolet` can name the modifiers and detect AltGr outright. **That is dead code on the
path v2 actually uses.** `ultraviolet/cancelreader_windows.go:52-58` and
`bubbletea/v2/tty_windows.go:19-32` put the console into `ENABLE_VIRTUAL_TERMINAL_INPUT`;
`terminal_reader_windows.go:89-107` then forwards only each key-down's *character*, and the
win32-record decoder at `decoder.go:1837-2054` is never reached.

The outcome is the same and the mechanism is simpler: the console composes the character, a
modifier press produces no event at all, and hop's two Windows keyboard hacks were both
deleted. What did **not** survive contact: ctrl+space and ctrl+2 are still indistinguishable
(both decode to `{Code: KeySpace, Mod: ModCtrl}`, `decoder.go:1128-1132`) — which does not
matter, since both mean NUL and hop can now send it.

Resize still works: `ENABLE_WINDOW_INPUT` records are re-encoded as `CSI 8;h;w t`
(`terminal_reader_windows.go:174-187`) and decoded into `WindowSizeMsg`.

## What changed

| Area | Change |
|---|---|
| `internal/tui/altgr.go` | **deleted** — `normalizeAltGr` and `phantomModifier` both obsolete |
| `internal/terminal/terminal.go` | key encoder rewritten against `Code`+`Mod`; the 30-case type switch is now two lookup maps, and ctrl chords are arithmetic on `Code` instead of a `String()` round-trip |
| `internal/terminal/mouse.go` | new `terminal.MouseEvent` — the encoder no longer takes a Bubble Tea message type |
| `internal/tui/mouse.go` | `mouseEvt` adapter flattens v2's four mouse messages back into button+action, so the ~15 routing functions kept their shape |
| `internal/tui/view.go` | `View() tea.View`; carries `AltScreen` and `MouseMode` on **every** return — the renderer diffs against the last frame, so an omission switches the mode off |
| `internal/tui/settings.go` | `applyMouse` is no longer a `tea.Cmd`; the mouse is view state |
| `internal/tui/model.go` | switches on `tea.KeyPressMsg`; `tea.PasteMsg` handled directly; `WithAltScreen()` gone, `WithFPS(120)` kept |
| input fields (7 sites) | `msg.Runes` → `Key.Text`, which is also correct for composed characters |
| `internal/keys` | untouched — v2 spells every bound key identically |

### Deliberate parity decisions

- **A bare alt keeps the ESC prefix.** v2 would let `modifiedKeyBytes` emit `CSI 1;3D` for
  alt+left; v1 sent `ESC ESC [ D`. Kept the v1 form (`terminal.go`, the `ModShift|ModCtrl`
  guard) — a migration is the wrong place to change what remote programs receive.
- **The paste burst heuristic stays.** v2 emits `PasteStartMsg`/`PasteMsg`/`PasteEndMsg`
  only where the host brackets the paste (DECSET 2004). Legacy conhost right-click paste is
  still unmarked and `ultraviolet` has no fallback (`decoder.go:558-563` is the only
  producer), so `paste.go` keeps detecting bursts.
- **lipgloss stayed at v1.1.0.** See the open risk below.

### Silent-breakage sites fixed

v1 named the space bar `" "`, v2 names it `"space"`. Three sites compared the old spelling
without going through `keys.Normalize` and would have stopped matching with no compile
error: `tui/keys.go:687`, `tui/tunnels.go:108`, `tui/tunnels.go:160`. `keys.Normalize` keeps
translating `" "` so config files written under v1 still work.

## Open risks

1. **lipgloss v1 with bubbletea v2 is undocumented.** The upgrade guide only notes the move
   to `charm.land/lipgloss/v2`. Two independent colour-profile detectors now exist (lipgloss
   against real stdout, v2 against `p.output`) and the more pessimistic wins; v2 cannot
   restore colour lipgloss already stripped. If colours look wrong, force
   `tea.WithColorProfile(colorprofile.TrueColor)` and pin lipgloss to match.
2. **`View.Content` is parsed, not blitted.** `uv.NewStyledString(...)` decodes the frame
   into cells and "normalizes newlines to emulate a raw terminal output"
   (`cursed_renderer.go:301,345`). hop's frame is ANSI-heavy pane content from `x/vt` inside
   lipgloss boxes, so it is now *interpreted*. Nothing in the suite covers how that renders.
3. **Not tested on a real terminal at all.** Everything below is untried.

## Manual verification — the open half

On **Windows (conhost *and* Windows Terminal)**, macOS and Linux:

- [ ] The frame renders correctly at all: borders, colours, the sidebar, a split
- [ ] AltGr characters — `@ [ ] { } | ~ €` — into: remote shell, AUTHENTICATION card,
      filter, host form, palette
- [ ] **A password containing `@` at a real `sudo` prompt**, byte-exact. This is the bug the
      whole exercise pays off
- [ ] ctrl+space reaches the remote program as a single NUL (tmux prefix, emacs set-mark)
- [ ] `alt+1..9` switches tabs; `alt+left`/`alt+right` still reach the editor
- [ ] Double-esc and `ctrl+o` — the escape hatch survives
- [ ] Leader chords and `gg`-style sequences
- [ ] Paste: bracketed on Unix, burst-detected on Windows; multi-line into shell, editor and
      single-line fields
- [ ] Mouse: click, drag-select, wheel, `ctrl+g` toggle, dragging a split
- [ ] Resize while a pane, a split and the browser are open
- [ ] Scrollback, editor tabs, browser keys
- [ ] Cursor: shape and blink in a focused pane (hop still paints its own; `View.Cursor` is
      unused and could replace the 530ms blink timer for the focused pane later)

## Left undone on purpose

- **F-keys still reach no remote program.** `keyBytes` has no `KeyF1..F12` case — true in v1
  too, so parity was kept. Real gap for vim/mc/htop; separate change.
- **DECCKM is ignored.** hop always sends `ESC[A` for arrows; `x/vt` respects application
  cursor mode and would send `ESC OA`. Latent bug for full-screen remote apps, predates this
  work.
- **`View.Cursor`** is left nil; hop keeps its own per-pane blink, which is the only way
  unfocused panes can show a cursor.
