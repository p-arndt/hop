---
id: scrollback
title: Scrolling back through history
nav: Scrollback
group: Terminal mode
label: Terminal mode
---

[[shift+↑]] pauses the live shell and steps one line up into its scrollback;
[[shift+pgup]] does it a page at a time. Both are deliberately chords a bare shell never
sends, and both decline (falling through to the shell) when there is nothing to show: on the
alt screen a full-screen program owns its own scrolling. The footer advertises
[[shift+↑]] *scrollback* exactly when the key is live.

Once paused, the keyboard drives the history viewport rather than the remote shell. The
[status bar](#modes) says `<host> › scrollback`, and how far back you are reading is the
`⇅ <offset>/<len>` chip at its right-hand end.

| Key | Action |
| --- | --- |
| [[↑]] [[↓]] [[j]] [[k]] | up / down one line ([[shift+↑]] [[shift+↓]] do the same, so the chord that got you here keeps working) |
| [[pgup]] [[pgdn]] [[ctrl+f]] | up / down a page ([[shift+pgup]] [[shift+pgdn]] too; [[ctrl+b]] is the sidebar) |
| [[ctrl+u]] [[ctrl+d]] | up / down half a page |
| [[g]] [[home]] | jump to the oldest line |
| [[G]] [[end]] | back to the live bottom (and leave scrollback) |
| [[esc]] [[q]] [[enter]] [[ctrl+o]] [[←]] | back to the live shell |
| [[?]] | the key card |
| *anything else* | leave scrollback and type it at the prompt |

The wheel enters and drives scrollback too, three lines a notch, and scrolling back down to
the live bottom returns you to the shell.

:::why not="readme" Why arriving at the bottom exits, and what [[ctrl+o]] does here
Reaching the live bottom by scrolling down is itself a way out — the point of scrollback is
to look at what went by, so arriving at the tail means you are done. [[ctrl+o]] here only
leaves scrollback; a second [[ctrl+o]] then opens the leader, the consistent "back one level"
the rest of hop keeps to.
:::
