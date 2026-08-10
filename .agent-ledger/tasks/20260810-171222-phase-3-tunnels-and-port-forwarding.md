---
id: "20260810-171222-phase-3-tunnels-and-port-forwarding"
title: "Phase 3 tunnels and port forwarding"
status: "completed"
updated: "2026-08-10T17:56:47+02:00"
base_commit: "7920ae52b9bf0c45f47e8e917fa64029d8c2e8b1"
branch: "main"
agent: null
tags: ["ssh", "tui", "tunnels"]
files:
  - "KEYBINDINGS.md"
  - "README.md"
  - "TODO.md"
  - "internal/sshx/tunnel.go"
  - "internal/sshx/tunnel_test.go"
  - "internal/store/store.go"
  - "internal/store/store_test.go"
  - "internal/tui/commands.go"
  - "internal/tui/details.go"
  - "internal/tui/help.go"
  - "internal/tui/hostkey.go"
  - "internal/tui/keys.go"
  - "internal/tui/list.go"
  - "internal/tui/model.go"
  - "internal/tui/msgs.go"
  - "internal/tui/paste.go"
  - "internal/tui/reconnect.go"
  - "internal/tui/session.go"
  - "internal/tui/tunnels.go"
  - "internal/tui/tunnels_test.go"
  - "internal/tui/view.go"
  - "internal/tui/view_test.go"
---

# Phase 3 tunnels and port forwarding

## Goal

Let users define local and remote SSH forwards per host, start and stop them from the TUI, and see their status while reusing the existing SSH connection.

## Scope

- TCP local and remote forwarding only. Dynamic/SOCKS and Unix-domain socket forwards are outside this phase.
- Automated coverage exercises persistence and the Bubble Tea model plus real forwarding traffic over an in-process `x/crypto/ssh` server. It is protocol-level integration coverage, not a black-box TUI run against OpenSSH.

## Discoveries

- Reconnect records persistent forward ids and restores exactly the set that was running; TCP LocalForward/RemoteForward directives are additively synced during SSH-config import, while dynamic and Unix-socket forwarding remain unsupported.

## Decisions

- **Decision:** Persist forwards in a separate table keyed by host id; default blank bind addresses to 127.0.0.1 on both sides.
  - **Reason:** Host edits and alias renames must preserve definitions, and a blank field must never expose a listener publicly.
  - **Trade-off:** Public or wildcard binds require an explicit address.

- **Decision:** Treat a tunnel as a session resource on the host's one authenticated client, with local listen/SSH dial and SSH listen/local dial as symmetric runtimes.
  - **Reason:** Shells, SFTP, editors, and forwards can share authentication and connection-loss handling; closing the last tunnel can release a tunnel-only session.
  - **Trade-off:** A local listener remains bound while a dead session is awaiting reconnect, but rejects new connections until the old session is closed and restored.

## Failures

## Validation

- `just ci` — formatting check, `go vet ./...`, and `go test ./...` passed.

- `go test -race ./internal/sshx ./internal/store ./internal/tui` — passed.

- `go test ./internal/sshx -run 'Test(LocalAndRemoteForwardRoundTripOverSSH|Tunnel)' -count=20` — local and remote TCP round trips over an in-process `x/crypto/ssh` server passed repeatedly.

- Windows and Linux amd64 cross-builds with `CGO_ENABLED=0` — passed.

## Remaining risks

- Only TCP forwarding is represented; DynamicForward and Unix-domain socket forwards are skipped on import.

- SSH-config sync adds/updates matching listeners but deliberately does not remove definitions when directives disappear, matching the host import's non-destructive behavior.

- Not tested: a black-box run that drives the complete TUI workflow against a real OpenSSH server. In particular, server-side `GatewayPorts` policy and allowed/rejected public remote binds were not exercised.

## Handoff

- If stronger regression coverage is needed, extend the Docker/OpenSSH fixture to drive tunnel creation through the TUI model and verify both traffic directions, including an allowed and a rejected `GatewayPorts` bind.
