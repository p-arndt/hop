package terminal

// Working-directory tracking: only the shell can report the remote cwd, over OSC 7
// (ESC ] 7 ; file://host/current/dir BEL, or ST — ESC \ — as the terminator).

import (
	"fmt"
	"net/url"
	"path"
	"strings"
	"time"

	"hop/internal/sshx"
)

// Cwd is the shell's directory as last reported over OSC 7, or "" if none has been.
func (p *Pane) Cwd() string {
	p.cwdMu.Lock()
	defer p.cwdMu.Unlock()
	return p.cwd
}

// setCwd records a directory reported by the remote.
func (p *Pane) setCwd(dir string) {
	p.cwdMu.Lock()
	p.cwd = dir
	p.cwdMu.Unlock()
}

// Caps on a buffered OSC payload; an over-long one fails rather than truncating.
const (
	maxOSCPayload  = 4096
	maxClipPayload = 1 << 20
)

// oscScanner scans output for OSC 7 (cwd) and OSC 52 (clipboard), carrying state across chunks.
type oscScanner struct {
	state oscState
	buf   []byte
	// clipSet distinguishes "none" from a deliberate clear.
	clip    string
	clipSet bool
	// over marks a payload past its cap, so the rest is discarded rather than buffered.
	over bool
	// ris marks a full reset (RIS, ESC c), which the emulator applies without a callback.
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
			case 'c': // RIS, a full terminal reset
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
			// Not a terminator: abandon the payload, re-read this byte as following an ESC.
			s.buf = s.buf[:0]
			s.over = false
			switch c {
			case ']':
				s.state = oscBody
			case 0x1b:
				s.state = oscEsc
			default:
				s.state = oscGround
			}
			continue
		}
	}
	return dir, found
}

// tookReset takes the reset flag. The cwd survives it: RIS resets the terminal, not the shell.
func (s *oscScanner) tookReset() bool {
	took := s.ris
	s.ris = false
	return took
}

// push appends a payload byte, marking it over-long once it passes the cap.
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

// cap picks the limit from the introducer; the larger one applies until it has arrived.
func (s *oscScanner) cap() int {
	if len(s.buf) < len(oscClipPrefix) || string(s.buf[:len(oscClipPrefix)]) == oscClipPrefix {
		return maxClipPayload
	}
	return maxOSCPayload
}

// finish parses a completed payload and returns to ground state.
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

// tookClipboard reports the last clipboard write seen since it was last asked.
func (s *oscScanner) tookClipboard() (string, bool) {
	if !s.clipSet {
		return "", false
	}
	text := s.clip
	s.clip, s.clipSet = "", false
	return text, true
}

// parseOSC7 pulls the directory out of a "7;file://<host>/<path>" payload; the host is
// ignored (shells disagree) and control characters are refused (the result is re-typed).
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

// Prompt hooks typed into a fresh shell, each prefixed \x15 (kill-line) plus a space
// (for HISTCONTROL=ignorespace) and ending in a call so the cwd is known immediately.
const (
	bashCwdHook = "\x15 hop_cwd() { printf '\\033]7;file://%s%s\\033\\\\' \"${HOSTNAME:-}\" \"$PWD\"; }; " +
		"PROMPT_COMMAND=\"hop_cwd${PROMPT_COMMAND:+;$PROMPT_COMMAND}\"; hop_cwd\r"

	zshCwdHook = "\x15 hop_cwd() { printf '\\033]7;file://%s%s\\033\\\\' \"${HOST:-}\" \"$PWD\"; }; " +
		"precmd_functions+=(hop_cwd); hop_cwd\r"
)

// cwdHookFor returns the hook for this login shell, or "" for anything but bash and zsh.
func cwdHookFor(shell string) string {
	switch strings.TrimPrefix(path.Base(strings.TrimSpace(shell)), "-") {
	case "bash":
		return bashCwdHook
	case "zsh":
		return zshCwdHook
	}
	return ""
}

// loginShellCmd prefers the passwd entry over the stale $SHELL an exec channel inherits.
const loginShellCmd = `sh -c 'p=$(getent passwd "$(id -un)" 2>/dev/null | cut -d: -f7); ` +
	`[ -n "$p" ] || p="$SHELL"; printf %s "$p"'`

// hookDelay bounds the wait for first output, so the echo misses a printing login banner.
const hookDelay = 2 * time.Second

// hookGrace is how long an already-integrated shell gets to say so before a hook is typed.
const hookGrace = 300 * time.Millisecond

// TrackCwd installs the OSC 7 prompt hook on its own goroutine, best effort. Nothing is
// typed into a non-bash/zsh shell, one already reporting, or one on the alt screen.
func (p *Pane) TrackCwd(cli *sshx.Client, startDir string) {
	if cli == nil {
		return
	}
	go func() {
		var hook string
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
		// Grace first: a program taking the screen must be seen before anything is typed.
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
		// A report is only owed when hop's hook or an already-reporting shell will emit one.
		p.injectHook(line, hook != "" || reports)
	}()
}

// startupLine is the line typed at a fresh shell's prompt: cd, hook, or both — joined
// with ";" because a function definition is not a valid right-hand side of "&&" in bash.
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

// shellQuotePath renders dir as one shell word; a leading "~" stays outside the quotes,
// since a quoted tilde is literal.
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

// isClosed reports whether the pane has been closed under us.
func (p *Pane) isClosed() bool {
	select {
	case <-p.closed:
		return true
	default:
		return false
	}
}

// injectHook types the hook at the prompt, then removes the line the shell echoed back.
func (p *Pane) injectHook(line string, expectReport bool) {
	pos := p.emu.CursorPosition()
	top, sbBefore := pos.Y, p.emu.ScrollbackLen()

	p.writeString(line)

	// The report arrives before the newline and prompt, so wait for it, then for the screen.
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

// The screen is settled once the cursor sits below the echo's first row for promptQuiet;
// measuring earlier counts too few rows.
const (
	promptWait  = 3 * time.Second
	promptQuiet = 90 * time.Millisecond
	promptPoll  = 30 * time.Millisecond
)

// waitPromptBelow waits for the prompt to be drawn below the echo, false if it never settles.
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
		case row <= floor: // still on the echoed line
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

// eraseEcho deletes the rows the hook was echoed onto, only if they hold nothing else.
// sbBefore is the scrollback length when top was measured; the difference comes off top.
func (p *Pane) eraseEcho(top, sbBefore int, hook string) {
	cur := p.emu.CursorPosition()
	top -= p.emu.ScrollbackLen() - sbBefore
	if top < 0 {
		// holdsEchoOnly requires the echo to begin on the clamped row, so a screen scrolled
		// past it keeps the echo in history.
		top = 0
	}

	n := cur.Y - top
	if n <= 0 || n >= p.emu.Height() {
		return
	}
	if !p.holdsEchoOnly(top, cur.Y, hook) {
		return
	}

	// CUP, DL n, CUP back. CUP is 1-based; emulator positions are not.
	_, _ = p.emu.Write(fmt.Appendf(nil, "\x1b[%d;1H\x1b[%dM\x1b[%d;%dH",
		top+1, n, top+1, cur.X+1))
	if p.onOutput != nil {
		p.onOutput()
	}
}

// holdsEchoOnly reports whether rows [top, bottom) joined read as a prompt plus hook and
// nothing else. Spaces are dropped: the wrap can fall on one of the hook's own spaces.
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
	// The echo must begin on the first row, or a span clamped to 0 takes the banner too.
	if i >= len(squeeze(stripSGR(rowAt(rows, top)))) {
		return false
	}
	return strings.HasPrefix(typed, echo[i:])
}

// echoHeadLen locates the echo's start within the prompt row: unmistakable, but short
// enough to survive a row boundary.
const echoHeadLen = 16

// rowAt returns row i, or "" when the screen has no such row.
func rowAt(rows []string, i int) string {
	if i < 0 || i >= len(rows) {
		return ""
	}
	return rows[i]
}

// squeeze removes every space, so a comparison across a wrap does not turn on padding.
func squeeze(s string) string {
	return strings.ReplaceAll(s, " ", "")
}

// stripSGR removes what Render emits: CSI sequences and OSCs.
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

// reportsCwd reports whether the shell says where it is on its own, waiting up to timeout.
func (p *Pane) reportsCwd(timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for {
		if p.Cwd() != "" {
			return true
		}
		if p.isClosed() || time.Now().After(deadline) {
			return false
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// waitFirstOutput blocks until the first chunk of server output is parsed, or timeout.
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
	// Unchecked: a dropped hook simply leaves Cwd empty.
	_ = p.send([]byte(s))
}
