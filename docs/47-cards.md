---
id: cards
title: The cards
nav: The cards
group: Every mode
label: Every mode
---

All of them are modal: while a card is up it takes every key, and [[esc]] closes it.

| Card | Opens with | Keys |
| --- | --- | --- |
| Import | [[i]] | [[enter]] imports from the path shown, [[ctrl+u]] clears it, any text edits it |
| Tunnels | [[T]] | [[↑]]/[[↓]] select, [[enter]]/[[space]] start or stop, [[a]] [[e]] [[x]] add / edit / delete, [[t]] closes and toggles the set |
| Authentication | by itself | [[enter]] submits, [[tab]]/[[shift+tab]] move between fields, [[ctrl+u]] clears, [[esc]] abandons the connect |
| Host key | by itself | [[y]] trusts the fingerprint and retries, [[n]]/[[esc]] trusts nothing |
| Add / edit host | [[a]] / [[e]] | [[↑]]/[[↓]] or [[tab]] move between fields, [[enter]] saves, [[esc]] cancels |
| Delete host | [[x]] | [[enter]] confirms, [[esc]] cancels |
| Keys | [[?]] — [[ctrl+o]] [[?]] in a shell or editor | any key closes it |
| [Action menu](#actions) | [[space]], or a right-click on a host | [[↑]]/[[↓]] select, [[enter]] runs, [[esc]] closes |
| [Palette](#actions) | [[ctrl+k]] — [[ctrl+o]] [[ctrl+k]] in a pane | any text searches, [[↑]]/[[↓]] select, [[enter]] runs |
| Welcome | by itself, once, on a first run | [[↑]]/[[↓]] pick a [guidance profile](#actions), [[enter]] starts hop |

The **keys card** opens on the section for the mode you are in — the shell's keys from a
shell, the browser's from the browser — and marks it *you are here*. That is what lets the
footer stay short: it names the two or three keys a mode cannot be worked without and leaves
the rest to this card, which is only a fair trade if the card starts where you are.

[[?]] reaches it from every mode hop owns the keyboard in. In a **shell** or an **editor** a
bare [[?]] is a question mark the remote is owed, so there it is [[ctrl+o]] [[?]] — the same
key, one level in.

The **host form** carries more than an address: a group, a default directory every session on
that host starts in, an identity file, and `ProxyCommand` / `ProxyJump` for hosts you reach
through a bastion. Renaming the alias keeps the host's visit history, so it does not lose its
place in the [frecency](#hostlist) order.
