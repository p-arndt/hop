---
id: "20260729-190813-interactive-ssh-auth-2fa-otp-passwords-with-docker-end"
title: "Interactive SSH auth (2FA/OTP, passwords) with Docker end-to-end tests"
status: "completed"
updated: "2026-07-29T19:09:08+02:00"
base_commit: "8b535da93e58785075ed72c4a28bc53c4f959db2"
branch: "main"
agent: null
tags: ["2fa", "auth", "docker", "ssh", "testing", "tui"]
files:
  - "KEYBINDINGS.md"
  - "README.md"
  - "TODO.md"
  - "index.html"
  - "internal/dockerenv/dockerenv.go"
  - "internal/dockerenv/testdata/twofactor/Dockerfile"
  - "internal/dockerenv/testdata/twofactor/entrypoint.sh"
  - "internal/dockerenv/totp.go"
  - "internal/sshx/keys_test.go"
  - "internal/sshx/prompt.go"
  - "internal/sshx/prompt_test.go"
  - "internal/sshx/sshx.go"
  - "internal/sshx/twofactor_docker_test.go"
  - "internal/tui/authprompt.go"
  - "internal/tui/authprompt_test.go"
  - "internal/tui/commands.go"
  - "internal/tui/keys.go"
  - "internal/tui/model.go"
  - "internal/tui/msgs.go"
  - "internal/tui/session.go"
  - "internal/tui/twofactor_docker_test.go"
  - "internal/tui/view.go"
  - "justfile"
---

# Interactive SSH auth (2FA/OTP, passwords) with Docker end-to-end tests

## Goal

Let hop connect to hosts requiring a keyboard-interactive second factor (pam_google_authenticator) or a password, asking the user from inside the SSH handshake — and prove it against a real sshd, not a mock.

## Scope

- hop holds one *ssh.Client per host (session.go openShell reuses s.client); extra shells, SFTP and editors are channels on it. Interactive auth therefore prompts once per host per run, and nothing needs caching or persisting.

- Did NOT add a keyboard-interactive mode to tools/demoserver: it is a NoClientAuth demo prop for the README recording. Non-Docker unit tests start their own in-process ssh.ServerConfig instead.

## Discoveries

- The host-key card's abort-and-replay pattern (openHostKeyConfirm -> ConnectTrusting) CANNOT be reused for interactive auth: a TOTP code is valid ~30s, single-use, and rate-limited to 3 per 30s by default, so a replayed dial spends a code per attempt. The question must be answered inside the live handshake.

- x/crypto/ssh ClientConfig.Timeout bounds only the TCP dial (client.go net.DialTimeout), NOT the handshake - verified in v0.54.0. The 15s dialTimeout therefore does not kill a dial while the user types a code.

- x/crypto/ssh clientAuthenticate does NOT return on the first method error: it surfaces the error only once no untried offered method remains (client_auth.go, 'if auth == nil && err != nil'). Without a sticky cancel, esc on the keyboard-interactive prompt falls straight through to the password method and prompts again.

- ssh.RetryableAuthMethod returns immediately when the inner method errors (it loops only on plain auth failure), so a Prompter error aborts cleanly instead of re-prompting maxTries times.

- sshd has NO option for choosing a PAM service (PAMServiceName does not exist). Portable OpenSSH defines SSHD_PAM_SERVICE as __progname, so serving two PAM stacks from one container means running copies of the binary named after their /etc/pam.d file. Verified on OpenSSH 9.6p1 / Ubuntu 24.04.

- Docker's port proxy binds a published port the moment the container starts and accepts TCP connections whether or not anything inside is listening. A dial-based readiness check passes against an empty container and the first real dial dies with 'ssh: handshake failed: EOF'. Readiness must read the SSH-2.0- banner.

- go test runs each package's tests in a separate process in parallel, so a fixed container name shared by internal/sshx and internal/tui raced ('container is marked for removal and cannot be started'). The name carries os.Getpid().

- In the TUI, openShell returns m.withSpinner(cmd) = a tea.Batch; a test calling cmd() directly gets tea.BatchMsg, not connectedMsg. The harness unwraps batches like the Bubble Tea loop does (dispatch() in tui/twofactor_docker_test.go). A skip guard must also run before any field of the shared server is read - twoFactorModel(t, server.CodePort, ...) evaluates the argument first and nil-panics without Docker.

## Decisions

- **Decision:** Block the dial goroutine on a channel round trip to the model (sshx.Prompter -> uiPrompter -> authPromptMsg -> reply chan) instead of aborting and replaying the dial.
  - **Reason:** A one-time code cannot be replayed; the challenge must be answered inside the handshake.
  - **Trade-off:** The only place a background command asks the UI something and waits, so every path out of the auth card must send exactly once on reply or the connect hangs forever.

- **Decision:** Offer keyboard-interactive unconditionally whenever a Prompter is present, rather than behind a per-host flag or a global setting.
  - **Reason:** User chose this explicitly; it is what plain ssh does and needs no schema migration or settings field.
  - **Trade-off:** On a host where key auth simply fails, the user now gets a password prompt instead of an immediate error.

- **Decision:** Wrap the Prompter in stickyCancel, shared by both interactive methods.
  - **Reason:** The SSH client keeps trying other offered methods after one errors, so one esc would otherwise be answered by a second prompt.
  - **Trade-off:** ErrAuthCanceled is final for the dial; a Prompter cannot cancel one method and answer another.

- **Decision:** Queue a second challenge (model.authQueue) instead of dropping or overdrawing it.
  - **Reason:** Two hosts can dial at once and the dial behind a dropped challenge would block forever in the handshake.

- **Decision:** Leave passphrase-protected key files reported as 'run ssh-add' rather than prompting for the passphrase.
  - **Reason:** keySigners runs before the server is contacted, so it would prompt on every dial even when the key is never needed.

- **Decision:** Serve four ports from one container, one per shape of two-factor login, and put the lifecycle in internal/dockerenv (a normal package, not _test.go).
  - **Reason:** 'The host has 2FA' is at least four different handshakes; port 2225 (keyboard-interactive OR password as alternatives) is the ONLY one that exercises stickyCancel. Both internal/sshx and internal/tui need the same server, and runtime.Caller resolves testdata/twofactor regardless of the importing package's cwd.
  - **Trade-off:** The original cancel test on port 2222 looked like it covered stickyCancel and did not - a mutation run proved it passed with the wrapper removed.

## Failures

- **Approach:** The test helper that records a host key dialled with a nil Prompter to reach the host-key check.
  - **Evidence:** sshx: no usable authentication: $SSH_AUTH_SOCK is not set ..., want *UnknownHostKeyError
  - **Lesson:** authMethods fails BEFORE the dial when there is nothing to offer, so the host key is never exchanged. A helper that only wants the fingerprint still needs a prompter - one that cancels, so it does not spend a code.

## Validation

- go build, go vet, gofmt, go test ./... and go test -race ./... - all packages pass with and without Docker.

- HOP_DOCKER_E2E=1 go test ./... -count=1 - 17 Docker-backed tests (12 in internal/sshx, 5 in internal/tui) against a real Ubuntu sshd + pam_google_authenticator, also green under -race with both packages in parallel, leaving no containers behind.

- Mutation-tested rather than trusting green: authRetries 3->1 failed the retry tests in both packages; removing the stickyCancel wrapper failed TestTwoFactorCancelSkipsRemainingMethods with 'asked 2 times'. Negative controls (wrong code, 10-minute-old code) guard against a container that accepts anything.

- Server side independently verified with the real OpenSSH client via expect on all three original ports before any Go test was written.

## Remaining risks

- Not verified against a hand-configured production host - only against the container, whose PAM stacks omit RATE_LIMIT and DISALLOW_REUSE so a repeated test run does not fail for reasons unrelated to hop.

- Offering password auth unconditionally means a host where key auth fails now shows a password card instead of failing fast. Deliberate, but it changes existing behaviour.

- The image pins nothing ('FROM ubuntu:24.04' + apt-get), and two tests match on the prompt wording 'verification code'/'password'. Tests also assume the host clock is close to the container's.

- The image is not built in CI, so a broken Dockerfile or entrypoint only surfaces when somebody runs 'just test-e2e' locally.

## Handoff

- Passphrase-protected keys still say 'run ssh-add' (internal/sshx/keys.go). Prompting needs a lazy signer that asks only when the key is actually offered - do not wire it into keySigners.

- If the Docker tests move into CI, add a docker-enabled job rather than setting HOP_DOCKER_E2E on the existing matrix - the Windows and macOS runners have no Linux Docker.

- internal/dockerenv is the place for other hard-to-fake servers (a bastion for ProxyJump, a host that drops connections for reconnect handling).
