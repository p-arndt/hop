---
id: tunnels
title: Tunnels — port forwarding
nav: Tunnels
group: Every mode
label: Every mode
---

Forwards are defined **per host** and ride the connection hop already holds, so a tunnel
costs no extra handshake and no second authentication.

| Key | Where | Action |
| --- | --- | --- |
| [[t]] | the host list | start every defined tunnel, or stop them all when any are running |
| [[T]] | the host list | open this host's tunnel manager |
| [[↑]] [[↓]] | the manager | select a definition |
| [[enter]] [[space]] | the manager | start or stop the selected one |
| [[a]] [[e]] [[x]] | the manager | add / edit / delete a definition |
| [[t]] | the manager | close the card and start or stop the whole set |
| [[esc]] | the manager | close |

A definition is a direction (**local** or **remote**), a bind address and port, and the host
and port to reach on the far side. `LocalForward` and `RemoteForward` lines are picked up by
the [SSH config import](#import), so the forwards you already have keep working. Editing a
running definition stops the old one on save, and a [reconnect](#reconnect) puts the set that
was running back up.

The status dot in the host list shows `⇄2` when two tunnels are up on that host.
