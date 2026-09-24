---
id: modes
title: The three modes
nav: How they fit together
group: The three modes
---

Every mode returns to the [sidebar](#sidebar) — a double [[esc]] inside 400 ms from any
pane, the browser or an editor, or [[ctrl+o]] [[o]] behind the leader. The cursor lands on
where you were, and [[esc]] in the sidebar goes straight back there. **[[esc]] never quits
hop**: [[q]] or [[ctrl+c]] in the sidebar does.

One row along the bottom says where you are and what to press. On the left, the **crumb**
names the place — the host, the tab, and the thing it is at: the directory a shell is
standing in, the file an editor tab holds, the listing the browser shows. A message such as
*connected* takes that side for the few seconds it is up. On the right is the key legend,
and it is deliberately short: it names the keys the mode cannot be worked without, adds more
as the window gets wider, and leaves the full table to [[?]] (the [key card](#cards)), which
opens on the section for the mode you are in.

:::modes
| Mode | You're here when | Who owns your keystrokes | Read next |
| --- | --- | --- | --- |
| **Navigation** | the sidebar has the keyboard (the default) | hop | [Sidebar keys](#hostlist) · [SSH config import](#import) · [Host keys](#hostkeys) |
| **Terminal** | you connected with `enter` or `s` | the **remote shell** | [Shells](#terminal) · [Scrollback](#scrollback) |
| **Browsing** | you opened the SFTP browser with `f` | hop | [File browser](#browser) · [Editor tabs](#editor) |
:::

Nothing here has to be memorised first. [[space]] opens the [action menu](#actions) on the
host under the cursor and [[ctrl+k]] the palette for whatever mode you are in — both list
what is possible *and* the key that does it, and how much hop keeps on screen without being
asked is one setting (see [Guidance](#actions)).

Everything else works in **all** of them: the [sidebar beside every tab](#sidebar), the
[settings popover](#settings), the [tunnels](#tunnels), the [mouse](#mouse) and the optional
[vim keys](#vim).

**Two rules explain most of the keyboard:**

- **In a pane and in the browser, [[ctrl+o]] is [hop's leader](#leader).** It does nothing on
  its own and it is on no clock — it opens a menu in the footer and waits. [[ctrl+o]] [[o]]
  goes to the sidebar.
- **Inside a pane, everything else is the remote's.** hop reserves as few keys as it can,
  because every one it takes is one the shell or editor no longer gets.
