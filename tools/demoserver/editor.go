package main

import (
	"fmt"
	"io"
	"path"
	"strings"
)

// runEditor is a fake vi: alt screen, a vim-ish status line, a cursor, and `:q`.
// The size is read once — the recording window never resizes.
func runEditor(stdin io.Reader, stdout io.Writer, file string, content string, cols, rows int) {
	lines := strings.Split(strings.TrimSuffix(content, "\n"), "\n")
	e := &editor{
		w: stdout, file: file, lines: lines,
		cols: max(cols, 20), rows: max(rows, 6),
	}

	// hop watches for the alt screen to decide the far end owns the keyboard.
	io.WriteString(stdout, "\x1b[?1049h\x1b[H\x1b[2J")
	defer io.WriteString(stdout, "\x1b[?1049l")

	e.draw()

	buf := make([]byte, 64)
	for {
		n, err := stdin.Read(buf)
		for _, b := range buf[:n] {
			if e.key(b) {
				return
			}
		}
		if err != nil {
			return
		}
	}
}

type editor struct {
	w     io.Writer
	file  string
	lines []string
	cols  int
	rows  int

	top  int // first file line on screen
	cur  int // cursor line, absolute
	cmd  string
	incm bool // typing a : command
}

// key handles one byte and reports whether the editor should quit.
func (e *editor) key(b byte) bool {
	if e.incm {
		switch b {
		case '\r', '\n':
			cmd := strings.TrimSpace(e.cmd)
			e.incm, e.cmd = false, ""
			switch cmd {
			case ":q", ":q!", ":wq", ":x", ":wq!":
				return true
			case ":w":
				e.status(fmt.Sprintf("%q %dL written", path.Base(e.file), len(e.lines)))
				return false
			default:
				e.status("E492: Not an editor command: " + strings.TrimPrefix(cmd, ":"))
				return false
			}
		case 0x1b: // esc abandons the command line
			e.incm, e.cmd = false, ""
			e.draw()
		case 0x7f, 0x08:
			if len(e.cmd) > 1 {
				e.cmd = e.cmd[:len(e.cmd)-1]
				e.status(e.cmd)
			}
		default:
			if b >= 0x20 {
				e.cmd += string(b)
				e.status(e.cmd)
			}
		}
		return false
	}

	switch b {
	case ':':
		e.incm, e.cmd = true, ":"
		e.status(e.cmd)
	case 'j':
		e.move(1)
	case 'k':
		e.move(-1)
	case 'G':
		e.jump(len(e.lines) - 1)
	case 'g':
		e.jump(0)
	case 0x04: // ctrl+d
		e.move(e.body() / 2)
	case 0x15: // ctrl+u
		e.move(-e.body() / 2)
	case 'i', 'a', 'o':
		e.status("-- INSERT --")
	case 0x1b:
		e.draw()
	}
	return false
}

// body is the number of file rows on screen — everything but the status line.
func (e *editor) body() int { return max(e.rows-1, 1) }

func (e *editor) move(d int) { e.jump(e.cur + d) }
func (e *editor) jump(to int) {
	e.cur = min(max(to, 0), len(e.lines)-1)
	switch {
	case e.cur < e.top:
		e.top = e.cur
	case e.cur >= e.top+e.body():
		e.top = e.cur - e.body() + 1
	}
	e.draw()
}

func (e *editor) draw() {
	var b strings.Builder
	b.WriteString("\x1b[H\x1b[2J")

	for row := 0; row < e.body(); row++ {
		i := e.top + row
		b.WriteString(fmt.Sprintf("\x1b[%d;1H", row+1))
		if i < len(e.lines) {
			b.WriteString(truncate(expandTabs(e.lines[i]), e.cols))
		} else {
			b.WriteString("\x1b[34m~\x1b[0m")
		}
	}

	b.WriteString(e.statusLine())
	// hop overlays its cursor wherever the emulator reports one, so park it on the file cursor.
	b.WriteString(fmt.Sprintf("\x1b[%d;1H", e.cur-e.top+1))
	io.WriteString(e.w, b.String())
}

func (e *editor) statusLine() string {
	left := fmt.Sprintf("%q %dL, %dB", path.Base(e.file), len(e.lines), byteLen(e.lines))
	right := fmt.Sprintf("%d,1", e.cur+1)
	if e.cur == 0 {
		right += "           Top"
	} else if e.cur == len(e.lines)-1 {
		right += "           Bot"
	} else {
		right += fmt.Sprintf("           %d%%", (e.cur+1)*100/len(e.lines))
	}

	gap := max(e.cols-len(left)-len(right), 1)
	bar := truncate(left+strings.Repeat(" ", gap)+right, e.cols)
	return fmt.Sprintf("\x1b[%d;1H\x1b[7m%s\x1b[0m", e.rows, bar)
}

func (e *editor) status(msg string) {
	io.WriteString(e.w, fmt.Sprintf("\x1b[%d;1H\x1b[2K%s", e.rows, truncate(msg, e.cols)))
}

func byteLen(lines []string) int {
	n := 0
	for _, l := range lines {
		n += len(l) + 1
	}
	return n
}

func expandTabs(s string) string { return strings.ReplaceAll(s, "\t", "    ") }

func truncate(s string, w int) string {
	if len(s) <= w {
		return s
	}
	return s[:w]
}
