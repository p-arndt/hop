---
id: cli
title: CLI
group: Start
---

For when you are already in a shell.

| Command | What it does |
| --- | --- |
| `hop` | launch the TUI |
| `hop import [path]` | sync hosts from `~/.ssh/config`, or from another file |
| `hop add web1 deploy@10.0.0.4:2222` | add a host by alias and target |
| `hop list` | print `alias  user@host:port` |
| `hop check-update` | is a newer release out? |
| `hop self-update` | upgrade this binary in place |
| `hop version` | print the version |
