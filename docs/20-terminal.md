---
id: terminal
title: Terminal — a live shell on a remote host
nav: Shells
group: Terminal mode
label: Terminal mode
---

| Key | Action |
| --- | --- |
| [[ctrl+o]] [[o]] | **out** — back to hop |
| [[esc]] [[esc]] | back to hop (two presses within 400 ms) |
| [[shift+→]] [[shift+←]] | next / previous shell on this host (wraps) |
| [[ctrl+o]] [[1]] … [[9]] | go straight to that shell, without leaving the pane |
| [[ctrl+o]] [[0]] | open **another** shell on this host, without leaving the pane |
| [[ctrl+o]] [[c]] | open **this directory** in VS Code Remote |
| [[shift+↑]] [[shift+pgup]] | scroll back into the pane's history |
| [[ctrl+b]] | hide / show the sidebar — the pane takes the whole window |
| [[ctrl+g]] | hand the mouse to your terminal (and take it back) |
| [[alt+0]], [[alt+←]]/[[alt+→]], [[alt+1]]…[[alt+9]] | aliases for the above, where your terminal sends them |
| *everything else* | sent to the remote shell |

## Several shells on one host

::::columns cols="minmax(0,1.2fr) minmax(0,1fr)"
:::col
[[S]] in the host list, or [[ctrl+o]] [[0]] from inside the pane, opens another shell on a
host you are already connected to. It is a second **channel** on the connection hop already
holds — no new handshake, no second authentication — and it appears as a tab strip above the
pane, which shows up only once there is a second shell to switch to. The new shell arrives
focused.

Type `exit` to close a shell: its tab goes away, the rest keep running. When the last one
exits, the connection is done and the host goes back to idle in the list — unless its SFTP
browser, an editor tab or a tunnel is still open on it, which keeps the connection alive.
[[d]] still tears down the whole host at once.
:::
:::figure only="site" src="assets/screens/shells.png" alt="Two shells on one host shown as a tab strip" width="1500" height="800"
Two shells, one connection — no second handshake.
:::
::::

:::why not="readme" Why [[←]] is not a way out
[[←]] backs out of a host in the list and out of a directory in the browser, but inside a
shell it is the **shell's key, always**. It moves the readline cursor back over a typo, it is
what word motions and every full-screen program are built on, and hop taking it — even at
what hop believes is an empty prompt — breaks editing on every server you connect to. hop
sees keystrokes going out and pixels coming back, not the buffer readline is holding, so
anything it could not count left it thinking the prompt was bare when it was not.

[[alt+o]] is deliberately unbound for the same family of reasons: a terminal sends it as
[[esc]] then [[o]], which is vim's "leave insert mode, open a line".
:::

## The cursor

The pane draws the cursor itself — the emulator hands hop cells, not a cursor — and draws
the one the remote asked for: a **block**, an **underline** or a **bar** (`DECSCUSR`, what
vim switches between as you enter and leave insert mode), and no cursor at all while the
program has it hidden (`DECTCEM`), the way a full-screen program hides it while it paints.
A bar has no half-cell to stand in, so it is drawn as the thinnest glyph there is in place
of the character; a full reset, and a program leaving the alternate screen, put the block
back rather than leaving its shape behind.

Blinking is the one part that is yours: it is a clock hop has to run, so it is off until
[[,]] → **Cursor blink** asks for it, and even then a cursor the remote asked to stand
still does.
