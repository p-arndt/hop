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
| `j` / `down` | move down |
| `k` / `up` | move up |
| `gg` | jump to first host |
| `G` | jump to last host |
| `H` / `M` / `L` | jump to first / middle / last host |
| `ctrl+d` / `ctrl+u` | half a page down / up |
| `ctrl+f` / `ctrl+b` | a full page down / up (also `pgdn` / `pgup`) |
| `enter` / `l` / `right` | connect (opens a terminal pane) |
| `esc` / `h` / `left` | back — leave the details view |
| `s` | focus the existing session for this host |
| `f` | open the SFTP browser |
| `o` | open the host in VS Code Remote |
| `d` | disconnect the session |
| `/` | filter hosts (`enter` applies, `esc` clears) |
| `q` / `ctrl+c` | quit |

Forward and back use the same keys as the browser: `enter`/`l`/`right` descends
into the thing under the cursor, `h`/`left` backs out of it.

Because the host list does not scroll — every host is on screen — `H`/`M`/`L`
coincide with `gg`, the midpoint, and `G`. They are bound anyway so the motions
stay consistent with the browser.

### Recent directories

The host under the cursor expands to show the directories you last browsed on
it, most frecent first (visit count, then recency — the same ranking the host
list itself uses):

```
  ● prod-web  root@10.0.0.4
   ├ …ases/2024-06-01/current 2m
  ▎├ /var/log/nginx           1h     ← cursor on a directory
   └ /etc/caddy               3d
  ○ db-01     pg@10.0.0.9
```

`j`/`k` walk through these rows as if they were hosts, so `j` off a host row
steps into its first directory and `k` off a host row lands on the *previous*
host's last one. Only the host at the cursor expands, so the sidebar stays a
screenful however much history you accumulate. The jump motions (`gg`, `G`,
`H`/`M`/`L`, `ctrl+d`/`ctrl+u`, `ctrl+f`/`ctrl+b`) always land on a host row.

With a directory selected, three keys change meaning:

| Key | Action |
| --- | --- |
| `enter` / `l` / `right` | connect (or focus the live session) and `cd` into it |
| `f` | open the SFTP browser rooted there instead of at `$HOME` |
| `x` | forget this directory |
| `esc` / `h` / `left` | back to the host row (a second press leaves the details view) |

**Where they come from.** Only the SFTP browser records them: every directory it
lands in is stored against that host. A `cd` you type in the shell leaves no
trace, because hop forwards keystrokes to the remote shell verbatim and never
parses what comes back.

**What `enter` does.** hop types `cd '<path>' && clear` into the shell — into the
live pane if there is one, otherwise into the shell it opens for you. The `&&`
means a `cd` that fails (directory gone, permissions changed) leaves the error on
screen instead of clearing it away.

## Browsing — the SFTP file browser

| Key | Action |
| --- | --- |
| `j` / `down` | move down |
| `k` / `up` | move up |
| `gg` | jump to first entry |
| `G` | jump to last entry |
| `H` / `M` / `L` | jump to top / middle / bottom **of the visible window** |
| `ctrl+d` / `ctrl+u` | half a page down / up |
| `ctrl+f` / `ctrl+b` | a full page down / up (also `pgdn` / `pgup`) |
| `enter` / `l` / `right` | enter a directory, or download a file to `~/Downloads` |
| `h` / `left` / `backspace` | up one directory |
| `r` | refresh the listing |
| `ctrl+o` | back to hop |
| `esc` `esc` | back to hop (two presses within 400 ms) |

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
| *everything else* | sent to the remote shell |

**`left` is not a back key here, and cannot be.** Once a pane is focused, every
keystroke is forwarded verbatim to the remote host, because the shell needs them
all: arrow keys move the readline cursor, `ctrl+d` sends EOF, `esc` leaves vim's
insert mode. Intercepting any of them would silently break editing on every
server you connect to.

`ctrl+o` — a chord no common shell binds — is the safe way out. `esc` `esc` is
the fast one.

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
