package main

import (
	"io"
	"strings"
)

// prompt mimics a stock Ubuntu bash prompt.
func prompt() string {
	return "\x1b[1;32m" + demoUser + "@" + demoHost + "\x1b[0m:\x1b[1;34m~\x1b[0m$ "
}

// runShell is the fake interactive shell; line editing is minimal on purpose.
func runShell(stdin io.Reader, stdout io.Writer, greet bool) {
	if greet {
		io.WriteString(stdout, crlf(motd))
	}
	io.WriteString(stdout, "\r\n"+prompt())

	var line strings.Builder
	buf := make([]byte, 256)

	for {
		n, err := stdin.Read(buf)
		if n > 0 {
			for _, b := range buf[:n] {
				switch b {
				case '\r', '\n':
					io.WriteString(stdout, "\r\n")
					if done := runCommand(stdout, strings.TrimSpace(line.String())); done {
						return
					}
					line.Reset()
					io.WriteString(stdout, prompt())

				case 0x7f, 0x08: // backspace
					if s := line.String(); s != "" {
						line.Reset()
						line.WriteString(s[:len(s)-1])
						io.WriteString(stdout, "\b \b")
					}

				case 0x03: // ctrl+c
					line.Reset()
					io.WriteString(stdout, "^C\r\n"+prompt())

				case 0x15: // ctrl+u
					io.WriteString(stdout, strings.Repeat("\b \b", len(line.String())))
					line.Reset()

				case 0x04: // ctrl+d at an empty line closes the shell, as bash does
					if line.Len() == 0 {
						io.WriteString(stdout, "logout\r\n")
						return
					}

				default:
					if b >= 0x20 {
						line.WriteByte(b)
						stdout.Write([]byte{b})
					}
				}
			}
		}
		if err != nil {
			return
		}
	}
}

// runCommand writes the answer for cmd and reports whether the shell should exit.
func runCommand(w io.Writer, cmd string) bool {
	switch cmd {
	case "":
		return false
	case "exit", "logout":
		io.WriteString(w, "logout\r\n")
		return true
	case "clear":
		io.WriteString(w, "\x1b[2J\x1b[H")
		return false
	}

	if out, ok := commands[cmd]; ok {
		io.WriteString(w, out)
		return false
	}

	io.WriteString(w, "bash: "+firstWord(cmd)+": command not found\r\n")
	return false
}

func firstWord(s string) string {
	if i := strings.IndexAny(s, " \t"); i >= 0 {
		return s[:i]
	}
	return s
}
