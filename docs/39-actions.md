---
id: actions
title: Actions — the menu and the palette
nav: Actions
group: Every mode
label: Every mode
---

Two keys reach everything hop can do without knowing a single binding, and both of them
show the key beside every line — so using them is how you stop needing them.

| Key | What it opens |
| --- | --- |
| [[space]] | the **action menu** for the host under the cursor, anchored to its row |
| [[ctrl+k]] | the **palette**: everything this mode can do, searchable |
| [[ctrl+o]] [[ctrl+k]] | the palette from inside a shell or an editor tab |

The menu is also a **right-click** on a host: the click stands the cursor on it and opens
the menu in one gesture.

**The menu is about the thing under the cursor.** It lists only what that host can take
right now — *connect* on an idle host, *focus its shell* on a live one, *reconnect and
reopen* on one whose connection dropped, *unpin it* on a pinned one. [[↑]]/[[↓]] move,
[[enter]] runs, [[esc]] closes and decides nothing.

**The palette is about the mode you are in.** In the host list it holds the host's actions
and then hop's own; in the [file browser](#browser) it holds the browser's; in a
[shell](#terminal) or an [editor tab](#editor) it holds the chords behind the
[leader](#leader) — which is the keyboard hardest to remember, and so the one it is worth
most for. Type to narrow it: the search matches the label *and* the key, so a
half-remembered [[ctrl+b]] finds the sidebar just as `sft` finds the browser.

## Guidance — how much hop keeps on screen

The first time hop starts it asks one question: how much of its keyboard to keep on
screen. Three answers, and **every key works in all three** — the profile changes what is
*shown*, never what a key does.

| Profile | What you get |
| --- | --- |
| `keys` | the short footer legend and nothing else |
| `hybrid` | the legend, the extra keys a wide window has room for, and the host's actions on the details card |
| `guided` | all of that, plus hop's own keys spelled out beside the host's, and the way to the palette held in the footer where truncation cannot reach it |

`hybrid` is the default, and the one an escape from that first question picks. Change it
any time with [[,]] → **Guidance**. An install that already had a config file is never
asked: it keeps working exactly as it did, on `hybrid`.

:::why not="readme" Why an action is a key, and never its own code path
Every row of the menu and the palette *is* a binding. Running one replays that key through
the same handler a keystroke goes through — a chord like [[ctrl+o]] [[o]] as the two
keystrokes it is. Nothing in hop can be reached from a menu but not from the keyboard, the
key printed beside a row is the key that ran, and a binding that grows a new condition
grows it in one place. The same list also feeds the details card, so what hop offers you
and what hop tells you it offers can never drift apart.
:::
