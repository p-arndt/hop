---
id: sidebar
title: The sidebar and the content area
nav: Sidebar
group: Every mode
label: Every mode
---

hop's screen is at most **two columns**: the **sidebar** on the left and the **content area**
beside it, with one row of footer under both. There is no header.

The sidebar is the host list. Every host is in it, pinned ones first. The host **in front** —
the one the content area shows — is opened out (▾): under it are its tabs in the order you
opened them, `$ app` for a shell by the directory it stands in, `▤ files` for the SFTP
browser, `✎ nginx.conf` for each open file, then `⇄ 2 tunnels` if it has any. The tab the
content area shows is bold, with a bar (▌) while the keyboard is in it. Other connected hosts are folded up (▸) with a green
dot and how many tabs they hold; a dropped one has a red dot; a host with nothing open is
dim, with a hollow one.

What the content area shows is decided by the tab in front:

- **a shell tab** — that shell, the whole content area.
- **an editor tab** — the open file, with the [terminal panel](#editor) under it if you asked
  for one. The file tree sits in the sidebar, in a box of its own **under the hosts**.
- **the files tab** — the same tree box in the sidebar, and in the content area a **preview**
  of the file under the tree's cursor.

A double [[esc]] from a shell, an editor, the tree or the panel puts the keyboard in the
sidebar, with the cursor on the tab you were in. [[↑]] [[↓]] walk hosts and tabs alike,
[[enter]] goes there — a tab exactly, a host back to where you were on it, connecting first
if it is not — [[→]] opens a host out and [[←]] folds it up. [[esc]] gives the keyboard back
with nothing changed; a double [[esc]] there does the same, and **[[esc]] never quits hop**
([[q]] does). [[ctrl+o]] [[o]] is the same trip from inside a pane, sending nothing to the
remote. Every [host key](#hostlist) works on a host row: [[/]] to filter, [[e]] to edit,
[[S]] for another shell, [[t]] for its tunnels, [[space]] for its menu. Clicking a tab row
goes to that tab, and clicking an open host goes to it.

With **no host in front** — when hop starts, or after the one in front was closed — the
content area lists the **recent places** across every host, which [[↑]] from the first host
reaches and [[enter]] lands on, then the details of the host under the cursor: how to reach
it, what is open on it, and which of those was the last place.

The files have a **terminal panel** under them, the way an IDE keeps one under its editor:
[[`]] in the tree or [[ctrl+o]] [[j]] shows it and moves into it, the same key inside it puts
it away. [[S]] on an entry puts the panel in that directory — a running panel is told to `cd`
there, so its history stays. Drag its top edge to resize it. From the keyboard, [[+]] and [[-]]
in the tree size it directly; from the panel or an editor, [[ctrl+o]] [[+]] or [[ctrl+o]] [[-]]
starts sizing, and [[+]] [[-]] [[↑]] [[↓]] keep going until any other key. It starts at a bit
under half the height. It is a shell of its own, not one of the host's shell tabs, so neither
ever resizes the other.

[[ctrl+o]] [[t]] is the tree's key: from a file it puts the keyboard in the tree box, from the
tree it hides the box, and hidden it brings it back with the keyboard in it. The hosts box
takes what its rows need, up to 40% of the height, so the tree always keeps a usable one.

**A narrow window** — under 94 columns — has no room for the sidebar beside the content.
There the sidebar is off screen until a double [[esc]], and then floats **over** the content
rather than pushing it aside. The files tab draws its tree across the content area, and an
editor tab is the file alone.

**Hiding it.** [[ctrl+o]] [[b]] — or [[shift+b]] in the sidebar — gives the sidebar's width
to the content in a window that has room for both, and the same key docks it again. Hidden,
it behaves as it does in a narrow window: a double [[esc]] floats it over the content, and
[[esc]] puts it away. Hiding and docking resize the panes once, because you asked; nothing
else ever does.

:::why not="readme" Why the sidebar never changes size
A column that comes and goes resizes every pane beside it, and a remote vim or `less`
redraws on each resize. So the sidebar's width is decided by the window and by
[[ctrl+o]] [[b]], never by where the keyboard is: docked, it is always there; too narrow or
hidden, it floats over the content and resizes nothing. A shell keeps one size however often you go to the sidebar and back. And there are
never more than two columns — the tree lives in the sidebar, under the hosts, rather than in a
third column between the hosts and the file.
:::
