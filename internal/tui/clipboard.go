package tui

// The pane decodes the remote host's OSC 52 (internal/terminal/clipboard.go) and hands the
// text to the sink installed here. The sink runs on the pane's output pump, not the UI
// goroutine, so the setting is read through an atomic and nothing here touches the model.

import (
	"hop/internal/clipboard"
	"hop/internal/terminal"
)

// applyClipboard brings the panes' view of the setting in line with the config.
func (m *model) applyClipboard() {
	m.clipOK.Store(m.cfg.Clipboard)
}

// armClipboard installs the sink on a pane that has just landed. The atomic is read at
// write time, so switching the setting off reaches panes that are already open.
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

// writeClipboard writes the desktop's clipboard — the real one, unless a test replaced it.
func (m *model) writeClipboard() func(string) error {
	if m.clipWrite != nil {
		return m.clipWrite
	}
	return clipboard.Write
}
