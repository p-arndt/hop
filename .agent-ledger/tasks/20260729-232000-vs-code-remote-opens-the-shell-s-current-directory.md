---
id: "20260729-232000-vs-code-remote-opens-the-shell-s-current-directory"
title: "VS Code Remote opens the shell's current directory"
status: "completed"
updated: "2026-07-30T00:05:00+02:00"
base_commit: "51904d5032899274dcc4df3949577592339074f4"
branch: "main"
agent: null
tags: ["e2e", "osc7", "review-followup", "shell-integration", "terminal", "vscode"]
files:
  - "KEYBINDINGS.md"
  - "README.md"
  - "TODO.md"
  - "internal/dockerenv/dockerenv.go"
  - "internal/dockerenv/shellhost.go"
  - "internal/dockerenv/testdata/shellhost/Dockerfile"
  - "internal/dockerenv/testdata/shellhost/entrypoint.sh"
  - "internal/sshx/sshx.go"
  - "internal/terminal/cursor_test.go"
  - "internal/terminal/cwd.go"
  - "internal/terminal/cwd_test.go"
  - "internal/terminal/terminal.go"
  - "internal/tui/commands.go"
  - "internal/tui/help.go"
  - "internal/tui/keys.go"
  - "internal/tui/twofactor_docker_test.go"
  - "internal/tui/view.go"
  - "internal/tui/vscode.go"
  - "internal/tui/vscode_docker_test.go"
  - "internal/tui/vscode_test.go"
---

# VS Code Remote opens the shell's current directory

## Goal

The VS Code binding opened the host's default directory; make it open the directory the focused shell is standing in, tracked over OSC 7, with the old no-path open as the fallback. Includes the hardening pass a code review of the first cut prompted: never type into a non-prompt, never erase the host's own output, never shadow vim's `<esc>o` (which cost alt+o its binding — see Decisions), never outlive the pane.

## Scope

- In scope: per-shell cwd tracking, hook injection for bash/zsh, the ctrl+o ctrl+o chord in the pane alongside the existing 'o' in the list, erase of the echoed line, docs. Out of scope: a file-browser-based variant, local VS Code, editors other than VS Code, fish/pwsh integration, a settings toggle for the integration, and the dead-pane keyboard gap the same review found (see Handoff).

## Discoveries

- action.OpenVSCodeRemote(alias, remotePath) already took a path (internal/action/action.go:18); every caller passed "". The work was entirely in learning which path to pass.

- A remote shell's cwd never leaves the shell process. OSC 7 (ESC]7;file://host/dir BEL|ST) is the only convention for it, and it exists only if the shell's prompt emits it — so hop installs the prompt hook itself. The login shell is probed over a second exec channel (new sshx.Client.Output); only bash and zsh get a hook, anything else (fish, sh, pwsh) would answer with a parse error and is left untouched.

- That probe answers what the *account* has, not what is on the other end of the pty: a .bash_profile ending in 'exec tmux attach', or an sshd ForceCommand, both answer /bin/bash. The alt screen is the only cheap tell, so it is what the injection checks.

- charmbracelet/x/vt implements CSI M (delete-line) and CUP — verified empirically before relying on it. That is what makes the echoed hook line removable from hop's own screen after it has run.

- The OSC 7 scan is a separate incremental scanner fed the same bytes as the emulator from the output pump (terminal.go), not a second parse: vt exposes no OSC callback. It must carry state across chunk boundaries, since the network splits sequences anywhere.

- hop's key convention, as the code and KEYBINDINGS.md have it: **plain keys** are commands where hop owns the keyboard (list, browser) and the remote's inside a pane; **ctrl chords** mean "talk to hop, not the program inside it" and work in every mode (ctrl+o back, ctrl+b sidebar) because every terminal sends control bytes unconfigured; **alt chords** are tab selection only (alt+0, alt+1..9, alt+←/→); **esc esc** leaves where a lone esc belongs to the remote. A new user-facing binding belongs in the ctrl namespace unless it selects a tab.

- macOS terminals send Option+letter as a composed character ('ø'), not as ESC+letter, unless the user turns on Option-as-Meta. So hop's whole alt namespace — the shell/editor tab keys — is dead on a default Mac, which is where hop's user actually runs it. KEYBINDINGS.md now documents the per-terminal setting.

- keyToBytes dropped the meta prefix on every alt key: an alt chord hop forwards reached the remote as the bare key. Now split into keyBytes + an ESC prefix when msg.Alt (except KeyEsc). This also fixes readline's alt+b/alt+f word motions in a hop pane, which never worked.

## Decisions

- **Decision:** Track the cwd with OSC 7 + an injected prompt hook, rather than probing /proc/<pid>/cwd or typing pwd into the shell.
  - **Reason:** OSC 7 is the standard, keeps tracking continuous (every prompt reports), and needs no PID handshake; /proc is Linux-only and needs the shell PID, which hop cannot learn without typing anyway.
  - **Trade-off:** Requires typing one line into the user's prompt, and works for bash/zsh only.

- **Decision:** Type nothing at all into a shell that already reports OSC 7, or whose screen is owned by a full-screen program; and prefix the hook with \x15 (kill-line) plus a leading space.
  - **Reason:** A user whose rc-file emits OSC 7 needs no help. The alt-screen guard is what keeps a shell command out of vim/tmux on a host whose login files hand the session to something else. The kill-line keeps a half-typed user line from being submitted together with the hook, and the space keeps it out of history where HISTCONTROL=ignorespace / HIST_IGNORE_SPACE is set.

- **Decision:** Erase the echoed hook line from hop's own emulator (CUP + CSI M), and confirm the span by reading the rows back and matching them against the exact line hop typed (holdsEchoOnly) rather than trusting the row arithmetic.
  - **Reason:** The echo cannot be prevented — the shell's line editor draws what it reads — but the emulator is in hop's process, so the rows are hop's to take back; 'clear' would destroy the login banner, which can carry admin messages. The span is measured *before* the write, so anything the host prints in between (a slow dynamic MOTD is the real case) lands inside it and would otherwise be deleted. Content is the only trustworthy check.
  - **Trade-off:** Spaces are squeezed out of both sides of the comparison, since a wrap can land on one of the hook's own spaces; the echo must begin on the span's first row, so an echo whose head has scrolled off is left alone rather than erased from row 0 (which would have taken the banner with it). A line left on screen is the failure mode, never a wrong-row erase.

- **Decision:** Reach it from a pane with the chord `ctrl+o` `ctrl+o` — the ordinary way out, pressed twice inside doubleEscWindow — and keep 'o' in the host list, both routed through model.openVSCodeAt. alt+o, which the first cut bound, is gone.
  - **Reason:** hop's key convention (see Discoveries) puts hop's own actions on control chords, which every terminal sends unconfigured, and reserves the pane's alt namespace for tab selection. alt+o broke both: it was not tab selection, and on default macOS terminals — where hop's own user runs it — Option+o types 'ø' and never reached hop at all. The first ctrl+o is a keypress the user was making anyway, so the chord costs one extra press and takes nothing from the remote shell.
  - **Trade-off:** A chord rather than a key, and it needs the footer/help card to be discoverable. Users whose Option is already meta lost a single-key path.

- **Decision:** Give Pane a closed channel that Close closes, and have the injection sequence check it after every wait.
  - **Reason:** The sequence spans up to ~8s of waiting; a tab closed inside that window would otherwise type into a dead session and write to a torn-down emulator, which is exactly the race Pane.Close was rewritten to avoid.

- **Decision:** Stub only the launcher in tests (tui.openVSCode var), never the SSH/shell/emulator path.
  - **Reason:** There is no VS Code on CI and the path handed to it is the whole feature, so the launch is the one honest seam; everything upstream is exercised against a real sshd in Docker.

## Failures

- **Approach:** A single shell-detecting hook line (if ZSH_VERSION ... elif BASH_VERSION ...) was ~340 chars and wrapped to four visible lines in the pane, which the user rejected on sight.
  - **Lesson:** The hook is typed at a real prompt and echoed there, so its length is a user-visible cost. Since TrackCwd already probes the login shell, send the per-shell one-liner (~105-135 chars) — and a test now caps it at 160 bytes.

- **Approach:** Measuring the screen a fixed 150ms after the cwd report arrived left the echo on screen for zsh under -race.
  - **Command:** `HOP_DOCKER_E2E=1 go test ./internal/tui/ -race -run VSCodeE2E`
  - **Evidence:** the hook hop typed is still on screen: ... precmd_functions+=(hop_cwd); hop_cwd
  - **Lesson:** The hook's own trailing call emits OSC 7 *before* the shell prints the newline and the next prompt, so the report is not the tell that the screen is ready. Wait for the cursor to move below the start row and hold still (waitPromptBelow), not for a fixed delay.

- **Approach:** The e2e first asserted the pane was clean the instant the cwd report arrived, and failed intermittently.
  - **Lesson:** The erase lands a beat after the report by design; the assertion has to poll (waitForCleanPane), not sample once.

## Validation

- go build ./... , go vet ./... , gofmt — clean.

- go test ./... -race — all packages pass. New: internal/terminal OSC 7 scanner (shapes, chunk splits, payload cap, interrupted OSC), TrackCwd against a probe server (hook only for bash/zsh, nothing onto the alt screen, stops when the pane closes), eraseEcho (erases the echo; declines when the host printed into the span, with no echo, on bad geometry), keyToBytes alt-prefix; internal/tui chord (fires on the second press, not the first, not a late one, not a spent arm), alt+o-is-not-hop's, fallback, dead-session and footer tests.

- HOP_DOCKER_E2E=1 go test ./internal/... -race -timeout 25m — passes, including TestVSCodeE2EOpensTheShellsDirectory (real bash + zsh: hook installs, /home/<user> then a cd into a path with a space is reported, the chord hands VS Code exactly that path, echoed line erased) and TestVSCodeE2EFallsBackOnAnUnhookableShell (fish: nothing typed, cwd stays unknown, binding opens with no path). Run -count=3 under -race, so the content-checked erase is confirmed to still fire against real prompts; no containers left behind.

## Remaining risks

- Two review findings accepted rather than fixed: the emulator write in eraseEcho can in principle interleave with the output pump mid-CSI (cosmetic, and the erase only runs after ~90ms of a quiet screen), and parseOSC7 always percent-decodes, so a directory literally named /srv/my%20app is reported wrong (inherent to OSC 7 — hop's own hook emits $PWD raw).

- A user's rc-file that overwrites PROMPT_COMMAND after hop's hook lands would silently stop the reporting; the binding then falls back to the default-directory open.

- The hook is submitted at the first prompt; a user typing in that first ~500ms has their partial line killed by the leading \x15.

- sshx.Client.Output now has a 10s deadline; no test covers it (a real test would cost 10s per run).

## Handoff

- internal/dockerenv grew a second server: StartShellHost (shellhost.go + testdata/shellhost) with one account per login shell — bashy /bin/bash, zshy /usr/bin/zsh, fishy /usr/bin/fish — sharing a generated client key on one loopback sshd. Its helpers (buildDir/publishedPort/waitForSSH) now take the container name so both servers share them. zsh needs an empty ~/.zshrc in the image or its first-run wizard takes the screen and no prompt is ever drawn. The container is started lazily from internal/tui (shellHostServer) and stopped from the package's existing TestMain.

- Bubble Tea v1 turns ESC followed by a rune arriving in one read into alt+<rune> (key.go:656-684). So an in-pane alt+<letter> binding shadows the vim idiom <esc><letter> — <esc>o above all — intermittently, depending on typing speed. That was one of the two reasons alt+o was dropped; alt+0 and alt+1..9 carry the same (rarer) exposure and are still bound.

- Live-verify on Patrick's real hosts (the ones with a custom PS1/starship prompt) that the hook lands, the echo is erased, and the chord opens the right folder. If starship/oh-my-zsh setups already emit OSC 7, injection should be skipped automatically — worth confirming.

- Out of scope but flagged by the same review: handleDeadPaneKey swallows shift+↑/shift+pgup and the alt tab chords, so a frozen pane cannot be scrolled and only the focused tab can be read — worth fixing where the reconnect feature lives.

- Possible follow-ons: a settings toggle for the shell integration, and using the tracked cwd for other actions (open the SFTP browser there, or start an extra shell in the same directory).
