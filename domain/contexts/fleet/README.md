---
type: context
name: fleet
title: fleet
subdomain: supporting
status: draft
owner: p-arndt
code:
  - internal/store/**
  - internal/config/**
  - internal/tui/hostlist.go
  - internal/tui/hostform.go
  - internal/tui/importer.go
  - internal/tui/details.go
  - internal/tui/pin.go
  - internal/tui/list.go
relationships: []
---

# fleet

## Purpose

The answer to *"which machines can I hop to, and which one do I want right now?"*

fleet owns the saved servers: their names, how to reach them, what the user has
labelled them, and — crucially — the order they appear in. An operator with sixty
hosts should find the one they want in two keystrokes without remembering its name,
so the list ranks itself by how often and how recently each host was actually used.

fleet is also the boundary where hop refuses to own the user's data. Reachability
directives live in an OpenSSH config file that `ssh` itself can read; only hop's own
metadata lives in hop's own file. Deleting hop leaves a working `~/.ssh/config`.

## Strategic classification

| Dimension | Value | Why |
|---|---|---|
| Domain type | supporting | Necessary, and specific to hop's file-layout promise, but nobody picks a terminal for its host list |
| Business model role | engagement | Frecency ordering is what makes the tool feel fast on day thirty |
| Evolution | product | The file formats are OpenSSH's; hop adds a thin, conventional layer |

The consequence: **build it simply.** The interesting decisions here were already
made (own the config file format, keep the sidecar small). Resist depth.

## Domain roles

**Registry** with a **ranking policy**. It stores facts and answers one query well
(*give me the hosts, best first*). It does not connect, does not render, does not
know a session exists.

## Ubiquitous language

The terms live in [`language.md`](language.md).

## Inbound communication

| Message | Type | From | Relationship | Note |
|---|---|---|---|---|
| Add / Edit / Rename / Delete host | command | workspace (host form) | customer/supplier | one alias is the identity |
| Pin / MovePin | command | workspace | customer/supplier | pin order is dense and 1-based |
| Touch(alias) | command | connection | customer/supplier | fired on a successful connect; the only writer of frecency |
| ImportSSHConfig | command | workspace (importer) | customer/supplier | refreshes without losing frecency |

## Outbound communication

| Message | Type | To | Relationship | Note |
|---|---|---|---|---|
| Hosts() ordered | query result | workspace | open host service | pinned first, then frecency |
| store.Host | published language | connection, files, pane | open host service | the whole reachability record, carried by value |

## Business decisions

- A host is identified by its **alias**, not by a number. Renaming an alias is a
  distinct operation because it moves the host's identity.
- **Pinned hosts always sort above unpinned ones**, in their pin order. Frecency
  only decides the rest.
- Unpinned hosts sort by **visit count, then last-connect time**. A host never
  visited sorts last, whatever else is true about it.
- **Editing a host must not reset its frecency.** Neither may re-importing the
  SSH config. History is the user's, not the record's.
- Reachability directives (`HostName`, `User`, `Port`, `IdentityFile`, `ProxyJump`,
  `ProxyCommand`, forwards) are written in **OpenSSH syntax to a file `ssh` can use**.
  Only hop-specific facts (visits, pins, tags, default dir) go in the JSON sidecar.
- **Wildcard `Host` patterns are not hosts** and are skipped on import — they are
  defaults, and hop cannot hop to them.
- `ProxyJump` **wins over** `ProxyCommand` when a host defines both.
- A port outside 1..65535 is not a host record, it is a typo: ignored on import,
  refused on save.
- Writes are **atomic** and must leave no temp file behind. Losing a fleet file
  loses the user's servers.

## Aggregates

| Aggregate | Protects | Doc |
|---|---|---|
| Host | alias uniqueness, pin density, forward validity, frecency monotonicity | _not yet written_ |

## Assumptions

- **`store` and `config` are one context, not two.** `config` is anchored here
  because the host sidecar lives under its `hosts` key — but it also carries UI
  settings (theme, vim keys, guidance) that belong to workspace. This is the
  weakest anchor in the model.
- The host-list *UI* files under `internal/tui/` are claimed by fleet on the grounds
  that they speak fleet's language (alias, pin, frecency, tag) rather than the
  workspace's. A human should confirm this reading before anyone acts on it.
- The legacy `hop.db` SQLite migration (`migrate.go`, `sqlitefile.go`) is treated as
  dead weight kept for one more release, not as part of the model.

## Verification metrics

- PRs touching `internal/store` and `internal/sshx` together > 30% → `store.Host` is
  doing double duty as a connection record and the boundary is in the wrong place.
- Growth in fields on `store.Host` that no `ssh` directive corresponds to → the
  sidecar is becoming a second model.

## Open questions

- Should the UI settings in `internal/config` move to a workspace-owned file, leaving
  `config` purely as fleet's sidecar?
- `Host.ID` and `Forward.HostID` are int64 leftovers from the SQLite era while alias
  is the real identity. Are they still load-bearing anywhere?
