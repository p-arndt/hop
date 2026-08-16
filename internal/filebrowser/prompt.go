package filebrowser

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// The browser's one-line modal. Every key that needs an answer before it can act — a
// name to rename to, a path to upload, a yes before an overwrite — asks on the status
// line rather than in a card: the listing behind it is the context for the question,
// and a card would cover the very row being renamed.
//
// While an overlay is up it owns the keyboard: Handle sends the key here first, so "d"
// typed into a filename is a "d" and not a download. The exits are esc and enter.

type overlayKind int

const (
	overlayNone overlayKind = iota
	// overlayInput reads a line of text. enter answers with it, esc cancels.
	overlayInput
	// overlayConfirm reads a yes/no. "y" answers, anything else cancels — a confirm
	// swallows the key it declined rather than letting it fall through to the listing.
	overlayConfirm
)

// overlay is the state of the open question: what was asked, what has been typed, and
// what to do with the answer. done is called only when the user answered; cancelling
// drops the overlay and calls nothing.
type overlay struct {
	kind  overlayKind
	label string
	value string
	done  func(b *Browser, value string) tea.Cmd
}

// active reports whether a question is open and owns the keyboard.
func (o *overlay) active() bool { return o.kind != overlayNone }

// ask opens a text prompt labelled label, pre-filled with initial (the cursor sits at
// its end, so a rename starts from the current name and edits it).
func (b *Browser) ask(label, initial string, done func(*Browser, string) tea.Cmd) {
	b.overlay = overlay{kind: overlayInput, label: label, value: initial, done: done}
	b.status, b.statusErr = "", false
}

// askConfirm opens a yes/no question. done receives "y".
func (b *Browser) askConfirm(label string, done func(*Browser, string) tea.Cmd) {
	b.overlay = overlay{kind: overlayConfirm, label: label, done: done}
	b.status, b.statusErr = "", false
}

// closeOverlay drops the open question without answering it.
func (b *Browser) closeOverlay() { b.overlay = overlay{} }

// overlayKey applies a key to the open question, reporting whether it consumed it. It
// consumes every key while one is open: that is what "owns the keyboard" means.
func (b *Browser) overlayKey(key string) (tea.Cmd, bool) {
	if !b.overlay.active() {
		return nil, false
	}

	if b.overlay.kind == overlayConfirm {
		done := b.overlay.done
		b.closeOverlay()
		if key == "y" || key == "Y" {
			return done(b, "y"), true
		}
		return nil, true
	}

	switch key {
	case "esc":
		b.closeOverlay()

	case "enter":
		value := strings.TrimSpace(b.overlay.value)
		done := b.overlay.done
		b.closeOverlay()
		if value == "" {
			// An empty answer is a cancel, not an operation on "".
			return nil, true
		}
		return done(b, value), true

	case "backspace":
		if r := []rune(b.overlay.value); len(r) > 0 {
			b.overlay.value = string(r[:len(r)-1])
		}

	case "ctrl+u":
		// The same "clear the line" every other hop input has.
		b.overlay.value = ""

	case "space":
		b.overlay.value += " "

	default:
		// Single printable runes only. A chord or a named key is swallowed rather than
		// typed: "ctrl+d" must never end up in a filename.
		if r := []rune(key); len(r) == 1 && r[0] >= 0x20 && r[0] != 0x7f {
			b.overlay.value += key
		}
	}
	return nil, true
}

// view renders the question onto the status line: the label in accent, the typed text
// after it, and a block for the cursor on an input.
func (o *overlay) view(w int) string {
	if !o.active() || w <= 0 {
		return ""
	}
	text := o.value
	if o.kind == overlayInput {
		text += "█"
	}
	label := o.label + " "
	avail := w - lipgloss.Width(label)
	if avail < 1 {
		return accentStyle.Render(truncateText(o.label, w))
	}
	return accentStyle.Render(label) + truncateText(stripControl(text), avail)
}
