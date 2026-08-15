package tui

// The copy half of copy-and-paste: text yanked on the remote host arriving on the local
// clipboard.
//
// The pane recognises the OSC 52 and decodes it (internal/terminal/clipboard.go), then
// hands the text to the sink installed here — which is what knows there is a setting, and
// where the desktop is finally written to.
//
// It runs on the pane's output pump rather than the UI goroutine, which is why the setting
// is read through an atomic and nothing here touches the model. Failure is silent: a Linux
// box with no clipboard helper is a normal thing to be sitting at.

import (
	"hop/internal/clipboard"
	"hop/internal/terminal"
)

// applyClipboard brings the panes' view of the setting in line with the config, at
// startup and after every settings save.
func (m *model) applyClipboard() {
	m.clipOK.Store(m.cfg.Clipboard)
}

// armClipboard installs the sink on a pane that has just landed. The closure captures the
// model only to read the atomic, and reads it at write time, so switching the setting off
// takes effect on panes that are already open.
func (m *model) armClipboard(p *terminal.Pane) {
	if p == nil {
		return
	}
	p.SetClipboardSink(func(text string) {
		if !m.clipOK.Load() {
			return
		}
		_ = m.writeClipboard()(text)
	})
}

// writeClipboard writes the desktop's clipboard — the real one, unless a test has
// replaced it. Nothing else in hop reaches the local clipboard.
func (m *model) writeClipboard() func(string) error {
	if m.clipWrite != nil {
		return m.clipWrite
	}
	return clipboard.Write
}
