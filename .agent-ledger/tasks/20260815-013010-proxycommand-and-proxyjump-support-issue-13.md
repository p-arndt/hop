---
id: "20260815-013010-proxycommand-and-proxyjump-support-issue-13"
title: "ProxyCommand and ProxyJump support (issue #13)"
status: "completed"
updated: "2026-08-15T01:30:38+02:00"
base_commit: "258c711b0c5ad77bc81ddb48e58d21667e87cbd7"
branch: "main"
agent: null
tags: ["issue-13", "proxy", "ssh"]
files:
  - "TODO.md"
  - "internal/sshx/proxy.go"
  - "internal/sshx/proxy_helper_test.go"
  - "internal/sshx/proxy_helper_windows_test.go"
  - "internal/sshx/proxy_test.go"
  - "internal/sshx/sshx.go"
  - "internal/store/store.go"
  - "internal/store/store_test.go"
  - "internal/tui/details.go"
  - "internal/tui/hostform.go"
  - "main.go"
---

# ProxyCommand and ProxyJump support (issue #13)

## Goal

Let hop reach hosts that no direct TCP dial can: through a local broker program (AWS SSM, cloudflared, IAP) or through a bastion, imported from ssh_config and editable per host.

## Scope

- ProxyCommand values reaching hop can come from an imported ~/.ssh/config, i.e. a file hop did not author.

## Discoveries
- A ProxyJump may name a host in the store whose own ProxyJump names the first, so the chain can close on itself; `dialViaJump` recursed until the stack gave out. `dialState` now records each bastion address and refuses a repeat or more than `maxJumpDepth` (10) hops.
- End-to-end tests against in-process SSH servers (`proxy_e2e_test.go`) found three bugs the unit tests could not: `=` in the issue's own `--parameters portNumber=%p` was rejected as a shell metacharacter; `procConn.RemoteAddr()` returned the program name, so every ProxyCommand dial failed the known_hosts lookup; and an unknown bastion key looped the fingerprint card forever because the retry re-dialled the bastion untrusted.
- The host-key approval is single-use per dial (`dialState.take`). A jump meeting two unknown hosts therefore asks twice — bastion, then target — instead of measuring the target against a fingerprint approved for the bastion and reporting a bogus "possible key swap".

- x/crypto/ssh has no ProxyCommand concept but needs none: ssh.NewClientConn takes any net.Conn, so a subprocess's pipes and a bastion channel are the same shape. Both routes funnel through sshx.clientOverConn, which keeps auth and host-key policy identical to a direct dial.

- procConn cannot honour deadlines - os/exec pipes have none, so ClientConfig.Timeout (read only by ssh.Dial) has no effect on a proxied dial. Replaced by a first-byte watchdog: the server banner arrives before any authentication, so bounding it is safe even though the handshake around it deliberately is not. The jump leg uses ssh.Client.DialContext with the same dialTimeout.

- A proxy that refuses (aws ssm against a stopped instance) writes its diagnosis to stderr and exits; without capturing it the dial fails as a bare EOF with no cause. procConn keeps a bounded 4KB stderr buffer and substitutes it for the EOF.

## Decisions

- **Decision:** Split ProxyCommand into argv in-process (quotes and backslash escapes only) and refuse any line containing shell metacharacters, instead of running it through sh -c / cmd /c.
  - **Reason:** An imported config must never become a path that executes arbitrary shell. The issue's own aws ssm line and ~all real ProxyCommands need no shell.
  - **Trade-off:** Pipes, redirects, globs and $VAR are unsupported; the error (ErrProxyNeedsShell) tells the user to wrap them in a script.

- **Decision:** ProxyJump is implemented as a second sshx.Connect plus client.Dial over the bastion, not as an ssh -W ProxyCommand.
  - **Reason:** Pure Go, no external ssh binary, and the bastion's own host key is verified by its own TOFU dial - so a compromised bastion still cannot pose as the target.
  - **Trade-off:** Only the first hop of a comma-separated ProxyJump chain is used; a multi-hop chain silently takes the first.

- **Decision:** ProxyJump wins over ProxyCommand when a host has both.
  - **Reason:** Matches OpenSSH's own precedence.

- **Decision:** The jump resolver is a package-level func in sshx, installed from main.go.
  - **Reason:** A dial is reached from sessions, reconnects and tunnels; threading a *store.Store through all of them would touch far more than this feature. Lets a ProxyJump name a hop alias and inherit its user, port and key.
  - **Trade-off:** Process-global state; a test that needs a resolver must set and restore it.

## Failures

- **Approach:** Treating backslash as an escape inside double quotes, the way sh does.
  - **Command:** `go test ./internal/sshx/ -run TestSplitProxyCommand`
  - **Evidence:** splitProxyCommand("\"C:\\Program Files\\aws\\aws.exe\" ssm") = ["C:Program Filesawsaws.exe" ...]
  - **Lesson:** hop is Windows-first: inside double quotes a backslash may only escape a quote or another backslash, otherwise every quoted Windows path loses its separators.

## Validation

- go build ./... , go vet ./... , gofmt -l . — clean

- go test ./... — all packages pass, including new internal/sshx/proxy_test.go and the three new store tests

## Remaining risks

- Neither route is exercised against a real broker or bastion; tests cover parsing, token expansion, stderr capture and persistence only. No dockerenv e2e for a bastion yet.

- Upsert overwrites proxy_command/proxy_jump from the config on re-import, so a value typed in the host form is lost if the same alias is re-imported without it. Pre-existing behaviour for default_dir too — not introduced here, but now affects two more fields.

## Handoff

- dialViaJump is covered end-to-end against in-process SSH servers (proxy_e2e_test.go: dials through a bastion, resolves an alias, two-step host-key first contact, refused forwarding, jump loop) rather than the two-container Docker rig first sketched here. The in-process servers are real ssh.NewServerConn peers, so the handshake and direct-tcpip channel are real; what they do not exercise is OpenSSH's own implementation of them.
- Residual gap, deliberately not closed: no test runs against a real sshd bastion. The realistic failure it would add is AllowTcpForwarding no, which TestProxyJumpBastionRefusesForwarding now stages directly. A Docker rig would mostly re-test x/crypto against OpenSSH, which internal/dockerenv already does for auth.

