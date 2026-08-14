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

- x/crypto/ssh has no ProxyCommand concept but needs none: ssh.NewClientConn takes any net.Conn, so a subprocess's pipes and a bastion channel are the same shape. Both routes funnel through sshx.clientOverConn, which keeps auth and host-key policy identical to a direct dial.

- procConn cannot honour deadlines - os/exec pipes have none. SetDeadline returns an error rather than a silent nil; x/crypto/ssh never sets one on a conn it did not dial itself, so the handshake is unaffected. This is why ClientConfig.Timeout has no effect on a proxied dial.

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

- For an e2e bastion test, internal/dockerenv already runs openssh-server; a second container plus ProxyJump between them would cover dialViaJump.

- sshx.dialProxyCommand ignores ClientConfig.Timeout (procConn has no deadlines) — a hanging broker hangs the dial. Bound it in dialWithProxy if that shows up.
