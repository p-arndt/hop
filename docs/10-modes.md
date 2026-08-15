---
id: modes
title: The three modes
nav: How they fit together
group: The three modes
---

The footer always shows the keys for the mode you are in, and every mode returns to the host
list — from a pane with [[ctrl+o]] [[o]], from the browser with [[ctrl+o]], and from either
with a double [[esc]] inside 400 ms.

:::modes
| Mode | You're here when | Who owns your keystrokes | Read next |
| --- | --- | --- | --- |
| **Navigation** | the host list is focused (the default) | hop | [Host list](#hostlist) · [SSH config import](#import) · [Host keys](#hostkeys) |
| **Terminal** | you connected with `enter` or `s` | the **remote shell** | [Shells](#terminal) · [Scrollback](#scrollback) |
| **Browsing** | you opened the SFTP browser with `f` | hop | [File browser](#browser) · [Editor tabs](#editor) |
:::

Everything else works in **all** of them: the [sidebar toggle](#sidebar), the
[settings popover](#settings), the [tunnels](#tunnels), the [mouse](#mouse) and the optional
[vim keys](#vim).

**Two rules explain most of the keyboard:**

- **Inside a pane, [[ctrl+o]] is [hop's leader](#leader).** It does nothing on its own and it
  is on no clock — it opens a menu in the footer and waits. [[ctrl+o]] [[o]] goes **out**.
- **Outside a pane** — in the browser, in a card — [[ctrl+o]] simply goes back. There is no
  remote program competing for keys there, so there is nothing to lead.
- **Inside a pane, everything else is the remote's.** hop reserves as few keys as it can,
  because every one it takes is one the shell or editor no longer gets.
