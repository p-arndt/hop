---
id: import
title: Import — the SSH config card
nav: SSH config import
group: Navigation mode
label: Navigation mode
---

[[i]] opens the import card, a modal like the rest: one field, pre-filled with
`~/.ssh/config`, so the usual answer is a single [[enter]].

| Key | Action |
| --- | --- |
| [[enter]] | import from the path shown |
| [[esc]] | close, importing nothing |
| [[ctrl+u]] | clear the path |
| [[backspace]] / text | edit the path (a leading `~` is expanded) |

The **first run** opens this card by itself: with no hosts yet and a `~/.ssh/config` on disk,
hop offers the import instead of showing an empty list. [[esc]] skips it — an empty list is
not an error, and [[a]] adds a host by hand.

It stays bound once the list is full, because importing is a **sync**, not a one-time step:
each host is upserted, so a re-import refreshes what the config knows (hostname, user, port,
identity file, `LocalForward`/`RemoteForward`, `ProxyCommand`/`ProxyJump`) and leaves hosts
hop added itself untouched. Wildcard patterns (`Host *`) are skipped. `hop import [path]`
does exactly the same thing.
