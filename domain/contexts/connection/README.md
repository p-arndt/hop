---
type: context
name: connection
title: connection
subdomain: core
status: draft
owner: p-arndt
code:
  - internal/sshx/**
  - internal/tui/authprompt.go
  - internal/tui/hostkey.go
  - internal/tui/reconnect.go
  - internal/tui/tunnels.go
relationships:
  - context: fleet
    role: downstream
    pattern: CF
    via: store.Host, read as given
---

# connection

## Purpose

**One connection per host, and everything else is a channel on it.** This is hop's
central promise: a second shell, a file browser, an editor and three tunnels all ride
the connection the user already authenticated once. Nothing re-handshakes, nothing
asks for the password twice, and closing a pane never tears the transport down.

connection also owns the two moments where hop faces the user with something
security-relevant: **who are you** (auth) and **is this really the machine you think
it is** (host key). Both must be answerable from inside a TUI, without an `ssh`
subprocess.

## Strategic classification

| Dimension | Value | Why |
|---|---|---|
| Domain type | core | The multiplexing promise *is* the product; getting auth and liveness right is what makes it usable |
| Business model role | engagement | "no re-authenticating" is the headline in the README |
| Evolution | custom-built | Pure-Go over `x/crypto/ssh` — deliberately not shelling out to `ssh` |

The consequence: **invest here.** Deep model, careful error messages, tests against
a real server. A bug here is felt in every other context.

## Domain roles

**Execution context** and **gateway**. It performs the irreversible act (dialling,
authenticating, trusting a key) and it is the only door to the remote machine.

## Ubiquitous language

The terms live in [`language.md`](language.md).

## Inbound communication

| Message | Type | From | Relationship | Note |
|---|---|---|---|---|
| Connect(host) | command | workspace | customer/supplier | carries the whole `store.Host` |
| answers to a Challenge | command | workspace (auth card) | customer/supplier | via the `Prompter` port |
| trust this fingerprint | command | workspace (host-key card) | customer/supplier | retries the dial, trusting once |
| Close / Disconnect | command | workspace | customer/supplier | tears down every channel with it |

## Outbound communication

| Message | Type | To | Relationship | Note |
|---|---|---|---|---|
| Challenge | query | workspace | published language | questions + echo flags, from keyboard-interactive |
| NewHostKey (fingerprint) | event | workspace | published language | first contact; SHA256 |
| lost | event | workspace, pane, files | published language | keepalive gave up or the server went away |
| Session | published language | pane | open host service | one interactive shell channel with a pty |
| `*ssh.Client` | published language | files | open host service | for `sftpx.Open` to raise the SFTP subsystem |
| Tunnel started/stopped | event | workspace | published language | per saved forward |

## Business decisions

- **One client per host.** Extra shells, the SFTP subsystem, editor ptys and tunnels
  are all channels on it. A second shell must never cost a second handshake.
- **The dial has a timeout; the handshake does not.** Auth is interactive and a human
  typing a TOTP code is not a hung connection.
- **A connection nobody is talking to must still be known to be alive.** Keepalive
  pings every 15s, 10s to answer, three misses closes it — a blackholed link is
  reported, not silently kept.
- **A host key is trusted once, by a human, explicitly.** First contact surfaces the
  SHA256 fingerprint and asks. hop never trusts silently.
- **A cancelled auth prompt stays cancelled.** Once the user backs out of a
  keyboard-interactive round, later questions in the same attempt are refused rather
  than re-asked — otherwise a server can loop the user forever.
- **A failed connection must say what to try next.** No agent, no keys, a
  passphrase-protected key, an unknown bastion key: each is a distinct, actionable
  message, not "handshake failed".
- **Saved forwards come back on reconnect.** A tunnel the user set up survives a link
  drop without them re-arming it.
- **hop does not shell out to `ssh`** — except where the user's own `ProxyCommand`
  says to, which is their program, run with their stdio as the transport.

## Aggregates

| Aggregate | Protects | Doc |
|---|---|---|
| Client | one live transport per alias; liveness; orderly teardown of every channel | _not yet written_ |

## Assumptions

- The auth card, host-key card, reconnect logic and tunnel manager under
  `internal/tui/` are anchored here because they speak connection's language
  (challenge, fingerprint, forward, keepalive) even though they live in the TUI
  package. Confirm with a human before treating this as settled.
- `sshx.Session` is modelled as belonging to connection, and the *pty* it carries as
  belonging to [[pane]]. That line is drawn where `Session.Resize` sits, which is
  arguably on the wrong side.

## Verification metrics

- Any code path outside this context calling `ssh.Dial` → the gateway has leaked.
- The number of distinct error strings for a failed connect shrinking → someone is
  collapsing actionable failures into one message.
- PRs touching `internal/sshx` and `internal/terminal` together > 30% → the
  session/pty line is in the wrong place.

## Open questions

- Should `Session` (a channel with a pty) belong to [[pane]] rather than here?
- Tunnels are a sub-model with their own lifecycle (defined in [[fleet]], run here,
  managed in [[workspace]]). Do they deserve their own context, or is that three
  contexts for one feature?
