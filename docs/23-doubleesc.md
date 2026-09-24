---
id: doubleesc
title: How double-esc works, and what it costs
nav: The double esc
group: Terminal mode
label: Terminal mode
---

A lone [[esc]] **is still forwarded to the shell.** hop cannot know a second [[esc]] is
coming without swallowing the first one and waiting out the timer, which would put a 400 ms
lag on every [[esc]] you press in vim. So the rule is:

| You press | The shell receives | hop does |
| --- | --- | --- |
| [[esc]] | [[esc]] | arms the window |
| [[esc]] [[esc]] (fast) | [[esc]] | hands the keyboard to the sidebar on the second |
| [[esc]] … pause … [[esc]] | [[esc]] [[esc]] | nothing |
| [[esc]] [[j]] [[esc]] | [[esc]] [[j]] [[esc]] | nothing — any key breaks the chord |

:::note
**The trade-off:** if you mash [[esc]] twice quickly in vim, you will land in the sidebar.
The shell will have seen one of those escapes, which in normal mode is a harmless no-op, so
nothing is lost — [[esc]] drops you straight back into the pane, and a double [[esc]] in the
sidebar does the same rather than sending an [[esc]] on. If that bothers you, [[ctrl+o]]
[[o]] goes to the sidebar and sends nothing to the remote at all. However many times you
press it, [[esc]] never quits hop.
:::
