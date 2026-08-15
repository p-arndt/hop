package main

import (
	"fmt"
	"html"
	"regexp"
	"strings"
)

// Fenced directives are the small set of layouts the page has that markdown
// has no syntax for — a card grid, a collapsed rationale, a figure with a
// caption. They nest by fence length, mdBook/MyST style:
//
//	::::shots
//	:::figure src="a.png" alt="…"
//	the caption
//	:::
//	::::
//
// Every one of them also has a plain-markdown lowering (see markdown.go), so
// README.md and KEYBINDINGS.md can be generated from the same source.
//
// A block that does not belong everywhere says so: `only="site"` renders it on
// the website alone, `not="readme"` renders it everywhere but there. The
// targets are the three outputs — site, readme, reference.
var (
	openRe  = regexp.MustCompile(`^(:{3,})\s*([a-z]+)\s*(.*)$`)
	attrRe  = regexp.MustCompile(`([a-z]+)\s*=\s*("[^"]*"|\S+)`)
	splitRe = regexp.MustCompile(`(?m)^#{1,6} `)
)

// The outputs a directive can be aimed at.
const (
	targetSite      = "site"
	targetReadme    = "readme"
	targetReference = "reference"
)

// wants reports whether a block with these attributes belongs in target.
func wants(attrs map[string]string, target string) bool {
	if only := attrs["only"]; only != "" {
		return listHas(only, target)
	}
	return !listHas(attrs["not"], target)
}

func listHas(list, want string) bool {
	for _, s := range strings.Split(list, ",") {
		if strings.TrimSpace(s) == want {
			return true
		}
	}
	return false
}

// stripAttrs removes the key=value pairs from a directive's argument line, so
// what is left is the free text — a :::why summary, say.
func stripAttrs(args string) string {
	return strings.TrimSpace(attrRe.ReplaceAllString(args, ""))
}

type directive struct {
	Kind  string
	Args  string
	Body  string
	Attrs map[string]string
}

func parseAttrs(args string) map[string]string {
	attrs := map[string]string{}
	for _, m := range attrRe.FindAllStringSubmatch(args, -1) {
		attrs[m[1]] = strings.Trim(m[2], `"`)
	}
	return attrs
}

// extractDirectives replaces every top-level directive block with a
// placeholder word and returns the HTML each one rendered to. The placeholder
// survives markdown conversion as its own paragraph, which spliceDirectives
// then swaps back out — that way the directive's own HTML never has to be
// escaped past goldmark.
func extractDirectives(src string, shift int) (string, []string, error) {
	var blocks []string
	lines := strings.Split(src, "\n")
	var out []string

	for i := 0; i < len(lines); i++ {
		m := openRe.FindStringSubmatch(lines[i])
		if m == nil {
			out = append(out, lines[i])
			continue
		}
		fence := m[1]
		end := -1
		for j := i + 1; j < len(lines); j++ {
			if strings.TrimRight(lines[j], " \t") == fence {
				end = j
				break
			}
		}
		if end < 0 {
			return "", nil, fmt.Errorf("unclosed %q directive", m[2])
		}
		body := strings.Join(lines[i+1:end], "\n")
		rendered, err := renderDirective(directive{
			Kind: m[2], Args: strings.TrimSpace(m[3]), Body: body, Attrs: parseAttrs(m[3]),
		}, shift)
		if err != nil {
			return "", nil, err
		}
		out = append(out, "", placeholder(len(blocks)), "")
		blocks = append(blocks, rendered)
		i = end
	}
	return strings.Join(out, "\n"), blocks, nil
}

func renderDirective(d directive, shift int) (string, error) {
	if !wants(d.Attrs, targetSite) {
		return "", nil
	}
	inner := func() (string, error) { return RenderHTML(d.Body, shift) }

	switch d.Kind {
	case "note":
		body, err := inner()
		return `<div class="note">` + body + `</div>`, err

	case "why", "details":
		class := ""
		if d.Kind == "why" {
			class = ` class="why"`
		}
		sum, err := renderInline(stripAttrs(d.Args))
		if err != nil {
			return "", err
		}
		body, err := inner()
		return "<details" + class + "><summary>" + sum + "</summary>" + body + "</details>", err

	case "cards":
		return renderCards(d, shift)

	case "modes":
		return renderModes(d)

	case "figure":
		return renderFigure(d, shift)

	case "shots":
		body, err := inner()
		return `<div class="shots">` + body + `</div>`, err

	case "columns":
		style := ""
		if cols := d.Attrs["cols"]; cols != "" {
			style = ` style="grid-template-columns:` + html.EscapeString(cols) + `"`
		}
		body, err := inner()
		return `<div class="shots"` + style + ">" + body + "</div>", err

	case "col":
		body, err := inner()
		return "<div>" + body + "</div>", err

	default:
		return "", fmt.Errorf("unknown directive %q", d.Kind)
	}
}

// renderModes turns the mode table — which GitHub renders as a table and needs
// no help with — into the strip of cards the website shows instead.
func renderModes(d directive) (string, error) {
	var b strings.Builder
	b.WriteString(`<div class="modes">`)
	for i, row := range tableRows(d.Body) {
		if i == 0 || len(row) < 3 {
			continue // header row, and the |---| under it
		}
		name, err := renderInline(row[0])
		if err != nil {
			return "", err
		}
		name = unwrap(name, "strong") // the card's own <b> is the emphasis here
		when, err := renderInline(row[1])
		if err != nil {
			return "", err
		}
		who, err := renderInline(row[2])
		if err != nil {
			return "", err
		}
		fmt.Fprintf(&b, `<div class="mode"><b>%s</b><span>%s</span><span class="who">keys go to %s</span>`, name, when, who)
		if len(row) > 3 {
			if links := linkRe.FindAllStringSubmatch(row[3], -1); len(links) > 0 {
				b.WriteString(`<ul class="modelinks">`)
				for _, l := range links {
					fmt.Fprintf(&b, `<li><a href="%s">%s</a></li>`, html.EscapeString(l[2]), html.EscapeString(l[1]))
				}
				b.WriteString("</ul>")
			}
		}
		b.WriteString("</div>")
	}
	b.WriteString("</div>")
	return b.String(), nil
}

// unwrap removes one enclosing tag, for a cell whose markdown emphasis the
// surrounding markup already provides.
func unwrap(in, tag string) string {
	open, close := "<"+tag+">", "</"+tag+">"
	if strings.HasPrefix(in, open) && strings.HasSuffix(in, close) {
		return in[len(open) : len(in)-len(close)]
	}
	return in
}

var linkRe = regexp.MustCompile(`\[([^\]]+)\]\(([^)]+)\)`)

// tableRows splits a GFM pipe table into cells, dropping the delimiter row.
func tableRows(body string) [][]string {
	var rows [][]string
	for _, line := range strings.Split(body, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "|") {
			continue
		}
		cells := strings.Split(strings.Trim(line, "|"), "|")
		for i := range cells {
			cells[i] = strings.TrimSpace(cells[i])
		}
		if strings.Trim(strings.Join(cells, ""), "-: ") == "" {
			continue
		}
		rows = append(rows, cells)
	}
	return rows
}

// renderCards splits the body on its headings: one heading plus everything
// under it is one card.
func renderCards(d directive, shift int) (string, error) {
	wrap, item, title := "grid", "card", "h4"
	var b strings.Builder
	fmt.Fprintf(&b, `<div class="%s">`, wrap)
	for _, part := range splitCards(d.Body) {
		head, rest, _ := strings.Cut(part, "\n")
		sum, err := renderInline(strings.TrimSpace(head))
		if err != nil {
			return "", err
		}
		body, err := RenderHTML(rest, shift)
		if err != nil {
			return "", err
		}
		fmt.Fprintf(&b, `<div class="%s"><%s>%s</%s>%s</div>`, item, title, sum, title, body)
	}
	b.WriteString("</div>")
	return b.String(), nil
}

// splitCards returns the body's heading-led chunks, heading marker removed.
func splitCards(body string) []string {
	idx := splitRe.FindAllStringIndex(body, -1)
	var parts []string
	for i, at := range idx {
		end := len(body)
		if i+1 < len(idx) {
			end = idx[i+1][0]
		}
		parts = append(parts, strings.TrimSpace(body[at[1]:end]))
	}
	return parts
}

func renderFigure(d directive, shift int) (string, error) {
	src, alt := d.Attrs["src"], d.Attrs["alt"]
	if src == "" {
		return "", fmt.Errorf("figure needs a src")
	}
	style := ""
	if max := d.Attrs["max"]; max != "" {
		style = ` style="max-width:` + html.EscapeString(max) + `"`
	}
	size := ""
	if w, h := d.Attrs["width"], d.Attrs["height"]; w != "" && h != "" {
		size = fmt.Sprintf(` width="%s" height="%s"`, html.EscapeString(w), html.EscapeString(h))
	}
	caption, err := RenderHTML(d.Body, shift)
	if err != nil {
		return "", err
	}
	caption = strings.TrimPrefix(caption, "<p>")
	caption = strings.TrimSuffix(caption, "</p>")

	img := fmt.Sprintf(`<img src="%s" alt="%s"%s loading="lazy" decoding="async">`,
		html.EscapeString(src), html.EscapeString(alt), size)
	if d.Attrs["eager"] == "true" {
		img = strings.Replace(img, ` loading="lazy"`, "", 1)
	}
	out := "<figure" + style + ">" + img
	if caption != "" {
		out += "<figcaption>" + caption + "</figcaption>"
	}
	return out + "</figure>", nil
}

// renderInline renders one line of markdown without its wrapping paragraph —
// for a <summary> or a card title.
func renderInline(src string) (string, error) {
	out, err := RenderHTML(src, 0)
	if err != nil {
		return "", err
	}
	out = strings.TrimPrefix(strings.TrimSpace(out), "<p>")
	return strings.TrimSuffix(out, "</p>"), nil
}
