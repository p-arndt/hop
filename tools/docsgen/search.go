package main

import (
	"encoding/json"
	"html"
	"regexp"
	"strings"
)

// A SearchEntry is one thing the in-page search can find: a block of text,
// the heading it sits under, and the anchor that scrolls there.
type SearchEntry struct {
	Section string `json:"s"` // the section's own title
	Heading string `json:"h"` // the nearest heading, empty at section level
	Anchor  string `json:"a"` // "#id" or "#id-subheading"
	Text    string `json:"t"`
}

var (
	blockRe = regexp.MustCompile(`(?s)<(h[2-6])(?: id="([^"]*)")?[^>]*>(.*?)</h[2-6]>|<(p|li|summary|figcaption|tr)[^>]*>(.*?)</(?:p|li|summary|figcaption|tr)>`)
	tagRe   = regexp.MustCompile(`<[^>]+>`)
	wsRe    = regexp.MustCompile(`\s+`)

	modelinksRe = regexp.MustCompile(`(?s)<ul class="modelinks">.*?</ul>`)
)

// BuildSearchIndex walks the rendered HTML of every section and collects one
// entry per block of prose, table row or heading. It works on the rendered
// HTML rather than the markdown so that whatever a reader can see on the page
// is exactly what the search can find.
func BuildSearchIndex(docs []*Doc, rendered map[string]string) []SearchEntry {
	var out []SearchEntry
	for _, d := range docs {
		if !d.Site {
			continue
		}
		heading, anchor := "", "#"+d.ID
		out = append(out, SearchEntry{Section: d.Title, Anchor: anchor, Text: d.Title})
		// The sidebar label is what a reader is likely to type — "Scrollback"
		// finds a section titled "Scrolling back through history".
		if nav := d.NavLabel(); nav != d.Title {
			out = append(out, SearchEntry{Section: d.Title, Anchor: anchor, Text: nav})
		}

		body := modelinksRe.ReplaceAllString(rendered[d.ID], "") // nav links, not prose
		for _, m := range blockRe.FindAllStringSubmatch(body, -1) {
			if m[1] != "" { // a heading: it becomes the context for what follows
				heading = plain(m[3])
				if m[2] != "" {
					anchor = "#" + m[2]
				}
				out = append(out, SearchEntry{Section: d.Title, Heading: heading, Anchor: anchor, Text: heading})
				continue
			}
			text := plain(m[5])
			if text == "" {
				continue
			}
			out = append(out, SearchEntry{Section: d.Title, Heading: heading, Anchor: anchor, Text: text})
		}
	}
	return out
}

// plain reduces a block of HTML to the words in it. Table cells are joined
// with a middle dot so a key and its action stay one searchable phrase without
// running together.
func plain(in string) string {
	in = strings.ReplaceAll(in, "</td>", " · ")
	in = strings.ReplaceAll(in, "</th>", " · ")
	in = tagRe.ReplaceAllString(in, "")
	in = html.UnescapeString(in)
	in = wsRe.ReplaceAllString(in, " ")
	return strings.Trim(strings.TrimSuffix(strings.TrimSpace(in), "·"), " ")
}

func (e SearchEntry) marshal() string { b, _ := json.Marshal(e); return string(b) }

// SearchIndexJSON is the compact array the page embeds.
func SearchIndexJSON(entries []SearchEntry) string {
	parts := make([]string, len(entries))
	for i, e := range entries {
		parts[i] = e.marshal()
	}
	return "[\n" + strings.Join(parts, ",\n") + "\n]"
}
