---
id: editor
title: Editing — editor tabs
nav: Editor tabs
group: Browsing mode
label: Browsing mode
---

[[enter]] on a file opens it in an editor **inside hop**, in the content area beside the
browser column, with a tab strip above it listing every open file. The tree stays on screen.

| Key | Action |
| --- | --- |
| [[shift+→]] [[shift+←]] | next / previous tab (wraps) |
| [[ctrl+o]] [[1]] … [[9]] | go straight to that tab, without leaving |
| [[ctrl+o]] [[o]] | back to the file browser |
| `:q` (i.e. quit the editor) | close the tab |
| [[alt+t]] | back to the tree, without closing anything |
| [[ctrl+t]] | hide / show the tree column |
| [[esc]] [[esc]] | back to the file browser (two presses within 400 ms) |
| [[alt+←]]/[[alt+→]], [[alt+h]]/[[alt+l]], [[alt+1]]…[[alt+9]] | aliases, where your terminal sends them |
| *everything else* | sent to the remote editor |

## How it works

The editor runs **on the remote host**, not locally: hop opens a second SSH channel on the
connection it already has and runs `${EDITOR:-vi} <file>` on a pty, then renders that pty in
a pane exactly as it renders a remote shell. There is no download and no copy — you are
editing the real file, and `:w` writes straight back to the server.

If the remote `$EDITOR` is unset (it usually is over SSH, since the rc-file that sets it is
never sourced for a non-interactive command), hop probes the remote `PATH` for `nvim`,
`vim`, `vi`, then `nano`, falling back to `vi` — POSIX requires it to exist.

Tabs are independent editor processes, so leaving with [[ctrl+o]] [[o]] keeps them all
running: come back and every file is where you left it, cursor included.

## Two files side by side

[[\]] in the browser opens the file **beside** the current one instead of behind it,
splitting the content area into two halves with their own tab strips. [[tab]]/[[alt+t]] pass
the keyboard between tree and content; [[shift+→]]/[[shift+←]] cycle the tabs of whichever
half has it. The same file is never shown in both halves — asking for one that is already
open just focuses the half it is in. Closing the last tab in a half folds the split back to
one, and a content area too narrow to give each half a readable 22 columns declines the split
rather than drawing two unreadable ones.

## Editing locally instead

[[o]] is the escape hatch for files a terminal editor is no good for — a PDF, an image. It
downloads the file to a scratch directory under the system temp dir and hands that copy to
the desktop (`start` on Windows, `open` on macOS, `xdg-open` elsewhere), returning
immediately so hop stays usable. Unlike [[enter]], this edits a *local copy*: nothing is
written back to the remote host. [[d]] downloads without opening anything.
