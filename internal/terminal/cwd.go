package terminal

// Working-directory tracking for a shell pane.
//
// A remote shell's current directory is not something SSH tells anyone: it lives
// inside the shell process, and the only thing that leaves it is what the shell
// chooses to print. The convention every terminal emulator settled on for that is
// OSC 7 — an escape sequence carrying the cwd as a file:// URL, emitted from the
// prompt hook:
//
//	ESC ] 7 ; file://host/current/dir BEL      (or ST, ESC \, as the terminator)
//
// So there are two halves here. scanOSC7 watches the server's byte stream for
// that sequence as it flows past into the emulator, which is what makes the pane
// know where its shell stands. TrackCwd is the other half: it installs the prompt
// hook that produces the sequence, for shells that have not got one already.
//
// Everything about this is best effort. A shell that emits no OSC 7 leaves the
// cwd empty, and the callers treat empty as "I do not know" rather than as an
// error — the VS Code binding falls back to opening the host's default directory,
// which is what it always did before any of this existed.

import (
	"fmt"
	"net/url"
	"path"
	"strings"
	"time"

	"hop/internal/sshx"
)

// Cwd is the shell's current directory as last reported over OSC 7, or "" when
// nothing has reported one: the shell has no prompt hook, it has not drawn a
// prompt yet, or this pane is not a shell at all.
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

// maxOSCPayload caps how much of an OSC sequence is buffered while waiting for
// its terminator. Real OSC 7 payloads are a path; anything longer than this is
// either a different (long) OSC — a title, a hyperlink, a clipboard write — or a
// sequence whose terminator never arrived, and neither is worth holding bytes
// for. The overflow is dropped, not truncated into a wrong path: a payload that
// stops being buffered is failed outright.
const maxOSCPayload = 4096

// oscScanner is an incremental scanner for OSC 7 sequences in a byte stream. It
// is fed the same bytes the emulator parses, in whatever chunks the network
// delivers them, and it carries its state across chunks — a sequence split down
// the middle of its payload (or between its ESC and the ']') is found all the
// same.
//
// It is deliberately not a full ANSI parser. It only needs to recognise one
// sequence, and it ignores every other OSC by matching the "7;" introducer, so
// the emulator remains the only thing that interprets the stream.
type oscScanner struct {
	state oscState
	buf   []byte
	// over is set when the current payload outgrew maxOSCPayload, so the rest of
	// it is consumed and discarded rather than buffered.
	over bool
	// ris is set when a full terminal reset (RIS, ESC c — what `reset` and `tput
	// reset` send) went past, and cleared by tookReset.
	//
	// It is the one sequence in here that has nothing to do with OSC 7, and it is
	// watched here because this is the only ANSI-aware pass hop makes over the stream
	// besides the emulator's own — and because the emulator will not say: its
	// fullReset rewrites the whole mode map directly, without the EnableMode /
	// DisableMode callbacks that every other mode change comes through. Anything
	// shadowing those modes (see mouseState) would otherwise still believe whatever
	// the program before the reset had asked for.
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
	if len(s.buf) >= maxOSCPayload {
		s.over = true
		s.buf = s.buf[:0]
		return
	}
	s.buf = append(s.buf, c)
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
	return parseOSC7(payload)
}

// parseOSC7 pulls the directory out of an OSC payload, or reports that this is
// not an OSC 7 carrying one.
//
// The payload is "7;file://<host>/<path>". The host is ignored: it names the
// machine the path is on, which the caller already knows — the shell that emitted
// it is on the far end of this very session — and shells disagree about whether
// to send a hostname, a FQDN or nothing at all.
//
// The path arrives percent-encoded by the encoders that bother (a space as %20);
// an unescape that fails is taken literally instead, since a shell that emitted a
// bare "%" in a directory name is describing a real directory. Control characters
// are refused outright: the result is rendered in the status line and passed to
// another program's command line, and neither should be made to carry them.
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

// The shell integration hop installs: a function that emits OSC 7, hung off the
// prompt so it runs before every prompt the shell draws. There is one per shell
// rather than one line that detects the shell it landed in, because this line is
// typed at a real prompt and is echoed there — it is the one part of this feature
// the user sees, so it is kept as short as a shell will allow. TrackCwd has
// already established which shell it is talking to by the time it picks one.
//
// Each begins with:
//
//	\x15    kill the line, in case the user had started typing into the prompt
//	        this is about to be submitted at. Their half-typed line would be
//	        corrupted by what follows either way; this way it is at least not
//	        also run.
//	' '     a leading space, for the shells configured to keep such lines out of
//	        history (bash's HISTCONTROL=ignorespace, zsh's HIST_IGNORE_SPACE).
//
// and ends with a call, so the pane knows where the shell is standing without
// waiting for the next prompt, and a carriage return, which submits it.
//
// A hook the user's own rc-file installs is left in place: this one is prepended
// to PROMPT_COMMAND rather than replacing it, and appended to zsh's
// precmd_functions. (TrackCwd does not send it at all when the shell is already
// reporting — see there.)
const (
	bashCwdHook = "\x15 hop_cwd() { printf '\\033]7;file://%s%s\\033\\\\' \"${HOSTNAME:-}\" \"$PWD\"; }; " +
		"PROMPT_COMMAND=\"hop_cwd${PROMPT_COMMAND:+;$PROMPT_COMMAND}\"; hop_cwd\r"

	zshCwdHook = "\x15 hop_cwd() { printf '\\033]7;file://%s%s\\033\\\\' \"${HOST:-}\" \"$PWD\"; }; " +
		"precmd_functions+=(hop_cwd); hop_cwd\r"
)

// cwdHookFor returns the hook to type into this login shell, or "" for a shell
// that has none — every shell but bash and zsh, which are left untouched rather
// than sent a line they would answer with a parse error.
//
// The name may arrive with the leading "-" a login shell is invoked under, and
// with whatever whitespace the probe's output carried.
func cwdHookFor(shell string) string {
	switch strings.TrimPrefix(path.Base(strings.TrimSpace(shell)), "-") {
	case "bash":
		return bashCwdHook
	case "zsh":
		return zshCwdHook
	}
	return ""
}

// loginShellCmd asks the remote which login shell the account has, preferring the
// passwd entry over $SHELL (which an exec channel inherits from the daemon and is
// occasionally stale). It runs under an explicit `sh -c` rather than the account's
// own shell, because the account's own shell is exactly what is in question: fish
// and pwsh would fail to parse this, and a syntax error is a worse answer than an
// empty one.
const loginShellCmd = `sh -c 'p=$(getent passwd "$(id -un)" 2>/dev/null | cut -d: -f7); ` +
	`[ -n "$p" ] || p="$SHELL"; printf %s "$p"'`

// hookDelay bounds how long TrackCwd waits for the shell's first output before
// sending the hook. Typing at a pty that has not finished starting is safe — the
// bytes queue in the tty buffer — but the shell echoes what it reads, and a line
// read while the login banner is still printing interleaves with it. Waiting for
// the first byte is enough to land after the banner has started; the timeout is
// there so a shell that prints nothing at all (a bare `sh` with an empty prompt)
// still gets the hook.
const hookDelay = 2 * time.Second

// hookGrace is how long TrackCwd gives an already-integrated shell to say so
// before typing a hook into it. A user whose own rc-file emits OSC 7 gets nothing
// typed into their prompt at all, which is the best version of this feature — and
// the report that proves it arrives with the first prompt, just after the first
// output. The wait is short because it is also the window in which the user might
// start typing.
const hookGrace = 300 * time.Millisecond

// TrackCwd installs the OSC 7 prompt hook in this pane's shell, so Cwd starts
// reporting where the shell stands. It returns immediately: the probe and the
// injection happen on a goroutine of their own, and both are best effort.
//
// Three shells get nothing typed into them:
//
//   - one whose login shell is neither bash nor zsh, because the line would only be
//     a parse error there;
//   - one that is reporting OSC 7 already, from the user's own rc-file — it needs no
//     help, and a hook it does not need is noise in its first prompt for nothing;
//   - one whose screen is owned by a full-screen program rather than a prompt.
//
// That last guard is the one that matters most. The login shell hop probed is what
// the account *has*, not necessarily what is on the other end of this pty: a
// `.bash_profile` ending in `exec tmux attach`, or an sshd with a ForceCommand, both
// answer "bash" to the probe and then hand the session to something else entirely.
// Typing a shell command into that is at best noise and at worst — vim in insert
// mode — an edit to somebody's file. A full-screen program takes the alt screen, so
// that is what is checked, and the cwd simply stays unknown behind one.
//
// It is for shell panes. An editor pane runs one program to completion and has no
// prompt to hang a hook off.
func (p *Pane) TrackCwd(cli *sshx.Client) {
	if cli == nil {
		return
	}
	go func() {
		shell, err := cli.Output(loginShellCmd)
		if err != nil {
			return
		}
		hook := cwdHookFor(shell)
		if hook == "" {
			return
		}
		p.waitFirstOutput(hookDelay)
		if p.isClosed() || p.reportsCwd(hookGrace) || p.emu.IsAltScreen() {
			return
		}
		p.injectHook(hook)
	}()
}

// isClosed reports whether the pane has been closed under us. Every wait in the
// injection sequence is followed by one of these: the sequence spans seconds, and a
// shell tab is closed with one keystroke.
func (p *Pane) isClosed() bool {
	select {
	case <-p.closed:
		return true
	default:
		return false
	}
}

// injectHook types the hook at the shell's prompt and then takes the line the
// shell echoed back off the screen, so the integration leaves no trace of itself
// in the pane.
//
// The echo cannot be prevented: the shell's line editor draws what it reads, and
// it is the shell's screen while it does. But it is *hop's* screen afterwards — the
// emulator is in this process — so the lines the echo occupied are deleted from it
// once the hook has run, and the prompt below them slides up to where the first one
// was. What is left is what was there before: the login banner, and a prompt.
//
// Nothing is erased on a guess. The rows are only deleted when what is on them is
// the line hop typed and nothing else — see eraseEcho. A visible line is a blemish;
// erasing the host's own output would be a defect.
func (p *Pane) injectHook(hook string) {
	pos := p.emu.CursorPosition()
	top, sbBefore := pos.Y, p.emu.ScrollbackLen()

	p.writeString(hook)

	// The report is the tell that the hook ran. It is not the tell that the screen is
	// ready to be measured: the hook's own trailing call emits it while the echoed
	// line is still the current one, before the shell has printed the newline and the
	// prompt that follow. So the report is waited for first, and then the screen is.
	if !p.reportsCwd(hookRunWindow) {
		return
	}
	if !p.waitPromptBelow(top, sbBefore) {
		return
	}
	if p.isClosed() {
		return
	}
	p.eraseEcho(top, sbBefore, hook)
}

// hookRunWindow is how long the hook is given to run — one round trip and a prompt
// — before its echo is left where it is.
const hookRunWindow = 3 * time.Second

// The screen is settled when the cursor has moved below the row the echo started
// on — the shell has finished the line and drawn a fresh prompt under it — and has
// then stayed put for promptQuiet. Measuring before that would count too few rows,
// and an erase that spans too few rows leaves half the echo behind.
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

// eraseEcho deletes the rows the hook was echoed onto, moving what follows up into
// their place — but only once it has read those rows back and found the hook and
// nothing else on them.
//
// The read-back is the whole safety of this. top is a row index measured *before*
// the hook was written, and the host is free to print in the meantime: a box with a
// slow dynamic MOTD is still writing lines when the hook goes in, and those lines
// land inside the span. Trusting the span would delete them. So the span is
// confirmed against the text hop typed, and a span holding anything else is left
// exactly where it is.
//
// sbBefore is the scrollback length at that same moment: the difference against the
// current one is how far the screen scrolled in between, which is what makes top
// stale and has to be taken off it.
func (p *Pane) eraseEcho(top, sbBefore int, hook string) {
	cur := p.emu.CursorPosition()
	top -= p.emu.ScrollbackLen() - sbBefore
	if top < 0 {
		// The span starts above the screen: it scrolled while the hook was going in.
		// Clamping to the first visible row is only useful when the echo itself is still
		// there to be recognised — holdsEchoOnly requires it to begin on that row — so a
		// screen that scrolled past the echo keeps it, in history, out of sight.
		top = 0
	}

	n := cur.Y - top
	if n <= 0 || n >= p.emu.Height() {
		return
	}
	if !p.holdsEchoOnly(top, cur.Y, hook) {
		return
	}

	// Park the cursor on the first row to go, delete n rows (which pulls the prompt
	// row up to it), then put the cursor back on that prompt, at the column it was
	// on. CUP is 1-based; the emulator's positions are not.
	_, _ = p.emu.Write([]byte(fmt.Sprintf("\x1b[%d;1H\x1b[%dM\x1b[%d;%dH",
		top+1, n, top+1, cur.X+1)))
	if p.onOutput != nil {
		p.onOutput()
	}
}

// holdsEchoOnly reports whether rows [top, bottom) hold the echo of hook and
// nothing besides it.
//
// The echo is one long line the shell wrapped across those rows, so read back and
// joined end to end they hold the hook's text verbatim: a prompt, and then the line
// as it was typed. What is checked is exactly that — from the hook's first characters
// onward, the span must be a prefix of the hook. A MOTD line, a background job's
// output, anything else that printed into the span breaks that prefix, and the erase
// is declined.
//
// Spaces are dropped from both sides of the comparison rather than matched. A row is
// as wide as the screen, so the wrap can fall on one of the hook's own spaces, and
// whether the emulator hands that row back with it or pads the next one is not
// something worth depending on. Nothing else is dropped, so text that does not
// belong to the hook still fails.
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
	// The echo has to begin on the span's *first* row, after the prompt and nothing
	// else. Without this, a span whose start was clamped up to row 0 would pass on the
	// strength of an echo further down it and take the rows above — the login banner —
	// along with it.
	if i >= len(squeeze(stripSGR(rowAt(rows, top)))) {
		return false
	}
	return strings.HasPrefix(typed, echo[i:])
}

// echoHeadLen is how much of the typed line is used to find where the echo starts
// within the prompt row. Long enough to be unmistakable, short enough to survive the
// row boundary the prompt may push it over.
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

// stripSGR removes the escape sequences a rendered row carries, leaving the
// characters that occupy cells. It is the inverse of what Render adds, and only
// needs to handle what Render emits: CSI sequences and OSCs.
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
		// A closed pane will report nothing further, and the caller must not carry on
		// typing into a session that has gone.
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
