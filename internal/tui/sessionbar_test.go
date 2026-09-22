package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

// cellFor finds the bar's cell for t, failing when it is not on the bar.
func cellFor(t *testing.T, m *model, want target) barCell {
	t.Helper()
	for _, c := range m.sessionBar() {
		if c.jump && c.t == want {
			return c
		}
	}
	t.Fatalf("no cell for %+v on %q", want, ansi.Strip(m.renderHeader()))
	return barCell{}
}

func TestSessionBarNamesTheHostInFrontAndWhatIsOpenOnIt(t *testing.T) {
	m, _ := placesModel(t)
	m.width = 160

	head := ansi.Strip(m.renderHeader())

	for _, want := range []string{"ha", "$1", "▤ srv", "✎ a.conf", "✎ nginx.conf", "● hb"} {
		if !strings.Contains(head, want) {
			t.Fatalf("the header is missing %q: %q", want, head)
		}
	}
	if strings.Contains(head, "sessions") {
		t.Fatalf("the header still counts sessions: %q", head)
	}
	if !strings.HasSuffix(strings.TrimRight(head, " "), "● hb") {
		t.Fatalf("the other hosts are not on the right: %q", head)
	}
}

func TestSessionBarHighlightsTheKeyboardsChip(t *testing.T) {
	m, s := placesModel(t)
	m.width = 160
	s.focusTab(1)
	m.mode = modeEditor

	c := cellFor(t, m, target{alias: "ha", kind: targetEditor, id: 12})

	if c.text != tabActive.Render("✎ nginx.conf") {
		t.Fatalf("the focused tab's chip is not highlighted: %q", c.text)
	}
	if other := cellFor(t, m, target{alias: "ha", kind: targetEditor, id: 11}); other.text != tabInactive.Render("✎ a.conf") {
		t.Fatalf("an unfocused chip is highlighted: %q", other.text)
	}
}

func TestSessionBarWithNoHostInFrontShowsEveryConnectedHost(t *testing.T) {
	m := switchModel(t)
	m.leaveAll()

	head := ansi.Strip(m.renderHeader())

	if !strings.HasPrefix(head, " hop ") || !strings.Contains(head, "● ha") || !strings.Contains(head, "● hb") {
		t.Fatalf("header = %q, want hop and both connected hosts", head)
	}
	if strings.Contains(head, "hc") {
		t.Fatalf("header = %q names a host with no session", head)
	}
}

// A transient status takes the right side while it lasts; the host's own chips stay.
func TestSessionBarGivesTheRightSideToAStatus(t *testing.T) {
	m, _ := placesModel(t)
	m.width = 160
	m.setStatus(statusOK, "connected to hb")

	head := ansi.Strip(m.renderHeader())

	if !strings.Contains(head, "✓ connected to hb") || !strings.Contains(head, "✎ a.conf") {
		t.Fatalf("header = %q, want the status beside ha's chips", head)
	}
	if strings.Contains(head, "● hb") {
		t.Fatalf("header = %q, the other hosts should give way to the status", head)
	}
}

func TestSessionBarDropsFromTheFarEndWhenNarrow(t *testing.T) {
	m, s := placesModel(t)
	m.width = 40
	s.activeSh = 0

	head := ansi.Strip(m.renderHeader())

	if len([]rune(head)) != 40 {
		t.Fatalf("header is %d wide in a 40-column window: %q", len([]rune(head)), head)
	}
	if !strings.Contains(head, "$1") || strings.Contains(head, "nginx") || !strings.Contains(head, "+") {
		t.Fatalf("header = %q, want the first chips kept and a +N for the rest", head)
	}
}

func TestSessionBarKeepsTheKeyboardsChipWhenNarrow(t *testing.T) {
	m, s := placesModel(t)
	m.width = 40
	s.focusTab(1)
	m.mode = modeEditor

	if head := ansi.Strip(m.renderHeader()); !strings.Contains(head, "nginx") {
		t.Fatalf("header = %q dropped the chip the keyboard is in", head)
	}
}

// Render and hit-test read the same cells, so every cell is drawn exactly where it is clicked.
func TestSessionBarCellsAreWhereTheyAreDrawn(t *testing.T) {
	for _, w := range []int{30, 60, 100, 160} {
		m, _ := placesModel(t)
		m.width = w
		head := []rune(ansi.Strip(m.renderHeader()))
		for _, c := range m.sessionBar() {
			want := ansi.Strip(c.text)
			if got := string(head[c.x : c.x+c.w]); got != want {
				t.Fatalf("width %d: the cell %q is drawn as %q", w, want, got)
			}
		}
	}
}

func TestClickingAChipJumpsThere(t *testing.T) {
	m, s := placesModel(t)
	m.width = 160
	c := cellFor(t, m, target{alias: "ha", kind: targetEditor, id: 12})

	m.handleMouse(click(c.x+1, 0))

	if m.active != "ha" || m.mode != modeEditor || s.editor().id != 12 {
		t.Fatalf("active = %q, mode = %v; want ha's nginx.conf", m.active, m.mode)
	}
}

func TestClickingAHostJumpsToItsLastPlace(t *testing.T) {
	m, _ := placesModel(t)
	m.width = 160
	c := cellFor(t, m, target{alias: "hb"})

	m.handleMouse(click(c.x, 0))

	if m.active != "hb" || m.mode != modeShell {
		t.Fatalf("active = %q, mode = %v; want hb's shell", m.active, m.mode)
	}
}

func TestClickingTheOverflowOpensTheSwitcher(t *testing.T) {
	m, _ := placesModel(t)
	m.width = 40
	for _, c := range m.sessionBar() {
		if c.more {
			m.handleMouse(click(c.x, 0))
			if !m.hostSwitch.open {
				t.Fatal("clicking +N did not open the switcher")
			}
			return
		}
	}
	t.Fatalf("no +N on %q", ansi.Strip(m.renderHeader()))
}
