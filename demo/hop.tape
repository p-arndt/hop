# hop — README demo tape.
#
# Recorded by `just demo` (scripts/demo.mjs), which builds hop, stands up the fake
# SSH server in tools/demoserver and points HOME at a throwaway directory with a
# seeded host list. Every host, file and command answer on screen is invented, so
# this tape can be re-run by anyone without exposing anything of their own.
#
# Run it directly only through the script — on its own it would record hop against
# your real host list.

Output assets/demo.gif

Require hop

Set Shell "bash"
Set FontSize 15
Set Width 1500
Set Height 800
Set Padding 26
Set Margin 18
Set MarginFill "#1a1420"
Set BorderRadius 10
Set Theme "Catppuccin Mocha"
Set TypingSpeed 55ms
Set Framerate 24

# ---- the host list ----

Hide
Type "clear" Enter
Show

Type "hop" Enter
Wait+Screen /HOSTS/
Sleep 2s
Screenshot assets/screens/hosts.png

# Move down the fleet, then fuzzy-find your way back to the top of it. Filtering to
# a single match and clearing it again is also what leaves the cursor on
# prod-web-1 — the host the fake shell prompts as.
Down 3
Sleep 1200ms
Type "/"
Sleep 400ms
Type "web-1"
Sleep 1400ms
Screenshot assets/screens/filter.png
Enter
Sleep 700ms
Escape
Sleep 900ms

# ---- a shell ----

Enter
Wait+Screen /\$/
Sleep 1500ms
Type "systemctl status caddy" Enter
Sleep 2500ms
Screenshot assets/screens/shell.png
Type "docker compose ps" Enter
Sleep 2200ms

# A second shell on the same host — a new channel, not a new connection.
# (`alt+0` does this from inside the pane; the tape uses `S` from the list because
# VHS cannot type alt+digit.)
Ctrl+o
Sleep 1000ms
Type "S"
Wait+Screen /shell 2/
Sleep 1000ms
Type "tail -n 5 logs/access.log" Enter
Sleep 2500ms
Screenshot assets/screens/shells.png

# ---- the file browser ----

Ctrl+o
Sleep 1200ms
Type "f"
Wait+Screen /docker-compose.yml/
Sleep 1800ms
Screenshot assets/screens/sftp.png

# Down to docker-compose.yml (app, backups, logs, .env.example, deploy.sh, …)
Down 5
Sleep 1000ms

# ---- a remote editor tab ----

Enter
Wait+Screen /ghcr.io/
Sleep 1800ms
Screenshot assets/screens/editor.png
Type "jjjj"
Sleep 1200ms
Type ":q" Enter
Sleep 1500ms

# Back out to the host list: one level per ctrl+o.
Ctrl+o
Sleep 1200ms

# ---- settings, live ----

Type ","
Sleep 1200ms
Screenshot assets/screens/settings.png
Down 2
Sleep 600ms
Right
Sleep 700ms
Right
Sleep 700ms
Right
Sleep 1200ms
Escape
Sleep 1000ms

# ---- the keys card ----

Type "?"
Sleep 2000ms
Screenshot assets/screens/keys.png
Escape
Sleep 800ms

Type "q"
Sleep 1500ms
