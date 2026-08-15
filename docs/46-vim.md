---
id: vim
title: Vim keys
nav: Vim keys
group: Every mode
label: Every mode
---

**Off until you turn them on**, in the settings popover ([[,]] → *Vim keys*). Off, they are
not bound to anything else either: a stray [[l]] in the host list does nothing at all.
[[pgdn]]/[[pgup]] are *not* part of the switch — they page without being vim, so turning vim
off never costs you a way to page.

::::columns cols="repeat(auto-fit,minmax(17rem,1fr))"
:::col
### The host list — step keys only

| Key | Action |
| --- | --- |
| [[j]] [[k]] | move down / up |
| [[l]] | connect — as [[enter]] does |
| [[h]] | back — as [[esc]] does |
:::
:::col
### The browser — the whole motion set

| Key | Action |
| --- | --- |
| [[j]] [[k]] | move down / up |
| [[gg]] / [[G]] | first / last entry |
| [[H]] [[M]] [[L]] | top / middle / bottom **of the visible window** |
| [[ctrl+d]] [[ctrl+u]] | half a page down / up |
| [[ctrl+f]] | a full page down ([[pgup]] pages back) |
| [[l]] / [[h]] | enter a directory / up one directory |
:::
::::

:::why not="readme" The rules the two sets follow
- **The two views bind different amounts of the keyboard**, never different meanings. A key
  the list binds does there what it does in the browser.
- **[[gg]] is a real two-key motion** — in the browser. A lone [[g]] arms it; any other key
  in between cancels it. In the host list it is not bound.
- **[[ctrl+b]] is not a motion anywhere.** It is the sidebar toggle in every mode.
- **Half and full pages are viewport-relative**, matching vim, and clamped to at least one
  row so they still work in a very short terminal.
- **The cursor never leaves the visible window.** Every motion re-clamps the scroll offset;
  `TestCursorStaysVisible` pins that invariant.
- Motions are inert on an empty listing rather than driving the cursor negative.
:::
