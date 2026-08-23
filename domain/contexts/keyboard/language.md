---
type: language
context: keyboard
status: draft
code:
  - internal/keys/**
---

# Ubiquitous language — keyboard

### Layer

**Is:** which part of hop currently owns the keyboard, and therefore which set of
bindings applies: global, list, browser, pane, scrollback, editor, leader, dead pane.

**Is not:** [[workspace]]'s *mode*, which is the closely-related but distinct question
of what is on screen. A layer is about key resolution only.

**In code:** `internal/keys/keys.go` — `keys.Layer`.

**Note:** `keys.Pane` is a layer, not a [[pane]]. See the glossary.

### Action

**Is:** what a key does, as a stable string id that handlers switch on — `list.pin`,
`motion.half-down`, `browser.rename`.

**Is not:** the local-application launching in `internal/action` (VS Code, a new
terminal tab), which is an unrelated use of the word.

**Rule:** these strings are the config file's vocabulary. Renaming one needs a
migration.

**In code:** `keys.Action`; `keys.None` means "no binding in this layer".

### Binding

**Is:** one row of the table — a key, the action it means, the layer it means it in,
whether the vim setting owns it, and the symbol shown in the docs.

**In code:** `keys.Binding`, the `bindings` table.

### Map

**Is:** the resolved keyboard: defaults with the user's overrides applied.

**In code:** `keys.Map`, `keys.Defaults()`, `keys.New(overrides)`.

### Motion

**Is:** a movement action shared by the list and the browser — up, down, top, bottom,
half-page, page, screen top/middle/bottom, in, out.

**Rule:** the list gets the step keys; the browser gets all of them. That scoping is
one column in the table, not two implementations.

### Override

**Is:** a user rebinding from `config.json`, keyed by action id.

**Rule:** refused if invalid — reported, never fatal. It cannot remove the escape hatch.

**In code:** `keys.New` returns `(Map, []error)`.

### Sequence / leader

**Is:** a binding of more than one key (`gg`, `ctrl+o ctrl+o`). The reader holds the
half-typed prefix within a window; a prefix key that also binds alone keeps that solo
meaning.

**In code:** `keys.Reader`, `keys.Result`, `keys.Leader`.

### Escape hatch

**Is:** the binding that always gets the user out of a pane, whatever they have
rebound. Double-`esc` and the reserved `ctrl+o`.

**Rule:** it survives every override. Non-negotiable.

### Normalize

**Is:** turning a written key name into the one spelling hop uses, so config and code
agree (`space` being the notable case).

**In code:** `keys.Normalize`.

### Keycast

**Is:** the on-screen display of keys as they are pressed, used in the demo and
available to users.

**In code:** `internal/tui/keycast.go` (and the build-tag-off variant).
