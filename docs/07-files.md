---
id: files
title: Where things live
nav: Where things live
group: Start
---

| What | Path |
| --- | --- |
| Hosts | `~/.ssh/hop.config` (OpenSSH config syntax, hand-editable) |
| Settings, plus host tags, pins and visit counts | `<config dir>/hop/config.json` (plain JSON, hand-editable) |
| Update check cache | `<config dir>/hop/update-check.json` (last check + latest version seen) |
| Known hosts | your usual `~/.ssh/known_hosts` |

| Platform | `<config dir>` |
| --- | --- |
| Windows | `%AppData%\hop\` |
| macOS | `~/Library/Application Support/hop/` |
| Linux | `~/.config/hop/` |

hop keeps its hosts in an OpenSSH config file of its own and adds a single `Include
hop.config` line to the top of your `~/.ssh/config`. Every host you save in hop is
therefore a host `ssh`, `scp` and `rsync` can reach by the same alias, and a host you
write into `~/.ssh/hop.config` by hand shows up in hop. hop rewrites that file when you
add, edit or remove a host, so comments inside it are not preserved — hosts you want hop
to leave alone belong in `~/.ssh/config` itself.

Anything OpenSSH has no keyword for — tags, groups, pin order, how often you connect — is
hop's own preference about a host, so it sits under the `hosts` key of `config.json`
alongside the rest of your settings, out of `~/.ssh` entirely. Losing that key costs your
pins and ordering, never a host.

A missing or malformed config file starts hop on defaults rather than refusing to start.

Upgrading from a version that kept its hosts in SQLite? The first start converts
`<config dir>/hop/hop.db` into the files above and leaves the database behind as
`hop.db.bak`. Nothing deletes it.
