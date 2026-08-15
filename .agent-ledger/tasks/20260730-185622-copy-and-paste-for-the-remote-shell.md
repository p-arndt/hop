---
id: "20260730-185622-copy-and-paste-for-the-remote-shell"
title: "Copy and paste for the remote shell"
status: "completed"
updated: "2026-07-30T18:57:23+02:00"
base_commit: "521faba481b76420502b79a2641dd73fdc20c48d"
branch: "main"
agent: null
tags: ["clipboard", "paste", "terminal", "windows"]
files:
  - "KEYBINDINGS.md"
  - "README.md"
  - "TODO.md"
  - "go.mod"
  - "internal/clipboard/clipboard.go"
  - "internal/clipboard/clipboard_darwin.go"
  - "internal/clipboard/clipboard_unix.go"
  - "internal/clipboard/clipboard_windows.go"
  - "internal/clipboard/exec.go"
  - "internal/config/config.go"
  - "internal/config/config_test.go"
  - "internal/terminal/clipboard.go"
  - "internal/terminal/clipboard_test.go"
  - "internal/terminal/cwd.go"
  - "internal/terminal/mouse_test.go"
  - "internal/terminal/paste.go"
  - "internal/terminal/paste_test.go"
  - "internal/terminal/terminal.go"
  - "internal/tui/clipboard.go"
  - "internal/tui/clipboard_test.go"
  - "internal/tui/hostform.go"
  - "internal/tui/keys.go"
  - "internal/tui/model.go"
  - "internal/tui/paste.go"
  - "internal/tui/paste_test.go"
  - "internal/tui/reconnect.go"
  - "internal/tui/settings.go"
  - "internal/tui/settings_test.go"
---

# Copy and paste for the remote shell

## Goal

Make a paste arrive at the remote program marked as a paste rather than as typing (the report: a large paste into vim on Windows destroyed the formatting), and give a remote yank a way onto the local clipboard.

## Scope

## Discoveries

- Bubble Tea v1 does NOT deliver a marked paste on Windows. key_windows.go/inputreader_windows.go read console *input records* (coninput), so a paste is synthesised key-down events — one per character, KeyMsg.Paste never set, pasted newlines arriving as VK_RETURN -> KeyEnter -> "\r". Paste:true only exists on the ANSI reader path (macOS/Linux). Neither Windows Terminal's ctrl+shift+v/ctrl+v nor a right-click in conhost can be bound: the terminal handles the chord itself and only the characters reach hop, so no keybinding can fix this — the burst has to be recognised by its shape.

- Before this, nothing in hop knew about pastes at all: keys.go forwarded every key through Pane.SendKey/keyToBytes. macOS only *looked* right because the whole clipboard arrived as one KeyRunes event and was written in one Stdin.Write — the ESC[200~/ESC[201~ brackets were still never sent, so a remote vim never entered paste-safe mode there either.

- vt's isModeSet is unexported and its mode map is written by the output pump, so DECSET 2004 is shadowed through emu.SetCallbacks(EnableMode/DisableMode) exactly as mouse.go shadows the mouse modes — and cleared on the RIS that oscScanner watches for, since a full reset rewrites the mode map without firing a callback.

## Decisions

- **Decision:** Paste gets no keybinding of its own on any platform; Windows is served by burst coalescing instead.
  - **Reason:** Windows Terminal binds both ctrl+v and ctrl+shift+v itself, so a hop chord would never see them, and every Windows console paste (WT, conhost right-click) arrives as the same synthesised keystroke burst — one mechanism covers all of them. Stealing ctrl+v would also cost vim's blockwise-visual and readline's literal-next inside every pane for no gain.
  - **Trade-off:** Coalescing holds pastable keys 8ms before sending. A burst counts as a paste when it carries a newline, or when it runs to pasteRun (4) characters that are not all the same — the review round tightened this from "two distinct characters", which turned a fast digraph into a paste and had vim *insert* a quickly-typed `dw` instead of deleting a word. Being wrong the other way is cheap: a short paste replayed as keystrokes types exactly what was pasted.

- **Decision:** The unbracketed and bracketed payloads are sanitised differently (pasteText).
  - **Reason:** Unbracketed, every byte is a keystroke to whatever is reading, so ESC and the other C0 controls are dropped — an ESC mid-paste is what drops vim out of insert. Bracketed, the far end knows it is a paste and will not act on it, so a file's own escapes go through and only ESC[200~/ESC[201~ are stripped, which is the only way a paste could pretend to have ended.
  - **Trade-off:** Unbracketed pastes lose ANSI colour codes; that is the price of not letting a paste run commands.

- **Decision:** A remote OSC 52 clipboard *read* (payload "?") is recognised and never answered.
  - **Reason:** Answering would put the local clipboard on the wire on the remote's say-so. Writes are a setting (cfg.Clipboard, default on, ',' -> Remote clipboard); reads are not offered at all.
  - **Trade-off:** Remote tools that expect a round-trip clipboard get silence.

## Failures

- **Approach:** Building a []uint16 over the address GlobalLock returned failed GOOS=windows go vet.
  - **Command:** `GOOS=windows go vet ./internal/clipboard/`
  - **Evidence:** clipboard_windows.go:126:30: possible misuse of unsafe.Pointer
  - **Lesson:** A uintptr an API returned must not be converted back to unsafe.Pointer. Copy into the locked allocation with RtlMoveMemory.Call(dst, uintptr(unsafe.Pointer(&src[0])), size) instead — the conversion inside the call expression is the pattern the runtime and vet both accept. CI vets on the windows matrix, so this would have been a red build.

- **Approach:** Adding a seventh settings field (Remote clipboard) broke TestSettingsCardFitsTheWindow.
  - **Command:** `go test ./internal/tui/`
  - **Evidence:** the packed card needs 25 rows; it must fit a standard 24-row terminal
  - **Lesson:** The card had already spent its slack: packed spacing was the last thing it could drop. The fix is settingsWindow() — below settingsPackedH the field list scrolls inside the card, centred on the cursor, with settingsMinFields=3 as the floor. settingsMinH is now a function of that floor rather than of how many settings exist, so an eighth field costs nothing.

The four findings of the `/code-review medium` round, all fixed in this entry's work (it is the same task):

- **Approach:** Stripping the bracketed-paste terminator with a single strings.ReplaceAll.
  - **Evidence:** pasteText("safe\x1b[2\x1b[201~01~rm -rf /", true) returned "safe\x1b[201~rm -rf /"
  - **Lesson:** Removing an inner occurrence splices a fresh one out of its neighbours, so one pass *creates* the sequence it was supposed to remove — and the paste ends early, with the rest read as keystrokes. stripAll repeats until the string stops changing (it terminates: every pass that changes anything shortens it). Any "remove this substring for safety" is the same trap.

- **Approach:** Gating the Windows burst buffer on the pane state alone (m.focused/m.editing).
  - **Evidence:** with m.auth.open and pasteCoalesce, typing 1,2,3 left auth.answers[0] == "" and wrote "123" to the shell pane's stdin
  - **Lesson:** The buffer is answered *above* the modal switch in handleKey, so it needs the modal check itself — m.cardOpen(). The 2FA challenge and the host-key card open asynchronously from a dial, so a card can appear while the keyboard is in another host's shell; without the guard a verification code goes to that shell.

- **Approach:** `go sink(text)` per OSC 52.
  - **Lesson:** The far end decides how often that runs, and on Unix each call spawns a clipboard helper. A remote in a loop had hop forking as fast as it could read, with the writes racing so the *older* text could win. Now one worker with a one-deep mailbox: latest wins, which is what a clipboard means.

- **Approach:** Adding msg.Paste guards handler by handler.
  - **Lesson:** Six handlers got one and eight did not, so a paste of "q" in the host list still quit hop and a pasted password kept its trailing newline in the 2FA card. A paste is a different *kind* of event from a key, not a special case inside each mode, so it is now dispatched once in handlePaste — which mirrors handleKey's order and is the only place that has to stay in step with it.

## Validation

- just ci (gofmt -l, go vet ./..., go test ./...) — passed

- go test -race ./... — passed

- GOOS=windows / GOOS=linux go build ./... and go vet ./... — passed (the Win32 clipboard and the coalescer are cross-compiled and vetted, not run)

## Remaining risks

- The Windows half is unrun on Windows: the burst coalescer and the Win32 clipboard writer are covered by unit tests and cross-vet only. Worth trying against a real box — paste a multi-line block into vim, hold j in normal mode, and yank with a clipboard-enabled remote vim.

- pasteGap is 8ms. A console or terminal that delivers a synthesised paste slower than that would have the burst split into several pastes (correct, just not one block); typing faster than that is not possible, so the other direction is not a live risk.

- cfg.Clipboard defaults to on, so any program on a host you connect to can write your local clipboard over OSC 52 (writes only — reads are never answered). ',' -> Remote clipboard -> off closes it, live, on panes already open.

## Handoff

- Ownership: internal/terminal/paste.go = the mode shadow + SendPaste/pasteText (what goes on the wire). internal/tui/paste.go = where a paste comes from (msg.Paste routing lives in keys.go's handlers; the Windows coalescer lives here). internal/terminal/clipboard.go = OSC 52 decode + the sink; the OSC scanner itself is still cwd.go, which now carries a second, larger payload cap for 52. internal/tui/clipboard.go = the sink hop installs (armClipboard, called from shellLanded/editorLanded) and the cfg gate. internal/clipboard = the only code in hop that touches the local clipboard.

- A paste is answered in one place, handlePaste (internal/tui/paste.go), above every mode — a clipboard holding "q", "r" or "esc" would otherwise be read as the binding, because every handler reads the key's *name*. handlePaste mirrors handleKey's dispatch order, so a new mode added to one has to be added to the other; text fields take pasteInline (first line, controls dropped), and views with nothing to type into drop the paste rather than guessing.

- internal/terminal/paste_test.go and internal/tui/paste_test.go are where the behaviour is pinned; internal/tui/clipboard_test.go replaces the clipboard writer via model.clipWrite so tests never touch the developer's clipboard.
