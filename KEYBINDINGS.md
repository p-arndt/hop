# hop — keybindings

hop has three input modes. The footer always shows the keys for the mode you are
in, and every mode returns to navigation with `ctrl+o`.

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
| `h` / `backspace` | up one directory (never leaves the browser) |
| `left` | up one directory — **or back to hop** when there is nowhere left to go up |
| `r` | refresh the listing |
| `ctrl+o` | back to hop |

### How `left` behaves

`left` is the back button. It walks up the directory tree, and once it reaches
the top it pops you out of the browser entirely — the way `left` backs out of a
screen in a Claude agent:

```
~/projects/api   --left-->  ~/projects
~/projects       --left-->  ~
~  (start dir)   --left-->  back to hop
```

"The top" means the directory the browser opened in, or `/`. `h` and `backspace`
stay strict *up a directory* and never dismiss the browser, so you always have a
way to bump against the top without falling out of it.

## Terminal — a live shell on a remote host

| Key | Action |
| --- | --- |
| `ctrl+o` | back to hop |
| *everything else* | sent to the remote shell |

**`left` is not a back key here, and cannot be.** Once a pane is focused, every
keystroke is forwarded verbatim to the remote host, because the shell needs them
all: arrow keys move the readline cursor, `ctrl+d` sends EOF, `esc` leaves vim's
insert mode. Intercepting any of them would silently break editing on every
server you connect to.

That is why `ctrl+o` — a chord no common shell binds — is deliberately the *only*
key hop reserves in this mode. The asymmetry with the browser is intentional: the
browser is hop's own UI, the terminal is someone else's.

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
