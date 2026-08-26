---
type: context
name: keyboard
title: keyboard
subdomain: supporting
status: draft
owner: p-arndt
code:
  - internal/keys/**
  - internal/tui/keycast.go
  - internal/tui/keycast_off.go
  - internal/tui/altgr.go
relationships: []
---

# keyboard

## Purpose

**What does this key mean, right now?** One table answers that for the whole
application, so the host list and the file browser move on the same keys without each
spelling the keyboard out, and so the docs can be generated from the same source the
app reads.

This context exists because hop embeds other people's programs. Most keys must reach
the remote program untouched; the few hop keeps for itself must be the same everywhere
and must work on terminals hop does not control.

## Strategic classification

| Dimension | Value | Why |
|---|---|---|
| Domain type | supporting | One table and a resolver — low complexity, but it is the *only* place this knowledge lives |
| Business model role | engagement | A TUI is its keyboard; a binding that does not work on the user's terminal is a broken feature |
| Evolution | custom-built | Small, hop-specific, and consumed by the docs generator as well as the app |

The consequence: **keep it small and keep it the single source.** The failure mode
here is not shallow modelling, it is a second table appearing somewhere else.

## Domain roles

**Published language** and **translation layer.** It publishes a stable vocabulary
(`Action` ids) that both the config file and the documentation depend on, and it
translates keystrokes into that vocabulary.

## Ubiquitous language

The terms live in [`language.md`](language.md). The rule: **these words, and only
these words, appear in this context's code, tests, APIs, commits and prompts.**

## Inbound communication

| Message | Type | From | Relationship | Note |
|---|---|---|---|---|
| overrides map | command | fleet (config.json) | conformist | user rebindings, by action id |
| key event + layer | query | workspace, files | customer/supplier | `Reader.Read` resolves it |

## Outbound communication

| Message | Type | To | Relationship | Note |
|---|---|---|---|---|
| Action | published language | workspace, files, pane | open host service | the stable id handlers switch on |
| Binding table | query result | tools/docsgen | published language | the keybinding docs are generated from it |
| refused overrides | event | workspace | published language | reported, never fatal |

## Business decisions

- **A key's meaning depends on the layer that owns the keyboard**, and only that. The
  same physical key legitimately means different things in the list and in a pane.
- **Action ids are the config file's vocabulary**, so they are renamed only with a
  migration. `list.pin` is an API.
- **The vim motions are one setting, in one place.** Whether a key belongs to the vim
  setting is a property of the binding, not a flag threaded through switch statements.
- **A bad override is refused, not fatal.** hop starts with a working keyboard and
  tells the user which rebindings it could not honour.
- **The escape hatch always survives.** No override can leave the user unable to get
  out of a pane.
- **Key names are normalised** (notably `space`) so a config file and the app agree on
  what was written.
- **A binding must work on the terminals users actually have.** On macOS, `alt+<key>`
  never reaches the application, so hop's own keys are `ctrl` chords or leader
  sequences — see [[hop-keybindings-must-work-on-default-macos-terminals]].
- **A modifier is not a key.** The Windows console reports AltGr, ctrl and alt key-downs
  as NUL-charactered key events; hop drops those *phantom keys* outright. A shell's line
  editor ignores them, but a password prompt reads every byte it is sent, so forwarding
  them corrupted the secret behind every AltGr character. The price is the NUL byte itself:
  ctrl+space and ctrl+2 are reported the same way, so hop can send no NUL to a remote
  program at all, and the drop is silent — the phantom fires on every ctrl press, so a
  status per dropped key would be noise.
- **Everything else belongs to the remote program.** hop holds the smallest set of
  keys it can.

## Aggregates

| Aggregate | Protects | Doc |
|---|---|---|
| Map | one action per key per layer; the escape hatch; override validity | _not yet written_ |

## Assumptions

- `internal/keys` is treated as its own context rather than part of [[workspace]]
  because it has a second consumer (`tools/docsgen`) and a published vocabulary. That
  second consumer is the whole justification — if it went away, this should merge.
- The keycast (showing keys as they are pressed) and AltGr handling are anchored here;
  they are arguably presentation and belong to workspace.

## Verification metrics

- A key-to-meaning `switch` appearing outside this context → the single source has
  forked.
- `docs/KEYBINDINGS.md` disagreeing with `keys.Defaults()` → the generator is no longer
  reading the table.

## Open questions

- The package was renamed `keymap` → `keys`, but `docs/61-internals.md` still says
  `internal/keymap`. Documentation outside `domain/` is not drift-checked. Should it be?
