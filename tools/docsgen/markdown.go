package main

import (
	"fmt"
	"regexp"
	"strings"
	"unicode"
)

// RenderMarkdown lowers a doc body to plain GitHub-flavoured markdown: the
// directives become the things GitHub already renders well (tables, blockquote
// alerts, <details>), and [[key]] becomes a code span, because GitHub strips
// <kbd> styling anyway. target says which file this is for, so a block can opt
// out of one of them.
func RenderMarkdown(src string, shift int, target string) (string, error) {
	out, err := lowerDirectives(src, shift, target)
	if err != nil {
		return "", err
	}
	out = shiftHeadings(out, shift)
	return strings.TrimSpace(lowerKbd(out)), nil
}

var kbdRe = regexp.MustCompile(`\[\[([^\[\]]+)\]\]`)

// lowerKbd rewrites [[key]] outside fenced code.
func lowerKbd(src string) string {
	lines := strings.Split(src, "\n")
	inFence := false
	for i, line := range lines {
		if fenceRe.MatchString(line) {
			inFence = !inFence
			continue
		}
		if inFence {
			continue
		}
		lines[i] = kbdRe.ReplaceAllString(line, "`$1`")
	}
	return strings.Join(lines, "\n")
}

func lowerDirectives(src string, shift int, target string) (string, error) {
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
			return "", fmt.Errorf("unclosed %q directive", m[2])
		}
		lowered, err := lowerDirective(directive{
			Kind: m[2], Args: strings.TrimSpace(m[3]),
			Body: strings.Join(lines[i+1:end], "\n"), Attrs: parseAttrs(m[3]),
		}, shift, target)
		if err != nil {
			return "", err
		}
		out = append(out, "", lowered, "")
		i = end
	}
	return strings.Join(out, "\n"), nil
}

func lowerDirective(d directive, shift int, target string) (string, error) {
	// A block aimed elsewhere leaves no trace here: `only="site"` is a figure
	// the README lays out its own way, `not="readme"` a rationale the full
	// reference carries and the README does not need to.
	if !wants(d.Attrs, target) {
		return "", nil
	}
	body, err := lowerDirectives(d.Body, shift, target)
	if err != nil {
		return "", err
	}
	body = strings.TrimSpace(body)

	switch d.Kind {
	case "note":
		return "> [!NOTE]\n" + prefixLines(body, "> "), nil

	case "why", "details":
		return "<details>\n<summary><b>" + stripAttrs(d.Args) + "</b></summary>\n\n" + body + "\n\n</details>", nil

	case "cards":
		return lowerCards(body), nil

	case "modes":
		return body, nil // already a table

	case "figure":
		return lowerFigure(d), nil

	case "shots", "columns", "col":
		return body, nil

	default:
		return "", fmt.Errorf("unknown directive %q", d.Kind)
	}
}

// lowerCards turns the card grid into the two-column table README has always
// used for it: an icon-and-name column, and the sentence.
func lowerCards(body string) string {
	rows := []string{"|  | |", "| --- | --- |"}
	for _, part := range splitCards(body) {
		head, rest, _ := strings.Cut(part, "\n")
		icon, name := splitIcon(strings.TrimSpace(head))
		text := strings.Join(strings.Fields(strings.ReplaceAll(rest, "\n", " ")), " ")
		rows = append(rows, fmt.Sprintf("| %s**%s** | %s |", icon, name, text))
	}
	return strings.Join(rows, "\n")
}

// splitIcon peels a leading emoji off a card title so it stays outside the
// bold, the way the README table reads best.
func splitIcon(title string) (icon, name string) {
	first, rest, ok := strings.Cut(title, " ")
	if !ok || first == "" {
		return "", title
	}
	for _, r := range first {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			return "", title
		}
	}
	return first + " ", rest
}

func lowerFigure(d directive) string {
	caption := strings.Join(strings.Fields(strings.ReplaceAll(d.Body, "\n", " ")), " ")
	src := d.Attrs["src"]
	if !strings.HasPrefix(src, "http") && !strings.HasPrefix(src, "./") {
		src = "./" + src
	}
	out := fmt.Sprintf(`<p align="center">%s<img src="%s" alt="%s" width="900">%s</p>`,
		"\n  ", src, d.Attrs["alt"], "\n")
	if caption != "" {
		out += "\n\n<sub>" + lowerKbd(caption) + "</sub>"
	}
	return out
}

func prefixLines(s, prefix string) string {
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		if l == "" {
			lines[i] = strings.TrimRight(prefix, " ")
		} else {
			lines[i] = prefix + l
		}
	}
	return strings.Join(lines, "\n")
}
