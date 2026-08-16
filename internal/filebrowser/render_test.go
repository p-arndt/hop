package filebrowser

import (
	"regexp"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

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

	return &Browser{
		client:  fbStub{ents},
		alias:   "web1",
		cwd:     "/home/u",
		entries: ents,
		w:       w,
		h:       h,
	}
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
// edge. A directory gets a trailing slash and no size — it has none to report — but keeps
// its time column.
func TestRenderRowColumns(t *testing.T) {
	b := renderBrowser(t, 40, 8)
	mtime := time.Date(2026, time.March, 4, 9, 5, 0, 0, time.Local).Unix()

	file := plain(b.renderRow(sftpx.Entry{Name: "notes.txt", Size: 2048, ModTime: mtime}, false))
	if !strings.HasPrefix(file, "  notes.txt") {
		t.Fatalf("file row = %q, want the name after the unselected gutter", file)
	}
	if !strings.HasSuffix(file, "2.0K Mar 04 09:05") {
		t.Fatalf("file row = %q, want the size and time at the right edge", file)
	}

	dir := plain(b.renderRow(sftpx.Entry{Name: "src", IsDir: true, ModTime: mtime}, false))
	if !strings.HasPrefix(dir, "  src/") {
		t.Fatalf("dir row = %q, want a trailing slash", dir)
	}
	if !strings.HasSuffix(dir, "Mar 04 09:05") {
		t.Fatalf("dir row = %q, want the time column kept", dir)
	}
	if strings.Contains(dir, "B ") {
		t.Fatalf("dir row = %q, want no size for a directory", dir)
	}

	// An entry the server reported no mtime for gets no time column at all.
	noTime := plain(b.renderRow(sftpx.Entry{Name: "notes.txt", Size: 2048}, false))
	if !strings.HasSuffix(noTime, "2.0K") {
		t.Fatalf("row without an mtime = %q, want the size alone", noTime)
	}
}

// The selected row is marked in the gutter, which is what the eye tracks while moving:
// the bar is the only structural difference, so the columns stay aligned with the rows
// above and below.
func TestRenderRowMarksTheSelection(t *testing.T) {
	b := renderBrowser(t, 40, 8)
	e := sftpx.Entry{Name: "notes.txt", Size: 100}

	sel := plain(b.renderRow(e, true))
	unsel := plain(b.renderRow(e, false))
	if !strings.HasPrefix(sel, "▎ ") {
		t.Fatalf("selected row = %q, want the accent bar in the gutter", sel)
	}
	if !strings.HasPrefix(unsel, "  ") || strings.Contains(unsel, "▎") {
		t.Fatalf("unselected row = %q, want a blank gutter", unsel)
	}
	if lipgloss.Width(sel) != lipgloss.Width(unsel) {
		t.Fatalf("selection changed the row width: %q vs %q", sel, unsel)
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
		row := plain(b.renderRow(e, false))
		if lipgloss.Width(row) > c.w {
			t.Fatalf("w=%d: row %q is wider than the pane", c.w, row)
		}
		if got := strings.Contains(row, "Mar 04 09:05"); got != c.wantTime {
			t.Fatalf("w=%d: time column present = %v, want %v (%q)", c.w, got, c.wantTime, row)
		}
		if got := strings.Contains(row, "2.0K"); got != c.wantSize {
			t.Fatalf("w=%d: size column present = %v, want %v (%q)", c.w, got, c.wantSize, row)
		}
		if !strings.Contains(row, e.Name[:c.wantNameChars]) {
			t.Fatalf("w=%d: row %q lost too much of the name", c.w, row)
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
