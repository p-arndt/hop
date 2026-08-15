---
id: sidebar
title: The sidebar — [[ctrl+b]]
nav: Sidebar
group: Every mode
label: Every mode
---

[[ctrl+b]] hides the host list and gives the whole window to the pane; [[ctrl+b]] again
brings it back. It is bound in **every** mode except while a card is up — from a focused
shell, from the browser, from an editor tab — because the moment you want the columns is the
moment you are reading something wide on the far side of them. The terminals reflow to the
new width immediately, both ways.

:::why not="readme" What it costs a remote tmux, and why it resets on restart
- **It resets on restart.** hop opens on its host list, so the collapse is a session thing
  rather than a setting.
- **A remote tmux never sees its prefix.** That is the usual deal between a multiplexer and
  the one above it — [[ctrl+o]] [[o]] still leaves the pane, and no other key is taken. It is
  also why [[ctrl+b]] is no longer a page-up anywhere: paging back is [[pgup]].
:::
