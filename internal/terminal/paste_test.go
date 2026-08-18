package terminal

import (
	"io"
	"strings"
	"testing"
	"unicode/utf8"

	"hop/internal/sshx"
)

// A shell that has not asked to be told about pastes gets the text and nothing else; one
// that asks gets it wrapped, and stops when it leaves.
func TestBracketedPasteFollowsTheRemote(t *testing.T) {
	out, w := io.Pipe()
	stdin := &syncBuf{}
	p := New(&sshx.Session{Stdin: stdin, Stdout: out}, 80, 24, nil)
	defer p.Close()

	if p.BracketedPaste() {
		t.Fatal("a fresh shell claims to have asked about pastes")
	}
	p.SendPaste("ls\n")
	p.Flush()
	if got := stdin.String(); got != "ls\r" {
		t.Fatalf("unbracketed paste wrote %q, want %q", got, "ls\r")
	}

	go io.WriteString(w, "\x1b[?2004h")
	if !waitFor(p.BracketedPaste) {
		t.Fatal("a program asking to be told about pastes was not noticed")
	}

	stdin.reset()
	p.SendPaste("ls\n")
	p.Flush()
	want := "\x1b[200~ls\r\x1b[201~"
	if got := stdin.String(); got != want {
		t.Fatalf("bracketed paste wrote %q, want %q", got, want)
	}

	go io.WriteString(w, "\x1b[?2004l")
	if !waitFor(func() bool { return !p.BracketedPaste() }) {
		t.Fatal("a program leaving bracketed paste was not noticed")
	}
}

// A full reset takes the mode with it. The emulator rewrites its mode map without telling
// anyone, so the scan for RIS is the only warning — and a stale shadow would put
// ESC[200~ on a command line nobody is reading brackets on.
func TestResetClearsBracketedPaste(t *testing.T) {
	out, w := io.Pipe()
	p := New(&sshx.Session{Stdin: &syncBuf{}, Stdout: out}, 80, 24, nil)
	defer p.Close()

	go io.WriteString(w, "\x1b[?2004h")
	if !waitFor(p.BracketedPaste) {
		t.Fatal("the mode was never picked up")
	}

	go io.WriteString(w, "\x1bc")
	if !waitFor(func() bool { return !p.BracketedPaste() }) {
		t.Fatal("a full reset left the mode set")
	}
}

// A paste of nothing writes nothing at all: the brackets alone would knock a program
// into and out of paste mode for no text.
func TestEmptyPasteWritesNothing(t *testing.T) {
	stdin := &syncBuf{}
	p := New(&sshx.Session{Stdin: stdin, Stdout: strings.NewReader("")}, 80, 24, nil)
	defer p.Close()

	p.SendPaste("")
	p.SendPaste("\x00\x1b")
	if got := stdin.String(); got != "" {
		t.Fatalf("an empty paste wrote %q", got)
	}
}

// The payload: line endings become the carriage return a pty reads as Enter, and what is
// done with the control characters turns on whether the far end knows this is a paste.
func TestPasteText(t *testing.T) {
	tests := []struct {
		name      string
		in        string
		bracketed bool
		want      string
	}{
		{
			name: "crlf becomes one carriage return",
			in:   "one\r\ntwo\r\n",
			want: "one\rtwo\r",
		},
		{
			name: "a bare linefeed becomes a carriage return",
			in:   "one\ntwo",
			want: "one\rtwo",
		},
		{
			name: "unbracketed, an escape is dropped rather than dropping vim out of insert",
			in:   "a\x1b[31mred\x1b[0m",
			want: "a[31mred[0m",
		},
		{
			name: "unbracketed, a control byte is dropped rather than run as a chord",
			in:   "rm -rf /\x03tmp",
			want: "rm -rf /tmp",
		},
		{
			name: "unbracketed, tabs survive: they are text in a paste",
			in:   "a\tb",
			want: "a\tb",
		},
		{
			name:      "bracketed, the file's own escapes go through",
			in:        "a\x1b[31mred",
			bracketed: true,
			want:      "a\x1b[31mred",
		},
		{
			name:      "bracketed, an embedded terminator cannot end the paste early",
			in:        "safe\x1b[201~rm -rf /",
			bracketed: true,
			want:      "saferm -rf /",
		},
		{
			name:      "bracketed, an embedded introducer goes too",
			in:        "safe\x1b[200~more",
			bracketed: true,
			want:      "safemore",
		},
		{
			// One pass splices a whole terminator out of the bytes left behind, ending
			// the paste early. The strip repeats until the text stops changing.
			name:      "bracketed, a terminator hidden inside a terminator goes too",
			in:        "safe\x1b[2\x1b[201~01~rm -rf /",
			bracketed: true,
			want:      "saferm -rf /",
		},
		{
			name:      "bracketed, nested twice over",
			in:        "a\x1b[2\x1b[2\x1b[201~01~01~b",
			bracketed: true,
			want:      "ab",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := pasteText(tt.in, tt.bracketed); got != tt.want {
				t.Fatalf("pasteText(%q, %v) = %q, want %q", tt.in, tt.bracketed, got, tt.want)
			}
		})
	}
}

// A Go string can hold any bytes at all, and a clipboard filled from a terminal showing
// mojibake holds exactly that. What comes out of here is always characters: the far end
// is a UTF-8 pty.
func TestPasteDropsBytesThatAreNotCharacters(t *testing.T) {
	// A truncated UTF-8 sequence between two words: what a half-copied emoji is.
	raw := "echo \xf0\x9f hi"

	for _, bracketed := range []bool{false, true} {
		got := pasteText(raw, bracketed)
		if !utf8.ValidString(got) {
			t.Fatalf("bracketed=%v: pasteText returned invalid UTF-8: %q", bracketed, got)
		}
		if !strings.Contains(got, "echo ") || !strings.Contains(got, "hi") {
			t.Fatalf("bracketed=%v: the text either side of the bad bytes was lost: %q", bracketed, got)
		}
	}
}
