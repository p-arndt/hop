---
type: language
context: fleet
status: draft
code:
  - internal/store/**
  - internal/config/**
---

# Ubiquitous language — fleet

### Host

**Is:** one saved machine the user can hop to — everything needed to reach it plus
everything hop has learned about it.

**Is not:** a live connection (that is a *client* in [[connection]]), and not the
`HostName` inside it.

**In code:** `internal/store/store.go` — `store.Host`.

### Alias

**Is:** the short name the user types and sees; the host's identity. It is the `Host`
keyword in OpenSSH config.

**Is not:** the `HostName`. `prod-db` is an alias; `10.0.4.19` is a host name.
Everything downstream — sessions, panes, browsers, tunnels — is keyed by alias.

**In code:** `store.Host.Alias`.

### Host name

**Is:** the address hop actually dials — DNS name or IP.

**Is not:** the alias. Defaults to the alias when the config omits it.

### Fleet

**Is:** the whole set of saved hosts, in the order hop presents them.

**In code:** `store.Store.Hosts()`.

### Frecency

**Is:** the ordering rule for unpinned hosts — visit count first, most recent connect
breaking ties. Hosts never visited sort last.

**Is not:** a score stored anywhere. It is computed from `Visits` and `LastConnect`
at sort time.

**In code:** the comparator in `internal/store/store.go`; `Touch` is its only writer.

### Touch

**Is:** the record that a connection to this alias succeeded — bumps `Visits` and
stamps `LastConnect`.

**Is not:** opening the details card, filtering, or a failed connect attempt.

### Pin

**Is:** a user-chosen position at the top of the list, outranking frecency entirely.

**Lifecycle:** unpinned → pinned at `PinOrder` n → moved → unpinned. Pin order stays
**dense and 1-based** across the pinned set; unpinning renumbers the rest.

**In code:** `store.Host.Pinned` / `PinOrder`, `SetPinned`, `MovePin`, `renumberPins`.

### Forward

**Is:** a TCP port-forward definition saved against a host — which side listens, where
it listens, and where the traffic goes.

**Is not:** a running tunnel. A forward is a record; a [[connection]] *tunnel* is a
forward that is currently listening.

**Lifecycle:** defined → validated → carried into a session as a tunnel on connect.

**In code:** `store.Forward`, `store.ForwardKind` (`local` | `remote`).

**Not to be confused with:** `sshx.Tunnel`, which is the live thing.

### Default dir

**Is:** the remote directory a session on this host starts in. Blank means "wherever
the login shell lands".

**In code:** `store.Host.DefaultDir`; honoured by [[pane]] and [[files]].

### Bastion (ProxyJump / ProxyCommand)

**Is:** the machine or local program the transport goes through to reach this host.
`ProxyJump` names another host (alias or `[user@]host[:port]`); `ProxyCommand` names a
local program whose stdio is the transport.

**Rule:** when a host has both, **ProxyJump wins**.

### Import

**Is:** reading `~/.ssh/config` and refreshing the fleet from it, **without losing
frecency, pins or tags**.

**Is not:** a replace. Wildcard patterns are skipped; unusable ports are ignored.

**In code:** `internal/store/sshconfig.go`.

### Sidecar

**Is:** the JSON file holding what OpenSSH has no word for — visits, last connect,
pins, tags, group, default dir — keyed by alias under `config.json`'s `hosts` key.

**Is not:** the source of truth for reachability. That is the OpenSSH file.

**In code:** `internal/store/meta.go`, `internal/config/config.go`.
