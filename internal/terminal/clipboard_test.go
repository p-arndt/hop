package terminal

import (
	"encoding/base64"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"hop/internal/sshx"
)

// osc52 builds the sequence a remote program emits to put text on the clipboard.
func osc52(text string) string {
	return "\x1b]52;c;" + base64.StdEncoding.EncodeToString([]byte(text)) + "\x07"
}

func TestClipboardWriteReachesTheSink(t *testing.T) {
	out, w := io.Pipe()
	p := New(&sshx.Session{Stdin: &syncBuf{}, Stdout: out}, 80, 24, nil)
	defer p.Close()

	var mu sync.Mutex
	var got string
	p.SetClipboardSink(func(text string) {
		mu.Lock()
		defer mu.Unlock()
		got = text
	})
	read := func() string {
		mu.Lock()
		defer mu.Unlock()
		return got
	}

	go io.WriteString(w, "some output"+osc52("yanked line")+"more output")
	if !waitFor(func() bool { return read() == "yanked line" }) {
		t.Fatalf("the sink saw %q, want the yanked text", read())
	}
}

// With no sink installed the sequence is parsed and dropped without stalling the pump.
func TestClipboardWriteWithoutASinkIsDropped(t *testing.T) {
	out, w := io.Pipe()
	p := New(&sshx.Session{Stdin: &syncBuf{}, Stdout: out}, 80, 24, nil)
	defer p.Close()

	go io.WriteString(w, osc52("nowhere")+"\x1b]7;file:///srv\x07")
	// The OSC 7 behind it still lands: the tell that the scanner carried on.
	if !waitForCwd(p, "/srv") {
		t.Fatal("the scanner stopped at a clipboard write it had nowhere to put")
	}
}

// A burst gets one worker behind a one-deep mailbox: the latest text wins.
func TestClipboardWritesAreSerialised(t *testing.T) {
	out, w := io.Pipe()
	p := New(&sshx.Session{Stdin: &syncBuf{}, Stdout: out}, 80, 24, nil)
	defer p.Close()

	var mu sync.Mutex
	var live, peak, calls int
	var last string
	p.SetClipboardSink(func(text string) {
		mu.Lock()
		live++
		calls++
		if live > peak {
			peak = live
		}
		last = text
		mu.Unlock()

		time.Sleep(5 * time.Millisecond)

		mu.Lock()
		live--
		mu.Unlock()
	})

	var burst strings.Builder
	for i := 0; i < 50; i++ {
		burst.WriteString(osc52("yank " + string(rune('a'+i%26))))
	}
	burst.WriteString(osc52("last"))
	go io.WriteString(w, burst.String())

	read := func() (string, int, int) {
		mu.Lock()
		defer mu.Unlock()
		return last, peak, calls
	}
	if !waitFor(func() bool { s, _, _ := read(); return s == "last" }) {
		t.Fatalf("the last write never landed (%q)", func() string { s, _, _ := read(); return s }())
	}

	text, peaked, called := read()
	if peaked > 1 {
		t.Fatalf("%d sinks ran at once, want one at a time", peaked)
	}
	if called > 51 {
		t.Fatalf("the sink ran %d times for 51 writes", called)
	}
	if text != "last" {
		t.Fatalf("the clipboard ended up holding %q, want the last write", text)
	}
}

func TestParseOSC52(t *testing.T) {
	tests := []struct {
		name    string
		payload string
		want    string
		ok      bool
	}{
		{
			name:    "a clipboard write",
			payload: "52;c;" + base64.StdEncoding.EncodeToString([]byte("hello")),
			want:    "hello",
			ok:      true,
		},
		{
			name:    "the selection named is ignored: hop writes the one clipboard it has",
			payload: "52;p;" + base64.StdEncoding.EncodeToString([]byte("primary")),
			want:    "primary",
			ok:      true,
		},
		{
			name:    "unpadded base64, which some emitters send",
			payload: "52;c;" + base64.RawStdEncoding.EncodeToString([]byte("hello")),
			want:    "hello",
			ok:      true,
		},
		{
			name:    "a long payload folded across lines",
			payload: "52;c;aGVsbG8g\nd29ybGQ=",
			want:    "hello world",
			ok:      true,
		},
		{
			name:    "clearing the clipboard is a write of nothing",
			payload: "52;c;",
			want:    "",
			ok:      true,
		},
		{
			name:    "a read is refused rather than taken for the text \"?\"",
			payload: "52;c;?",
			ok:      false,
		},
		{
			name:    "an OSC 7 is not one of these",
			payload: "7;file:///srv",
			ok:      false,
		},
		{
			name:    "a payload that is not base64",
			payload: "52;c;not base64 at all!!",
			ok:      false,
		},
		{
			name:    "newlines and tabs are text and survive",
			payload: "52;c;" + base64.StdEncoding.EncodeToString([]byte("one\ntwo\tthree")),
			want:    "one\ntwo\tthree",
			ok:      true,
		},
		{
			name:    "escape sequences do not: what is pasted elsewhere later must be text",
			payload: "52;c;" + base64.StdEncoding.EncodeToString([]byte("a\x1b[31mred\x07")),
			want:    "a[31mred",
			ok:      true,
		},
		{
			name:    "invalid UTF-8 is refused outright",
			payload: "52;c;" + base64.StdEncoding.EncodeToString([]byte{0xff, 0xfe}),
			ok:      false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := parseOSC52(tt.payload)
			if ok != tt.ok {
				t.Fatalf("parseOSC52(%q) ok = %v, want %v", tt.payload, ok, tt.ok)
			}
			if ok && got != tt.want {
				t.Fatalf("parseOSC52(%q) = %q, want %q", tt.payload, got, tt.want)
			}
		})
	}
}

// A clipboard payload is long enough that the network will split it across chunks.
func TestScannerTakesAClipboardWriteInPieces(t *testing.T) {
	seq := osc52("split down the middle")
	var s oscScanner

	for i := 0; i < len(seq)-1; i++ {
		s.feed([]byte(seq[i : i+1]))
		if _, ok := s.tookClipboard(); ok {
			t.Fatalf("the write was reported at byte %d, before its terminator", i)
		}
	}

	s.feed([]byte(seq[len(seq)-1:]))
	text, ok := s.tookClipboard()
	if !ok || text != "split down the middle" {
		t.Fatalf("after the terminator: %q, %v — want the whole text", text, ok)
	}
	if _, ok := s.tookClipboard(); ok {
		t.Fatal("the same write was reported twice")
	}
}

// Clipboard payloads get a larger cap than OSC 7's path's worth.
func TestClipboardPayloadCapIsGenerous(t *testing.T) {
	big := strings.Repeat("x", 64*1024)
	var s oscScanner

	s.feed([]byte(osc52(big)))
	text, ok := s.tookClipboard()
	if !ok || text != big {
		t.Fatalf("a %d-byte yank did not survive the payload cap (got %d bytes, ok %v)",
			len(big), len(text), ok)
	}
}

func TestOversizeClipboardPayloadIsDropped(t *testing.T) {
	var s oscScanner
	s.feed([]byte("\x1b]52;c;" + strings.Repeat("A", maxClipPayload+16) + "\x07"))
	if text, ok := s.tookClipboard(); ok {
		t.Fatalf("an over-long payload was reported anyway (%d bytes)", len(text))
	}
}

// An ordinary OSC does not inherit the clipboard's larger cap.
func TestOversizeOSC7PayloadIsStillDropped(t *testing.T) {
	var s oscScanner
	dir, ok := s.feed([]byte("\x1b]7;file:///" + strings.Repeat("d", maxOSCPayload+16) + "\x07"))
	if ok {
		t.Fatalf("an over-long OSC 7 reported %q", dir)
	}
}
