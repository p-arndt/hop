---
id: mouse
title: The mouse
nav: Mouse
group: Every mode
label: Every mode
---

Every gesture is an existing binding reached by pointing, so nothing is mouse-only.

| Gesture | Where | What it does |
| --- | --- | --- |
| wheel | the sidebar | moves the cursor, one row a notch |
| wheel | a shell pane | pauses into its [scrollback](#scrollback), three lines a notch |
| wheel | a full-screen program | [[↑]] / [[↓]], three of them a notch — it keeps no scrollback here |
| wheel | the SFTP browser | moves the cursor three entries a notch |
| click | a tab under a host in the sidebar | goes to that tab, as [[enter]] on it does |
| click | a host with something open, or a dropped one | goes to it — where you last were, or its reconnect screen; nothing dials |
| click | a host with nothing open | stands on it, the keyboard in the sidebar |
| click | a box the keyboard is not in | takes it: the tree, the file, the shell, the panel |
| click | a recent place, with no host in front | goes there |
| drag | the terminal panel's top edge | resizes the panel |
| drag | a pane | selects text; it lands on the clipboard when you let go |
| shift+drag | a pane whose program has the mouse | selects with hop anyway, and copies |
| wheel *while dragging* | a pane | scrolls under the selection, which grows to follow |
| drag to the top / bottom row | a pane | keeps scrolling by itself while you hold it there |
| double-click | a host with nothing open, or a browser entry | opens it — [[enter]], by pointing |

A selection is not limited to the screenful it started on: while the button is down the
wheel scrolls the view under it and the selection grows, and a drag held against the top or
bottom row of the pane scrolls by itself until you let go. A selection also rides the text it
was made on, so scrolling leaves the highlight over the same words. Anything you type takes
it down.

A remote program that asks for the mouse (vim with `set mouse=a`, htop) gets the pointer
verbatim instead, so a drag in vim becomes a visual selection that copies nothing. Hold
shift as you press to select with hop instead — the first plain drag says so on the status
line. The cards are keyboard-only. [[ctrl+g]] hands mouse reporting back to your
terminal for a moment — for a selection spanning the sidebar and a pane, or anything else
that wants your terminal's own pointer.
