// Command docsgen renders docs/*.md into index.html, README.md and KEYBINDINGS.md.
package main

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// A Doc is one section: its frontmatter plus its markdown body.
type Doc struct {
	ID    string // anchor + placeholder name, e.g. "hostlist"
	Title string // the section heading
	Nav   string // sidebar label; empty falls back to Title
	Group string // sidebar group heading
	Label string // the small caps line above the heading on the site
	Site  bool   // false keeps a section out of index.html
	Order int    // sort key, from the NN- filename prefix
	Body  string // markdown, headings starting at "##"
	File  string // source path, for error messages
}

func (d *Doc) NavLabel() string {
	if d.Nav != "" {
		return d.Nav
	}
	return d.Title
}

// LoadDocs reads docs/NN-id.md, ordered by the numeric filename prefix.
func LoadDocs(dir string) ([]*Doc, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var docs []*Doc
	seen := map[string]string{}
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".md") || strings.HasPrefix(name, "_") {
			continue
		}
		path := filepath.Join(dir, name)
		raw, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		doc, err := ParseDoc(string(raw))
		if err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
		doc.File = path
		doc.Order = orderOf(name)
		if doc.ID == "" {
			doc.ID = strings.TrimSuffix(strings.TrimPrefix(name, fmt.Sprintf("%02d-", doc.Order)), ".md")
		}
		if prev, dup := seen[doc.ID]; dup {
			return nil, fmt.Errorf("%s: duplicate id %q (also in %s)", path, doc.ID, prev)
		}
		seen[doc.ID] = path
		docs = append(docs, doc)
	}
	sort.SliceStable(docs, func(i, j int) bool { return docs[i].Order < docs[j].Order })
	return docs, nil
}

// orderOf reads the NN- prefix; a file without one sorts last.
func orderOf(name string) int {
	cut := strings.IndexByte(name, '-')
	if cut <= 0 {
		return 1 << 30
	}
	n, err := strconv.Atoi(name[:cut])
	if err != nil {
		return 1 << 30
	}
	return n
}

// ParseDoc splits the `---` frontmatter from the body; unknown keys are errors.
func ParseDoc(raw string) (*Doc, error) {
	doc := &Doc{Site: true}
	raw = strings.ReplaceAll(raw, "\r\n", "\n")
	if !strings.HasPrefix(raw, "---\n") {
		return nil, fmt.Errorf("missing frontmatter")
	}
	end := strings.Index(raw[4:], "\n---")
	if end < 0 {
		return nil, fmt.Errorf("unterminated frontmatter")
	}
	front, body := raw[4:4+end], raw[4+end+4:]

	sc := bufio.NewScanner(strings.NewReader(front))
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, val, ok := strings.Cut(line, ":")
		if !ok {
			return nil, fmt.Errorf("frontmatter line %q is not key: value", line)
		}
		key = strings.TrimSpace(key)
		val = strings.Trim(strings.TrimSpace(val), `"`)
		switch key {
		case "id":
			doc.ID = val
		case "title":
			doc.Title = val
		case "nav":
			doc.Nav = val
		case "group":
			doc.Group = val
		case "label":
			doc.Label = val
		case "site":
			doc.Site = val != "false"
		default:
			return nil, fmt.Errorf("unknown frontmatter key %q", key)
		}
	}
	if doc.Title == "" {
		return nil, fmt.Errorf("frontmatter needs a title")
	}
	doc.Body = strings.Trim(body, "\n")
	return doc, nil
}
