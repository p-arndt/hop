package filebrowser

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// The browser's one-line modal: while an overlay is up it owns the keyboard, and esc or enter exits.

type overlayKind int

const (
	overlayNone overlayKind = iota
	// overlayInput reads a line of text. enter answers with it, esc cancels.
	overlayInput
	// overlayConfirm reads a yes/no: "y" answers, anything else cancels and is swallowed.
	overlayConfirm
)

// overlay is the state of the open question; done is called only when the user answered.
type overlay struct {
	kind  overlayKind
	label string
	value string
	done  func(b *Browser, value string) tea.Cmd
}

// active reports whether a question is open and owns the keyboard.
func (o *overlay) active() bool { return o.kind != overlayNone }

// Prompting reports whether a question is open, so the enclosing model hands over keys like "," and "?" that the browser does not bind.
func (b *Browser) Prompting() bool { return b.overlay.active() }

// ask opens a text prompt labelled label, pre-filled with initial.
func (b *Browser) ask(label, initial string, done func(*Browser, string) tea.Cmd) {
	b.overlay = overlay{kind: overlayInput, label: label, value: initial, done: done}
	b.clearNote()
}

// askConfirm opens a yes/no question. done receives "y".
func (b *Browser) askConfirm(label string, done func(*Browser, string) tea.Cmd) {
	b.overlay = overlay{kind: overlayConfirm, label: label, done: done}
	b.clearNote()
}

// closeOverlay drops the open question without answering it.
func (b *Browser) closeOverlay() { b.overlay = overlay{} }

// overlayKey applies a key to the open question and consumes every key while one is open.
// It reads msg.Runes, not String(): key names are not text, and a paste arrives several runes at a time.
func (b *Browser) overlayKey(msg tea.KeyMsg) (tea.Cmd, bool) {
	if !b.overlay.active() {
		return nil, false
	}
	key := msg.String()

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
		b.overlay.value = ""

	default:
		// Typed text only: a chord or named key carries no runes, so "ctrl+d" cannot end up in a filename.
		if len(msg.Runes) > 0 {
			b.overlay.value += string(msg.Runes)
		}
	}
	return nil, true
}

// view renders the question onto the status line.
func (o *overlay) view(w int) string {
	if !o.active() || w <= 0 {
		return ""
	}
	text := o.value
	if o.kind == overlayInput {
		text += "█"
	}
	// The label is stripped like the text: it carries remote filenames one keystroke from being answered.
	label := stripControl(o.label) + " "
	avail := w - lipgloss.Width(label)
	if avail < 1 {
		return accentStyle.Render(truncateText(stripControl(o.label), w))
	}
	return accentStyle.Render(label) + truncateText(stripControl(text), avail)
}
