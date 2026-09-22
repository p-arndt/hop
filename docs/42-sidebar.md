---
id: sidebar
title: Views and the host list
nav: Views
group: Every mode
label: Every mode
---

A host in front has two **views**, and the screen never shows more than one of them:

- **the shell view** — one shell, the whole width of the window. No list, no tree beside it.
- **the files view** — the SFTP tree beside the open files. With no file open, the browser
  takes the whole width itself.

[[ctrl+o]] [[f]] crosses from the shell to the files on the same host, [[ctrl+o]] [[t]] and
[[ctrl+o]] [[s]] from an editor tab. hop remembers which view each host was in, and more:
entering a host — [[enter]] in the list, a host in the switcher, [[ctrl+o]] [[tab]] — lands
on the **last place** you were on it, the same shell tab, file, browser or panel. Only an
explicit new shell ([[ctrl+o]] [[0]], [[S]] in the list) opens another shell.

The top row is the **session bar**. With a host in front it names that host, then a chip for
everything open on it — shells, the browser, each file, the panel, a tunnel count — with the
one that has the keyboard highlighted, and the other connected hosts on the right with their
status dots. With no host in front it lists every connected host. Click a chip or a host to
go there, exactly as the switcher would; when the row is too narrow the far end gives way to
a **+N**, which opens the switcher. A status message such as *connected* takes the right side
for the few seconds it is up.

The files view has a **terminal panel** under the files, the way an IDE keeps one under its
editor: [[`]] in the tree or [[ctrl+o]] [[j]] shows it and moves into it, the same key inside
it puts it away, and a double [[esc]] hands the keys back to the files. [[S]] on an entry puts
the panel in that directory — a running panel is told to `cd` there, so its history stays.
Drag its top edge to resize it. From the keyboard, [[+]] and [[-]] in the tree size it
directly; from the panel or an editor, [[ctrl+o]] [[+]] or [[ctrl+o]] [[-]] starts sizing, and
[[+]] [[-]] [[↑]] [[↓]] keep going until any other key. It starts at a bit under half the height. It is a shell of its own, not one of the shell view's tabs, so neither ever resizes the other.

The **host list** is a column only when no host is in front. Once one is, going back to the
list ([[ctrl+o]] [[o]], [[ctrl+o]] in the browser, a double [[esc]]) draws it **over** the view
instead — the host's view stays where it is underneath, and nothing is resized. For hopping
without the list at all there is the switcher, [[ctrl+o]] [[space]].

:::why not="readme" Why the list floats instead of taking a column
A column that comes and goes resizes every pane beside it, and a remote vim or `less`
redraws on each resize. With the list drawn over the view, a shell keeps one width however
often you go to the list and back — and a single shell gets the whole window, which is what
you opened it for. [[ctrl+b]] is not hop's any more either: a remote `tmux` gets its prefix.
:::
