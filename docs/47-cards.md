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
| Keys | [[?]] | any key closes it |

The **host form** carries more than an address: a group, a default directory every session on
that host starts in, an identity file, and `ProxyCommand` / `ProxyJump` for hosts you reach
through a bastion. Renaming the alias keeps the host's visit history, so it does not lose its
place in the [frecency](#hostlist) order.
