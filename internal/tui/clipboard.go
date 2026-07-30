package tui

// The copy half of copy-and-paste: text yanked on the remote host arriving on the
// local clipboard.
//
// The pane does the recognising — a remote program asks for this with OSC 52, and
// internal/terminal/clipboard.go decodes it — and hands the text to a sink hop
// installs here. This file is that sink: it is what knows there is a setting, and
// it is where the desktop is finally written to.
//
// It runs on the pane's output pump rather than on the UI goroutine, which is why
// the setting is read through an atomic and why nothing here touches the model.
// Failure is silent on purpose: a Linux box with no clipboard helper installed is
// a normal thing to be sitting at, and a status line reporting it on every yank
// would be noise about a machine that is not going to change.

import (
	"hop/internal/clipboard"
	"hop/internal/terminal"
)

// applyClipboard brings the panes' view of the setting in line with the config.
// Called at startup and after every settings save.
func (m *model) applyClipboard() {
	m.clipOK.Store(m.cfg.Clipboard)
}

// armClipboard installs the sink on a pane that has just landed. The closure
// captures the model only to read the atomic, which is safe from the pump
// goroutine it will be called on — and it is consulted at *write* time rather than
// here, so switching the setting off takes effect on panes that are already open.
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

// writeClipboard is what actually writes the desktop's clipboard: the real one,
// unless a test has put something else in its place. Nothing else in hop reaches
// the local clipboard, so this is the whole seam.
func (m *model) writeClipboard() func(string) error {
	if m.clipWrite != nil {
		return m.clipWrite
	}
	return clipboard.Write
}
