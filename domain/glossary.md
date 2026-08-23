---
type: glossary
status: draft
verified: 2026-08-23
---

# Cross-context glossary

Only two kinds of entries belong here: language that crosses a boundary, and words
that deliberately mean different things on either side of one. Everything else lives
in `contexts/<name>/language.md`.

## Published language

### Alias
The short name of a saved host, and the **identity key for everything in hop**.
Sessions, panes, browsers, tunnels and messages are all keyed by it.
Owned by: **fleet**. Carried in: `store.Host.Alias`, `filebrowser.Msg.Alias`, the
session map, every status line.

### store.Host
The whole reachability record, passed **by value** across boundaries.
Owned by: **fleet**. Read by: connection, workspace, and the demo server.

### keys.Action
A stable string id for what a key does (`list.pin`, `browser.rename`).
Owned by: **keyboard**. Consumed by: workspace, files, pane, and `tools/docsgen`.
It is also `config.json`'s vocabulary, so renaming one requires a migration.

### Absolute remote path
The identity of a file on a host. Marks are keyed by it; `OpenFileMsg` carries it.
Owned by: **files**. Read by: workspace, pane.

### SHA256 fingerprint
How a host key is shown to a human for a trust decision.
Owned by: **connection**. Shown by: workspace.

## Collisions — same word, different meaning

| Word | Context | Means | Do not conflate with |
|---|---|---|---|
| **Session** | connection | one interactive shell channel with a pty (`sshx.Session`) | workspace's session |
| **Session** | workspace | everything hop holds open for one host — shells, editors, browser, tunnels | connection's session |
| **Client** | connection | one authenticated SSH transport (`sshx.Client`) | the other two |
| **Client** | files (impl) | the SFTP subsystem raised on that transport (`sftpx.Client`) | the other two |
| **Client** | files (port) | the ten filesystem operations a browser needs (`filebrowser.Client`) | the other two |
| **Pane** | pane | an embedded terminal — an emulator bound to a channel (`terminal.Pane`) | the keyboard layer |
| **Pane** | keyboard | a keyboard **layer** — the bindings that apply while a pane holds the keys (`keys.Pane`) | the terminal |
| **Action** | keyboard | what a key means, as a stable id (`keys.Action`) | launching local apps |
| **Action** | *(generic)* | launching a local application — VS Code, a new terminal tab (`internal/action`) | the key's meaning |
| **Forward** | fleet | the saved port-forward **definition** (`store.Forward`) | the running tunnel |
| **Tunnel** | connection | a forward that is **currently listening** (`sshx.Tunnel`) | the saved definition |
| **Mode** | workspace | where keystrokes go — list, shell, scrollback, browser, editor | the keyboard layer |
| **Layer** | keyboard | which binding set resolves a key | workspace's mode |
| **Entry** | files | one thing in a remote directory (`sftpx.Entry`) | a host record |
| **Host** | fleet | a saved machine, identified by alias (`store.Host`) | `HostName`, the address |
| **Host name** | fleet | the address hop dials — DNS name or IP | the alias |

The **Session** and **Client** collisions are the two an agent is most likely to
"unify". Both are load-bearing: one connection holding many shells is the product's
central promise, and collapsing the names would collapse the idea.

## Banned words

| Banned | Say instead |
|---|---|
| "connection" (bare, in code) | *client* (the transport) or *session* — and say which session |
| "session" (bare, across a boundary) | `sshx.Session` (a shell channel) or the workspace session (a host's whole state) |
| "client" (bare) | the transport, the SFTP client, or the browser's port |
| "terminal" (bare) | *pane* (hop's embedded one) or *the terminal hop runs in* |
| "file manager" | *browser* — it is the word the code and the docs both use |
