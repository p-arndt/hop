---
id: hostlist
title: Navigation — the host list
nav: Host list
group: Navigation mode
label: Navigation mode
---

| Key | Action |
| --- | --- |
| [[↓]] [[↑]] | move |
| [[pgdn]] [[pgup]] | a full page down / up |
| [[enter]] [[→]] | connect (opens a terminal pane), or focus the shell already open |
| [[esc]] [[←]] | back — leave the details view |
| [[s]] | focus the existing session for this host |
| [[S]] | open **another** shell on this host, alongside the ones already open |
| [[1]] … [[9]] | go straight to that shell of the host under the cursor |
| [[f]] | open the SFTP browser |
| [[t]] | start all defined tunnels, or stop them when any are running |
| [[T]] | manage this host's tunnel definitions |
| [[o]] | open the host in VS Code Remote, in the directory its shell is standing in |
| [[d]] | disconnect the session |
| [[r]] | reconnect a session whose connection dropped, reopening what it held |
| [[a]] [[e]] [[x]] | add / edit / delete a host (delete asks first) |
| [[p]] | pin the host to the **PINNED** section at the top, or unpin it |
| [[shift+k]] [[shift+j]] | move a pinned host up / down inside that section |
| [[i]] | import hosts from an OpenSSH config (`~/.ssh/config` by default) |
| [[/]] | filter hosts ([[enter]] applies, [[esc]] clears) |
| [[space]] | the [action menu](#actions) for this host — everything above, with its key beside it |
| [[ctrl+k]] | the [palette](#actions): every action, searchable |
| [[,]] [[?]] | settings / the keys card |
| [[ctrl+b]] | hide / show the sidebar |
| [[ctrl+g]] | hand the mouse to your terminal (and take it back) |
| [[q]] [[ctrl+c]] | quit |
| [[esc]] [[esc]] | quit (two presses within 400 ms — one esc only drops the selected host) |

With [vim keys](#vim) on, [[j]]/[[k]] move, [[l]] connects as [[enter]] does, and [[h]] goes
back as [[esc]] does.

:::why not="readme" Why the jump keys belong to the browser, not the list
The list binds the **step** keys and nothing more. The jumps and the ctrl chords — [[gg]],
[[G]], [[H]]/[[M]]/[[L]], [[ctrl+d]]/[[ctrl+u]]/[[ctrl+f]] — belong to the file browser,
which walks directories that actually run past a screen. The host list does not scroll, so
each of them landed a [[j]] or two from where the cursor already was, while holding a letter
the list wants as a command. Paging is [[pgdn]]/[[pgup]].
:::

:::figure only="site" src="assets/screens/keys.png" alt="The keys card listing every binding hop binds" width="1500" height="800" max="34rem"
**Every key hop binds** — [[?]]. It lists the keyboard you actually have, with vim motions
included only if you turned them on.
:::
