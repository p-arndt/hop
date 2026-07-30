package terminal

// Copying out of a pane.
//
// The other half of copy-and-paste, and the harder one: the text being copied is
// on the far end of an SSH connection, and the clipboard is here. The convention
// every terminal settled on for that is OSC 52 — a program that wants to put
// something on your clipboard writes it, base64-encoded, to its own standard
// output:
//
//	ESC ] 52 ; c ; <base64> BEL      (or ST, ESC \, as the terminator)
//
// vim's `set clipboard=unnamed` with a compatible clipboard provider, tmux's
// `set -g set-clipboard on`, and yank/copy plugins of every kind emit it. hop
// watches the same byte stream it watches for OSC 7 (see cwd.go), decodes the
// payload and hands it to a sink the TUI installs, which is what writes the local
// clipboard — see internal/clipboard.
//
// The sink is optional and it is a setting, because this is a channel from a
// remote machine to your desktop: everything running on the far end can write
// your clipboard through it, including the parts of it you did not start. Off,
// the sequence is decoded and dropped.

import (
	"encoding/base64"
	"strings"
	"unicode/utf8"
)

// oscClipPrefix introduces a clipboard write. It is also what oscScanner.cap
// recognises to give the payload room for a clipboard rather than a path.
const oscClipPrefix = "52;"

// SetClipboardSink installs the function a clipboard write from the remote is
// handed to, replacing any previous one; nil switches the feature off. It is
// called on the pane's output pump, off the UI goroutine, so what is installed
// must be safe to call from there.
func (p *Pane) SetClipboardSink(sink func(string)) {
	p.clipMu.Lock()
	defer p.clipMu.Unlock()
	p.clipSink = sink
}

// copyOut hands a decoded clipboard write to the sink, off the output pump —
// writing the system clipboard means talking to a desktop (or spawning a helper),
// and the pump behind this call is the one thing keeping the pane's screen up to
// date.
//
// It is one worker with a mailbox of one, rather than a goroutine per sequence.
// The far end decides how often this is called, and a host emitting OSC 52 in a
// loop would otherwise have hop forking a clipboard helper as fast as it can read —
// with the writes racing each other, so the clipboard could even end up holding the
// older text. Serialised, a burst costs one write and one pending write: the
// latest text wins, which is what a clipboard means.
func (p *Pane) copyOut(text string) {
	p.clipMu.Lock()
	defer p.clipMu.Unlock()

	if p.clipSink == nil {
		return
	}
	// Replace whatever was queued: it is superseded text nobody has asked for yet.
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

// drainClipboard runs the sink until the mailbox is empty, then stands down. The
// flag it clears is taken under the same lock copyOut queues under, so a write
// arriving as this one finishes either finds the worker still running or starts
// another — never neither.
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
// The payload is "52;<targets>;<base64>". The targets name which of the X
// selections to write — "c" for the clipboard, "p" for the primary, "s" for
// whichever is configured — and are ignored: a desktop that has only one
// clipboard is the common case, and hop writes the one clipboard it can.
//
// A payload whose data is "?" is a *read* of the clipboard rather than a write.
// hop does not answer it: the reply would put the contents of the local clipboard
// on the wire to the remote host on the remote's say-so, which is the one thing
// this direction of the feature must not do without being asked. It is recognised
// here only so it is not mistaken for a write of the literal text "?".
//
// The decoded text is refused unless it is valid UTF-8, and the control
// characters are dropped from it apart from tab and newline. What lands on the
// clipboard is pasted somewhere else later — into another terminal, most likely —
// and text that carries escape sequences into that paste is not text.
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

// decodeBase64 decodes the payload's data, tolerating the two things emitters
// disagree about: the padding (tmux pads, some plugins do not) and the line
// breaks a long payload is sometimes folded with.
func decodeBase64(data string) ([]byte, error) {
	data = strings.NewReplacer("\n", "", "\r", "", " ", "").Replace(data)
	if b, err := base64.StdEncoding.DecodeString(data); err == nil {
		return b, nil
	}
	return base64.RawStdEncoding.DecodeString(data)
}
