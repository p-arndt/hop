package tui

import (
	"strings"
	"testing"
	"time"

	"hop/internal/dockerenv"
)

// A host's default directory, end to end against a real OpenSSH server running real
// shells: connect the way a user does and the shell has to be standing in that directory,
// with nothing on screen to say how it got there.
//
//	HOP_DOCKER_E2E=1 go test ./internal/tui/ -run StartDirE2E -v
//
// bash and zsh get the cd on the same line as the OSC 7 hook, so both halves are checked
// at once: the shell moved, and the hook ran after the move.
func TestStartDirE2ELandsInTheDefaultDirectory(t *testing.T) {
	for _, c := range []struct{ user, dir string }{
		{dockerenv.BashUser, "/etc/ssh"},
		{dockerenv.ZshUser, dockerenv.SpaceDir}, // a name with a space in it stays one word
	} {
		t.Run(c.user, func(t *testing.T) {
			m, sh := connectShellHostIn(t, c.user, c.dir)

			if dir := waitForPaneCwd(t, m, c.dir); dir != c.dir {
				t.Fatalf("the shell reported %q on login, want the default dir %q\npane:\n%s",
					dir, c.dir, sh.pane.View())
			}
			if view := waitForCleanPane(sh); strings.Contains(view, "hop_cwd") || strings.Contains(view, "cd ") {
				t.Fatalf("the line hop typed is still on screen:\n%s", view)
			}

			// And the binding that reads the cwd sees it, so opening the host opens the
			// directory it was configured to start in.
			rec := stubVSCode(t, nil)
			m.handleKey(key(t, "ctrl+o"))
			m.handleKey(key(t, "ctrl+o"))
			if rec.calls != 1 || rec.path != c.dir {
				t.Fatalf("opened (%q, %q) in %d calls, want %q once", rec.alias, rec.path, rec.calls, c.dir)
			}
		})
	}
}

// fish gets no OSC 7 hook, but "cd" is a line every shell understands, so the default
// directory still takes effect. The proof is on the screen rather than in a cwd report:
// hop typed the cd, the shell ran it, and nothing was left behind.
func TestStartDirE2EWorksOnAnUnhookableShell(t *testing.T) {
	m, sh := connectShellHostIn(t, dockerenv.FishUser, "/etc/ssh")

	if !waitForPaneText(sh, "/etc/ssh", 20*time.Second) {
		t.Fatalf("fish never showed the default directory in its prompt:\n%s", sh.pane.View())
	}
	if view := sh.pane.View(); strings.Contains(view, "hop_cwd") {
		t.Fatalf("a hook was typed into fish; pane:\n%s", view)
	}
	if m.shellCwd("shellhost") != "" {
		t.Fatalf("fish reported a directory (%q); nothing should have installed a hook",
			m.shellCwd("shellhost"))
	}
}

// A default directory that is not there is not silently swallowed: the shell's own
// error stays on screen, which is how the user finds out the setting is stale.
func TestStartDirE2EShowsAMissingDirectory(t *testing.T) {
	_, sh := connectShellHostIn(t, dockerenv.BashUser, "/srv/definitely-not-here")

	if !waitForPaneText(sh, "definitely-not-here", 20*time.Second) {
		t.Fatalf("the failed cd left nothing on screen:\n%s", sh.pane.View())
	}
}

// waitForPaneText polls the pane until want shows up in its view.
func waitForPaneText(sh *shellTab, want string, within time.Duration) bool {
	deadline := time.Now().Add(within)
	for time.Now().Before(deadline) {
		if strings.Contains(sh.pane.View(), want) {
			return true
		}
		time.Sleep(50 * time.Millisecond)
	}
	return strings.Contains(sh.pane.View(), want)
}
