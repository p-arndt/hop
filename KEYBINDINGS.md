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

## Settings — the popover

`,` opens the settings card, floating over whatever is on screen (it works from the
host list and from the file browser; a terminal pane and an editor own every key,
so it is not reachable from those). It is modal: while it is up, keys go to it.

| Key | Action |
| --- | --- |
| `j` / `k` / `↑` / `↓` | move between settings |
| `h` / `l` / `←` / `→` | pick a color (on the accent field) |
| `enter` / `i` | edit the selected setting |
| `enter` (while editing) | save |
| `esc` (while editing) | cancel the edit |
| `ctrl+u` (while editing) | clear the value |
| `r` | reset the setting to its default |
| `esc` / `q` / `,` | close |

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
| `enter` / `l` / `right` | enter a directory, or open a file in an editor tab |
| `o` | open the file in the local OS default app (GUI) |
| `d` | download the file to `~/Downloads` |
| `h` / `left` / `backspace` | up one directory |
| `r` | refresh the listing |
| `ctrl+o` | back to hop |
| `esc` `esc` | back to hop (two presses within 400 ms) |

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
