package terminal

import (
	"fmt"
	"strings"
	"testing"

	"github.com/charmbracelet/x/vt"
)

// ansiRE-free stripping: the rendered lines carry SGR escape sequences around the
// glyphs, but the plain text (e.g. "line-3") sits between them literally, so the
// tests mostly search for substrings directly. stripANSI is here for the one place
// a whole visible line is wanted without styling — it walks the same ESC[...m shape
// the emulator emits and drops it.
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

// newFilledPane builds an in-package pane on a bare SafeEmulator (no SSH needed)
// and writes count distinct numbered lines through the parser. With a 24-row screen
// and count well above 24, the early lines are pushed off the top into scrollback.
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

	// Non-positive n is a no-op guard.
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

	// At offset 0 the window is the live screen: the newest line shows, the very
	// oldest (long since scrolled off) does not.
	if !strings.Contains(view, "line-59") {
		t.Fatalf("live view is missing the newest line 'line-59':\n%s", stripANSI(view))
	}
	if strings.Contains(view, "line-0\r") || containsExactToken(view, "line-0") {
		t.Fatalf("live view unexpectedly contains the oldest line 'line-0':\n%s", stripANSI(view))
	}

	// It must also equal View() with the cursor overlay removed — the property the
	// whole windowing scheme rests on at offset 0.
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

	// An early line that was off-screen live must now be in the window — proof the
	// window actually moved up into history.
	if !containsExactToken(top, "line-1") {
		t.Fatalf("scrolled-to-top view does not surface early 'line-1':\n%s", stripANSI(top))
	}
}

// containsExactToken reports whether the plain text of view contains name as a
// whole token — "line-1" but not the "line-1" inside "line-10". It splits on the
// hyphen-number shape the fixtures use so a prefix match cannot masquerade.
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
