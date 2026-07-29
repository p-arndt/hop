# hop — keybindings

hop has three input modes. The footer always shows the keys for the mode you are
in, and every mode returns to navigation with `ctrl+o` (from a terminal pane or
the file browser, `esc` `esc` also works).

| Mode | You are here when | Who owns your keystrokes |
| --- | --- | --- |
| **Navigation** | the host list is focused (the default) | hop |
| **Browsing** | you opened the SFTP browser with `f` | hop |
| **Terminal** | you connected to a host with `enter` or `s` | the **remote shell** |

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
| `o` | open the host in VS Code Remote, in the directory its shell is standing in |
| `d` | disconnect the session |
| `r` | reconnect a session whose connection dropped, reopening what it held |
| `a` | add a host |
| `e` | edit the host under the cursor |
| `x` | delete the host under the cursor (asks first) |
| `i` | import hosts from an OpenSSH config (`~/.ssh/config` by default) |
| `/` | filter hosts (`enter` applies, `esc` clears) |
| `,` | settings |
| `?` | the keys card |
| `ctrl+b` | hide / show the sidebar |
| `q` / `ctrl+c` | quit |

With **vim keys** turned on (`,` → *Vim keys*, off by default), these are bound as
well:

| Key | Action |
| --- | --- |
| `j` / `k` | move down / up |
| `l` | connect — as `enter` does |
| `h` | back — as `esc` does |

Forward and back then use the same keys as the browser: `enter`/`l`/`right`
descends into the thing under the cursor, `h`/`left` backs out of it.

The list binds the **step** keys and nothing more. The jumps and the ctrl chords —
`gg`, `G`, `H`/`M`/`L`, `ctrl+d`/`ctrl+u`/`ctrl+f` — belong to the file browser,
which walks directories that actually run past a screen. The host list does not
scroll (every host is on screen), so each of them landed a `j` or two from where
the cursor already was, and those letters are worth more to the list as commands.
Paging is `pgdn`/`pgup`.

## The sidebar — `ctrl+b`

`ctrl+b` hides the host list and gives the whole window to the pane; `ctrl+b` again
brings it back. It is bound in **every** mode except while a card is up — from a
focused shell, from the browser, from an editor tab — because the moment you want
the columns is the moment you are reading something wide on the far side of them.
The terminals reflow to the new width immediately, both ways.

Two consequences worth knowing:

- It resets on restart. hop opens on its host list, which is where you start from,
  so the collapse is a session thing rather than a setting.
- hop holds `ctrl+b` in a shell pane, so a **remote tmux never sees its prefix**.
  That is the usual deal between a multiplexer and the one above it — `ctrl+o`
  still leaves the pane, and no other key is taken. It is also why `ctrl+b` is no
  longer bound as a page-up anywhere: paging back is `pgup`.

## Import — the SSH config card

`i` opens the import card, a modal like the rest: one field, pre-filled with
`~/.ssh/config`, so the usual answer is a single `enter`.

| Key | Action |
| --- | --- |
| `enter` | import from the path shown |
| `esc` | close, importing nothing |
| `ctrl+u` | clear the path |
| `backspace` / any text | edit the path (a leading `~` is expanded) |

The **first run** opens this card by itself: with no hosts yet and an
`~/.ssh/config` on disk, hop offers the import instead of showing an empty list and
telling you to go back to the shell for `hop import`. `esc` skips it — an empty list
is not an error, and `a` adds a host by hand.

It stays bound once the list is full, because importing is a **sync**, not a
one-time step: each host is upserted, so a re-import refreshes what the config
knows (hostname, user, port, identity file) and leaves hosts hop added itself
untouched. Wildcard patterns (`Host *`) are skipped.

`hop import [path]` on the command line does exactly the same thing.

## Authentication — the 2FA / password card

Some hosts want more than your key. A server running `pam_google_authenticator`
asks for a verification code; one with `PasswordAuthentication yes` asks for a
password. Either way hop shows a card with the server's own prompt on it, at the
moment the server asks.

| Key | Action |
| --- | --- |
| `enter` | submit — or, on a round with several questions, move to the next one |
| `esc` | cancel; the connect is abandoned |
| `tab` / `shift+tab` | move between fields when the server asked more than one thing |
| `ctrl+u` | clear the field |
| `backspace` / any text | type the answer |

What you type is **masked** unless the server says it may be echoed, which for a
code or a password it does not.

This card is more modal than the others: a dial is parked *inside the SSH
handshake* waiting for it, so it takes every key and it always answers — submit or
cancel. That is also why it cannot work the way the host-key card does, closing
the connection and dialling again with your answer: a one-time code is valid for
about thirty seconds, cannot be used twice, and PAM rate-limits attempts, so a
replayed dial would burn a code every time. The question is answered in place
instead.

A wrong code re-prompts on the same connection rather than failing the dial (three
attempts, which is what the server's own rate limit allows). Cancelling once ends
the attempt outright — it does not move you on to the next method the server
offers.

Nothing is stored, and nothing needs to be: hop holds **one connection per host**,
and every extra shell (`S`), the SFTP browser (`f`) and every editor tab are
channels on it. You are asked once per host, per hop run.

Two hosts connecting at once each get their turn — the second card comes up when
the first is answered.

## When a connection drops — the reconnect keys

Links go down: a laptop suspends, a VPN drops, a server reboots under you. hop
notices, says so, and keeps the pane.

The session is marked **dead** rather than closed. Its dot in the host list turns
red, the details card reads *connection lost*, and the pane keeps the last screen
the host drew — the command that was running, whatever the server printed on its way
out — under a banner naming the reason. Nothing is torn down, because the useful
thing to do next is read that screen and then get back on the host.

A dead pane forwards nothing to the far end (there is nothing there), so it has its
own small keyboard:

| Key | Action |
| --- | --- |
| `r` / `enter` | reconnect: dial again and reopen what was open |
| `d` / `x` | drop the session — the pane goes, the host is idle again |
| `ctrl+o` / `esc` / `q` | back to the host list, leaving the pane on screen |

`r` is bound in the host list too (as are `enter`, `s`, `S` and `f` on a dropped
host, all of which mean "get me back on this one"), because a drop you notice by the
red dot in the sidebar is as likely as one you notice by the pane going still.

**What comes back.** A reconnect is a fresh connection, so the *processes* are gone
for good — what is restored is the shape of the session: as many shell tabs as you
had, and the SFTP browser in the directory it was standing in. Whichever half you
were in when the link dropped is dialed first, so you come back where you were.
Editor tabs are **not** reopened: an editor holds a buffer, and quietly reopening the
file on a fresh pty would look like nothing had been lost. The status line says how
many were left behind.

**How the drop is detected.** Two ways, whichever gets there first. A connection that
is closed or reset ends the SSH transport, which hop is watching. A connection that
is merely *blackholed* — the suspended laptop — ends nothing at all, so hop also
sends OpenSSH's keepalive probe every 15 seconds and gives up on the connection after
three go unanswered, the same shape as `ServerAliveInterval` / `ServerAliveCountMax`
in plain ssh. Without the probe such a session simply stops updating forever.

## Settings — the popover

`,` opens the settings card, floating over whatever is on screen (it works from the
host list and from the file browser; a terminal pane and an editor own every key,
so it is not reachable from those). It is modal: while it is up, keys go to it.

| Key | Action |
| --- | --- |
| `↑` / `↓` (`k` / `j`) | move between settings |
| `←` / `→` (`h` / `l`) | pick a color (accent), or flip a switch (vim keys) |
| `enter` / `i` | edit the selected setting — or flip it, if it is a switch |
| `enter` (while editing) | save |
| `esc` (while editing) | cancel the edit |
| `ctrl+u` (while editing) | clear the value |
| `r` | reset the setting to its default |
| `esc` / `q` / `,` | close |

The popover honours the vim setting like everything else — with it off, `hjkl` do
nothing here either. Turning the keys off from this very card cannot strand you: the
arrows and `enter` drive every row and are never gated, and they are what the hint
line at the foot of the card names. While you are *typing* a value, the gate is off:
`h` is then a letter of the value, not a motion.

The accent is a **swatch picker**, not a number to be looked up: `←`/`→` walk a
palette of twelve colors — pink, magenta, red, orange, yellow, green, teal, cyan,
blue, indigo, purple, gray — each drawn in the color it actually is, and each
applied to hop the instant you land on it. Nothing to confirm; you judge a color by
seeing it. `enter` still opens text entry if you want a specific 256-code or a
`#hex`, and a value that isn't in the palette gets its own swatch on the strip, so
what's in force is always on screen.

Settings are written to `%AppData%\hop\config.json` (next to `hop.db`) the moment
you save one, and applied on the spot — a new accent recolours hop immediately.
The file is plain JSON and can be edited by hand; if it is missing or malformed,
hop starts on defaults rather than refusing to start.

| Setting | What it is | Blank means |
| --- | --- | --- |
| Editor | the command `enter` runs on the remote host, e.g. `nvim`, `vim -R` | auto: remote `$EDITOR`, else probe for nvim/vim/vi/nano |
| Download dir | where `d` puts a file | `~/Downloads` |
| Accent color | picked from the swatch strip with `←`/`→`, or typed | `212`, hop's pink |
| Open with | the local command `o` opens a file with, e.g. `code -n` | the OS default app |
| Vim keys | the vim motions in the list and the browser — a switch, `←`/`→` or `enter` | **off** — the arrows, `enter` and `esc` are all of navigation |

**Vim keys are opt-in.** They are a dozen plain letters, and hop holding `h` and `l`
for "out of" and "into a host" is a surprise to anyone who did not ask for it — so
it asks. Turn the switch on and the motions below appear in the host list, in the
file browser, and in the keys card (`?`), which always lists the keyboard you
actually have rather than the one hop could give you.

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

With **vim keys** turned on — the browser keeps the *whole* motion set, the host
list only the step keys:

| Key | Action |
| --- | --- |
| `j` / `k` | move down / up |
| `gg` | jump to first entry |
| `G` | jump to last entry |
| `H` / `M` / `L` | jump to top / middle / bottom **of the visible window** |
| `ctrl+d` / `ctrl+u` | half a page down / up |
| `ctrl+f` | a full page down (`pgup` pages back — `ctrl+b` is the sidebar) |
| `l` | enter a directory / open a file — as `enter` does |
| `h` | up one directory — as `left` does |

## Editing — editor tabs

`enter` on a file opens it in an editor **inside hop**, in the same right-hand pane
the browser lives in, with a tab strip above it listing every open file.

| Key | Action |
| --- | --- |
| `alt+→` / `alt+l` | next tab (wraps) |
| `alt+←` / `alt+h` | previous tab (wraps) |
| `alt+1` … `alt+9` | jump straight to that tab |
| `:q` (i.e. quit the editor) | close the tab |
| `ctrl+o` | back to the file browser |
| `esc` `esc` | back to the file browser (two presses within 400 ms) |

Every other key goes to the editor — it is a full-screen terminal program and owns
its own keymap. Only alt combinations are reserved, because neither vim nor nano
binds them.

### How it works

The editor runs **on the remote host**, not locally: hop opens a second SSH channel
on the connection it already has and runs `${EDITOR:-vi} <file>` on a pty, then
renders that pty in a pane exactly as it renders a remote shell. So there is no
download and no copy — you are editing the real file, and `:w` writes straight back
to the server.

If the remote `$EDITOR` is unset (it usually is over SSH, since the rc-file that
sets it is never sourced for a non-interactive command), hop probes the remote
`PATH` for `nvim`, `vim`, `vi`, then `nano`, falling back to `vi` — POSIX requires
it to exist.

Tabs are independent editor processes, so leaving with `ctrl+o` keeps them all
running: come back and every file is where you left it, cursor included. A tab is
closed by quitting its editor.

### Editing locally instead

`o` is the escape hatch for files a terminal editor is no good for — a PDF, an
image. It downloads the file to a scratch directory under the system temp dir and
hands that copy to the desktop (`start` on Windows, `open` on macOS, `xdg-open`
elsewhere), returning immediately so hop stays usable. Unlike `enter`, this edits a
*local copy*: nothing is written back to the remote host.

### Leaving the browser

`left` is pure motion: it walks up the directory tree and stops at `/`. It never
leaves the browser, because the directory you open in is usually your home
directory — so a `left` there would drop you back to hop exactly when you meant
to go up to `/home`.

Leaving is always explicit, with the same two chords the terminal pane uses:
`ctrl+o`, or `esc` `esc` within 400 ms. Unlike in a pane, a lone `esc` here is
swallowed rather than forwarded: the browser has no use for it.

## Terminal — a live shell on a remote host

| Key | Action |
| --- | --- |
| `ctrl+o` | back to hop |
| `esc` `esc` | back to hop (two presses within 400 ms) |
| `alt+0` | open **another** shell on this host, and go to it |
| `alt+→` / `alt+←` | next / previous shell on this host (wraps) |
| `alt+1` … `alt+9` | jump straight to that shell |
| `ctrl+o` `ctrl+o` | open **this directory** in VS Code Remote |
| `shift+↑` / `shift+pgup` | scroll back into the pane's history (see below) |
| `ctrl+b` | hide / show the sidebar — the pane takes the whole window |
| *everything else* | sent to the remote shell |

### VS Code Remote, where you actually are — `ctrl+o` `ctrl+o`

`code --remote ssh-remote+host` on its own lands in whatever directory the host
logs you into, which is rarely the one you were working in. `ctrl+o` `ctrl+o` from a
shell pane (or `o` in the host list, which asks that host's shell the same question)
opens VS Code on the directory the shell is standing in — `cd` somewhere, `ctrl+o` out
of the pane, `ctrl+o` again, and the editor opens there.

It is a chord rather than a key because of the two rules above it: the remote shell
owns every plain key, and hop's own bindings are control chords — the only thing every
terminal sends without being configured to. The first `ctrl+o` is the ordinary way out
of a pane, so the chord costs nothing that was not already pressed; the second one has
to arrive within 400 ms, the same window as `esc` `esc`, and on its own does nothing.

`alt+o` is deliberately *not* bound. The alt namespace in a pane is tab selection, and
a terminal sends `alt+o` as `esc` then `o` — vim's "leave insert mode, open a line
below" — so it belongs to the program in the pane.

hop learns the directory the way every terminal emulator does: **OSC 7**, an escape
sequence the shell emits from its prompt hook carrying its cwd. Most shells send
none by default, so hop installs the hook itself — one line, typed into the prompt
once when the shell opens, and then deleted from the pane again as soon as it has
run: the emulator is in hop's own process, so the rows the shell echoed it into are
hop's to take back. What you are left looking at is the login banner and a prompt.

The hook is only sent to **bash** and **zsh** — anything else would answer it with
a parse error — and not at all to a shell that already emits OSC 7 because your own
rc-file does it, nor while a full-screen program owns the screen. That last one
matters on a host whose login files end in something like `exec tmux attach`, or an
sshd with a `ForceCommand`: the account's shell is bash, but bash is not what is on
the other end, and a shell command typed into vim would edit a file. Behind a
full-screen program the directory simply stays unknown.

Erasing is equally cautious: hop reads the rows back before deleting them, and if
anything but its own line is on them — a slow dynamic MOTD still printing, a
background job's output — the line is left where it is. A visible line is a blemish;
deleting the host's own output would be a defect.

### macOS: the `alt` keys need one setting

On macOS, `Option`+letter types a *character* (`ø`, `é`, `∑`) instead of sending the
ESC-prefixed meta key hop reads — so **every** `alt+…` binding above (`alt+0`,
`alt+1…9`, `alt+←/→`) does nothing until the terminal is told otherwise:

- **Terminal.app** — Settings → Profiles → Keyboard → *Use Option as Meta key*
- **iTerm2** — Settings → Profiles → Keys → Left Option key: *Esc+*
- **Ghostty** — `macos-option-as-alt = true`
- **WezTerm** — `send_composed_key_when_left_alt_is_pressed = false`
- **VS Code's terminal** — `"terminal.integrated.macOptionIsMeta": true`

Nothing hop *reserves* needs this — `ctrl+o`, `ctrl+b`, `esc` `esc` and the chord above
are control bytes and arrive everywhere. It is only the tab keys, which is why they are
the only thing in the alt namespace.

Anywhere hop cannot learn a directory — fish, a shell with no prompt hook, a host
with no shell open at all — the key still opens VS Code on the host, in its default
directory, and the status line says so.

### Several shells on one host

`S` in the host list, or `alt+0` from inside the pane, opens another shell on a
host you are already connected to. It is a second **channel** on the connection
hop already holds — no new handshake, no second authentication — and it appears
as a tab strip above the pane (`shell 1 │ shell 2 │ …`), which shows up only once
there is a second shell to switch to. The new shell arrives focused, so `alt+0`
is one key from "I need another terminal here" to typing in it.

Switch with `alt+←`/`alt+→`, or jump with `alt+1`…`alt+9`. Unlike the editor, the
alt+**letters** are *not* bound here: readline owns them (`alt+b` walks back a
word, `alt+l` downcases one), and taking them would break the shell. The
alt+**digits** are the one exception hop makes, and `alt+0` is why the new-shell
key is a digit rather than the more memorable `alt+n`: it costs the remote shell
nothing that `alt+1`…`alt+9` has not already cost it.

The other key hop keeps from the shell is `ctrl+b`, the sidebar toggle — the same
key tmux and screen use for "this one is for the multiplexer". A remote tmux
therefore never sees its prefix through hop; nothing else is taken.

Type `exit` to close a shell: its tab goes away, the rest keep running. When the
last one exits, the connection is done and the host goes back to idle in the list
— unless its SFTP browser or an editor tab is still open on it, which keeps the
connection alive. `d` still tears down the whole host at once: every shell, the
browser and the editors.

### Scrolling back through history

`shift+↑` pauses the live shell and steps one line up into its scrollback;
`shift+pgup` does it a page at a time. Both are deliberately chords a bare shell
never sends, so they can be hop's without taking anything the shell wants — and
both decline (falling through to the shell) when there is nothing to show: on the
alt screen a full-screen program owns its own scrolling, and with nothing scrolled
off there is no history to see. The footer advertises `shift+↑ scrollback` exactly
when the key is live.

Once paused, the keyboard drives the history viewport rather than the remote shell,
and the mode chip reads `⇅ scrollback <offset>/<len>` so you can see where you are:

| Key | Action |
| --- | --- |
| `↑` / `↓`, `j` / `k` | up / down one line |
| `pgup` / `pgdn`, `ctrl+f` | up / down a page (`ctrl+b` is the sidebar) |
| `ctrl+u` / `ctrl+d` | up / down half a page |
| `g` / `home` | jump to the oldest line |
| `G` / `end` | back to the live bottom (and leave scrollback) |
| `esc` / `q` / `enter` / `ctrl+o` / `←` | back to the live shell |
| *anything else* | leave scrollback and type it at the prompt |

Reaching the live bottom by scrolling down is itself a way out — the point of
scrollback is to look at what went by, so arriving at the tail means you are done.
`ctrl+o` here only leaves scrollback, back to the live shell; a second `ctrl+o` then
leaves the pane, the consistent "back one level" the rest of hop keeps to.

### Why `left` is not a way out

`left` backs out of a host in the list and out of a directory in the browser, but
inside a shell it is the **shell's key, always**. It moves the readline cursor back
over a typo, it is what `alt+b`/`alt+f` word motions and every full-screen program
(`vim`, `htop`, `less`) are built on, and hop taking it — even at what hop believes
is an empty prompt — breaks editing on every server you connect to.

hop used to claim `left` at a bare prompt, on the theory that the key was a no-op
there. It is not reliable: hop sees keystrokes going out and pixels coming back, not
the buffer readline is holding, so anything it could not count (a `ctrl+w`, a
tab-completion, a program that reads arrows inline) left it thinking the prompt was
bare when it was not — and `left` ejected you out of the line you were editing.

`ctrl+o` — a chord no common shell binds — is the unconditional way out, and works no
matter what is on the line or what is running. `esc` `esc` is the fast one.

### How double-esc works, and what it costs

A lone `esc` **is still forwarded to the shell.** hop cannot know a second `esc`
is coming without swallowing the first one and waiting out the timer, which would
put a 400 ms lag on every `esc` you press in vim. So the rule is:

| You press | The shell receives | hop does |
| --- | --- | --- |
| `esc` | `esc` | arms the window |
| `esc` `esc` (fast) | `esc` | leaves the pane on the second |
| `esc` … pause … `esc` | `esc` `esc` | nothing |
| `esc` `j` `esc` | `esc` `j` `esc` | nothing — any key breaks the chord |

The trade-off: **if you mash `esc` twice quickly in vim, you will land back in
the host list.** The shell will have seen one of those escapes, which in normal
mode is a harmless no-op, so nothing is lost — press `enter` or `s` to drop
straight back into the session. If that bothers you, `ctrl+o` is unambiguous and
never fires by accident.

---

## Notes on the vim motions

- **They are off until you turn them on**, in the settings popover (`,` → *Vim
  keys*). Off, they are not bound to anything else either: a stray `l` in the host
  list does nothing at all. `pgdn`/`pgup` are *not* part of the switch — they page
  without being vim, so turning vim off never costs you a way to page.
- **The two views bind different amounts of the keyboard**, never different
  meanings. The browser has all of it; the host list has the step keys, `hjkl` and
  the page keys. A key the list binds does there what it does in the browser.
- **`gg` is a real two-key motion** — in the browser. A lone `g` arms it; the next
  `g` jumps to the top. Any other key in between cancels it, so `g` `j` is just a
  `j`. In the host list it is not bound, and a `g` there arms nothing.
- **`ctrl+b` is not a motion anywhere.** It is the sidebar toggle in every mode, so
  paging back is `pgup` (and `ctrl+f`'s partner is missing on purpose).
- **Half and full pages are viewport-relative**, matching vim: `ctrl+d` moves by
  half the visible rows, `ctrl+f` by all of them. Both are clamped to at least
  one row, so they still work in a very short terminal.
- **`H`/`M`/`L` move within the visible window**, not the whole list. In a long
  directory, `L` lands on the last row *on screen*, while `G` lands on the last
  entry in the directory.
- **The cursor never leaves the visible window.** Every motion re-clamps the
  scroll offset to keep it in view; `TestCursorStaysVisible` pins that invariant.
- Motions are inert on an empty listing rather than driving the cursor negative.

## Where a binding lives

The host list and the file browser move on the same keys, so they do not each spell
that keyboard out. `internal/keymap` holds it: one table, one row per key, saying
what the key *means* (a `Motion`), whether the vim setting owns it, and whether the
host list binds it as well as the browser. Both views resolve keys through it —
passing `keymap.Full` or `keymap.List` — and act on the motion they get back: what a
key means is decided in one place, what it does is decided by the view, which is the
only part that knows how tall it is or what is under the cursor.

So:

| To add… | Touch |
| --- | --- |
| a motion key, in one or both views | the `bindings` table in `internal/keymap` (the `list` column is the split) |
| a key hop holds in *every* mode | `toggleSidebarKey`'s branch in `handleKey`, `internal/tui/keys.go` |
| what a motion *does* to the list | `model.move` in `internal/tui/keys.go` |
| what a motion *does* to the browser | `Browser.move` in `internal/filebrowser` |
| a command key (`d`, `o`, `r`, `f`, …) | the command switch in whichever view owns it |
| a setting | the `settingsFields` table in `internal/tui/settings.go` |

A mode with no motions of its own — the settings popover — asks `keymap.Vim(key)`
instead, which is the same table answering the narrower question: *is this a key the
vim setting owns?* That is why turning the setting off is one fact in the config
rather than a flag threaded through three switch statements.
