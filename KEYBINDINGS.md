# hop — keybindings

Every key hop binds, by mode. The reasoning behind a binding lives next to the code
that implements it — mostly `internal/tui/keys.go`.

**Two rules explain most of this:**

- **Inside a pane, `ctrl+o` is hop's leader.** It does nothing on its own and it is
  on no clock — it opens a menu in the footer and waits. `ctrl+o` `o` goes **out**.
- **Outside a pane** — in the browser, in a card — `ctrl+o` simply goes back. There is
  no remote program competing for keys there, so there is nothing to lead.
- **Inside a pane, everything else is the remote's.** hop reserves as few keys as it
  can, because every one it takes is one the shell or editor no longer gets.

> **macOS:** hop's bindings use `ctrl` and `shift`, which every terminal delivers.
> The `alt+…` chords listed below are *aliases* and need one setting — see
> [macOS and the alt keys](#macos-and-the-alt-keys).

---

## Navigation — the host list

| Key | Action |
| --- | --- |
| `down` / `up` | move |
| `pgdn` / `pgup` | a full page down / up |
| `enter` / `right` | connect (opens a terminal pane), or focus the shell already open |
| `esc` / `left` | back — leave the details view |
| `s` | focus the existing session for this host |
| `S` | open **another** shell on this host, alongside the ones already open |
| `f` | open the SFTP browser |
| `t` | start all defined tunnels, or stop them when any are running |
| `T` | manage this host's tunnel definitions |
| `o` | open the host in VS Code Remote, in the directory its shell is standing in |
| `d` | disconnect the session |
| `r` | reconnect a session whose connection dropped, reopening what it held |
| `a` | add a host |
| `e` | edit the host under the cursor |
| `x` | delete the host under the cursor (asks first) |
| `p` | pin the host to the **PINNED** section at the top, or unpin it |
| `shift+k` / `shift+j` | move a pinned host up / down inside that section |
| `i` | import hosts from an OpenSSH config (`~/.ssh/config` by default) |
| `/` | filter hosts (`enter` applies, `esc` clears) |
| `,` | settings |
| `?` | the keys card |
| `ctrl+b` | hide / show the sidebar |
| `ctrl+g` | hand the mouse to your terminal (and take it back) |
| `q` / `ctrl+c` | quit |

| Key | Action |
| --- | --- |
| `1` … `9` | go straight to that shell of the host under the cursor |

With **vim keys** on (`,` → *Vim keys*, off by default): `j`/`k` move, `l` connects
as `enter` does, `h` goes back as `esc` does.

The list binds the **step** keys and nothing more. The jumps and ctrl chords (`gg`,
`G`, `H`/`M`/`L`, `ctrl+d`/`ctrl+u`/`ctrl+f`) belong to the file browser, which walks
directories that run past a screen; the host list does not scroll, so each of them
landed a `j` or two from the cursor while holding a letter the list wants as a command.

## Terminal — a live shell

| Key | Action |
| --- | --- |
| `esc` `esc` | back to hop (two presses within 400 ms) |
| `shift+→` / `shift+←` | next / previous shell on this host (wraps) |
| `ctrl+o` `o` | **out** — back to hop |
| `ctrl+o` `1` … `9` | go straight to that shell, without leaving the pane |
| `ctrl+o` `0` | open **another** shell on this host, without leaving the pane |
| `ctrl+o` `c` | open **this directory** in VS Code Remote |
| `shift+↑` / `shift+pgup` | scroll back into the pane's history |
| `ctrl+b` | hide / show the sidebar — the pane takes the whole window |
| `ctrl+g` | hand the mouse to your terminal (and take it back) |
| `alt+0`, `alt+←/→`, `alt+1…9` | aliases for the above, where your terminal sends them |
| *everything else* | sent to the remote shell |

`←` is **not** a way out: readline needs it, `alt+b`/`alt+f` word motions are built on
it, and every full-screen program navigates with it. `alt+o` is deliberately unbound —
a terminal sends it as `esc` then `o`, which is vim's "leave insert mode, open a line".

### How the leader works

`ctrl+o` inside a pane has **no effect of its own**, and no timeout. It opens the
leader, the footer becomes the menu, and hop waits as long as you take:

| after `ctrl+o` | |
| --- | --- |
| `o` | out — back to hop |
| `1` … `9` | that tab, selected **in place** |
| `0` | another shell on this host |
| `c` | this directory in VS Code Remote |
| anything else | closes the leader and does nothing |

A key that names no chord is **swallowed**, not passed to the remote: while the leader
is open hop has the keyboard, and a program that received the tail of an abandoned
chord would act on a key you were not typing at it. The leader also outranks `ctrl+b`
and `ctrl+g`, which are otherwise held in every mode.

This is the tmux and wezterm arrangement, and the reason for it is worth stating: a
leader that *also acts* forces a timeout, and every value for that timeout is wrong —
too short and the chords are unreachable, too long and leaving feels broken. Earlier
versions of hop tried both and neither worked. Paying one extra keystroke for `out`
buys back all of the timing.

### Scrolling back through history

| Key | Action |
| --- | --- |
| `↑` / `↓`, `j` / `k` | up / down one line |
| `pgup` / `pgdn`, `ctrl+f` | up / down a page (`ctrl+b` is the sidebar) |
| `ctrl+u` / `ctrl+d` | up / down half a page |
| `g` / `home` | jump to the oldest line |
| `G` / `end` | back to the live bottom (and leave scrollback) |
| `esc` / `q` / `enter` / `ctrl+o` / `←` | back to the live shell |
| *anything else* | leave scrollback and type it at the prompt |

Off while a full-screen program owns the screen, and when there is no history yet.

## Browsing — the SFTP file browser

| Key | Action |
| --- | --- |
| `down` / `up` | move |
| `pgdn` / `pgup` | a full page down / up |
| `enter` / `right` | enter a directory, or open a file in an editor tab |
| `o` | open the file in the local OS default app (GUI) |
| `d` | download the file to `~/Downloads` |
| `left` / `backspace` | up one directory |
| `r` | refresh the listing |
| `,` | settings |
| `ctrl+b` | hide / show the sidebar |
| `ctrl+o` | back to hop |
| `esc` `esc` | back to hop (two presses within 400 ms) |
With **vim keys** on the browser keeps the *whole* motion set (the host list only the
step keys): `j`/`k`, `gg`, `G`, `H`/`M`/`L`, `ctrl+d`/`ctrl+u`, `ctrl+f`, plus `l` to
descend and `h` to back out.

`left` at the top of the tree pops back to hop rather than doing nothing.

## Editing — editor tabs

| Key | Action |
| --- | --- |
| `shift+→` / `shift+←` | next / previous tab (wraps) |
| `ctrl+o` `1` … `9` | go straight to that tab, without leaving |
| `ctrl+o` `o` | back to the file browser |
| `:q` (i.e. quit the editor) | close the tab |
| `esc` `esc` | back to the file browser (two presses within 400 ms) |
| `alt+←/→`, `alt+h/l`, `alt+1…9` | aliases, where your terminal sends them |
| *everything else* | sent to the remote editor |

`enter` on a file in the browser runs `${EDITOR:-vi}` **on the host**, on a second SSH
channel with a pty. Nothing is downloaded: `:w` writes the real remote file. To edit
locally instead, use `d` (download) or `o` (open in the local default app).

Jumping to a tab by number is `ctrl+o` then the digit, which selects it in place.
`ctrl+o` `o` goes back to the browser.

## The cards

All of them are modal: while a card is up it takes every key, and `esc` closes it.

**Import** (`i`) — `enter` imports from the path shown, `ctrl+u` clears it, any text
edits it (a leading `~` is expanded).

**Tunnels** (`T`) — `↑`/`↓` select, `enter`/`space` start or stop the selected one,
`a` adds, `e` edits (a running old definition is stopped on save), `x` deletes, `t`
closes the card and starts/stops the whole set.

**Authentication** (2FA / password, opens by itself) — `enter` submits or moves to the
next question, `tab`/`shift+tab` move between fields, `ctrl+u` clears, `esc` cancels
and abandons the connect.

**Host key** (opens by itself) — `y` trusts the fingerprint and retries the dial,
`n`/`esc` trusts nothing.

**Add / edit host** (`a` / `e`) and **delete** (`x`) — `↑`/`↓` or `tab` move between
fields, `enter` saves, `esc` cancels.

## When a connection drops

| Key | Action |
| --- | --- |
| `r` / `enter` | reconnect: dial again and reopen what was open |
| `d` / `x` | drop the session — the pane goes, the host is idle again |
| `ctrl+o` / `esc` / `q` | back to the host list, leaving the pane on screen |

The pane keeps the last screen the host drew, so the command that was running is still
there to read. Shell tabs and the browser's directory come back on reconnect; editor
tabs do not (an editor holds a buffer, and reopening the file on a fresh pty would look
like nothing was lost), and the status says how many were left behind.

## Settings — the popover

`,` opens it from the list, a pane or the browser.

| Key | Action |
| --- | --- |
| `↑` / `↓` (`k` / `j`) | move between settings |
| `←` / `→` (`h` / `l`) | pick a color (accent), or flip a switch |
| `enter` / `i` | edit the selected setting — or flip it, if it is a switch |
| `enter` / `esc` / `ctrl+u` (while editing) | save / cancel / clear |
| `r` | reset the setting to its default |
| `esc` / `q` / `,` | close |

| Setting | What it is | Blank means |
| --- | --- | --- |
| Editor | the command `enter` runs on the remote host, e.g. `nvim`, `vim -R` | auto: remote `$EDITOR`, else probe for nvim/vim/vi/nano |
| Download dir | where `d` puts a file | `~/Downloads` |
| Accent color | picked from the swatch strip with `←`/`→`, or typed | `212`, hop's pink |
| Open with | the local command `o` opens a file with, e.g. `code -n` | the OS default app |
| Vim keys | the vim motions in the list and the browser — a switch | **off** |
| Mouse | wheel, click and drag-to-copy — a switch | **on** (`ctrl+g` lends the pointer back for a moment) |
| Remote clipboard | a yank on the remote host (OSC 52) lands on yours — a switch | **on** |

## The mouse

Every gesture is an existing binding reached by pointing, so nothing is mouse-only.

| Gesture | Where | What it does |
| --- | --- | --- |
| wheel | the host list | moves the selection, one host a notch |
| wheel | a shell pane | pauses into its scrollback, three lines a notch; scrolling back to the live bottom returns to the shell |
| wheel | the SFTP browser | moves the cursor three entries a notch |
| click | the host list | stands on that host — and, from a pane, hands the keyboard back, as `ctrl+o` does |
| click | a pane the list has the keyboard in | takes it: the pointer's `s` or `f` |
| click | a tab strip | switches to that shell or file tab |
| drag | a pane | selects text; it lands on the clipboard when you let go |
| double-click | a host, or a browser entry | opens it — `enter`, by pointing |

A remote program that asks for the mouse (vim with `set mouse=a`, htop) gets the
pointer verbatim instead. The cards are keyboard-only. `ctrl+g` hands mouse reporting
back to your terminal for a moment — for a selection spanning the sidebar and a pane,
or anything else that wants your terminal's own pointer.

## Copy and paste

Paste has no key of its own: **paste the way your terminal pastes**, and hop marks it
as a paste for the remote program — which is what stops vim indenting a pasted block
into a staircase.

Copying *out* of a pane is a drag (above), or your terminal's own selection after
`ctrl+g`. A yank on the remote host travels to your clipboard over OSC 52, unless you
turn *Remote clipboard* off. A remote asking to **read** your clipboard is never
answered.

## macOS and the alt keys

hop's own bindings are `ctrl` and `shift` chords, which every terminal sends — nothing
below is needed to use hop.

The `alt+…` aliases are another matter. On macOS, `Option`+letter types a *character*
(`ø`, `é`, `∑`) instead of sending the ESC-prefixed meta key hop reads, so every
`alt+…` binding is simply absent until the terminal is told otherwise:

- **Terminal.app** — Settings → Profiles → Keyboard → *Use Option as Meta key*
- **iTerm2** — Settings → Profiles → Keys → Left Option key: *Esc+*
- **Ghostty** — `macos-option-as-alt = true`
- **VS Code's terminal** — `"terminal.integrated.macOptionIsMeta": true`

This is also why `shift+k`/`shift+j` reorder pinned hosts, and why the sidebar is
`ctrl+b` and the mouse toggle `ctrl+g` rather than the `alt` mnemonics they would
otherwise be.

## How double-esc works, and what it costs

In a pane, the **first** `esc` is still sent to the remote — it has to be, because a
lone `esc` belongs to the shell (it drops vim out of insert mode) and hop cannot know a
second one is coming without swallowing the first. A second `esc` within 400 ms leaves
the pane; the stray extra one is harmless, since in vim's normal mode it is a no-op.

If that bothers you, `ctrl+o` `o` leaves and sends nothing to the remote at all.

## What hop takes from the remote

The full list, so there are no surprises: `ctrl+o`, `ctrl+b`, `ctrl+g`, `shift+←/→`,
`shift+↑`, `shift+pgup`, and the first `esc` of a double. Everything else reaches the
program on the other end.

Two costs worth naming: a remote `tmux` never sees its own `ctrl+b` prefix through hop,
and `shift+←/→` no longer reaches the remote as a selection motion.

## Where a binding lives

| Mode | Handler |
| --- | --- |
| host list | `handleNavKey` (`internal/tui/keys.go`) |
| shell pane | `handleShellKey` |
| scrollback | `handleScrollbackKey` |
| SFTP browser | `handleBrowserKey` → `filebrowser.Handle` |
| editor tabs | `handleEditorKey` |
| filter | `handleFilterKey` |
| the cards | `handleHelpKey`, `settings.go`, `hostform.go`, `confirm.go`, `importer.go`, `tunnels.go`, `hostkey.go`, `authprompt.go` |
| shared motions | `internal/keymap` (scoped: the list gets the step keys, the browser all of them) |
