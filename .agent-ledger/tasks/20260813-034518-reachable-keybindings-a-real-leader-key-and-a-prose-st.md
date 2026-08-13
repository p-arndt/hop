---
id: "20260813-034518-reachable-keybindings-a-real-leader-key-and-a-prose-st"
title: "Reachable keybindings, a real leader key, and a prose/structure cleanup"
status: "completed"
updated: "2026-08-13T03:46:19+02:00"
base_commit: "1f4ae01fa69d766b3f4f3b38c222e08ed7cc0618"
branch: "main"
agent: null
tags: ["keybindings", "macos", "refactor", "tui"]
files:
  - "KEYBINDINGS.md"
  - "README.md"
  - "TODO.md"
  - "internal/terminal/terminal.go"
  - "internal/tui/editor_test.go"
  - "internal/tui/help.go"
  - "internal/tui/hostkey.go"
  - "internal/tui/hostlist.go"
  - "internal/tui/keys.go"
  - "internal/tui/keys_test.go"
  - "internal/tui/landing.go"
  - "internal/tui/model.go"
  - "internal/tui/mouse.go"
  - "internal/tui/mouse_test.go"
  - "internal/tui/msgs.go"
  - "internal/tui/paste.go"
  - "internal/tui/paste_test.go"
  - "internal/tui/reconnect.go"
  - "internal/tui/scrollback_test.go"
  - "internal/tui/session.go"
  - "internal/tui/sidebar_test.go"
  - "internal/tui/tabkeys_test.go"
  - "internal/tui/tabs.go"
  - "internal/tui/view.go"
  - "internal/tui/view_test.go"
  - "internal/tui/vscode_test.go"
---

# Reachable keybindings, a real leader key, and a prose/structure cleanup

## Goal

Make every hop binding reachable on a stock macOS terminal (alt never arrives there), replace the ctrl+o chord with a leader that has no effect of its own, and cut the comment prose and file sizes the codebase had accumulated.

## Scope

- Keys hop takes from the remote inside a pane, in full: ctrl+o, ctrl+b, ctrl+g, shift+left/right, shift+up, shift+pgup, and the first esc of a double. Two named costs: a remote tmux never sees its own ctrl+b prefix, and shift+arrows no longer reach the remote as a selection motion.

## Discoveries

- Tab switching was unreachable by keyboard on a stock macOS terminal. The only paths to session.activeSh were the alt chords (alt+1..9, alt+arrows) and a mouse click on the tab strip (internal/tui/mouse.go, tabAt). Option+key types a character there rather than sending the meta escape Bubble Tea reads, so the whole feature was absent, not merely awkward.

- There is no free ctrl key left to take inside a pane. Checked the tea.KeyType constants in bubbletea@v1.3.10 key.go against the usual remote bindings: ctrl+t transposes (and is termios STATUS on macOS), ctrl+w is termios WERASE, ctrl+n/ctrl+p walk readline history, ctrl+k kills a line, ctrl+y yanks, ctrl+\\ is SIGQUIT; ctrl+i/ctrl+m/ctrl+[ collide with tab/enter/esc; ctrl+1..9 is not transmitted at all. Only ctrl+^ (0x1e) and ctrl+_ (0x1f) are genuinely unbound, and both are shift combos that sit elsewhere on a German keyboard. Any future 'just take one more key' plan has to start here.

- Pane.Resize (internal/terminal/terminal.go) had no no-op guard, so every focus change sent an SSH window-change down every shell of the host — the callers resize whole sessions at a time. Full-screen programs redraw on one. Now guarded by paneW/paneH, seeded in New from the size the pty was opened at.

- internal/tui/model.go was 852 lines doing four jobs. Split into model.go (struct, Run, Init, Update dispatch, status helpers, 408 lines), landing.go (the async shellLanded/shellExited/browserLanded/editorLanded tails, 206) and hostlist.go (reload, filter, pin order, row model, 237). Pure moves, no logic change. The seven half-typed-input fields (esc/leader/click timings) are now one chordState in keys.go.

## Decisions

- **Decision:** shift+left/right is the primary tab-switch binding; the alt chords stay bound as aliases.
  - **Reason:** shift is delivered by every terminal on every platform, and shift+up was already hop's for scrollback, so shift+arrow was the namespace with a precedent. Keeping alt costs nothing where it does arrive and avoids a second, platform-divergent binding table.
  - **Trade-off:** shift+left/right stops reaching the remote as a selection motion.

- **Decision:** ctrl+o became a leader with no effect of its own and no timeout; leaving a pane is ctrl+o then o.
  - **Reason:** A key that both leads and acts forces a timeout, and every value for it is wrong: short puts the chords out of reach, long makes leaving feel broken. Dropping the self-action removes the clock entirely — the leader waits indefinitely and the footer becomes its menu. This is the tmux/wezterm arrangement and the reason they use it.
  - **Trade-off:** Leaving a pane costs two keystrokes instead of one. esc esc still leaves in one gesture.

- **Decision:** A key that names no chord closes the leader and is swallowed rather than forwarded to the remote.
  - **Reason:** While the leader is open hop owns the keyboard; a program receiving the tail of an abandoned chord would act on a key the user was not typing at it. The footer lists the choices for as long as the leader is open, so a wrong key is visible rather than mysterious.

- **Decision:** Digits 1-9 in the host list focus that shell of the host under the cursor, as an ordinary binding.
  - **Reason:** No chord and no window: in the list a digit has nothing else to be, and it gives a chordless way into a numbered shell. Distinct from the leader's digits, which select a tab in place inside a pane.

## Failures

- **Approach:** Made ctrl+o leave the pane immediately and then treated the next key as a chord (ctrl+o 3 = re-enter at tab 3).
  - **Evidence:** The leader acted before its second key arrived, so every chord flashed out to the host list and back; ctrl+o 0 left the pane and only then opened the new shell.
  - **Lesson:** A leader must not have an effect of its own. Arming and acting on the same keypress cannot be made to look right, no matter where the second half is answered.

- **Approach:** Second attempt: kept ctrl+o acting, but deferred it behind a leaderWindow timer so a digit could resolve it first.
  - **Evidence:** 200ms was too short to reach the second key at all; raising it to 600ms put a visible lag on the most common way out of a pane.
  - **Lesson:** The timeout was a symptom, not the problem. Once ctrl+o stopped acting on its own the clock could be deleted outright — leaderWindow, leaderExpiredMsg, expireLeaderCmd and the generation counter all went with it.

- **Approach:** The Windows paste coalescer swallowed the leader's second key.
  - **Command:** `go test ./internal/tui/ -run TestAnUnbufferableKeyFlushesFirst`
  - **Evidence:** ctrl+o was swallowed by the flush instead of leaving the pane
  - **Lesson:** takeKey (internal/tui/paste.go) runs at the very top of handleKey, before any mode routing, and only checks pasteCoalesce/pastable/cardOpen/forwardingPane. Every leader chord key (o, c, 0-9) is an ordinary pastable rune and the pane is still focused while the leader is open, so the burst buffer held them. Any future in-pane chord built on plain characters needs the same m.leaderArmed() style guard, and it only breaks on Windows — the one platform CI never exercises interactively.

## Validation

- go test ./... — all 16 packages pass (Docker E2E not run: opt-in via HOP_DOCKER_E2E=1)

- go vet ./... and gofmt -l . — clean

- go build ./... — clean

## Remaining risks

- focused/browsing/editing/scrolling in model are documented as mutually exclusive but nothing enforces it — every call site sets the other three by hand. Left alone deliberately: it wants one mode enum plus the mode-switch tests that are still open in TODO.md, which is a behavioural refactor, not a cleanup.

- The leader waits indefinitely and swallows the key that closes it. A stray ctrl+o therefore eats the next keystroke, with only the footer menu to say why. Tolerable (tmux behaves the same) but unverified against real use — worth watching for on a live host.

- Everything here is verified by unit tests against fake panes only; no run against a real host in this session. The Windows paste-coalescer guard in particular is covered by a test that simulates the platform (m.pasteCoalesce = true), never by Windows itself.

## Handoff

- Try ctrl+o on a live host: the leader, out (o), a tab digit, c for VS Code, and confirm the footer menu reads well. Then the two entries added to TODO.md: the footer is overcrowded (always show ?, make the help card context-aware), and the mode flags want one enum.

- Item 3 of the review — whether modernc.org/sqlite is the right store for a list of hosts — is still undiscussed. internal/store/store.go is 783 lines for 34 statements across two tables.
