---
id: files
title: Where things live
nav: Where things live
group: Start
---

| What | Path |
| --- | --- |
| Host database | `<config dir>/hop/hop.db` (SQLite) |
| Settings | `<config dir>/hop/config.json` (plain JSON, hand-editable) |
| Update check cache | `<config dir>/hop/update-check.json` (last check + latest version seen) |
| Known hosts | your usual `~/.ssh/known_hosts` |

| Platform | `<config dir>` |
| --- | --- |
| Windows | `%AppData%\hop\` |
| macOS | `~/Library/Application Support/hop/` |
| Linux | `~/.config/hop/` |

A missing or malformed config file starts hop on defaults rather than refusing to start.
