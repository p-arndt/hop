---
type: language
context: connection
status: draft
code:
  - internal/sshx/**
---

# Ubiquitous language — connection

### Client

**Is:** one established, authenticated SSH transport to one host — the thing every
channel rides on. It knows whether it is still alive and when it was lost.

**Is not:** `sftpx.Client` (the SFTP subsystem raised *on* this one) and not
`filebrowser.Client` (a port [[files]] defines for its own needs).

**Lifecycle:** dialling → authenticating → established → **lost** (closed by us, by
the server, or by keepalive giving up).

**In code:** `internal/sshx/sshx.go` — `sshx.Client`.

**Not to be confused with:** the two other `Client`s above. See the glossary.

### Session

**Is:** one interactive shell channel on a client, with a pty. Stdout is the merged
stdout+stderr stream.

**Is not:** hop's per-host workspace, which [[workspace]] also calls a *session* and
which holds many of these. This collision is the sharpest one in the codebase.

**In code:** `sshx.Session`.

**Not to be confused with:** the `session` in [[workspace]], which is a host's whole
state and holds many of these. The collision is deliberate — see the glossary.

### Channel

**Is:** anything multiplexed over one client — a shell session, the SFTP subsystem, an
editor pty, a tunnel.

**Rule:** opening a channel never re-authenticates. If it does, the promise is broken.

### Keepalive

**Is:** the periodic ping that decides a silent connection is dead. 15s interval, 10s
to answer, three consecutive misses closes the client.

**Is not:** a reconnect. It only detects.

**In code:** `Client.keepalive`, `Client.ping`.

### Lost

**Is:** the moment the transport stopped being usable, whatever the cause — server
gone, network gone, we closed it, keepalive gave up.

**In code:** the `lost` channel on `sshx.Client`; `waitErr` is only safe to read after
observing that close.

### Reconnect

**Is:** dialling again for an alias whose client was lost, and restoring what the user
had — notably the tunnels.

**Is not:** keepalive, and not re-authenticating a live connection.

**In code:** `internal/tui/reconnect.go`.

### Prompter

**Is:** the port through which connection asks a human a question mid-handshake.
[[workspace]] implements it with a card.

**In code:** `sshx.Prompter`, `sshx.PrompterFunc`.

### Challenge

**Is:** one round of keyboard-interactive auth: an instruction plus questions, each
flagged whether the answer should echo.

**Is not:** a password prompt specifically — a TOTP code, a security question and a
banner all arrive this way. Banner-only rounds carry no questions and are answered
with nothing.

**In code:** `sshx.Challenge`, `sshx.Question`.

### Sticky cancel

**Is:** the rule that once a user cancels a challenge, every later question in the
same auth attempt is refused rather than asked again.

**Is not:** a failure of other kinds — a wrong answer does not stick.

**In code:** `sshx.stickyCancel`.

### Auth method

**Is:** one way of proving identity, tried in order: the agent, identity files, then
interactive. hop reports precisely which ones were unavailable and why.

**In code:** `internal/sshx/agent_*.go`, `keys.go`, `prompt.go`.

### Host key / fingerprint

**Is:** the server's public key, and its SHA256 digest as shown to the user on first
contact. Trusting it writes it to known_hosts.

**Rule:** never trusted without a human saying so.

**In code:** `Client.NewHostKey`, `internal/tui/hostkey.go`.

### Bastion

**Is:** the intermediate hop the transport goes through — a `ProxyJump` host dialled
first, or a local `ProxyCommand` program whose stdio is the wire.

**In code:** `internal/sshx/proxy.go`.

### Tunnel

**Is:** a **running** port forward — a listener currently accepting and shuttling
traffic across the client.

**Is not:** `store.Forward`, which is the saved *definition*. A forward becomes a
tunnel when a connection starts it, and stops being one when the client is lost.

**Lifecycle:** forward defined in [[fleet]] → started on connect → listening → stopped
(explicitly, or with the client) → restarted on reconnect.

**In code:** `sshx.Tunnel`.
