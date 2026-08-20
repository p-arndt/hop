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
| [[enter]] [[→]] | expand a directory, or open a file in an editor tab |
| [[o]] | open the file in the local OS default app (GUI) |
| [[d]] | download the file to `~/Downloads` |
| [[u]] | upload a local file into this directory |
| [[R]] | rename the entry |
| [[x]] | delete the entry |
| [[m]] | make a directory here |
| [[s]] | sort by name / size / modified |
| [[space]] | mark the entry and step down — mark a run by holding it |
| [[a]] | mark / unmark everything in this directory |
| [[t]] | make the directory under the cursor the **target** |
| [[c]] | copy what is marked into the target |
| [[v]] | move what is marked into the target |
| [[tab]] | focus the content pane |
| [[\]] | open the file beside the current one, not as another tab |
| [[←]] [[backspace]] | collapse, or step out to the parent |
| [[r]] | refresh the listing |
| [[ctrl+k]] | the [palette](#actions): everything the browser can do, searchable |
| [[,]] | settings |
| [[?]] | the key card |
| [[ctrl+t]] | hide / show the tree column |
| [[ctrl+b]] | hide / show the sidebar |
| [[ctrl+o]] | back to hop |
| [[esc]] [[esc]] | back to hop (two presses within 400 ms) |

With [vim keys](#vim) on the browser keeps the *whole* motion set (the host list only the
step keys): [[j]]/[[k]], [[gg]], [[G]], [[H]]/[[M]]/[[L]], [[ctrl+d]]/[[ctrl+u]],
[[ctrl+f]], plus [[l]] to descend and [[h]] to back out.

Anything that needs an answer — a name to rename to, a local path to upload, a yes before
an overwrite — asks on the status line, and while the question is up every key is its
answer: a [[,]] typed into a filename is a comma, not the settings popover. [[enter]]
answers, [[esc]] cancels, [[ctrl+u]] clears the line.

## Marks and the target

Every file operation is plural. [[space]] marks the entry and steps down, so a run of files
is marked by holding it; [[a]] takes the whole directory. With nothing marked, an operation
falls back to the entry under the cursor, which is why the single-file keys above still read
the way they always did. Marks are keyed by absolute path and survive a refresh, so one
inside a collapsed directory is still marked — the footer always names the total, so it
cannot quietly follow you around.

Copying and moving need somewhere to go, and that somewhere is the **target**: [[t]] pins the
directory under the cursor, the tree draws it in green, and [[c]] and [[v]] send the marked
entries there. Nothing is ever typed as a path, and both ends stay on screen — that is what
the tree column buys that a single listing could not.

Both ask before they destroy anything, as [[d]] and [[u]] already do: a name already taken
in the target is a confirmed overwrite for [[c]] — the question names the files, since one
answer covers all of them — and for [[v]] a refusal, because a move cannot clear the way
without a recursive remote delete, so it says so before it starts rather than failing
halfway through the batch. Anything in the selection that already *lives* in the target is
simply skipped, and the outcome says how many, so a count short of what you marked explains
itself.

A batch stops at the first failure and says where it stopped: `delete b: permission denied —
1 of 4 done, 2 skipped`. The marks stay up, so the same keystroke retries what is left.

Transfers run off the UI, so a large file no longer freezes the browser — the status line
becomes a progress line until it lands, counting `3/7 · name.txt` through a batch. Deleting
asks first, and so does overwriting a file that is already in your download directory.

:::why not="readme" Why a copy costs more than a download
A remote-to-remote copy has no shortcut. SFTP has no server-side copy — OpenSSH added a
`copy-data` extension, but the Go client hop uses does not speak it — so hop reads every byte
down to your machine and writes it back up the same connection. Copying a file across a
remote disk therefore costs *twice* what downloading it costs, which is the opposite of the
intuition. [[v]] is free by comparison whenever source and target share a filesystem, because
that is a rename the server does by itself; only across a mount boundary does it fall back to
the same copy.
:::

The browser is a **column**, not a screen it takes over: it stays drawn while you read a file
beside it, and [[tab]] and [[alt+t]] pass the keyboard between the two. [[ctrl+t]] gives the
column's width back to the file. Below 96 columns of room there is no space for both anyway,
and the browser goes back to filling the pane while it has the keyboard.

:::why not="readme" Why [[←]] walks the tree instead of leaving
[[←]] is pure motion: it collapses the directory you are in, steps out to its parent, and
only at the top of the tree does it pop back to hop. The directory you open in is usually your
home directory — so a [[←]] that left straight away would drop you back to hop exactly when
you meant to go up to `/home`. Leaving is otherwise always explicit: [[ctrl+o]], or a
[double esc](#doubleesc) — though unlike in a pane, a lone [[esc]] here is swallowed rather
than forwarded.
:::
