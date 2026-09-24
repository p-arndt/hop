package filebrowser

import (
	"bytes"
	"io/fs"
	"path"
	"strings"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"
)

// PreviewMax is the most a preview reads: a config file or a script, never a transfer. A
// file larger than this is not read at all.
const PreviewMax = 64 << 10

// headReader is the part of a client that can read the start of a file. A client without it
// still browses; its preview says there is none.
type headReader interface {
	ReadHead(p string, n int64) ([]byte, error)
}

// preview is what the preview shows: the entry under the cursor, loaded off the UI's
// goroutine. seq numbers the loads, so a result that arrives after the cursor moved on is
// dropped rather than shown against the wrong file.
type preview struct {
	path    string
	name    string
	seq     int
	loading bool
	// why replaces the text when there is nothing to show: a directory, a binary, too big.
	why  string
	text string
	err  error
}

// previewLoadedMsg carries one load's result back to the browser that asked.
type previewLoadedMsg struct {
	seq  int
	path string
	data []byte
	err  error
}

// PreviewCmd loads the preview of the entry under the cursor, when that is not the one
// already shown or on its way. Only a small regular file is read; everything else is
// described instead. It never blocks: the read runs as a command.
func (b *Browser) PreviewCmd() tea.Cmd {
	n := b.cur()
	if n == nil {
		b.preview = preview{seq: b.preview.seq, why: "nothing here"}
		return nil
	}
	if n.path == b.preview.path {
		return nil
	}
	p := preview{path: n.path, name: stripControl(n.e.Name), seq: b.preview.seq + 1}
	hr, canRead := b.client.(headReader)
	switch {
	case n.e.IsDir:
		p.why = "a directory · enter opens it"
	case n.e.Mode&(fs.ModeDevice|fs.ModeNamedPipe|fs.ModeSocket|fs.ModeCharDevice|fs.ModeIrregular) != 0:
		p.why = "not a regular file"
	case n.e.Size > PreviewMax:
		p.why = "too large to preview (" + humanizeBytes(n.e.Size) + ") · enter opens it"
	case !canRead:
		p.why = "no preview on this connection"
	default:
		p.loading = true
	}
	b.preview = p
	if !p.loading {
		return nil
	}
	seq, file, alias := p.seq, n.path, b.alias
	return func() tea.Msg {
		data, err := hr.ReadHead(file, PreviewMax)
		return Msg{Alias: alias, Body: previewLoadedMsg{seq: seq, path: file, data: data, err: err}}
	}
}

// previewLanded takes a load's result, unless the cursor has moved on since it was asked.
// The path is checked too: seq starts again in a reopened browser, so a late read for the
// old one could otherwise carry the right number for the wrong file.
func (b *Browser) previewLanded(msg previewLoadedMsg) {
	if msg.seq != b.preview.seq || msg.path != b.preview.path {
		return
	}
	b.preview.loading = false
	switch {
	case msg.err != nil:
		b.preview.err = msg.err
	case len(msg.data) == 0:
		b.preview.why = "an empty file"
	case binary(msg.data):
		b.preview.why = "a binary file · enter opens it anyway"
	default:
		b.preview.text = string(msg.data)
	}
}

// binary is the usual sniff: a NUL, or bytes that are not UTF-8. The end of a capped read
// may split a rune, so the last few bytes are forgiven.
func binary(data []byte) bool {
	if bytes.IndexByte(data, 0) >= 0 {
		return true
	}
	head := data
	if len(head) >= PreviewMax {
		head = head[:len(head)-utf8.UTFMax]
	}
	return !utf8.Valid(head)
}

// PreviewView draws the preview at w by h: a title naming the file, then its first lines.
func (b *Browser) PreviewView(w, h int) string {
	if w <= 0 || h <= 0 {
		return ""
	}
	p := b.preview
	title := dimStyle.Render("preview")
	if p.name != "" {
		title += faintStyle.Render(" · ") + truncateText(p.name, max(w-12, 1))
	}
	lines := []string{title, faintStyle.Render(strings.Repeat("─", w))}

	switch {
	case p.path == "" && p.why == "":
		lines = append(lines, dimStyle.Render("move onto a file to see it here"))
	case p.loading:
		lines = append(lines, dimStyle.Render("loading "+path.Base(p.path)+"…"))
	case p.err != nil:
		lines = append(lines, redStyle.Render(truncateText(stripControl(p.err.Error()), w)))
	case p.why != "":
		lines = append(lines, dimStyle.Render(truncateText(p.why, w)))
	default:
		for _, l := range strings.Split(p.text, "\n") {
			if len(lines) >= h {
				break
			}
			// Tabs expanded before stripping, since stripControl would drop them.
			l = stripControl(strings.ReplaceAll(strings.TrimRight(l, "\r"), "\t", "    "))
			lines = append(lines, truncateText(l, w))
		}
	}
	if len(lines) > h {
		lines = lines[:h]
	}
	return strings.Join(lines, "\n")
}
