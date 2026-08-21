package terminal

// Copying out of a pane, via OSC 52: a program writes the text base64-encoded to its own
// stdout as ESC ] 52 ; c ; <base64> BEL (or ST). The sink is optional and a setting, since
// it is a channel from the remote machine to your desktop's clipboard.

import (
	"encoding/base64"
	"strings"
	"unicode/utf8"
)

// oscClipPrefix is what oscScanner.cap recognises to give the payload clipboard-sized room.
const oscClipPrefix = "52;"

// SetClipboardSink installs the sink for clipboard writes from the remote; nil switches the
// feature off. Called on the output pump, so the sink must be safe off the UI goroutine.
func (p *Pane) SetClipboardSink(sink func(string)) {
	p.clipMu.Lock()
	defer p.clipMu.Unlock()
	p.clipSink = sink
}

// copyOut hands a decoded clipboard write to the sink off the output pump, which must stay
// free to keep the pane's screen up to date. One worker with a mailbox of one, so racing
// writes cannot leave the clipboard holding the older text.
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

// drainClipboard runs the sink until the mailbox is empty. The flag it clears is taken
// under copyOut's lock, so a write arriving as it finishes never finds no worker at all.
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

// parseOSC52 pulls the text out of a "52;<targets>;<base64>" payload. The targets name an
// X selection and are ignored. Data of "?" is a clipboard read, which hop refuses to
// answer; it is recognised here only so it is not taken for a write of a literal "?".
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

// decodeBase64 tolerates the two things emitters disagree about: the padding, and the line
// breaks a long payload is sometimes folded with.
func decodeBase64(data string) ([]byte, error) {
	data = strings.NewReplacer("\n", "", "\r", "", " ", "").Replace(data)
	if b, err := base64.StdEncoding.DecodeString(data); err == nil {
		return b, nil
	}
	return base64.RawStdEncoding.DecodeString(data)
}
