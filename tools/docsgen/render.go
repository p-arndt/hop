package main

import (
	"fmt"
	"html"
	"regexp"
	"strconv"
	"strings"
)

var placeholderRe = regexp.MustCompile(`(?m)^[ \t]*<!--\s*docs:([a-z0-9-]+)((?:\s+[a-z]+=(?:"[^"]*"|\S+))*)\s*-->[ \t]*$`)

// RenderSite assembles index.html: sidebar, sections and the search index.
func RenderSite(tmpl string, docs []*Doc) (string, error) {
	rendered := map[string]string{}
	var sections, nav strings.Builder
	group := ""

	for _, d := range docs {
		if !d.Site {
			continue
		}
		body, err := RenderHTML(d.Body, 1)
		if err != nil {
			return "", fmt.Errorf("%s: %w", d.File, err)
		}
		rendered[d.ID] = body

		sections.WriteString("\n    <section id=\"" + d.ID + "\">\n")
		if d.Label != "" {
			sections.WriteString(`      <p class="sectlabel">` + html.EscapeString(d.Label) + "</p>\n")
		}
		sections.WriteString("      <h2>" + inlineTitle(d.Title) + "</h2>\n")
		sections.WriteString(indent(body, "      ") + "\n    </section>\n")

		if d.Group != group {
			group = d.Group
			nav.WriteString(`        <li><div class="grp">` + html.EscapeString(group) + "</div></li>\n")
		}
		nav.WriteString(fmt.Sprintf("        <li><a href=\"#%s\">%s</a></li>\n", d.ID, html.EscapeString(d.NavLabel())))
	}

	out := tmpl
	out = strings.Replace(out, "<!-- docsgen:nav -->", strings.TrimRight(nav.String(), "\n"), 1)
	out = strings.Replace(out, "<!-- docsgen:sections -->", strings.TrimRight(sections.String(), "\n"), 1)
	out = strings.Replace(out, "<!-- docsgen:search -->", SearchIndexJSON(BuildSearchIndex(docs, rendered)), 1)
	return out, nil
}

// inlineTitle lets a section title carry [[keys]] and `code`.
func inlineTitle(title string) string {
	out, err := renderInline(title)
	if err != nil {
		return html.EscapeString(title)
	}
	return out
}

func indent(s, with string) string {
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		if l != "" {
			lines[i] = with + l
		}
	}
	return strings.Join(lines, "\n")
}

// RenderMarkdownFile fills a template's <!-- docs:id --> placeholders; fallback
// is the file a cross-reference points at when this one has no heading for it.
func RenderMarkdownFile(tmpl string, docs []*Doc, fallback, target string) (string, error) {
	byID := map[string]*Doc{}
	for _, d := range docs {
		byID[d.ID] = d
	}
	var firstErr error
	local := headingIDs(tmpl, byID)
	out := placeholderRe.ReplaceAllStringFunc(tmpl, func(m string) string {
		g := placeholderRe.FindStringSubmatch(m)
		d, ok := byID[g[1]]
		if !ok {
			if firstErr == nil {
				firstErr = fmt.Errorf("no docs/ section with id %q", g[1])
			}
			return m
		}
		attrs := parseAttrs(g[2])
		level := 2
		if l, err := strconv.Atoi(attrs["level"]); err == nil {
			level = l
		}
		body, err := RenderMarkdown(d.Body, level-1, target)
		if err != nil && firstErr == nil {
			firstErr = fmt.Errorf("%s: %w", d.File, err)
		}
		if sum, ok := attrs["details"]; ok {
			return "<details>\n<summary><b>" + lowerKbd(sum) + "</b></summary>\n\n" + body + "\n\n</details>"
		}
		title := d.Title
		if t, ok := attrs["title"]; ok {
			title = t
		}
		if title == "none" || title == "" {
			return body
		}
		return strings.Repeat("#", level) + " " + lowerKbd(title) + "\n\n" + body
	})
	return retargetLinks(collapseBlanks(out), byID, local, fallback), firstErr
}

// headingIDs collects the sections this file gives a heading of their own —
// the only ones a "#id" link inside it can reach.
func headingIDs(tmpl string, byID map[string]*Doc) map[string]bool {
	ids := map[string]bool{}
	for _, m := range placeholderRe.FindAllStringSubmatch(tmpl, -1) {
		attrs := parseAttrs(m[2])
		if _, hidden := attrs["details"]; hidden {
			continue
		}
		if t, ok := attrs["title"]; ok && (t == "none" || t == "") {
			continue
		}
		if _, ok := byID[m[1]]; ok {
			ids[m[1]] = true
		}
	}
	return ids
}

// retargetLinks rewrites "#id" anchors: in markdown the anchor is GitHub's
// heading slug, and a section this file omits must be linked in the one that has it.
func retargetLinks(in string, byID map[string]*Doc, local map[string]bool, fallback string) string {
	return anchorRe.ReplaceAllStringFunc(in, func(m string) string {
		id := anchorRe.FindStringSubmatch(m)[1]
		d, ok := byID[id]
		if !ok {
			return m
		}
		if local[id] {
			return "](#" + slug(d.Title) + ")"
		}
		if fallback == "" {
			return m
		}
		return "](" + fallback + "#" + slug(d.Title) + ")"
	})
}

var (
	anchorRe = regexp.MustCompile(`\]\(#([a-z0-9-]+)\)`)
	slugDrop = regexp.MustCompile("[^a-z0-9 -]")
	blankRe  = regexp.MustCompile(`\n{3,}`)
)

// slug is GitHub's heading anchor: lowercase, punctuation dropped, spaces to dashes.
func slug(title string) string {
	s := strings.ToLower(lowerKbd(title))
	s = slugDrop.ReplaceAllString(s, "")
	return strings.ReplaceAll(s, " ", "-")
}

func collapseBlanks(in string) string { return blankRe.ReplaceAllString(in, "\n\n") }
