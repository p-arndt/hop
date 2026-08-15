---
id: update
title: Staying current
nav: Staying current
group: Start
---

```bash
hop check-update   # is there a newer release?
hop self-update    # download it, verify its checksum, swap this binary
```

`self-update` fetches the archive for *your* platform from the latest GitHub release, checks
its SHA-256 against that release's `checksums.txt`, and replaces the running binary
atomically. On Windows the old `hop.exe` is renamed aside and swept up the next time hop
starts. Source builds (`version = dev`) are refused: there is nothing to compare them against.

:::note
hop also checks once a day in the background and mentions a newer version in the footer and
on the CLI. `HOP_NO_UPDATE_CHECK=1` turns that off; the two commands above still work.
:::
