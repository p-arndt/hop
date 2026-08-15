---
id: browser
title: Browsing — the SFTP file browser
nav: File browser
group: Browsing mode
label: Browsing mode
---

| Key | Action |
| --- | --- |
| [[↓]] [[↑]] | move |
| [[pgdn]] [[pgup]] | a full page down / up |
| [[enter]] [[→]] | enter a directory, or open a file in an editor tab |
| [[o]] | open the file in the local OS default app (GUI) |
| [[d]] | download the file to `~/Downloads` |
| [[←]] [[backspace]] | up one directory |
| [[r]] | refresh the listing |
| [[,]] | settings |
| [[?]] | the key card |
| [[ctrl+b]] | hide / show the sidebar |
| [[ctrl+o]] | back to hop |
| [[esc]] [[esc]] | back to hop (two presses within 400 ms) |

With [vim keys](#vim) on the browser keeps the *whole* motion set (the host list only the
step keys): [[j]]/[[k]], [[gg]], [[G]], [[H]]/[[M]]/[[L]], [[ctrl+d]]/[[ctrl+u]],
[[ctrl+f]], plus [[l]] to descend and [[h]] to back out.

:::why not="readme" Why [[←]] walks the tree instead of leaving
[[←]] is pure motion: it walks up the directory tree, and only at the top of it does it pop
back to hop. The directory you open in is usually your home directory — so a [[←]] that left
straight away would drop you back to hop exactly when you meant to go up to `/home`. Leaving
is otherwise always explicit: [[ctrl+o]], or a [double esc](#doubleesc) — though unlike in a
pane, a lone [[esc]] here is swallowed rather than forwarded.
:::
