---
id: hostlist
title: Navigation — the sidebar
nav: Sidebar keys
group: Navigation mode
label: Navigation mode
---

| Key | Action |
| --- | --- |
| [[↓]] [[↑]] | move — over the hosts and the tabs of an opened-out host alike |
| [[pgdn]] [[pgup]] | a full page down / up |
| [[enter]] [[→]] | go there: on a tab, that tab; on a host, back to where you were on it — the same tab, file or panel — connecting first if it is not |
| [[→]] | open the host out into its tabs; on an open host, step into them |
| [[←]] | from a tab, back up to its host; on an open host, fold it up |
| [[esc]] | give the keyboard back to where it was, with nothing changed |
| [[shift+b]] | hide the sidebar, giving its width to the content — or dock it again |
| [[ctrl+o]] | the [leader](#leader), as in a pane — [[ctrl+o]] [[space]] for go to, [[ctrl+o]] [[→]] for the next host |
| [[s]] | focus the existing session for this host |
| [[S]] | open **another** shell on this host, alongside the ones already open |
| [[1]] … [[9]] | go straight to that tab of the host under the cursor |
| [[f]] | open the SFTP browser |
| [[t]] | start all defined tunnels, or stop them when any are running |
| [[T]] | manage this host's tunnel definitions |
| [[o]] | open the host in VS Code Remote, in the directory its shell is standing in |
| [[d]] | disconnect the session |
| [[r]] | reconnect a session whose connection dropped, reopening what it held |
| [[a]] [[e]] [[x]] | add / edit / delete a host (delete asks first) |
| [[x]] on a tab row | close that tab: the files tab and a shell go at once, an editor asks first since unsaved changes would be lost |
| [[p]] | pin the host to the **PINNED** section at the top, or unpin it |
| [[shift+k]] [[shift+j]] | move a pinned host up / down inside that section |
| [[i]] | import hosts from an OpenSSH config (`~/.ssh/config` by default) |
| [[/]] | filter hosts ([[enter]] applies, [[esc]] clears) |
| [[space]] | the [action menu](#actions) for this host — everything above, with its key beside it |
| [[ctrl+k]] | the [palette](#actions): every action, searchable |
| [[,]] [[?]] | settings / the keys card |
| [[ctrl+g]] | hand the mouse to your terminal (and take it back) |
| [[q]] [[ctrl+c]] | quit |

[[esc]] never quits: a second one straight after the first is the same [[esc]], and with no
host in front there is nowhere to go back to, so it does nothing.

With [vim keys](#vim) on, [[j]]/[[k]] move, [[l]] opens a host out as [[→]] does, and [[h]]
folds it up as [[←]] does.

:::why not="readme" Why the jump keys belong to the browser, not the list
The list binds the **step** keys and nothing more. The jumps and the ctrl chords — [[gg]],
[[G]], [[H]]/[[M]]/[[L]], [[ctrl+d]]/[[ctrl+u]]/[[ctrl+f]] — belong to the file browser,
which walks directories that actually run past a screen. The sidebar rarely scrolls, so
each of them landed a [[j]] or two from where the cursor already was, while holding a letter
the list wants as a command. Paging is [[pgdn]]/[[pgup]].
:::

:::figure only="site" src="assets/screens/keys.png" alt="The keys card listing every binding hop binds" width="1500" height="800" max="34rem"
**Every key hop binds** — [[?]]. It lists the keyboard you actually have, with vim motions
included only if you turned them on.
:::
