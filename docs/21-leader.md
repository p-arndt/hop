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
| anything else | closes the leader and does nothing |

A key that names no chord is **swallowed**, not passed to the remote: while the leader is
open hop has the keyboard, and a program that received the tail of an abandoned chord would
act on a key you were not typing at it. The leader also outranks [[ctrl+b]] and [[ctrl+g]],
which are otherwise held in every mode.

:::why not="readme" Why the leader does nothing on its own
This is the tmux and wezterm arrangement, and the reason for it is worth stating: a leader
that *also acts* forces a timeout, and every value for that timeout is wrong — too short and
the chords are unreachable, too long and leaving feels broken. Earlier versions of hop tried
both and neither worked. Paying one extra keystroke for `out` buys back all of the timing.
:::
