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
| wheel | the host list | moves the selection, one host a notch |
| wheel | a shell pane | pauses into its [scrollback](#scrollback), three lines a notch |
| wheel | a full-screen program | [[↑]] / [[↓]], three of them a notch — it keeps no scrollback here |
| wheel | the SFTP browser | moves the cursor three entries a notch |
| click | the host list | stands on that host — and, from a pane, hands the keyboard back |
| click | a pane the list has the keyboard in | takes it: the pointer's [[s]] or [[f]] |
| click | a tab strip | switches to that shell or file tab |
| drag | a pane | selects text; it lands on the clipboard when you let go |
| wheel *while dragging* | a pane | scrolls under the selection, which grows to follow |
| drag to the top / bottom row | a pane | keeps scrolling by itself while you hold it there |
| double-click | a host, or a browser entry | opens it — [[enter]], by pointing |

A selection is not limited to the screenful it started on: while the button is down the
wheel scrolls the view under it and the selection grows, and a drag held against the top or
bottom row of the pane scrolls by itself until you let go. A selection also rides the text it
was made on, so scrolling leaves the highlight over the same words. Anything you type takes
it down.

A remote program that asks for the mouse (vim with `set mouse=a`, htop) gets the pointer
verbatim instead. The cards are keyboard-only. [[ctrl+g]] hands mouse reporting back to your
terminal for a moment — for a selection spanning the sidebar and a pane, or anything else
that wants your terminal's own pointer.
