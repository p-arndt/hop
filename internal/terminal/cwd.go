package terminal

// Working-directory tracking for a shell pane. SSH does not report the remote cwd; only
// the shell can, over OSC 7:
//
//	ESC ] 7 ; file://host/current/dir BEL      (or ST, ESC \, as the terminator)
//
// oscScanner watches the output stream for it; TrackCwd installs the prompt hook that
// emits it. Best effort: no report leaves Cwd empty, which callers read as "unknown".

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

// setCwd records a directory reported by the remote. Written by the output pump, read
// by the UI, hence the mutex.
func (p *Pane) setCwd(dir string) {
	p.cwdMu.Lock()
	p.cwd = dir
	p.cwdMu.Unlock()
}

// Caps on a buffered OSC payload. An over-long one is failed outright, never truncated
// into a wrong path. maxClipPayload is generous because OSC 52 carries base64.
const (
	maxOSCPayload  = 4096
	maxClipPayload = 1 << 20
)

// oscScanner scans the output stream for the two OSCs hop acts on: OSC 7 (working
// directory) and OSC 52 (clipboard write). State carries across chunks, so a sequence
// split mid-payload is still found. Deliberately not a full ANSI parser.
type oscScanner struct {
	state oscState
	buf   []byte
	// clip is the last clipboard write (OSC 52) seen; clipSet distinguishes "none" from
	// a deliberate clear. Last one wins.
	clip    string
	clipSet bool
	// over is set when the current payload outgrew maxOSCPayload, so the rest of
	// it is consumed and discarded rather than buffered.
	over bool
	// ris is set when a full terminal reset (RIS, ESC c) went past, and cleared by
	// tookReset. Watched here because the emulator rewrites the mode map directly,
	// without the callbacks every other mode change comes through.
	ris bool
}

type oscState int

const (
	oscGround  oscState = iota // ordinary output
	oscEsc                     // saw ESC, waiting to see whether an OSC follows
	oscBody                    // inside an OSC payload
	oscBodyEsc                 // inside an OSC payload, saw ESC (maybe the ST terminator)
)

// feed consumes a chunk of server output and returns the last directory reported in it.
// A chunk carrying no complete OSC 7 returns false; a half-carried one is remembered.
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
			// Not a terminator: the OSC was interrupted by another escape sequence. Abandon
			// the payload and re-read this byte as the one following an ESC.
			s.buf = s.buf[:0]
			s.over = false
			switch c {
			case ']':
				s.state = oscBody
			case 0x1b:
				// ESC ESC: still waiting on the byte that says what the second introduces.
				s.state = oscEsc
			default:
				s.state = oscGround
			}
			continue
		}
	}
	return dir, found
}

// tookReset reports whether a full reset went past since it was last asked, and clears
// the flag. The cwd is not cleared with it: RIS resets the terminal, not the shell.
func (s *oscScanner) tookReset() bool {
	took := s.ris
	s.ris = false
	return took
}

// push appends a payload byte, marking the payload over-long once it passes the cap.
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

// cap picks the buffer limit from the payload's introducer: a clipboard write gets room
// for a clipboard, everything else room for a path. Before the introducer has arrived
// the larger cap applies.
func (s *oscScanner) cap() int {
	if len(s.buf) < len(oscClipPrefix) || string(s.buf[:len(oscClipPrefix)]) == oscClipPrefix {
		return maxClipPayload
	}
	return maxOSCPayload
}

// finish parses a completed payload and returns to ground state, yielding a directory
// for OSC 7. A clipboard write is recorded for tookClipboard instead.
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

// tookClipboard reports the last clipboard write seen since it was last asked, and
// forgets it.
func (s *oscScanner) tookClipboard() (string, bool) {
	if !s.clipSet {
		return "", false
	}
	text := s.clip
	s.clip, s.clipSet = "", false
	return text, true
}

// parseOSC7 pulls the directory out of a "7;file://<host>/<path>" payload. The host is
// ignored: shells disagree about sending a hostname, a FQDN or nothing. Control
// characters are refused — the result reaches the status line and another command line.
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

// The prompt hooks hop types into a fresh shell: a function emitting OSC 7, hung off
// the prompt. One per shell rather than one self-detecting line, because it is echoed
// at a real prompt and is the only part of the feature the user sees.
//
// Each begins with \x15 (kill-line) and a space (for HISTCONTROL=ignorespace), and ends
// with a call plus CR so the cwd is known without waiting for the next prompt. A hook
// from the user's own rc-file is preserved, not replaced.
const (
	bashCwdHook = "\x15 hop_cwd() { printf '\\033]7;file://%s%s\\033\\\\' \"${HOSTNAME:-}\" \"$PWD\"; }; " +
		"PROMPT_COMMAND=\"hop_cwd${PROMPT_COMMAND:+;$PROMPT_COMMAND}\"; hop_cwd\r"

	zshCwdHook = "\x15 hop_cwd() { printf '\\033]7;file://%s%s\\033\\\\' \"${HOST:-}\" \"$PWD\"; }; " +
		"precmd_functions+=(hop_cwd); hop_cwd\r"
)

// cwdHookFor returns the hook for this login shell, or "" for anything but bash and
// zsh, which would answer with a parse error. The name may arrive with a login shell's
// leading "-" and the probe's whitespace.
func cwdHookFor(shell string) string {
	switch strings.TrimPrefix(path.Base(strings.TrimSpace(shell)), "-") {
	case "bash":
		return bashCwdHook
	case "zsh":
		return zshCwdHook
	}
	return ""
}

// loginShellCmd asks the remote for the account's login shell, preferring the passwd
// entry over the stale $SHELL an exec channel inherits. Explicit `sh -c` because the
// account's own shell is what is in question.
const loginShellCmd = `sh -c 'p=$(getent passwd "$(id -un)" 2>/dev/null | cut -d: -f7); ` +
	`[ -n "$p" ] || p="$SHELL"; printf %s "$p"'`

// hookDelay bounds the wait for the shell's first output before the hook is sent.
// Typing early is safe, but the echo would interleave with a login banner still
// printing. The timeout covers a shell that prints nothing at all.
const hookDelay = 2 * time.Second

// hookGrace is how long an already-integrated shell gets to say so before a hook is
// typed into it. Short, because the user might start typing inside it.
const hookGrace = 300 * time.Millisecond

// TrackCwd installs the OSC 7 prompt hook in this shell pane so Cwd starts reporting.
// It returns immediately; the probe and injection run on their own goroutine, best
// effort throughout.
//
// Three shells get nothing typed into them: one that is neither bash nor zsh, one
// already reporting OSC 7 from the user's rc-file, and one whose screen is owned by a
// full-screen program. The last matters most: the probe reports what the account has,
// not what is on this pty, so a `.bash_profile` ending in `exec tmux attach` answers
// "bash" and then hands the session elsewhere — where typing is an edit to a file.
func (p *Pane) TrackCwd(cli *sshx.Client, startDir string) {
	if cli == nil {
		return
	}
	go func() {
		var hook string
		// A shell hop cannot identify still gets the cd, which every shell understands;
		// only the hook is per-shell.
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
		// The grace comes first: a full-screen program that takes the screen inside that
		// window has to be seen before anything is typed.
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
		// The cwd report tells injectHook the line has run, and is only owed when
		// something will emit one: hop's hook, or a shell already reporting.
		p.injectHook(line, hook != "" || reports)
	}()
}

// startupLine is the single line typed at a fresh shell's prompt: the cd into the
// host's default directory, the OSC 7 hook, or both — cd first, so the hook's trailing
// call reports where the session actually starts.
//
// Joined with ";" rather than "&&": a function definition is not a valid right-hand side
// of "&&" in bash, and a failed cd should still leave a reporting shell. A failed cd is
// not silenced — its error lands inside the echoed span, which stops eraseEcho, so the
// line stays on screen with the reason beside it.
func startupLine(dir, hook string) string {
	if dir == "" {
		return hook
	}
	// The hooks' "\x15 " prefix belongs on the joined line exactly once, at the front.
	cd := "\x15 cd " + shellQuotePath(dir)
	if hook == "" {
		return cd + "\r"
	}
	return cd + "; " + strings.TrimPrefix(hook, "\x15 ")
}

// shellQuotePath renders dir as a single shell word, so a space or quote stays part of
// one argument and nothing is executed. A leading "~" stays outside the quotes, since a
// quoted tilde is literal.
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
// injection sequence is followed by one: it spans seconds, and closing a tab is a key.
func (p *Pane) isClosed() bool {
	select {
	case <-p.closed:
		return true
	default:
		return false
	}
}

// injectHook types the hook at the shell's prompt, then removes the line the shell
// echoed back. The echo cannot be prevented, but the emulator is in this process, so
// the rows can be deleted afterwards. Nothing is erased on a guess — see eraseEcho: a
// leftover line is a blemish, erasing the host's own output would be a defect.
func (p *Pane) injectHook(line string, expectReport bool) {
	pos := p.emu.CursorPosition()
	top, sbBefore := pos.Y, p.emu.ScrollbackLen()

	p.writeString(line)

	// The report says the hook ran, not that the screen is ready to measure: it arrives
	// before the newline and prompt. Wait for the report first, then for the screen.
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

// hookRunWindow is how long the hook is given to run before its echo is left in place.
const hookRunWindow = 3 * time.Second

// The screen is settled once the cursor has moved below the echo's first row and stayed
// put for promptQuiet. Measuring earlier counts too few rows, leaving half the echo.
const (
	promptWait  = 3 * time.Second
	promptQuiet = 90 * time.Millisecond
	promptPoll  = 30 * time.Millisecond
)

// waitPromptBelow waits for the shell to draw the prompt below the echo, reporting
// false if it never settles — in which case the echo is left alone.
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
			// Still on the echoed line.
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

// eraseEcho deletes the rows the hook was echoed onto and pulls what follows up, but
// only after reading those rows back and finding the hook and nothing else. top was
// measured before the hook was written and the host prints in the meantime, so trusting
// the span would delete that output.
//
// sbBefore is the scrollback length at that same moment; the difference against the
// current one is how far the screen scrolled, which has to come off top.
func (p *Pane) eraseEcho(top, sbBefore int, hook string) {
	cur := p.emu.CursorPosition()
	top -= p.emu.ScrollbackLen() - sbBefore
	if top < 0 {
		// The span scrolled off the top. holdsEchoOnly requires the echo to begin on the
		// clamped row, so a screen that scrolled past it keeps the echo in history.
		top = 0
	}

	n := cur.Y - top
	if n <= 0 || n >= p.emu.Height() {
		return
	}
	if !p.holdsEchoOnly(top, cur.Y, hook) {
		return
	}

	// Park on the first row to go, delete n rows, then restore the cursor onto the
	// prompt that came up. CUP is 1-based; emulator positions are not.
	_, _ = p.emu.Write(fmt.Appendf(nil, "\x1b[%d;1H\x1b[%dM\x1b[%d;%dH",
		top+1, n, top+1, cur.X+1))
	if p.onOutput != nil {
		p.onOutput()
	}
}

// holdsEchoOnly reports whether rows [top, bottom) hold the echo of hook and nothing
// else. The echo is one long line wrapped across them, so joined end to end they read
// as a prompt followed by the hook verbatim; a MOTD line or a background job's output
// breaks that prefix and the erase is declined.
//
// Spaces are dropped from both sides rather than matched, because the wrap can fall on
// one of the hook's own spaces. Nothing else is dropped.
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
	// The echo must begin on the span's first row, right after the prompt: otherwise a
	// span clamped to row 0 would take the login banner above along with it.
	if i >= len(squeeze(stripSGR(rowAt(rows, top)))) {
		return false
	}
	return strings.HasPrefix(typed, echo[i:])
}

// echoHeadLen is how much of the typed line locates the echo's start within the prompt
// row: long enough to be unmistakable, short enough to survive a row boundary.
const echoHeadLen = 16

// rowAt returns row i, or "" when the screen has no such row.
func rowAt(rows []string, i int) string {
	if i < 0 || i >= len(rows) {
		return ""
	}
	return rows[i]
}

// squeeze removes every space from s, so a comparison across a wrapped row boundary
// does not turn on where padding fell.
func squeeze(s string) string {
	return strings.ReplaceAll(s, " ", "")
}

// stripSGR removes the escape sequences a rendered row carries. Only handles what
// Render emits: CSI sequences and OSCs.
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

// reportsCwd reports whether the shell says where it is on its own, waiting up to
// timeout for the first report.
func (p *Pane) reportsCwd(timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for {
		if p.Cwd() != "" {
			return true
		}
		// A closed pane reports nothing further, and must not be typed into.
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

// writeString sends s to the remote as input, through the pane's input queue.
func (p *Pane) writeString(s string) {
	// The result is not checked: this is hop's own shell integration, and a far end too
	// stalled to take it has nothing to report to the user about a hook they never asked
	// for. A dropped hook simply leaves Cwd empty.
	_ = p.send([]byte(s))
}
