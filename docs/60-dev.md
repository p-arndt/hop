---
id: dev
title: Development
group: Project
---

```bash
just            # list recipes
just run list   # go run . list
just build      # dev binary
just test       # go test ./...
just test-e2e   # + the Docker 2FA end-to-end tests (needs Docker)
just vet
just fmt        # gofmt -w .
just ci         # fmt-check + vet + test (what CI runs)
just docs       # regenerate index.html, README.md and KEYBINDINGS.md from docs/
just demo       # re-record assets/demo.gif + the stills (needs vhs)
```

:::details How the docs, the demo, the tests and the release are put together
**The docs.** `docs/*.md` is the only source for the website, the README and the keybinding
reference; `tools/docsgen` renders all three, and `just ci` fails if a generated file is out
of date. Sections carry frontmatter saying where they belong, and a handful of fenced
directives (`:::cards`, `:::why`, `:::figure`) cover the layouts markdown has no syntax for —
each with a plain-markdown lowering, so the same section reads well on GitHub.

**The justfile** is deliberately universal: recipe bodies are plain commands that run under
both `sh` and PowerShell, and the two that need real shell logic (`fmt-check`, `clean`) are
split with `[unix]` / `[windows]` attributes.

**The demo.** `just demo` records the GIF and the stills on this page. `scripts/demo.mjs`
builds hop, points `HOME` at a throwaway directory with a seeded host database, and starts
`tools/demoserver` — a loopback-only SSH server that invents everything on screen: a fake
shell with a table of canned command output, an in-memory filesystem over SFTP, and a fake
vi. The keypress overlay in the corner is hop's own, compiled in only under `-tags hopdemo`
(`internal/tui/keycast.go`), so a released binary does not carry it.

**Testing.** Headless tests drive the real Bubble Tea model with real keystrokes against
in-process Go SSH/SFTP servers and temp-file stores — see `internal/tui/hostmgmt_test.go`,
`TestEmbeddedRoundTrip`, `TestSFTPRoundTrip`. CI runs vet + test + build on a Windows /
Linux / macOS matrix, because the agent transport and the local-open handler are
per-platform: a single-OS run cannot tell whether the others still compile.

**The 2FA end-to-end tests.** An in-process server that answers whatever you tell it to
proves nothing about real two-factor auth, so `internal/dockerenv` brings up Ubuntu with the
real `openssh-server` and `libpam-google-authenticator`, listening four ways: code alone,
hardened `publickey,keyboard-interactive`, password-then-code, and both offered as
alternatives. The tests compute TOTP codes the way a phone does and log in, with wrong and
expired codes as negative controls. Opt in with `just test-e2e`; without `HOP_DOCKER_E2E=1`
they skip.

**Releasing.**

```bash
just release          # patch bump: stamps VERSION, commits, tags, pushes
just release minor    # or major, or an explicit 1.0.0
```

The tag push triggers the release workflow: it gates on the three-OS test matrix, then
cross-compiles all six targets (windows/linux/darwin × amd64/arm64) from one Linux runner,
with checksums and a git-cliff changelog. Windows gets a `.zip`, everything else a `.tar.gz`
so the exec bit survives.
:::
