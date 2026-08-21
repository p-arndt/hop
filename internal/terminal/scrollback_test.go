package terminal

import (
	"fmt"
	"strings"
	"testing"

	"github.com/charmbracelet/x/vt"
)

// stripANSI drops the ESC[...m sequences the emulator wraps its glyphs in.
func stripANSI(s string) string {
	var b strings.Builder
	runes := []rune(s)
	for i := 0; i < len(runes); {
		if runes[i] == 0x1b && i+1 < len(runes) && runes[i+1] == '[' {
			j := i + 2
			for j < len(runes) && !(runes[j] >= 0x40 && runes[j] <= 0x7e) {
				j++
			}
			if j < len(runes) {
				j++
			}
			i = j
			continue
		}
		b.WriteRune(runes[i])
		i++
	}
	return b.String()
}

// newFilledPane builds a pane on a bare SafeEmulator and writes count numbered lines;
// with count above the row count the early ones land in scrollback.
func newFilledPane(t *testing.T, w, h, count int) *Pane {
	t.Helper()
	p := &Pane{emu: vt.NewSafeEmulator(w, h)}
	var sb strings.Builder
	for i := 0; i < count; i++ {
		fmt.Fprintf(&sb, "line-%d\r\n", i)
	}
	if _, err := p.emu.Write([]byte(sb.String())); err != nil {
		t.Fatalf("emu.Write: %v", err)
	}
	return p
}

func TestScrollbackPopulated(t *testing.T) {
	p := newFilledPane(t, 80, 24, 60)
	if got := p.ScrollbackLen(); got <= 0 {
		t.Fatalf("ScrollbackLen() = %d, want > 0 after writing 60 lines to a 24-row screen", got)
	}
}

func TestScrollOffsetStartsLive(t *testing.T) {
	p := newFilledPane(t, 80, 24, 60)
	if got := p.ScrollOffset(); got != 0 {
		t.Fatalf("ScrollOffset() = %d, want 0 initially", got)
	}
	if !p.AtBottom() {
		t.Fatalf("AtBottom() = false, want true initially")
	}
}

func TestScrollUpDownClamp(t *testing.T) {
	p := newFilledPane(t, 80, 24, 60)

	p.ScrollUp(5)
	if got := p.ScrollOffset(); got != 5 {
		t.Fatalf("after ScrollUp(5): offset = %d, want 5", got)
	}

	p.ScrollDown(2)
	if got := p.ScrollOffset(); got != 3 {
		t.Fatalf("after ScrollDown(2): offset = %d, want 3", got)
	}

	p.ScrollDown(100)
	if got := p.ScrollOffset(); got != 0 {
		t.Fatalf("after ScrollDown(100): offset = %d, want 0 (clamped)", got)
	}
	if !p.AtBottom() {
		t.Fatalf("AtBottom() = false after clamping to 0, want true")
	}

	p.ScrollUp(4)
	p.ScrollUp(0)
	p.ScrollUp(-3)
	if got := p.ScrollOffset(); got != 4 {
		t.Fatalf("after ScrollUp(4), ScrollUp(0), ScrollUp(-3): offset = %d, want 4", got)
	}
}

func TestScrollTopBottomClamp(t *testing.T) {
	p := newFilledPane(t, 80, 24, 60)
	sbLen := p.ScrollbackLen()

	p.ScrollUp(10_000)
	if got := p.ScrollOffset(); got != sbLen {
		t.Fatalf("after ScrollUp(10000): offset = %d, want ScrollbackLen() = %d", got, sbLen)
	}

	p.ScrollToBottom()
	if got := p.ScrollOffset(); got != 0 {
		t.Fatalf("after ScrollToBottom(): offset = %d, want 0", got)
	}

	p.ScrollToTop()
	if got := p.ScrollOffset(); got != sbLen {
		t.Fatalf("after ScrollToTop(): offset = %d, want ScrollbackLen() = %d", got, sbLen)
	}
}

func TestViewScrollbackLiveEqualsScreen(t *testing.T) {
	p := newFilledPane(t, 80, 24, 60)

	view := p.ViewScrollback()
	lines := strings.Split(view, "\n")
	if len(lines) != p.emu.Height() {
		t.Fatalf("ViewScrollback() has %d lines, want emu.Height() = %d", len(lines), p.emu.Height())
	}

	// At offset 0 the window is the live screen.
	if !strings.Contains(view, "line-59") {
		t.Fatalf("live view is missing the newest line 'line-59':\n%s", stripANSI(view))
	}
	if strings.Contains(view, "line-0\r") || containsExactToken(view, "line-0") {
		t.Fatalf("live view unexpectedly contains the oldest line 'line-0':\n%s", stripANSI(view))
	}

	// At offset 0 it must equal the live render.
	if got, want := stripANSI(p.ViewScrollback()), stripANSI(p.emu.Render()); got != want {
		t.Fatalf("ViewScrollback() at offset 0 != live Render()\n got:\n%s\nwant:\n%s", got, want)
	}
}

func TestViewScrollbackScrolledUpSurfacesHistory(t *testing.T) {
	p := newFilledPane(t, 80, 24, 60)

	live := p.ViewScrollback()
	if containsExactToken(live, "line-1") {
		t.Fatalf("precondition failed: 'line-1' is already visible live:\n%s", stripANSI(live))
	}

	p.ScrollToTop()
	top := p.ViewScrollback()

	lines := strings.Split(top, "\n")
	if len(lines) != p.emu.Height() {
		t.Fatalf("scrolled-up view has %d lines, want emu.Height() = %d", len(lines), p.emu.Height())
	}

	if !containsExactToken(top, "line-1") {
		t.Fatalf("scrolled-to-top view does not surface early 'line-1':\n%s", stripANSI(top))
	}
}

// containsExactToken matches name as a whole token: "line-1" but not "line-10".
func containsExactToken(view, name string) bool {
	for _, ln := range strings.Split(stripANSI(view), "\n") {
		for _, f := range strings.Fields(ln) {
			if f == name {
				return true
			}
		}
	}
	return false
}
