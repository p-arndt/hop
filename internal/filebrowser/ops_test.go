package filebrowser

import (
	"errors"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"hop/internal/sftpx"
)

// liveClient is a fakeClient whose listing actually changes when a mutation succeeds, so
// a test can assert where the cursor ends up after the browser re-lists — which is the
// half of these operations the recorded calls say nothing about.
type liveClient struct {
	fakeClient
}

func (c *liveClient) Remove(p string) error {
	if err := c.fakeClient.Remove(p); err != nil {
		return err
	}
	name := baseName(p)
	for i, e := range c.entries {
		if e.Name == name {
			c.entries = append(c.entries[:i:i], c.entries[i+1:]...)
			break
		}
	}
	return nil
}

func (c *liveClient) Rename(oldp, newp string) error {
	if err := c.fakeClient.Rename(oldp, newp); err != nil {
		return err
	}
	old := baseName(oldp)
	for i, e := range c.entries {
		if e.Name == old {
			c.entries[i].Name = baseName(newp)
			break
		}
	}
	return nil
}

func (c *liveClient) Mkdir(p string) error {
	if err := c.fakeClient.Mkdir(p); err != nil {
		return err
	}
	c.entries = append(c.entries, sftpx.Entry{Name: baseName(p), IsDir: true})
	return nil
}

func baseName(p string) string {
	if i := strings.LastIndex(p, "/"); i >= 0 {
		return p[i+1:]
	}
	return p
}

// opsBrowser builds a browser over a named listing backed by a liveClient.
func opsBrowser(ents ...sftpx.Entry) (*Browser, *liveClient) {
	c := &liveClient{fakeClient{entries: ents}}
	return &Browser{
		client:  c,
		cwd:     "/home/u",
		entries: ents,
		opts:    Options{VimKeys: true},
		w:       40,
		h:       13, // contentRows() == 10
	}, c
}

func files(names ...string) []sftpx.Entry {
	ents := make([]sftpx.Entry, len(names))
	for i, n := range names {
		ents[i] = sftpx.Entry{Name: n, Size: 1}
	}
	return ents
}

// typeText feeds s to the open prompt one rune at a time, the way a user does.
func typeText(t *testing.T, b *Browser, s string) {
	t.Helper()
	for _, r := range s {
		b.Handle(key(t, string(r)))
	}
}

// "x" alone must never delete: it opens a question naming the entry, and until that is
// answered nothing has reached the server.
func TestRemoveAsksFirst(t *testing.T) {
	b, c := opsBrowser(files("a.txt", "b.txt")...)
	b.cursor = 1

	b.Handle(key(t, "x"))

	if len(c.removes) != 0 {
		t.Fatalf("x on its own deleted %v, want the confirm first", c.removes)
	}
	if !b.overlay.active() {
		t.Fatal("x did not open a question")
	}
	if !strings.Contains(b.overlay.label, "b.txt") {
		t.Fatalf("confirm reads %q; it must name the entry so a mis-pressed x is visible", b.overlay.label)
	}

	// "n" — like any key that is not y — declines, and is swallowed rather than falling
	// through to the listing behind it.
	b.Handle(key(t, "n"))
	if len(c.removes) != 0 {
		t.Fatalf("n deleted %v, want a cancel", c.removes)
	}
	if b.overlay.active() {
		t.Fatal("n left the question open")
	}
}

// "y" commits, and the cursor stays on the row it was on rather than jumping to the top
// of the re-listed directory.
func TestRemoveDeletesAndKeepsTheRow(t *testing.T) {
	b, c := opsBrowser(files("a", "b", "c", "d")...)
	b.cursor = 1 // b

	b.Handle(key(t, "x"))
	b.Handle(key(t, "y"))

	if len(c.removes) != 1 || c.removes[0] != "/home/u/b" {
		t.Fatalf("removes = %v, want one /home/u/b", c.removes)
	}
	if len(b.entries) != 3 {
		t.Fatalf("entries = %v, want the listing re-read without b", b.entries)
	}
	if b.cursor != 1 {
		t.Fatalf("cursor = %d, want it to stay on row 1 (now c)", b.cursor)
	}
	if b.statusErr || !strings.Contains(b.status, "deleted b") {
		t.Fatalf("status = %q (err=%v), want a deleted message", b.status, b.statusErr)
	}
}

// Deleting the last entry has no successor row to stand on, so the cursor clamps up
// instead of pointing past the end.
func TestRemoveLastEntryClampsTheCursor(t *testing.T) {
	b, _ := opsBrowser(files("a", "b")...)
	b.cursor = 1

	b.Handle(key(t, "x"))
	b.Handle(key(t, "y"))

	if b.cursor != 0 {
		t.Fatalf("cursor = %d after deleting the last entry, want 0", b.cursor)
	}
}

// A non-empty directory is refused by the server, and that refusal is reported — the
// browser has no recursive delete to fall back on and must not pretend otherwise.
func TestRemoveNonEmptyDirectoryReportsTheRefusal(t *testing.T) {
	b, c := opsBrowser(sftpx.Entry{Name: "sub", IsDir: true})
	c.errs = map[string]error{"remove": errors.New("sftp: \"Failure\" (SSH_FX_FAILURE)")}

	b.Handle(key(t, "x"))
	b.Handle(key(t, "y"))

	if len(c.removes) != 1 {
		t.Fatalf("removes = %v, want the delete to have been attempted", c.removes)
	}
	if !b.statusErr {
		t.Fatalf("status = %q (err=%v), want the refusal reported as an error", b.status, b.statusErr)
	}
	if !strings.Contains(b.status, "empty") {
		t.Fatalf("status = %q, want it to say the directory must be empty first", b.status)
	}
	if len(b.entries) != 1 {
		t.Fatalf("entries = %v, want the directory still listed", b.entries)
	}
}

// "R" prefills the current name, renames within the directory, and leaves the cursor on
// the entry under its new name.
func TestRenameRoundTrip(t *testing.T) {
	b, c := opsBrowser(files("a.txt", "b.txt", "c.txt")...)
	b.cursor = 1

	b.Handle(key(t, "R"))
	if b.overlay.value != "b.txt" {
		t.Fatalf("prompt prefilled with %q, want the current name", b.overlay.value)
	}
	// Clear the prefill the way the user does, then type the new name.
	b.Handle(key(t, "ctrl+u"))
	typeText(t, b, "zeta.txt")
	b.Handle(key(t, "enter"))

	if len(c.renames) != 1 || c.renames[0] != [2]string{"/home/u/b.txt", "/home/u/zeta.txt"} {
		t.Fatalf("renames = %v, want one {/home/u/b.txt /home/u/zeta.txt}", c.renames)
	}
	if e, _ := b.selected(); e.Name != "zeta.txt" {
		t.Fatalf("cursor stands on %q, want the renamed entry", e.Name)
	}
	if b.statusErr || !strings.Contains(b.status, "zeta.txt") {
		t.Fatalf("status = %q (err=%v), want a renamed message", b.status, b.statusErr)
	}
}

// A typed answer is a name, not a path: a slash would move the file out of the directory
// the user is looking at, and "." or ".." address the directory itself.
func TestRenameRefusesPathsAndDots(t *testing.T) {
	for _, name := range []string{"sub/b.txt", "/etc/passwd", "..", "."} {
		t.Run(name, func(t *testing.T) {
			b, c := opsBrowser(files("a.txt")...)

			b.Handle(key(t, "R"))
			b.Handle(key(t, "ctrl+u"))
			typeText(t, b, name)
			b.Handle(key(t, "enter"))

			if len(c.renames) != 0 {
				t.Fatalf("rename to %q reached the server as %v, want a refusal", name, c.renames)
			}
			if !b.statusErr {
				t.Fatalf("rename to %q: status = %q, want an error", name, b.status)
			}
		})
	}
}

// Opening the prompt and confirming the name that is already there is a no-op, not a
// failed rename: the user changed nothing and nothing is wrong.
func TestRenameToTheSameNameIsANoop(t *testing.T) {
	b, c := opsBrowser(files("a.txt")...)

	b.Handle(key(t, "R"))
	b.Handle(key(t, "enter"))

	if len(c.renames) != 0 {
		t.Fatalf("renames = %v, want nothing sent for an unchanged name", c.renames)
	}
	if b.statusErr {
		t.Fatalf("status = %q, want no error for an unchanged name", b.status)
	}
}

// esc drops the question without renaming anything.
func TestRenameEscapeCancels(t *testing.T) {
	b, c := opsBrowser(files("a.txt")...)

	b.Handle(key(t, "R"))
	b.Handle(key(t, "ctrl+u"))
	typeText(t, b, "b.txt")
	b.Handle(tea.KeyMsg{Type: tea.KeyEscape})

	if len(c.renames) != 0 {
		t.Fatalf("renames = %v, want esc to have cancelled", c.renames)
	}
	if b.overlay.active() {
		t.Fatal("esc left the question open")
	}
}

// "m" creates the directory under cwd and stands the cursor on it.
func TestMkdirCreatesAndFocuses(t *testing.T) {
	b, c := opsBrowser(files("a.txt")...)

	b.Handle(key(t, "m"))
	typeText(t, b, "docs")
	b.Handle(key(t, "enter"))

	if len(c.mkdirs) != 1 || c.mkdirs[0] != "/home/u/docs" {
		t.Fatalf("mkdirs = %v, want one /home/u/docs", c.mkdirs)
	}
	if e, _ := b.selected(); e.Name != "docs" {
		t.Fatalf("cursor stands on %q, want the new directory", e.Name)
	}
	if b.statusErr || !strings.Contains(b.status, "docs") {
		t.Fatalf("status = %q (err=%v), want a created message", b.status, b.statusErr)
	}
}

// The same name rules apply to "m": Mkdir creates parents, so a slash would build a tree
// rather than the directory that was asked for.
func TestMkdirRefusesPathsAndDots(t *testing.T) {
	for _, name := range []string{"a/b", "..", "."} {
		t.Run(name, func(t *testing.T) {
			b, c := opsBrowser(files("a.txt")...)

			b.Handle(key(t, "m"))
			typeText(t, b, name)
			b.Handle(key(t, "enter"))

			if len(c.mkdirs) != 0 {
				t.Fatalf("mkdir %q reached the server as %v, want a refusal", name, c.mkdirs)
			}
			if !b.statusErr {
				t.Fatalf("mkdir %q: status = %q, want an error", name, b.status)
			}
		})
	}
}

// A server that says no must land on the status line as an error. The refresh that
// follows a success clears the status, so an error swallowed here would leave the user
// believing the operation went through.
func TestServerErrorsReachTheStatusLine(t *testing.T) {
	cases := []struct {
		name string
		op   string
		keys []string
		text string
	}{
		{"mkdir", "mkdir", []string{"m"}, "newdir"},
		{"rename", "rename", []string{"R"}, "other.txt"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			b, c := opsBrowser(files("a.txt")...)
			c.errs = map[string]error{tc.op: errors.New("permission denied")}

			b.Handle(key(t, tc.keys[0]))
			b.Handle(key(t, "ctrl+u"))
			typeText(t, b, tc.text)
			b.Handle(key(t, "enter"))

			if !b.statusErr {
				t.Fatalf("%s: status = %q (err=%v), want the server error shown", tc.name, b.status, b.statusErr)
			}
			if !strings.Contains(b.status, "permission denied") {
				t.Fatalf("%s: status = %q, want it to carry the server's reason", tc.name, b.status)
			}
		})
	}
}

// None of the three keys does anything in an empty directory: there is no entry to
// delete or rename, and mkdir still has a directory to create in.
func TestOpsOnAnEmptyListing(t *testing.T) {
	b, c := opsBrowser()

	b.Handle(key(t, "x"))
	b.Handle(key(t, "R"))
	if b.overlay.active() {
		t.Fatal("x or R opened a question with nothing under the cursor")
	}
	if len(c.removes) != 0 || len(c.renames) != 0 {
		t.Fatalf("empty listing produced removes=%v renames=%v", c.removes, c.renames)
	}

	b.Handle(key(t, "m"))
	typeText(t, b, "new")
	b.Handle(key(t, "enter"))
	if len(c.mkdirs) != 1 || c.mkdirs[0] != "/home/u/new" {
		t.Fatalf("mkdirs = %v, want mkdir to work in an empty directory", c.mkdirs)
	}
}

func TestCheckTypedName(t *testing.T) {
	for _, ok := range []string{"a.txt", "my report.pdf", "übersicht.md", "..hidden", "...", "a..b"} {
		if err := checkTypedName(ok); err != nil {
			t.Errorf("checkTypedName(%q) = %v, want nil", ok, err)
		}
	}
	for _, bad := range []string{"", ".", "..", "a/b", "/abs", "trail/", "esc\x1b[2J"} {
		if err := checkTypedName(bad); err == nil {
			t.Errorf("checkTypedName(%q) = nil, want a refusal", bad)
		}
	}
}

// A mutation that lands on a server whose listing then fails must not report success:
// the entry is still on screen, and a green "deleted" over a hidden connection error is
// the worst of both.
func TestMutationDoesNotPaintOverAListingError(t *testing.T) {
	for _, tc := range []struct{ name, key, answer string }{
		{"delete", "x", "y"},
		{"rename", "R", ""},
		{"mkdir", "m", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b, c := opsBrowser(files("a.txt", "b.txt")...)
			b.cursor = 1

			b.Handle(key(t, tc.key))
			// The mutation itself succeeds; only the re-list behind it fails.
			c.listErr = errors.New("connection lost")
			if tc.answer != "" {
				b.Handle(key(t, tc.answer))
			} else {
				typeText(t, b, "other")
				b.Handle(key(t, "enter"))
			}

			if !b.statusErr {
				t.Fatalf("status = %q (err=false), want the listing error to survive", b.status)
			}
			if !strings.Contains(b.status, "connection lost") {
				t.Fatalf("status = %q, want the listing error", b.status)
			}
		})
	}
}

// The directory an operation was aimed at is the one it acts in, even if something moves
// the browser between the question and the answer.
func TestOpsActInTheDirectoryTheyWereAimedAt(t *testing.T) {
	b, c := opsBrowser(files("a.txt", "b.txt")...)
	b.cursor = 1
	name := b.entries[1].Name

	b.Handle(key(t, "x"))
	// Nothing in the keyboard or the pointer can do this now, but the callbacks must not
	// depend on that: the path was fixed when the question was asked.
	b.cwd = "/somewhere/else"
	b.Handle(key(t, "y"))

	if len(c.removes) != 1 {
		t.Fatalf("removes = %v, want exactly one", c.removes)
	}
	if want := "/home/u/" + name; c.removes[0] != want {
		t.Fatalf("removed %q, want %q — the answer followed the browser instead of the question", c.removes[0], want)
	}
}
