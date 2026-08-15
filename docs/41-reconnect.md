---
id: reconnect
title: When a connection drops
nav: Reconnecting
group: Every mode
label: Every mode
---

Drops are found with keepalive probes rather than silence, so a suspended laptop or a dead
VPN does not leave a pane quietly frozen.

| Key | Action |
| --- | --- |
| [[r]] [[enter]] | reconnect: dial again and reopen what was open |
| [[d]] [[x]] | drop the session — the pane goes, the host is idle again |
| [[?]] | the key card |
| [[ctrl+o]] [[esc]] [[q]] | back to the host list, leaving the pane on screen |

The pane keeps the last screen the host drew, under a banner saying what happened, so the
command that was running is still there to read. Nothing is forwarded to the far end, because
there is no far end.

Shell tabs, the browser's directory and the running tunnels come back on reconnect. Editor
tabs do not — an editor holds a buffer, and reopening the file on a fresh pty would look like
nothing was lost — and the status says how many were left behind.
