---
id: what
title: What hop is
nav: What hop is
group: Start
---

hop is a terminal UI over your SSH fleet. It holds one connection per host and opens
everything else as extra channels on that same connection: more shells, an SFTP browser,
an editor running on the remote box, and any tunnels you defined. Nothing is a new window,
nothing re-authenticates, and leaving a pane never tears down what is inside it.

A **status bar** above the footer always tells you **where you are and where your keystrokes
are going** — the host, the mode, the directory or file, and the machine behind the alias.
That is the single most disorienting thing about a TUI that embeds other people's programs,
so it gets permanent screen space, directly above the keys that act on it.

::::shots only="site"
:::figure src="assets/screens/hosts.png" alt="The hop host list with a details card for the host under the cursor" width="1500" height="800"
**The host list.** Status dot, group, and what [[enter]] would do to the host under the cursor.
:::
:::figure src="assets/screens/shell.png" alt="A live remote shell inside a hop pane" width="1500" height="800"
**A shell.** A real terminal in the pane; the footer shows the ways back out.
:::
:::figure src="assets/screens/sftp.png" alt="The SFTP file browser inside hop" width="1500" height="800"
**The SFTP browser.** [[f]], over the connection that is already open.
:::
:::figure src="assets/screens/editor.png" alt="A file open in a remote editor tab inside hop" width="1500" height="800"
**A remote editor tab.** [[enter]] on a file runs the editor *on the server*, so `:w` writes the real file.
:::
::::
