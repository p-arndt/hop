---
id: settings
title: Settings — the popover
nav: Settings
group: Every mode
label: Every mode
---

[[,]] opens the settings card, floating over whatever is on screen. It works from the host
list, from a pane and from the file browser. It is modal: while it is up, keys go to it.

::::columns cols="minmax(0,1fr) minmax(0,1.1fr)"
:::col
| Key | Action |
| --- | --- |
| [[↑]] [[↓]] ([[k]] [[j]]) | move between settings |
| [[←]] [[→]] ([[h]] [[l]]) | pick a colour (accent), walk a profile, or flip a switch |
| [[enter]] [[i]] | edit the selected setting — or flip it, if it is a switch |
| [[enter]] [[esc]] [[ctrl+u]] | while editing: save / cancel / clear |
| [[r]] | reset the setting to its default |
| [[esc]] [[q]] [[,]] | close |
:::
:::figure only="site" src="assets/screens/settings.png" alt="The hop settings popover with the accent colour swatch strip" width="1500" height="800"
The accent is a swatch strip that recolours hop as you walk it.
:::
::::

| Setting | What it is | Blank means |
| --- | --- | --- |
| Guidance | how much of the keyboard hop keeps on screen — `keys`, `hybrid` or `guided`, walked with [[←]]/[[→]] (see [Actions](#actions)) | `hybrid` |
| Editor | the command [[enter]] runs on the remote host, e.g. `nvim`, `vim -R` | auto: remote `$EDITOR`, else probe for nvim/vim/vi/nano |
| Download dir | where [[d]] puts a file | `~/Downloads` |
| Accent colour | picked from the swatch strip with [[←]]/[[→]], or typed | `212`, hop's pink |
| Open with | the local command [[o]] opens a file with, e.g. `code -n` | the OS default app |
| Vim keys | the vim motions in the list and the browser — a switch | **off** |
| Mouse | wheel, click and drag-to-copy — a switch | **on** ([[ctrl+g]] lends the pointer back for a moment) |
| Cursor blink | blink the cursor in a pane — a switch. Its shape and its hiding are always the remote's | **off** |
| Remote clipboard | a yank on the remote host (OSC 52) lands on yours — a switch | **on** |

:::why not="readme" The swatch picker, and when a setting is written
The accent is a **swatch picker**, not a number to be looked up: [[←]]/[[→]] walk a palette
of twelve colours — pink, magenta, red, orange, yellow, green, teal, cyan, blue, indigo,
purple, gray — each drawn in the colour it actually is, and each applied to hop the instant
you land on it. [[enter]] still opens text entry if you want a specific 256-code or a
`#hex`, and a value that is not in the palette gets its own swatch on the strip.

Settings are written to `config.json` the moment you save one, and applied on the spot.
Turning vim keys off from this very card cannot strand you: the arrows and [[enter]] drive
every row and are never gated. While you are *typing* a value the gate is off — [[h]] is
then a letter of the value, not a motion.
:::
