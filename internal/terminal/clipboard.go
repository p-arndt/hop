package terminal

// Copying out of a pane — the harder half of copy-and-paste, since the text is on the far
// end of an SSH connection and the clipboard is here. The convention every terminal
// settled on is OSC 52: a program writes the text, base64-encoded, to its own stdout:
//
//	ESC ] 52 ; c ; <base64> BEL      (or ST, ESC \, as the terminator)
//
// hop watches the same byte stream it watches for OSC 7 (see cwd.go), decodes the payload
// and hands it to a sink the TUI installs — see internal/clipboard.
//
// The sink is optional and a setting, because this is a channel from a remote machine to
// your desktop: everything running on the far end can write your clipboard through it.
// Off, the sequence is decoded and dropped.

import (
	"encoding/base64"
	"strings"
	"unicode/utf8"
)

// oscClipPrefix introduces a clipboard write, and is what oscScanner.cap recognises to
// give the payload room for a clipboard rather than a path.
const oscClipPrefix = "52;"

// SetClipboardSink installs the function a clipboard write from the remote is handed to;
// nil switches the feature off. It is called on the pane's output pump, off the UI
// goroutine, so what is installed must be safe to call from there.
func (p *Pane) SetClipboardSink(sink func(string)) {
	p.clipMu.Lock()
	defer p.clipMu.Unlock()
	p.clipSink = sink
}

// copyOut hands a decoded clipboard write to the sink, off the output pump — writing the
// system clipboard may mean spawning a helper, and the pump is what keeps the pane's
// screen up to date.
//
// One worker with a mailbox of one, rather than a goroutine per sequence: the far end
// decides how often this is called, and racing writes could leave the clipboard holding
// the older text. Serialised, a burst costs one write and one pending write.
func (p *Pane) copyOut(text string) {
	p.clipMu.Lock()
	defer p.clipMu.Unlock()

	if p.clipSink == nil {
		return
	}
	// Replace whatever was queued: superseded text nobody has asked for yet.
	select {
	case <-p.clipQueue:
	default:
	}
	p.clipQueue <- text

	if p.clipBusy {
		return
	}
	p.clipBusy = true
	go p.drainClipboard()
}

// drainClipboard runs the sink until the mailbox is empty, then stands down. The flag it
// clears is taken under copyOut's lock, so a write arriving as it finishes either finds
// the worker running or starts another — never neither.
func (p *Pane) drainClipboard() {
	for {
		p.clipMu.Lock()
		select {
		case text := <-p.clipQueue:
			sink := p.clipSink
			p.clipMu.Unlock()
			if sink != nil {
				sink(text)
			}
		default:
			p.clipBusy = false
			p.clipMu.Unlock()
			return
		}
	}
}

// parseOSC52 pulls the text out of an OSC payload, or reports that this is not a
// clipboard write carrying any.
//
// The payload is "52;<targets>;<base64>". The targets name which X selection to write and
// are ignored: hop writes the one clipboard it can.
//
// Data of "?" is a read of the clipboard rather than a write. hop does not answer it —
// that would put the local clipboard on the wire on the remote's say-so — and recognises
// it here only so it is not mistaken for a write of the literal "?".
//
// The decoded text is refused unless it is valid UTF-8, and control characters other than
// tab and newline are dropped: what lands on the clipboard is pasted somewhere else later.
func parseOSC52(payload string) (string, bool) {
	rest, ok := strings.CutPrefix(payload, oscClipPrefix)
	if !ok {
		return "", false
	}
	i := strings.IndexByte(rest, ';')
	if i < 0 {
		return "", false
	}
	data := rest[i+1:]
	if data == "?" {
		return "", false
	}

	raw, err := decodeBase64(data)
	if err != nil {
		return "", false
	}
	text := string(raw)
	if !utf8.ValidString(text) {
		return "", false
	}

	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")
	return strings.Map(func(r rune) rune {
		switch {
		case r == '\t' || r == '\n':
			return r
		case r < 0x20 || r == 0x7f:
			return -1
		}
		return r
	}, text), true
}

// decodeBase64 decodes the payload's data, tolerating the two things emitters disagree
// about: the padding, and the line breaks a long payload is sometimes folded with.
func decodeBase64(data string) ([]byte, error) {
	data = strings.NewReplacer("\n", "", "\r", "", " ", "").Replace(data)
	if b, err := base64.StdEncoding.DecodeString(data); err == nil {
		return b, nil
	}
	return base64.RawStdEncoding.DecodeString(data)
}
