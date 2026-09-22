---
id: leader
title: The leader — [[ctrl+o]]
nav: The leader
group: Terminal mode
label: Terminal mode
---

[[ctrl+o]] inside a pane has **no effect of its own**, and no timeout. It opens the leader,
the footer becomes the menu, and hop waits as long as you take:

| after [[ctrl+o]] | |
| --- | --- |
| [[o]] | out — back to hop |
| [[1]] … [[9]] | that tab, selected **in place** |
| [[0]] | another shell on this host |
| [[c]] | this directory in VS Code Remote |
| [[space]] | the **host switcher**: every host, connected ones first — type to narrow, [[enter]] lands in its shell |
| [[tab]] | back to the **last host**, in the mode it was showing — press it again to come back |
| [[ctrl+k]] | the [palette](#actions) — this pane's chords, searchable |
| [[?]] | the key card |
| anything else | closes the leader and does nothing |

A key that names no chord is **swallowed**, not passed to the remote: while the leader is
open hop has the keyboard, and a program that received the tail of an abandoned chord would
act on a key you were not typing at it. The leader also outranks [[ctrl+b]] and [[ctrl+g]],
which are otherwise held in every mode.

**Hopping without going back to the list.** [[ctrl+o]] [[space]] raises the host switcher
over whatever you are in: hosts you already have a session on come first, then the rest in
the list's order, and typing narrows them the way [[/]] narrows the list. [[enter]] focuses
that host's shell, or connects and opens one; [[esc]] closes it and changes nothing.
[[ctrl+o]] [[tab]] is alt-tab for hosts: it goes back to the host you were on before this
one, landing in its shell, browser or editor — whichever it was showing — and a second press
comes back. The [file browser](#browser) opens the same switcher with [[p]].

:::why not="readme" Why the leader does nothing on its own
This is the tmux and wezterm arrangement, and the reason for it is worth stating: a leader
that *also acts* forces a timeout, and every value for that timeout is wrong — too short and
the chords are unreachable, too long and leaving feels broken. Earlier versions of hop tried
both and neither worked. Paying one extra keystroke for `out` buys back all of the timing.
:::
