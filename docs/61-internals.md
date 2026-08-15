---
id: internals
title: Where a binding lives
nav: Where a binding lives
group: Project
---

The host list and the file browser move on the same keys, so they do not each spell that
keyboard out. `internal/keymap` holds it: one table, one row per key, saying what the key
*means* (a `Motion`), whether the vim setting owns it, and whether the host list binds it as
well as the browser. Both views resolve keys through it — passing `keymap.Full` or
`keymap.List` — and act on the motion they get back.

| Mode | Handler |
| --- | --- |
| host list | `handleNavKey` (`internal/tui/keys.go`) |
| shell pane | `handleShellKey` |
| scrollback | `handleScrollbackKey` |
| SFTP browser | `handleBrowserKey` → `filebrowser.Handle` |
| editor tabs | `handleEditorKey` |
| filter | `handleFilterKey` |
| the cards | `handleHelpKey`, `settings.go`, `hostform.go`, `confirm.go`, `importer.go`, `tunnels.go`, `hostkey.go`, `authprompt.go` |
| shared motions | `internal/keymap` (scoped: the list gets the step keys, the browser all of them) |

| To add… | Touch |
| --- | --- |
| a motion key, in one or both views | the `bindings` table in `internal/keymap` (the `list` column is the split) |
| a key hop holds in *every* mode | `toggleSidebarKey`'s branch in `handleKey`, `internal/tui/keys.go` |
| what a motion *does* to the list | `model.move` in `internal/tui/keys.go` |
| what a motion *does* to the browser | `Browser.move` in `internal/filebrowser` |
| a command key ([[d]], [[o]], [[r]], [[f]], …) | the command switch in whichever view owns it |
| a setting | the `settingsFields` table in `internal/tui/settings.go` |

A mode with no motions of its own — the settings popover — asks `keymap.Vim(key)` instead,
the same table answering the narrower question: *is this a key the vim setting owns?* That is
why turning the setting off is one fact in the config rather than a flag threaded through
three switch statements.
