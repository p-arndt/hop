# Migrating hop to Bubble Tea v2

**Status:** not started. This is a plan, not a record — nothing below has been done.

Measured against `bubbletea v1.3.10` (current) and `bubbletea/v2 v2.0.9` (checked 2026-08-26).
Counts exclude test files unless said otherwise. This file lives outside `docs/NN-id.md`, so
`tools/docsgen` does not read it and the published site is unaffected.

## Why

hop's Windows keyboard has two hacks in it, and both exist because v1 throws away the
console's `ControlKeyState` and virtual key code before hop ever sees the key:

- `normalizeAltGr` — an AltGr composition arrives as an alt chord, recognised by the *shape*
  of its runes rather than by the modifier state that actually distinguishes it.
- `phantomModifier` — the console reports a modifier's own key-down as a NUL-charactered key.
  Indistinguishable from a real ctrl+space in a v1 `KeyMsg`, so hop drops both. See the
  ledger entry `20260826-200440-altgr-no-longer-corrupts-remote-password-prompts`.

v2 reads input through `ultraviolet`, which keeps that information:

| Fact | Where |
|---|---|
| Modifier key-downs become named keys (`KeyLeftCtrl`, `KeyRightAlt`), not NUL runes | `ultraviolet/decoder.go:1884-1905` |
| AltGr is detected as `LEFT_CTRL\|RIGHT_ALT`, explicitly | `ultraviolet/decoder.go:2024-2028` |
| `Key.Text` carries the printable characters; `Key.Code`/`Key.Mod` carry the key | `ultraviolet/key.go:267` |
| `Key.BaseCode` is the US-layout key — what `alt+1..9` tab switching actually wants | `ultraviolet/key.go:291` |

**The payoff:** delete `internal/tui/altgr.go` entirely, get ctrl+space and ctrl+2 sendable
again, and replace hop's rune-shape guessing with the modifier state.

Do this as its own piece of work, not as a bugfix. The blast radius is the whole keyboard,
the mouse and the renderer, on every platform.

## Prerequisites

- [ ] Confirm `bubbletea/v2` is still the current major and re-read its changelog from v2.0.9.
- [ ] hop has **no `bubbles` dependency** — verified in `go.mod`. Nothing to migrate there.
- [ ] Decide on lipgloss. v1.1.0 only produces ANSI strings and can likely stay; the risk is
      colour-profile detection, which v2 owns via `colorprofile` (`tea.WithColorProfile`).
      Check truecolor and 256-colour output on legacy conhost before committing to the mix.

## API deltas

| v1 | v2 | Note |
|---|---|---|
| `tea.KeyMsg` (struct) | `tea.KeyPressMsg` / `tea.KeyReleaseMsg` | `tea.KeyMsg` is now an **interface** matching both — see trap 1 |
| `msg.Type` + `tea.KeyCtrlX` consts | `Key.Code` + `Key.Mod` | the big rewrite, concentrated in `terminal.go` |
| `msg.Runes` | `Key.Text` (printable) / `Key.Code` | cleaner for hop's input fields |
| `msg.Alt` | `Mod & ModAlt` | |
| `msg.Paste` | `PasteMsg`, `PasteStartMsg` | `bubbletea/v2/paste.go:5` |
| `View() string` | `View() tea.View`, via `tea.NewView(s)` | `bubbletea/v2/tea.go:53-65` |
| `tea.WithAltScreen()` | `View.AltScreen = true` | `bubbletea/v2/tea.go:161` |
| `tea.EnableMouseCellMotion` / `tea.DisableMouse` (commands) | `View.MouseMode` | mouse becomes *view state*, not a command |
| `tea.MouseMsg` | `MouseClickMsg` / `MouseReleaseMsg` / `MouseMotionMsg` / `MouseWheelMsg` | |
| `Init() Cmd`, `Update(Msg) (Model, Cmd)` | unchanged | |

## Work list

### The concentrated part

- [ ] `internal/terminal/terminal.go` — **43** `tea.Key*` references. `keyBytes`,
      `keyToBytes` and `modifiedKeyBytes` encode a key back into VT bytes for the remote pty.
      Rewrite against `Code`+`Mod`. **First check whether `ultraviolet` or `x/ansi` already
      ships a key encoder** — if it does, most of this file goes away and gains
      modifyOtherKeys support for free.
- [ ] `internal/tui/paste.go` — **13** references. `pastable`, `pasteString`, `takeKey`,
      `handlePaste`. See trap 4: the Windows burst heuristic **stays**.
- [ ] `internal/tui/keys.go` — **9** references, the routing table in `handleKey`.
- [ ] `internal/tui/mouse.go` (**37** `tea.Mouse*`) and `internal/terminal/mouse.go` (**20**).
- [ ] `internal/tui/altgr.go` — **delete**, together with the guard at `model.go:333` and its
      42 test references. Keep two regression tests, rewritten: AltGr+q types exactly `@`,
      and ctrl+space reaches the pane as a single NUL.

### Signature-only files

One `tea.KeyMsg` each, in the handler signature. Mechanical:

- [ ] `internal/tui/`: `authprompt.go`, `confirm.go`, `guidance.go`, `hostform.go`,
      `hostkey.go`, `importer.go`, `menu.go`, `palette.go`, `reconnect.go`, `settings.go`,
      `tunnels.go`, `model.go`
- [ ] `internal/filebrowser/`: `filebrowser.go`, `prompt.go`

### Field-level

- [ ] The **33** uses of `msg.Alt` / `msg.Runes` / `msg.Paste` across 12 files:
      `filebrowser/prompt.go`, `terminal/mouse.go`, `terminal/terminal.go`, `tui/altgr.go`,
      `tui/authprompt.go`, `tui/hostform.go`, `tui/importer.go`, `tui/keys.go`,
      `tui/palette.go`, `tui/paste.go`, `tui/settings.go`, `tui/tunnels.go`.
      The seven `if len(msg.Runes) > 0 { field += string(msg.Runes) }` sites all become
      `Key.Text` and get *better* (composed characters, IME).

### Program level

- [ ] `internal/tui/model.go:211` — `tea.NewProgram(m, tea.WithAltScreen(), tea.WithFPS(120))`:
      drop `WithAltScreen`, set it on the View.
- [ ] `internal/tui/view.go:14` — `View() string` → `View() tea.View`. This is also where
      `AltScreen` and `MouseMode` now live.
- [ ] `internal/tui/settings.go` — the live mouse toggle currently returns
      `tea.EnableMouseCellMotion` / `tea.DisableMouse`; it becomes state the View reads.
- [ ] Decide on the cursor: hop draws and blinks its own (`cursorBlinkMsg`), v2 has
      `View.Cursor`. Pick one, do not mix.

### The keyboard registry — config compatibility

`internal/keys` publishes key **name strings** (`ctrl+u`, `shift+tab`, `space`) as the
config file's vocabulary. They are an API; a rename needs a migration.

- [ ] Verify v2's `String()` spells every bound key exactly as `keys.Defaults()` does.
- [ ] If any differ, translate in `keys.Normalize` — **not** in the bindings table, so
      existing `config.json` overrides keep working.
- [ ] `tools/docsgen/keydocs_test.go` guards `docs/*.md` against the registry; it must stay
      green without editing the docs.

### Tests

- [ ] `internal/tui/keys_test.go:16` — the `key(t, name)` helper. **331 call sites go through
      it**, so rewriting this one function carries most of the suite. Do it first.
- [ ] Raw construction sites, in order of size: `tui/altgr_test.go` (42),
      `terminal/keys_test.go` (42), `terminal/cursor_test.go` (30), `tui/paste_test.go` (20),
      `filebrowser/filebrowser_test.go` (13), `tui/tabkeys_test.go` (7),
      `terminal/input_test.go` (7), `tui/mouse_test.go` (6), `tui/editor_test.go` (5),
      `tui/view_test.go` (3), `tui/selection_test.go` (3).

## Traps

1. **`tea.KeyMsg` is an interface in v2** and matches both press and release. `model.update`'s
   `case tea.KeyMsg:` would then fire on key-up too and every keystroke would double. Switch on
   `tea.KeyPressMsg` explicitly. Related: the Windows console delivers key-up records natively
   and `ultraviolet` turns them into `KeyReleaseEvent`. **Verify whether v2 suppresses release
   events unless `View.KeyboardEnhancements.ReportEventTypes` is requested** — this is the
   single most likely way to break everything at once.
2. **The phantom-key drop must be removed in the same commit.** With v2, ctrl+space and ctrl+2
   arrive as distinct `Code`+`Mod`; leaving `phantomModifier` in place would keep eating them
   for no reason.
3. **Mouse is view state now.** The settings toggle has to cause a re-render to take effect,
   which is a different failure mode than a command not firing.
4. **The Windows paste heuristic stays.** `ultraviolet` only decodes bracketed paste on the
   ANSI path (`decoder.go:559`); the Windows console still delivers a paste as synthesised
   keystrokes. Do not assume `PasteMsg` covers Windows — verify before deleting anything in
   `paste.go`.
5. **Colour.** If lipgloss v1 stays, confirm the profile hop renders with matches what v2's
   renderer emits, on conhost and on Windows Terminal.

## Verification

Automated:

- [ ] `go build ./...`, `go vet ./...`, `go test ./...`
- [ ] `tools/docsgen` drift test green without touching `docs/`

Manual, on **Windows (conhost *and* Windows Terminal)**, macOS and Linux:

- [ ] AltGr characters — `@ [ ] { } | ~ €` — into: remote shell, hop's AUTHENTICATION card,
      the filter, the host form, the palette
- [ ] **A password containing `@` at a real `sudo` prompt**, byte-exact. This is the bug the
      whole exercise pays off; test it against a live prompt, not a unit test
- [ ] ctrl+space and ctrl+2 reach the remote program as a single NUL
- [ ] `alt+1..9` still switches tabs (use `BaseCode` if the layout gets in the way)
- [ ] Double-esc and `ctrl+o` — the escape hatch survives, per the keyboard context's rule
- [ ] Leader chords and `gg`-style sequences
- [ ] Paste: bracketed on Unix, burst-detected on Windows; multi-line into shell, editor and
      single-line fields
- [ ] Mouse: click, drag-select, wheel, the settings toggle, dragging a split
- [ ] Resize while a pane, a split and the browser are open
- [ ] Scrollback, editor tabs and browser keys

## Rollback

One commit, so `git revert` is the exit. If release events or the colour profile cannot be
resolved on Windows, abort and stay on v1 — the current `phantomModifier` drop remains a
correct fix, it just costs the NUL byte.

## Effort

1–2 days of code, plus a manual pass per platform. The mechanical share is large and the
risky share is small and named above.
