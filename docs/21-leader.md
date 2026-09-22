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
| [[f]] | this directory in the [file browser](#browser) — the open one moves there |
| [[c]] | this directory in VS Code Remote |
| [[j]] | show / hide the [terminal panel](#sidebar) under the files — pressed inside it, hides it |
| [[t]] | the file tree, if the host has a browser open — the [files view](#sidebar) |
| [[s]] | from an editor tab: this host's shell — the [shell view](#sidebar) |
| [[space]] | the **switcher**: every shell, browser, editor tab and terminal panel open on every host, then every host — type to narrow, [[enter]] lands exactly there |
| [[tab]] | back to the **last host**, on the tab it was showing — press it again to come back |
| [[ctrl+k]] | the [palette](#actions) — this pane's chords, searchable |
| [[?]] | the key card |
| anything else | closes the leader and does nothing |

[[f]] and [[c]] follow the directory the shell reports as you `cd`. A shell that has not
reported one (a login shell hop could not hook, or one still starting) leaves [[f]] opening
the browser where it would anyway, and the status line says so.

A key that names no chord is **swallowed**, not passed to the remote: while the leader is
open hop has the keyboard, and a program that received the tail of an abandoned chord would
act on a key you were not typing at it. The leader also outranks [[ctrl+g]], which is otherwise
held in every mode.

**Getting back to anything.** [[ctrl+o]] [[space]] raises the switcher over whatever you are
in. It lists everything open on every connected host — each shell tab with its directory,
the browser, each editor tab by path, the terminal panel, a host's tunnels — most recently
used first, so [[ctrl+o]] [[space]] [[enter]] goes back to the place before this one. Below
them come the hosts: connected ones first, then the rest in the list's order. Typing narrows
all of it by host, path or name, the way [[/]] narrows the list. [[enter]] lands exactly on
the row: that shell tab, that file, the panel. A host row lands where you last were on that
host, connects one that has no session, and reconnects one that dropped; [[esc]] closes the
card and changes nothing. [[ctrl+o]] [[tab]] is alt-tab for hosts: it goes back to the host
you were on before this one, on the tab you left it on, and a second press comes back. The
[file browser](#browser) opens the same switcher with [[p]].

:::why not="readme" Why the leader does nothing on its own
This is the tmux and wezterm arrangement, and the reason for it is worth stating: a leader
that *also acts* forces a timeout, and every value for that timeout is wrong — too short and
the chords are unreachable, too long and leaving feels broken. Earlier versions of hop tried
both and neither worked. Paying one extra keystroke for `out` buys back all of the timing.
:::
