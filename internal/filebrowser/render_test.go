package filebrowser

import (
	"regexp"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"hop/internal/keys"
	"hop/internal/sftpx"
)

var ansiRE = regexp.MustCompile(`\x1b\[[0-9;]*m`)

// plain strips the SGR styling so a test can assert on the text and the column layout
// without depending on whether the environment reports a color profile.
func plain(s string) string { return ansiRE.ReplaceAllString(s, "") }

// lines splits a rendered view into its rows.
func lines(s string) []string { return strings.Split(s, "\n") }

// renderBrowser builds a browser of the given size over ents, with its clock pinned so
// the modified-time column is the same in every year the tests are run.
func renderBrowser(t *testing.T, w, h int, ents ...sftpx.Entry) *Browser {
	t.Helper()
	fixed := time.Date(2026, time.August, 16, 12, 0, 0, 0, time.Local)
	old := now
	now = func() time.Time { return fixed }
	t.Cleanup(func() { now = old })

	b := &Browser{
		client: fbStub{ents},
		alias:  "web1",
		w:      w,
		h:      h,
	}
	return plant(b, "/home/u", ents)
}

// row builds the node a renderRow test is about: one entry at the given depth, hanging
// off the browser's root so it has a path the marks and the target can name.
func row(b *Browser, e sftpx.Entry, depth int) *node {
	n := newNode(b.root, e)
	n.depth = depth
	return n
}

// fbStub is the Client a rendering test needs: enough to build a browser, nothing more.
type fbStub struct{ ents []sftpx.Entry }

func (s fbStub) Home() (string, error)                                    { return "/home/u", nil }
func (s fbStub) List(string) ([]sftpx.Entry, error)                       { return s.ents, nil }
func (fbStub) DownloadProgress(_, _ string, _ func(int64)) (int64, error) { return 0, nil }
func (fbStub) UploadProgress(_, _ string, _ func(int64)) (int64, error)   { return 0, nil }
func (fbStub) Mkdir(string) error                                         { return nil }
func (fbStub) Remove(string) error                                        { return nil }
func (fbStub) Rename(_, _ string) error                                   { return nil }
func (fbStub) Copy(_, _ string, _ func(int64)) (int64, error)             { return 0, nil }
func (fbStub) Move(_, _ string, _ func(int64)) error                      { return nil }
func (fbStub) Close() error                                               { return nil }

// The view is a fixed frame: the path, a rule, the content rows, and the footer on the
// last line whatever the listing holds. A pane that changed height between renders would
// tear the layout around it.
func TestViewFillsItsBox(t *testing.T) {
	b := renderBrowser(t, 40, 8, sftpx.Entry{Name: "a.txt", Size: 10})

	got := lines(b.View())
	if len(got) != 8 {
		t.Fatalf("view has %d lines, want the full height of 8:\n%s", len(got), b.View())
	}
	if plain(got[0]) != "/home/u" {
		t.Fatalf("first line = %q, want the cwd", plain(got[0]))
	}
	if want := strings.Repeat("─", 40); plain(got[1]) != want {
		t.Fatalf("second line = %q, want a full-width rule", plain(got[1]))
	}
	if !strings.Contains(plain(got[2]), "a.txt") {
		t.Fatalf("first content row = %q, want the entry", plain(got[2]))
	}
	for _, l := range got {
		if lipgloss.Width(plain(l)) > 40 {
			t.Fatalf("line %q is wider than the pane", plain(l))
		}
	}
}

// A pane with no room renders nothing rather than a negative-width frame.
func TestViewOfAZeroSizedPane(t *testing.T) {
	for _, size := range [][2]int{{0, 10}, {40, 0}, {-1, -1}} {
		b := renderBrowser(t, size[0], size[1], sftpx.Entry{Name: "a.txt"})
		if got := b.View(); got != "" {
			t.Fatalf("View() at %v = %q, want empty", size, got)
		}
	}
}

// An empty directory says so: a blank content area would read as "still loading".
func TestViewOfAnEmptyListing(t *testing.T) {
	b := renderBrowser(t, 40, 8)
	if !strings.Contains(plain(b.View()), "(empty)") {
		t.Fatalf("empty listing did not say so:\n%s", b.View())
	}
}

// Only the window of entries starting at the scroll offset is drawn, so a listing longer
// than the pane cannot spill past the footer.
func TestViewShowsOnlyTheScrolledWindow(t *testing.T) {
	ents := make([]sftpx.Entry, 20)
	for i := range ents {
		ents[i] = sftpx.Entry{Name: "f" + string(rune('a'+i)), Size: 1}
	}
	b := renderBrowser(t, 40, 8, ents...) // contentRows() == 5
	b.scroll, b.cursor = 3, 3

	view := plain(b.View())
	for _, name := range []string{"fd", "fe", "ff", "fg", "fh"} {
		if !strings.Contains(view, name) {
			t.Fatalf("view is missing %q:\n%s", name, view)
		}
	}
	for _, name := range []string{"fa", "fb", "fc", "fi"} {
		if strings.Contains(view, name) {
			t.Fatalf("view shows %q, which is outside the window:\n%s", name, view)
		}
	}
}

// A row carries the entry's name, then its size and modified time pushed to the right
// edge. A directory gets a trailing slash, a twisty and no size — it has none to report —
// but keeps its time column.
func TestRenderRowColumns(t *testing.T) {
	b := renderBrowser(t, 40, 8)
	mtime := time.Date(2026, time.March, 4, 9, 5, 0, 0, time.Local).Unix()

	file := plain(b.renderRow(row(b, sftpx.Entry{Name: "notes.txt", Size: 2048, ModTime: mtime}, 0), false))
	// Two cells of gutter and two of the twisty column, which a file leaves blank so its
	// name starts in the same column as the directory beside it.
	if !strings.HasPrefix(file, "    notes.txt") {
		t.Fatalf("file row = %q, want the name after the gutter and the twisty column", file)
	}
	if !strings.HasSuffix(file, "2.0K Mar 04 09:05") {
		t.Fatalf("file row = %q, want the size and time at the right edge", file)
	}

	dir := plain(b.renderRow(row(b, sftpx.Entry{Name: "src", IsDir: true, ModTime: mtime}, 0), false))
	if !strings.HasPrefix(dir, "  ▸ src/") {
		t.Fatalf("dir row = %q, want a closed twisty and a trailing slash", dir)
	}
	if !strings.HasSuffix(dir, "Mar 04 09:05") {
		t.Fatalf("dir row = %q, want the time column kept", dir)
	}
	if strings.Contains(dir, "B ") {
		t.Fatalf("dir row = %q, want no size for a directory", dir)
	}

	// An open directory says so in the same cell, so the twisty is the only thing that
	// changes and the names stay in their column.
	opened := row(b, sftpx.Entry{Name: "src", IsDir: true}, 0)
	opened.expanded = true
	if got := plain(b.renderRow(opened, false)); !strings.HasPrefix(got, "  ▾ src/") {
		t.Fatalf("open dir row = %q, want an open twisty", got)
	}

	// Depth is two more cells per level, in front of the twisty.
	deep := plain(b.renderRow(row(b, sftpx.Entry{Name: "notes.txt", Size: 1}, 2), false))
	if !strings.HasPrefix(deep, "        notes.txt") {
		t.Fatalf("row at depth 2 = %q, want it indented four cells further", deep)
	}

	// An entry the server reported no mtime for gets no time column at all.
	noTime := plain(b.renderRow(row(b, sftpx.Entry{Name: "notes.txt", Size: 2048}, 0), false))
	if !strings.HasSuffix(noTime, "2.0K") {
		t.Fatalf("row without an mtime = %q, want the size alone", noTime)
	}
}

// The selected row is marked in the gutter, which is what the eye tracks while moving;
// a marked row is ticked in the cell beside it. The two are separate cells because a row
// is very often both, and the columns must stay aligned whichever it is.
func TestRenderRowGutterHoldsCursorAndMark(t *testing.T) {
	b := renderBrowser(t, 40, 8)
	n := row(b, sftpx.Entry{Name: "notes.txt", Size: 100}, 0)

	sel := plain(b.renderRow(n, true))
	unsel := plain(b.renderRow(n, false))
	if !strings.HasPrefix(sel, "▎ ") {
		t.Fatalf("selected row = %q, want the accent bar in the gutter", sel)
	}
	if !strings.HasPrefix(unsel, "  ") || strings.Contains(unsel, "▎") {
		t.Fatalf("unselected row = %q, want a blank gutter", unsel)
	}

	b.marks = map[string]bool{n.path: true}
	marked := plain(b.renderRow(n, false))
	both := plain(b.renderRow(n, true))
	if !strings.HasPrefix(marked, " ✓") {
		t.Fatalf("marked row = %q, want the tick in the second gutter cell", marked)
	}
	if !strings.HasPrefix(both, "▎✓") {
		t.Fatalf("marked and selected row = %q, want the bar and the tick side by side", both)
	}
	for _, r := range []string{sel, marked, both} {
		if lipgloss.Width(r) != lipgloss.Width(unsel) {
			t.Fatalf("the gutter changed the row width: %q vs %q", r, unsel)
		}
	}
}

// The target directory is told apart by colour rather than by a column: at sidebar widths
// there is no cell to spare, and there is only ever one target to find.
func TestRenderRowShowsTheTarget(t *testing.T) {
	b := renderBrowser(t, 40, 8)
	n := row(b, sftpx.Entry{Name: "dst", IsDir: true}, 0)

	before := b.renderRow(n, false)
	b.target = n.path
	after := b.renderRow(n, false)
	if plain(before) != plain(after) {
		t.Fatalf("the target marker cost a column: %q vs %q", plain(before), plain(after))
	}
}

// As the pane narrows the right-hand columns are dropped before the name is: a truncated
// name is still recognisable, a truncated size column is noise. The time goes first, then
// the size, and the name never falls below a readable stub.
func TestRenderRowDropsColumnsAsThePaneNarrows(t *testing.T) {
	mtime := time.Date(2026, time.March, 4, 9, 5, 0, 0, time.Local).Unix()
	e := sftpx.Entry{Name: "a-fairly-long-file-name.txt", Size: 2048, ModTime: mtime}

	widths := []struct {
		w             int
		wantSize      bool
		wantTime      bool
		wantNameChars int // how much of the name must survive
	}{
		{60, true, true, 12},
		{30, true, false, 12},
		{16, false, false, 4},
	}
	for _, c := range widths {
		b := renderBrowser(t, c.w, 8)
		line := plain(b.renderRow(row(b, e, 0), false))
		if lipgloss.Width(line) > c.w {
			t.Fatalf("w=%d: row %q is wider than the pane", c.w, line)
		}
		if got := strings.Contains(line, "Mar 04 09:05"); got != c.wantTime {
			t.Fatalf("w=%d: time column present = %v, want %v (%q)", c.w, got, c.wantTime, line)
		}
		if got := strings.Contains(line, "2.0K"); got != c.wantSize {
			t.Fatalf("w=%d: size column present = %v, want %v (%q)", c.w, got, c.wantSize, line)
		}
		if !strings.Contains(line, e.Name[:c.wantNameChars]) {
			t.Fatalf("w=%d: row %q lost too much of the name", c.w, line)
		}
	}
}

// The footer is one row and one message: an open prompt owns it while it is asking,
// because the keyboard is answering it and nothing else; a fresh note comes next; a
// running transfer only shows through once nothing more urgent is on the line. The row
// stays present when there is nothing to say, which is what holds the pane's height.
func TestFooterLinePriority(t *testing.T) {
	b := renderBrowser(t, 40, 8)
	if got := b.footerLine(40); got != "" {
		t.Fatalf("idle footer = %q, want an empty row", got)
	}

	b.note = note{text: "renamed"}
	if got := plain(b.footerLine(40)); got != "renamed" {
		t.Fatalf("footer = %q, want the standing note", got)
	}

	b.xfer = &transfer{}
	if got := plain(b.footerLine(40)); got == "renamed" {
		t.Fatalf("footer = %q, want the running transfer to take the row", got)
	}

	b.note = note{text: "just happened", until: time.Now().Add(time.Minute)}
	if got := plain(b.footerLine(40)); got != "just happened" {
		t.Fatalf("footer = %q, want the live note over the transfer", got)
	}

	b.askConfirm("Delete notes.txt?", func(*Browser, string) tea.Cmd { return nil })
	if got := plain(b.footerLine(40)); !strings.Contains(got, "Delete notes.txt?") {
		t.Fatalf("footer = %q, want the open question", got)
	}
}

// Everything the footer shows comes from the remote or from an error text, so it is
// control-stripped and cut to the pane like every other line.
func TestFooterLineIsFittedAndStripped(t *testing.T) {
	b := renderBrowser(t, 20, 8)
	b.note = note{text: "\x1b[2Jremote said something far too long to fit", err: true}

	got := plain(b.footerLine(20))
	if strings.Contains(got, "\x1b") {
		t.Fatalf("footer leaked a control sequence: %q", got)
	}
	if lipgloss.Width(got) > 20 {
		t.Fatalf("footer = %q, wider than the pane", got)
	}
	if !strings.HasSuffix(got, "…") {
		t.Fatalf("footer = %q, want an ellipsis where it was cut", got)
	}
}

func TestHumanizeBytes(t *testing.T) {
	cases := []struct {
		n    int64
		want string
	}{
		{0, "0B"},
		{999, "999B"},
		{1023, "1023B"},
		{1024, "1.0K"},
		{1536, "1.5K"},
		{1024 * 1024, "1.0M"},
		{1024 * 1024 * 1024, "1.0G"},
		// The scale stops at G, as the doc comment says: a terabyte-sized file keeps
		// counting in gibibytes rather than wrapping around to a smaller-looking number.
		{5 * 1024 * 1024 * 1024 * 1024, "5120.0G"},
	}
	for _, c := range cases {
		if got := humanizeBytes(c.n); got != c.want {
			t.Errorf("humanizeBytes(%d) = %q, want %q", c.n, got, c.want)
		}
	}
}

func TestTruncateText(t *testing.T) {
	cases := []struct {
		name string
		s    string
		w    int
		want string
	}{
		{"fits", "notes.txt", 20, "notes.txt"},
		{"exact fit", "notes", 5, "notes"},
		{"cut", "notes.txt", 5, "note…"},
		{"one cell", "notes", 1, "…"},
		{"no room", "notes", 0, ""},
		{"negative", "notes", -3, ""},
		{"empty", "", 5, ""},
		// Two wide runes are 4 cells; a third would overflow the 4 left by the ellipsis.
		{"wide runes count double", "ああああ", 5, "ああ…"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := truncateText(c.s, c.w)
			if got != c.want {
				t.Fatalf("truncateText(%q,%d) = %q, want %q", c.s, c.w, got, c.want)
			}
			if lipgloss.Width(got) > max(c.w, 0) {
				t.Fatalf("truncateText(%q,%d) = %q, wider than %d", c.s, c.w, got, c.w)
			}
		})
	}
}

// A path is cut from the front: the directory you are in is the tail, and it is the part
// that tells you where you are.
func TestTruncPath(t *testing.T) {
	cases := []struct {
		name string
		p    string
		w    int
		want string
	}{
		{"fits", "/home/u", 20, "/home/u"},
		{"exact fit", "/home/u", 7, "/home/u"},
		{"keeps the tail", "/home/u/projects/hop/internal", 12, "…/p/internal"},
		{"no room for the marker", "/home/u/projects", 2, "/…"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := truncPath(c.p, c.w)
			if got != c.want {
				t.Fatalf("truncPath(%q,%d) = %q, want %q", c.p, c.w, got, c.want)
			}
			if lipgloss.Width(got) > c.w {
				t.Fatalf("truncPath(%q,%d) = %q, wider than %d", c.p, c.w, got, c.w)
			}
		})
	}
}

// A remote name can carry anything; none of it may reach the user's terminal as a
// sequence. stripControl removes C0, DEL and C1 and leaves the printable text.
func TestStripControl(t *testing.T) {
	cases := []struct{ in, want string }{
		{"plain", "plain"},
		{"a\x1b[31mb", "a[31mb"},
		{"a\x00\x07\x1f b", "a b"},
		{"tab\there", "tabhere"},
		{"del\x7fnow", "delnow"},
		{"c1\u009bnow", "c1now"},
		{"ünïcode ok", "ünïcode ok"},
	}
	for _, c := range cases {
		if got := stripControl(c.in); got != c.want {
			t.Errorf("stripControl(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// ---- the tree, drawn ----

// nestedBrowser is a browser over a tree three levels deep with every level open, sized
// as the caller asks. It is the shape the sidebar has to survive.
func nestedBrowser(t *testing.T, w, h int) *Browser {
	t.Helper()
	c := &dirClient{dirs: map[string][]sftpx.Entry{
		"/home/u":                {dir("project"), {Name: "readme.md", Size: 120}},
		"/home/u/project":        {dir("internal"), {Name: "go.mod", Size: 40}},
		"/home/u/project/intern": {},
	}}
	c.dirs["/home/u/project/internal"] = []sftpx.Entry{{Name: "filebrowser.go", Size: 26482}}

	b := &Browser{client: c, alias: "web1", w: w, h: h}
	if !b.load("/home/u") {
		t.Fatalf("load: %s", b.note.text)
	}
	b.Select(0)
	b.Do(keys.In) // project
	b.Select(1)
	b.Do(keys.In) // project/internal
	return b
}

// View still renders a flat list of rows whatever the tree is doing, and every one of
// them is drawn at its own depth. The row order is the order the flattened tree is in,
// which is the order the mouse indexes.
func TestViewRendersTheOpenTreeFlat(t *testing.T) {
	b := nestedBrowser(t, 60, 10)

	got := lines(plain(b.View()))
	// Each row is the twisty (open, closed or blank for a file) after two cells of indent
	// per level, so the depth reads off the left edge.
	want := []string{"▾ project/", "  ▾ internal/", "    filebrowser.go", "  go.mod", "  readme.md"}
	for i, w := range want {
		row := got[i+2] // past the path header and the rule
		if !strings.Contains(row, w) {
			t.Fatalf("row %d = %q, want it to hold %q", i, row, w)
		}
	}
}

// The pane is becoming a sidebar, so it has to stay readable at widths where the size and
// time columns cannot fit at all: nothing wraps, nothing overruns, and every name is still
// on screen in some form.
func TestTreeFitsANarrowPane(t *testing.T) {
	for _, w := range []int{28, 34, 40} {
		b := nestedBrowser(t, w, 10)
		view := b.View()

		for _, l := range lines(view) {
			if lipgloss.Width(plain(l)) > w {
				t.Fatalf("w=%d: line %q is wider than the pane", w, plain(l))
			}
		}
		if got := len(lines(view)); got != 10 {
			t.Fatalf("w=%d: view has %d lines, want the full height", w, got)
		}
		// The deepest name is indented six cells and still has to leave a stub behind.
		if !strings.Contains(plain(view), "filebrow") {
			t.Fatalf("w=%d: the deepest row lost its name:\n%s", w, plain(view))
		}
	}
}

// Once nothing more urgent wants the footer it says what an operation would act on: the
// size of the selection, and where the target is aimed. Both are otherwise carried by a
// one-cell tick and a colour.
func TestFooterShowsMarksAndTarget(t *testing.T) {
	b := renderBrowser(t, 40, 8, sftpx.Entry{Name: "a.txt", Size: 1}, sftpx.Entry{Name: "b.txt", Size: 2})

	if got := b.footerLine(40); got != "" {
		t.Fatalf("idle footer = %q, want an empty row", got)
	}

	b.marks = map[string]bool{"/home/u/a.txt": true, "/home/u/b.txt": true}
	if got := plain(b.footerLine(40)); !strings.Contains(got, "2 marked") {
		t.Fatalf("footer = %q, want the count of marked entries", got)
	}

	b.target = "/home/u/dst"
	got := plain(b.footerLine(40))
	if !strings.Contains(got, "2 marked") || !strings.Contains(got, "/home/u/dst") {
		t.Fatalf("footer = %q, want both the count and the target", got)
	}

	// It is the last thing in the order: an outcome the user just caused outranks it.
	b.note = note{text: "deleted a.txt"}
	if got := plain(b.footerLine(40)); got != "deleted a.txt" {
		t.Fatalf("footer = %q, want the note over the standing summary", got)
	}
}
