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
| `o` | open the host in VS Code Remote |
| `d` | disconnect the session |
| `a` | add a host |
| `e` | edit the host under the cursor |
| `x` | delete the host under the cursor (asks first) |
| `i` | import hosts from an OpenSSH config (`~/.ssh/config` by default) |
| `/` | filter hosts (`enter` applies, `esc` clears) |
| `,` | settings |
| `?` | the keys card |
| `q` / `ctrl+c` | quit |

With **vim keys** turned on (`,` → *Vim keys*, off by default), these are bound as
well:

| Key | Action |
| --- | --- |
| `j` / `k` | move down / up |
| `gg` | jump to first host |
| `G` | jump to last host |
| `H` / `M` / `L` | jump to first / middle / last host |
| `ctrl+d` / `ctrl+u` | half a page down / up |
| `ctrl+f` / `ctrl+b` | a full page down / up |
| `l` | connect — as `enter` does |
| `h` | back — as `esc` does |

Forward and back then use the same keys as the browser: `enter`/`l`/`right`
descends into the thing under the cursor, `h`/`left` backs out of it.

Because the host list does not scroll — every host is on screen — `H`/`M`/`L`
coincide with `gg`, the midpoint, and `G`. They are bound anyway so the motions
stay consistent with the browser.

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
| `ctrl+o` | back to hop |
| `esc` `esc` | back to hop (two presses within 400 ms) |

With **vim keys** turned on:

| Key | Action |
| --- | --- |
| `j` / `k` | move down / up |
| `gg` | jump to first entry |
| `G` | jump to last entry |
| `H` / `M` / `L` | jump to top / middle / bottom **of the visible window** |
| `ctrl+d` / `ctrl+u` | half a page down / up |
| `ctrl+f` / `ctrl+b` | a full page down / up |
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
| `shift+↑` / `shift+pgup` | scroll back into the pane's history (see below) |
| *everything else* | sent to the remote shell |

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
| `pgup` / `pgdn`, `ctrl+b` / `ctrl+f` | up / down a page |
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
  list does nothing at all. `pgdn`/`pgup` are *not* part of the switch — they mean
  what `ctrl+f`/`ctrl+b` mean, but they are not vim, so turning vim off never costs
  you a way to page.
- **`gg` is a real two-key motion.** A lone `g` arms it; the next `g` jumps to
  the top. Any other key in between cancels it, so `g` `j` is just a `j`.
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
what the key *means* (a `Motion`) and whether the vim setting owns it. Both views
resolve keys through it and act on the motion they get back — what a key means is
decided in one place, what it does is decided by the view, which is the only part
that knows how tall it is or what is under the cursor.

So:

| To add… | Touch |
| --- | --- |
| a motion key, in both views at once | the `bindings` table in `internal/keymap` |
| what a motion *does* to the list | `model.move` in `internal/tui/keys.go` |
| what a motion *does* to the browser | `Browser.move` in `internal/filebrowser` |
| a command key (`d`, `o`, `r`, `f`, …) | the command switch in whichever view owns it |
| a setting | the `settingsFields` table in `internal/tui/settings.go` |

A mode with no motions of its own — the settings popover — asks `keymap.Vim(key)`
instead, which is the same table answering the narrower question: *is this a key the
vim setting owns?* That is why turning the setting off is one fact in the config
rather than a flag threaded through three switch statements.
