package filebrowser

import (
	"errors"
	"path"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"hop/internal/keys"
	"hop/internal/sftpx"
)

// liveClient is a fakeClient whose listing changes when a mutation succeeds, so a test
// can assert where the cursor ends up after the browser re-lists.
type liveClient struct {
	fakeClient
}

func (c *liveClient) Remove(p string) error {
	if err := c.fakeClient.Remove(p); err != nil {
		return err
	}
	name := path.Base(p)
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
	old := path.Base(oldp)
	for i, e := range c.entries {
		if e.Name == old {
			c.entries[i].Name = path.Base(newp)
			break
		}
	}
	return nil
}

func (c *liveClient) Mkdir(p string) error {
	if err := c.fakeClient.Mkdir(p); err != nil {
		return err
	}
	c.entries = append(c.entries, sftpx.Entry{Name: path.Base(p), IsDir: true})
	return nil
}

// opsBrowser builds a browser over a named listing backed by a liveClient.
func opsBrowser(ents ...sftpx.Entry) (*Browser, *liveClient) {
	c := &liveClient{fakeClient{entries: ents}}
	b := &Browser{
		client: c,
		alias:  "web1",
		opts:   Options{VimKeys: true},
		w:      40,
		h:      13, // contentRows() == 10
	}
	return plant(b, "/home/u", ents), c
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

// "x" opens a question naming the entry, and nothing reaches the server until it is answered.
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

	// "n", like any key that is not y, declines and is swallowed.
	b.Handle(key(t, "n"))
	if len(c.removes) != 0 {
		t.Fatalf("n deleted %v, want a cancel", c.removes)
	}
	if b.overlay.active() {
		t.Fatal("n left the question open")
	}
}

func TestRemoveDeletesAndKeepsTheRow(t *testing.T) {
	b, c := opsBrowser(files("a", "b", "c", "d")...)
	b.cursor = 1 // b

	b.Handle(key(t, "x"))
	b.Handle(key(t, "y"))

	if len(c.removes) != 1 || c.removes[0] != "/home/u/b" {
		t.Fatalf("removes = %v, want one /home/u/b", c.removes)
	}
	if len(b.rows) != 3 {
		t.Fatalf("rows = %v, want the listing re-read without b", rowNames(b))
	}
	if b.cursor != 1 {
		t.Fatalf("cursor = %d, want it to stay on row 1 (now c)", b.cursor)
	}
	if b.note.err || !strings.Contains(b.note.text, "deleted b") {
		t.Fatalf("status = %q (err=%v), want a deleted message", b.note.text, b.note.err)
	}
}

func TestRemoveLastEntryClampsTheCursor(t *testing.T) {
	b, _ := opsBrowser(files("a", "b")...)
	b.cursor = 1

	b.Handle(key(t, "x"))
	b.Handle(key(t, "y"))

	if b.cursor != 0 {
		t.Fatalf("cursor = %d after deleting the last entry, want 0", b.cursor)
	}
}

// There is no recursive delete to fall back on, so the server's refusal is what is shown.
func TestRemoveNonEmptyDirectoryReportsTheRefusal(t *testing.T) {
	b, c := opsBrowser(sftpx.Entry{Name: "sub", IsDir: true})
	c.errs = map[string]error{"remove": errors.New("sftp: \"Failure\" (SSH_FX_FAILURE)")}

	b.Handle(key(t, "x"))
	b.Handle(key(t, "y"))

	if len(c.removes) != 1 {
		t.Fatalf("removes = %v, want the delete to have been attempted", c.removes)
	}
	if !b.note.err {
		t.Fatalf("status = %q (err=%v), want the refusal reported as an error", b.note.text, b.note.err)
	}
	if !strings.Contains(b.note.text, "empty") {
		t.Fatalf("status = %q, want it to say the directory must be empty first", b.note.text)
	}
	if len(b.rows) != 1 {
		t.Fatalf("rows = %v, want the directory still listed", rowNames(b))
	}
}

// "R" prefills the current name and leaves the cursor on the entry under its new name.
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
	if b.note.err || !strings.Contains(b.note.text, "zeta.txt") {
		t.Fatalf("status = %q (err=%v), want a renamed message", b.note.text, b.note.err)
	}
}

// A typed answer is a name, not a path.
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
			if !b.note.err {
				t.Fatalf("rename to %q: status = %q, want an error", name, b.note.text)
			}
		})
	}
}

func TestRenameToTheSameNameIsANoop(t *testing.T) {
	b, c := opsBrowser(files("a.txt")...)

	b.Handle(key(t, "R"))
	b.Handle(key(t, "enter"))

	if len(c.renames) != 0 {
		t.Fatalf("renames = %v, want nothing sent for an unchanged name", c.renames)
	}
	if b.note.err {
		t.Fatalf("status = %q, want no error for an unchanged name", b.note.text)
	}
}

func TestRenameEscapeCancels(t *testing.T) {
	b, c := opsBrowser(files("a.txt")...)

	b.Handle(key(t, "R"))
	b.Handle(key(t, "ctrl+u"))
	typeText(t, b, "b.txt")
	b.Handle(tea.KeyPressMsg{Code: tea.KeyEscape})

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
	if b.note.err || !strings.Contains(b.note.text, "docs") {
		t.Fatalf("status = %q (err=%v), want a created message", b.note.text, b.note.err)
	}
}

// Mkdir creates parents, so a slash would build a tree rather than one directory.
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
			if !b.note.err {
				t.Fatalf("mkdir %q: status = %q, want an error", name, b.note.text)
			}
		})
	}
}

// The refresh after a success clears the status, so a swallowed error would read as success.
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

			if !b.note.err {
				t.Fatalf("%s: status = %q (err=%v), want the server error shown", tc.name, b.note.text, b.note.err)
			}
			if !strings.Contains(b.note.text, "permission denied") {
				t.Fatalf("%s: status = %q, want it to carry the server's reason", tc.name, b.note.text)
			}
		})
	}
}

// x and R have no entry to act on; mkdir still has a directory to create in.
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

			if !b.note.err {
				t.Fatalf("status = %q (err=false), want the listing error to survive", b.note.text)
			}
			if !strings.Contains(b.note.text, "connection lost") {
				t.Fatalf("status = %q, want the listing error", b.note.text)
			}
		})
	}
}

// Even if something moves the browser between the question and the answer.
func TestOpsActInTheDirectoryTheyWereAimedAt(t *testing.T) {
	b, c := opsBrowser(files("a.txt", "b.txt")...)
	b.cursor = 1
	name := b.rows[1].e.Name

	b.Handle(key(t, "x"))
	// The path was fixed when the question was asked.
	b.cwd = "/somewhere/else"
	b.Handle(key(t, "y"))

	if len(c.removes) != 1 {
		t.Fatalf("removes = %v, want exactly one", c.removes)
	}
	if want := "/home/u/" + name; c.removes[0] != want {
		t.Fatalf("removed %q, want %q — the answer followed the browser instead of the question", c.removes[0], want)
	}
}

// ---- the plural operations ----

func TestDeleteConfirmNamesTheCount(t *testing.T) {
	b, c := opsBrowser(files("a", "b", "c")...)
	b.Do(keys.BrowserMark)
	b.Do(keys.BrowserMark) // a and b

	b.Handle(key(t, "x"))

	if !strings.Contains(b.overlay.label, "2 files") {
		t.Fatalf("confirm reads %q, want it to name the count", b.overlay.label)
	}
	if strings.Contains(b.overlay.label, "a?") {
		t.Fatalf("confirm reads %q, want no single file standing in for the selection", b.overlay.label)
	}

	b.Handle(key(t, "y"))
	if len(c.removes) != 2 || c.removes[0] != "/home/u/a" || c.removes[1] != "/home/u/b" {
		t.Fatalf("removes = %v, want both marked entries", c.removes)
	}
	if len(b.marks) != 0 {
		t.Fatalf("marks = %v, want them cleared once the delete went through", b.marks)
	}
	if b.note.err || !strings.Contains(b.note.text, "deleted 2 entries") {
		t.Fatalf("status = %q (err=%v), want the plural outcome", b.note.text, b.note.err)
	}
}

// A directory in the selection is the expensive mistake, so it is spelled out separately.
func TestDeleteConfirmCountsDirectories(t *testing.T) {
	b, _ := opsBrowser(sftpx.Entry{Name: "sub", IsDir: true}, sftpx.Entry{Name: "a", Size: 1})
	b.Do(keys.BrowserMarkAll)

	b.Handle(key(t, "x"))

	for _, want := range []string{"2 entries", "1 files", "1 directories"} {
		if !strings.Contains(b.overlay.label, want) {
			t.Fatalf("confirm reads %q, want it to mention %q", b.overlay.label, want)
		}
	}
}

// The message carries all three numbers: what got through, what failed and why, what was skipped.
func TestDeleteStopsAtTheFirstFailure(t *testing.T) {
	b, c := opsBrowser(files("a", "b", "c", "d")...)
	c.errs = map[string]error{"remove": errors.New("permission denied")}
	c.badName = "b"
	b.Do(keys.BrowserMarkAll)

	b.Handle(key(t, "x"))
	b.Handle(key(t, "y"))

	if len(c.removes) != 2 {
		t.Fatalf("removes = %v, want the batch to have stopped at the failure", c.removes)
	}
	if !b.note.err {
		t.Fatalf("status = %q (err=false), want the failure reported", b.note.text)
	}
	for _, want := range []string{"delete b", "permission denied", "1 of 4 done", "2 skipped"} {
		if !strings.Contains(b.note.text, want) {
			t.Fatalf("status = %q, want it to say %q", b.note.text, want)
		}
	}
	// The marks stay up, so the same keystroke retries exactly what did not happen.
	if len(b.marks) != 3 {
		t.Fatalf("marks = %v, want the three entries that are still there", b.marks)
	}
}

func TestDeleteOfOneStillNamesIt(t *testing.T) {
	b, _ := opsBrowser(files("a.txt", "b.txt")...)
	b.cursor = 1

	b.Handle(key(t, "x"))
	if !strings.Contains(b.overlay.label, "b.txt") {
		t.Fatalf("confirm reads %q, want the single name", b.overlay.label)
	}
}
