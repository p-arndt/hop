package terminal

// Working-directory tracking for a shell pane. SSH does not report the remote
// cwd; only the shell can, which it does over OSC 7:
//
//	ESC ] 7 ; file://host/current/dir BEL      (or ST, ESC \, as the terminator)
//
// oscScanner watches the output stream for it; TrackCwd installs the prompt hook
// that emits it. All best effort: no report leaves Cwd empty, which callers treat
// as "unknown" rather than as an error.

import (
	"fmt"
	"net/url"
	"path"
	"strings"
	"time"

	"hop/internal/sshx"
)

// Cwd is the shell's current directory as last reported over OSC 7, or "" if
// nothing has reported one.
func (p *Pane) Cwd() string {
	p.cwdMu.Lock()
	defer p.cwdMu.Unlock()
	return p.cwd
}

// setCwd records a directory reported by the remote. Called from the output pump
// goroutine, read by the UI goroutine, hence the mutex.
func (p *Pane) setCwd(dir string) {
	p.cwdMu.Lock()
	p.cwd = dir
	p.cwdMu.Unlock()
}

// Caps on a buffered OSC payload — the remote is not to be trusted with an
// unbounded one. An over-long payload is failed outright, never truncated into a
// wrong path. maxClipPayload is generous because OSC 52 carries base64 and
// yanking a whole file is a normal thing to do.
const (
	maxOSCPayload  = 4096
	maxClipPayload = 1 << 20
)

// oscScanner incrementally scans the output stream for the two OSCs hop acts on:
// OSC 7 (working directory) and OSC 52 (clipboard write). State carries across
// chunks, so a sequence split mid-payload is still found. Deliberately not a full
// ANSI parser — the emulator stays the only thing that interprets the stream.
type oscScanner struct {
	state oscState
	buf   []byte
	// clip is the last clipboard write (OSC 52) seen; clipSet distinguishes "none"
	// from a deliberate clear. Last one wins: two writes in one chunk are two copies
	// in a row, and the second is what the clipboard would hold anyway.
	clip    string
	clipSet bool
	// over is set when the current payload outgrew maxOSCPayload, so the rest of
	// it is consumed and discarded rather than buffered.
	over bool
	// ris is set when a full terminal reset (RIS, ESC c — what `reset` sends) went
	// past, and cleared by tookReset. Watched here because the emulator will not
	// report it: fullReset rewrites the mode map directly, without the EnableMode /
	// DisableMode callbacks every other mode change comes through, so anything
	// shadowing those modes (see mouseState) would keep the pre-reset state.
	ris bool
}

type oscState int

const (
	oscGround  oscState = iota // ordinary output
	oscEsc                     // saw ESC, waiting to see whether an OSC follows
	oscBody                    // inside an OSC payload
	oscBodyEsc                 // inside an OSC payload, saw ESC (maybe the ST terminator)
)

// feed consumes a chunk of server output and returns the last directory reported
// within it. A chunk carrying no complete OSC 7 returns ok false, which includes
// the chunk that only carries the first half of one: the rest is remembered.
func (s *oscScanner) feed(b []byte) (string, bool) {
	var dir string
	var found bool

	for _, c := range b {
		switch s.state {
		case oscGround:
			if c == 0x1b {
				s.state = oscEsc
			}

		case oscEsc:
			switch c {
			case ']':
				s.state = oscBody
				s.buf = s.buf[:0]
				s.over = false
			case 0x1b:
				// ESC ESC: still waiting on the byte that says what this is.
			case 'c':
				// RIS, a full terminal reset — see ris.
				s.ris = true
				s.state = oscGround
			default:
				s.state = oscGround
			}

		case oscBody:
			switch c {
			case 0x07: // BEL terminator
				if d, ok := s.finish(); ok {
					dir, found = d, true
				}
			case 0x1b: // maybe ST (ESC \)
				s.state = oscBodyEsc
			default:
				s.push(c)
			}

		case oscBodyEsc:
			if c == '\\' { // ST terminator
				if d, ok := s.finish(); ok {
					dir, found = d, true
				}
				continue
			}
			// Not a terminator after all: the OSC was interrupted by another escape
			// sequence, which is malformed. Abandon the payload and re-read this byte
			// as the one following an ESC, so an OSC starting right here is not lost.
			s.buf = s.buf[:0]
			s.over = false
			switch c {
			case ']':
				s.state = oscBody
			case 0x1b:
				// ESC ESC inside a payload: still waiting on the byte that says what the
				// second one introduces, so stay where an ESC leaves us.
				s.state = oscEsc
			default:
				s.state = oscGround
			}
			continue
		}
	}
	return dir, found
}

// tookReset reports whether a full reset went past since it was last asked, and
// clears the flag. The cwd is deliberately not cleared with it: RIS resets the
// terminal, not the shell, which is standing where it always was.
func (s *oscScanner) tookReset() bool {
	took := s.ris
	s.ris = false
	return took
}

// push appends a payload byte, marking the payload over-long once it passes the
// cap instead of growing without bound.
func (s *oscScanner) push(c byte) {
	if s.over {
		return
	}
	if len(s.buf) >= s.cap() {
		s.over = true
		s.buf = s.buf[:0]
		return
	}
	s.buf = append(s.buf, c)
}

// cap picks the buffer limit from the payload's introducer: a clipboard write
// gets room for a clipboard, everything else room for a path. Before the
// introducer has arrived the larger cap applies — nothing is discarded on a guess.
func (s *oscScanner) cap() int {
	if len(s.buf) < len(oscClipPrefix) || string(s.buf[:len(oscClipPrefix)]) == oscClipPrefix {
		return maxClipPayload
	}
	return maxOSCPayload
}

// finish parses a completed payload and returns to ground state. It returns a
// directory when the payload was an OSC 7; a clipboard write is recorded on the
// scanner instead, to be taken by tookClipboard.
func (s *oscScanner) finish() (string, bool) {
	payload := string(s.buf)
	over := s.over
	s.buf = s.buf[:0]
	s.over = false
	s.state = oscGround
	if over {
		return "", false
	}
	if text, ok := parseOSC52(payload); ok {
		s.clip, s.clipSet = text, true
		return "", false
	}
	return parseOSC7(payload)
}

// tookClipboard reports the last clipboard write seen since it was last asked,
// and forgets it. Like tookReset, it is the way a sequence that is neither a
// directory nor an error leaves this scanner.
func (s *oscScanner) tookClipboard() (string, bool) {
	if !s.clipSet {
		return "", false
	}
	text := s.clip
	s.clip, s.clipSet = "", false
	return text, true
}

// parseOSC7 pulls the directory out of a "7;file://<host>/<path>" payload.
//
// The host is ignored — the caller already knows it, and shells disagree about
// sending a hostname, a FQDN or nothing. A percent-unescape that fails falls back
// to the literal path. Control characters are refused: the result reaches the
// status line and another program's command line.
func parseOSC7(payload string) (string, bool) {
	rest, ok := strings.CutPrefix(payload, "7;")
	if !ok {
		return "", false
	}
	rest, ok = strings.CutPrefix(rest, "file://")
	if !ok {
		return "", false
	}
	i := strings.IndexByte(rest, '/')
	if i < 0 {
		return "", false
	}
	dir := rest[i:]
	if unescaped, err := url.PathUnescape(dir); err == nil {
		dir = unescaped
	}
	if strings.ContainsFunc(dir, func(r rune) bool { return r < 0x20 || r == 0x7f }) {
		return "", false
	}
	return dir, true
}

// The prompt hooks hop types into a fresh shell: a function emitting OSC 7, hung
// off the prompt. One per shell rather than one self-detecting line, because this
// line is echoed at a real prompt and is the only part of the feature the user
// sees — so it is kept short.
//
// Each begins with \x15 (kill-line, so a half-typed line is not submitted along
// with it) and a space (for HISTCONTROL=ignorespace / HIST_IGNORE_SPACE), and
// ends with a call plus CR, so the cwd is known without waiting for the next
// prompt. A hook from the user's own rc-file is preserved, not replaced.
const (
	bashCwdHook = "\x15 hop_cwd() { printf '\\033]7;file://%s%s\\033\\\\' \"${HOSTNAME:-}\" \"$PWD\"; }; " +
		"PROMPT_COMMAND=\"hop_cwd${PROMPT_COMMAND:+;$PROMPT_COMMAND}\"; hop_cwd\r"

	zshCwdHook = "\x15 hop_cwd() { printf '\\033]7;file://%s%s\\033\\\\' \"${HOST:-}\" \"$PWD\"; }; " +
		"precmd_functions+=(hop_cwd); hop_cwd\r"
)

// cwdHookFor returns the hook for this login shell, or "" for anything but bash
// and zsh — they would answer with a parse error. The name may arrive with the
// leading "-" of a login shell and with the probe output's whitespace.
func cwdHookFor(shell string) string {
	switch strings.TrimPrefix(path.Base(strings.TrimSpace(shell)), "-") {
	case "bash":
		return bashCwdHook
	case "zsh":
		return zshCwdHook
	}
	return ""
}

// loginShellCmd asks the remote for the account's login shell, preferring the
// passwd entry over the occasionally stale $SHELL an exec channel inherits. It
// runs under an explicit `sh -c` because the account's own shell is what is in
// question — under fish or pwsh this would be a syntax error.
const loginShellCmd = `sh -c 'p=$(getent passwd "$(id -un)" 2>/dev/null | cut -d: -f7); ` +
	`[ -n "$p" ] || p="$SHELL"; printf %s "$p"'`

// hookDelay bounds the wait for the shell's first output before the hook is sent.
// Typing early is safe (the bytes queue in the tty buffer) but the echo would
// interleave with a login banner still printing. The timeout covers a shell that
// prints nothing at all.
const hookDelay = 2 * time.Second

// hookGrace is how long an already-integrated shell gets to say so before a hook
// is typed into it — a user whose rc-file emits OSC 7 should get nothing typed at
// all. Short, because it is also a window in which the user might start typing.
const hookGrace = 300 * time.Millisecond

// TrackCwd installs the OSC 7 prompt hook in this shell pane, so Cwd starts
// reporting. It returns immediately; the probe and injection run on their own
// goroutine, best effort throughout.
//
// Three shells get nothing typed into them: one that is neither bash nor zsh (a
// parse error), one already reporting OSC 7 from the user's own rc-file, and one
// whose screen is owned by a full-screen program.
//
// That last guard matters most. The probe reports what the account *has*, not what
// is on this pty: a `.bash_profile` ending in `exec tmux attach`, or an sshd
// ForceCommand, both answer "bash" and then hand the session elsewhere. Typing into
// that is noise at best and — vim in insert mode — an edit to someone's file at
// worst. Full-screen programs take the alt screen, so that is what is checked.
func (p *Pane) TrackCwd(cli *sshx.Client, startDir string) {
	if cli == nil {
		return
	}
	go func() {
		var hook string
		// A shell hop cannot identify still gets the cd — that line is one every shell
		// understands — it just gets no hook, since the hook is written per shell.
		if shell, err := cli.Output(loginShellCmd); err == nil {
			hook = cwdHookFor(shell)
		}
		if hook == "" && startDir == "" {
			return
		}
		p.waitFirstOutput(hookDelay)
		if p.isClosed() {
			return
		}
		// The grace comes before the alt-screen check, not after: it is 300 ms of the
		// session's first moments, and a full-screen program that takes the screen
		// inside that window has to be seen before anything is typed.
		reports := p.reportsCwd(hookGrace)
		if p.emu.IsAltScreen() {
			return
		}
		if reports {
			hook = ""
		}
		line := startupLine(startDir, hook)
		if line == "" {
			return
		}
		// The cwd report is what tells injectHook the line has run. It is only owed
		// when something is going to emit one: the hook hop is installing, or a shell
		// that was already reporting before hop typed anything.
		p.injectHook(line, hook != "" || reports)
	}()
}

// startupLine is the single line typed at a fresh shell's prompt: the cd into the
// host's default directory, the OSC 7 hook, or both — cd first, so the hook's
// trailing call reports where the session actually starts.
//
// Joined with ";" rather than "&&": a function definition is not a valid right-hand
// side of "&&" in bash, and a failed cd should still leave a reporting shell.
//
// A failed cd is deliberately not silenced. The shell's own "no such file or
// directory" lands inside the echoed span, which stops eraseEcho — so the line
// stays on screen with the reason next to it instead of being swallowed.
func startupLine(dir, hook string) string {
	if dir == "" {
		return hook
	}
	// The hooks' own "\x15 " prefix (kill-line + history-hiding space); the joined
	// line needs it exactly once, at the front.
	cd := "\x15 cd " + shellQuotePath(dir)
	if hook == "" {
		return cd + "\r"
	}
	return cd + "; " + strings.TrimPrefix(hook, "\x15 ")
}

// shellQuotePath renders dir as a single shell word, so a space or quote in it
// stays one argument and nothing in it is executed. A leading "~" is left outside
// the quotes, since a quoted tilde is a literal one; nothing else expands.
func shellQuotePath(dir string) string {
	prefix := ""
	switch {
	case dir == "~":
		return "~"
	case strings.HasPrefix(dir, "~/"):
		prefix, dir = "~/", strings.TrimPrefix(dir, "~/")
	}
	return prefix + "'" + strings.ReplaceAll(dir, "'", `'\''`) + "'"
}

// isClosed reports whether the pane has been closed under us. Every wait in the
// injection sequence is followed by one: the sequence spans seconds, and closing a
// shell tab takes one keystroke.
func (p *Pane) isClosed() bool {
	select {
	case <-p.closed:
		return true
	default:
		return false
	}
}

// injectHook types the hook at the shell's prompt, then removes the line the shell
// echoed back, so the integration leaves no trace in the pane.
//
// The echo cannot be prevented — the shell's line editor draws what it reads — but
// the emulator is in this process, so the rows can be deleted afterwards and the
// prompt slid up. Nothing is erased on a guess: see eraseEcho. A leftover line is a
// blemish; erasing the host's own output would be a defect.
func (p *Pane) injectHook(line string, expectReport bool) {
	pos := p.emu.CursorPosition()
	top, sbBefore := pos.Y, p.emu.ScrollbackLen()

	p.writeString(line)

	// The report says the hook ran, not that the screen is ready to measure: it is
	// emitted while the echoed line is still current, before the newline and prompt
	// that follow. So wait for the report first, then for the screen.
	if expectReport && !p.reportsCwd(hookRunWindow) {
		return
	}
	if !p.waitPromptBelow(top, sbBefore) {
		return
	}
	if p.isClosed() {
		return
	}
	p.eraseEcho(top, sbBefore, line)
}

// hookRunWindow is how long the hook is given to run — one round trip and a prompt
// — before its echo is left where it is.
const hookRunWindow = 3 * time.Second

// The screen is settled once the cursor has moved below the echo's first row and
// stayed put for promptQuiet. Measuring earlier counts too few rows, and an erase
// spanning too few rows leaves half the echo behind.
const (
	promptWait  = 3 * time.Second
	promptQuiet = 90 * time.Millisecond
	promptPoll  = 30 * time.Millisecond
)

// waitPromptBelow waits for the shell to finish drawing the prompt below the echo,
// reporting false if it never settles — in which case the echo is left alone rather
// than half-erased on a guess.
func (p *Pane) waitPromptBelow(top, sbBefore int) bool {
	deadline := time.Now().Add(promptWait)
	lastY, quiet := -1, time.Duration(0)

	for time.Now().Before(deadline) {
		if p.isClosed() {
			return false
		}
		row := p.emu.CursorPosition().Y
		floor := max(top-(p.emu.ScrollbackLen()-sbBefore), 0)

		switch {
		case row <= floor:
			// Still on the echoed line: the prompt is not down yet.
			lastY, quiet = -1, 0
		case row == lastY:
			if quiet += promptPoll; quiet >= promptQuiet {
				return true
			}
		default:
			lastY, quiet = row, 0
		}
		time.Sleep(promptPoll)
	}
	return false
}

// eraseEcho deletes the rows the hook was echoed onto and pulls what follows up —
// but only after reading those rows back and finding the hook and nothing else.
//
// The read-back is the whole safety of this: top was measured *before* the hook was
// written, and the host prints in the meantime (a slow dynamic MOTD lands inside
// the span). Trusting the span would delete that output.
//
// sbBefore is the scrollback length at that same moment; the difference against the
// current one is how far the screen scrolled, which has to come off top.
func (p *Pane) eraseEcho(top, sbBefore int, hook string) {
	cur := p.emu.CursorPosition()
	top -= p.emu.ScrollbackLen() - sbBefore
	if top < 0 {
		// The span scrolled off the top. Clamping only helps while the echo is still
		// there to recognise (holdsEchoOnly requires it to begin on the clamped row), so
		// a screen that scrolled past it keeps the echo in history, out of sight.
		top = 0
	}

	n := cur.Y - top
	if n <= 0 || n >= p.emu.Height() {
		return
	}
	if !p.holdsEchoOnly(top, cur.Y, hook) {
		return
	}

	// Park on the first row to go, delete n rows (pulling the prompt up to it), then
	// restore the cursor onto that prompt. CUP is 1-based; emulator positions are not.
	_, _ = p.emu.Write([]byte(fmt.Sprintf("\x1b[%d;1H\x1b[%dM\x1b[%d;%dH",
		top+1, n, top+1, cur.X+1)))
	if p.onOutput != nil {
		p.onOutput()
	}
}

// holdsEchoOnly reports whether rows [top, bottom) hold the echo of hook and
// nothing else.
//
// The echo is one long line wrapped across those rows, so joined end to end they
// read as a prompt followed by the hook verbatim. From the hook's first characters
// onward the span must be a prefix of the hook; a MOTD line or a background job's
// output breaks that prefix and the erase is declined.
//
// Spaces are dropped from both sides rather than matched, because the wrap can fall
// on one of the hook's own spaces and whether the emulator keeps or pads it is not
// worth depending on. Nothing else is dropped.
func (p *Pane) holdsEchoOnly(top, bottom int, hook string) bool {
	typed := squeeze(strings.TrimSuffix(strings.TrimPrefix(hook, "\x15"), "\r"))
	head := typed
	if len(head) > echoHeadLen {
		head = head[:echoHeadLen]
	}

	rows := strings.Split(p.emu.Render(), "\n")
	var joined strings.Builder
	for i := top; i < bottom && i < len(rows); i++ {
		joined.WriteString(squeeze(stripSGR(rows[i])))
	}

	echo := joined.String()
	i := strings.Index(echo, head)
	if i < 0 {
		return false
	}
	// The echo must begin on the span's *first* row, right after the prompt. Without
	// this, a span clamped up to row 0 would pass on an echo further down it and take
	// the login banner above along with it.
	if i >= len(squeeze(stripSGR(rowAt(rows, top)))) {
		return false
	}
	return strings.HasPrefix(typed, echo[i:])
}

// echoHeadLen is how much of the typed line locates the echo's start within the
// prompt row: long enough to be unmistakable, short enough to survive the row
// boundary the prompt may push it over.
const echoHeadLen = 16

// rowAt returns row i, or "" when the screen has no such row.
func rowAt(rows []string, i int) string {
	if i < 0 || i >= len(rows) {
		return ""
	}
	return rows[i]
}

// squeeze removes every space from s, so a comparison across a wrapped row boundary
// does not turn on where the padding fell.
func squeeze(s string) string {
	return strings.ReplaceAll(s, " ", "")
}

// stripSGR removes the escape sequences a rendered row carries, leaving the cells'
// characters. Only needs to handle what Render emits: CSI sequences and OSCs.
func stripSGR(row string) string {
	var b strings.Builder
	for i := 0; i < len(row); {
		if row[i] != 0x1b {
			b.WriteByte(row[i])
			i++
			continue
		}
		i++ // ESC
		if i >= len(row) {
			break
		}
		switch row[i] {
		case '[': // CSI ... final byte in 0x40-0x7e
			for i++; i < len(row) && (row[i] < 0x40 || row[i] > 0x7e); i++ {
			}
			i++
		case ']': // OSC ... BEL or ST
			for i++; i < len(row); i++ {
				if row[i] == 0x07 {
					i++
					break
				}
				if row[i] == 0x1b && i+1 < len(row) && row[i+1] == '\\' {
					i += 2
					break
				}
			}
		default:
			i++
		}
	}
	return b.String()
}

// reportsCwd reports whether the shell tells us where it is on its own, waiting up
// to timeout for the first report to arrive.
func (p *Pane) reportsCwd(timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for {
		if p.Cwd() != "" {
			return true
		}
		// A closed pane reports nothing further, and the caller must not keep typing
		// into a session that is gone.
		if p.isClosed() || time.Now().After(deadline) {
			return false
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// waitFirstOutput blocks until the pane has parsed its first chunk of server
// output, or timeout elapses.
func (p *Pane) waitFirstOutput(timeout time.Duration) {
	t := time.NewTimer(timeout)
	defer t.Stop()
	select {
	case <-p.firstOutput:
	case <-p.closed:
	case <-t.C:
	}
}

// writeString sends s to the remote as input, under the same mutex every other
// write to this session takes.
func (p *Pane) writeString(s string) {
	if p.isClosed() {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	_, _ = p.sess.Stdin.Write([]byte(s))
}
