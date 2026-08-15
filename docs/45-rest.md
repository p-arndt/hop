---
id: rest
title: The rest of the keyboard
nav: The rest of the keyboard
group: Every mode
label: Every mode
---

## Copy and paste

Paste has no key of its own: **paste the way your terminal pastes**, and hop marks it as a
paste for the remote program — which is what stops vim indenting a pasted block into a
staircase. It works on Windows too.

Copying *out* of a pane is a drag ([the mouse](#mouse)), or your terminal's own selection
after [[ctrl+g]]. A yank on the remote host travels to your clipboard over OSC 52, unless you
turn *Remote clipboard* off. A remote asking to **read** your clipboard is never answered.

## What hop takes from the remote

The full list, so there are no surprises: [[ctrl+o]], [[ctrl+b]], [[ctrl+g]],
[[shift+←]]/[[shift+→]], [[shift+↑]], [[shift+pgup]], and the first [[esc]] of a double.
Everything else reaches the program on the other end.

Two costs worth naming: a remote `tmux` never sees its own [[ctrl+b]] prefix through hop, and
[[shift+←]]/[[shift+→]] no longer reaches the remote as a selection motion.

## macOS and the alt keys

hop's own bindings are [[ctrl]] and [[shift]] chords, which every terminal sends — nothing
below is needed to use hop.

The `alt+…` aliases are another matter. On macOS, `Option`+letter types a *character*
(`ø`, `é`, `∑`) instead of sending the ESC-prefixed meta key hop reads, so every `alt+…`
binding is simply absent until the terminal is told otherwise:

- **Terminal.app** — Settings → Profiles → Keyboard → *Use Option as Meta key*
- **iTerm2** — Settings → Profiles → Keys → Left Option key: *Esc+*
- **Ghostty** — `macos-option-as-alt = true`
- **VS Code's terminal** — `"terminal.integrated.macOptionIsMeta": true`

This is also why [[shift+k]]/[[shift+j]] reorder pinned hosts, and why the sidebar is
[[ctrl+b]] and the mouse toggle [[ctrl+g]] rather than the `alt` mnemonics they would
otherwise be.
